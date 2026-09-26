package versions

import (
	"context"
	"fmt"
	"strings"
)

// publishFinishedAndDeleteRemote publishes every finished target, its exact
// annotated tag, and deletes the source in one atomic server transaction. A
// separate target/tag push cannot safely justify a later source deletion:
// Git omits no-op refspecs from receive-pack, so their leases are not server
// assertions. The persisted plan binds this transaction to exact identities.
func (w *Workflows) publishFinishedAndDeleteRemote(ctx context.Context, path string, j *journal, targets []string, tag string) error {
	if j == nil || j.SourceSHA == "" || j.TagTargetSHA == "" {
		return fmt.Errorf("cannot publish finished refs without journaled source and tag target identities")
	}
	if err := w.ensureSafeRef(j.Source); err != nil {
		return err
	}
	if err := w.ensureSafeRef(tag); err != nil {
		return err
	}
	if len(j.RemoteTargets) == 0 || j.TagObjectSHA == "" {
		if err := w.prepareFinishedRemotePlan(ctx, path, j, targets, tag); err != nil {
			return err
		}
	}
	remoteSource, exists, err := w.remoteBranchSHA(ctx, j.Source)
	if err != nil {
		return fmt.Errorf("verify finished source %s: %w", j.Source, err)
	}
	if !exists {
		return w.verifyFinishedRemoteReplay(ctx, j, tag)
	}
	if remoteSource != j.SourceSHA {
		return fmt.Errorf("finished source %s changed: expected %s, got %s", j.Source, j.SourceSHA, remoteSource)
	}
	if err := w.validateFinishedRemotePlan(ctx, j, targets, tag); err != nil {
		return err
	}
	if err := w.verifyRemotePlanBeforePublish(ctx, j, tag); err != nil {
		return err
	}

	args := []string{"push", "--atomic"}
	for _, target := range j.RemoteTargets {
		args = append(args, "--force-with-lease=refs/heads/"+target.Name+":"+target.BeforeSHA)
	}
	tagRef := "refs/tags/" + tag
	args = append(args,
		"--force-with-lease="+tagRef+":",
		"--force-with-lease=refs/heads/"+j.Source+":"+j.SourceSHA,
		w.remote,
	)
	for _, target := range j.RemoteTargets {
		args = append(args, target.DesiredSHA+":refs/heads/"+target.Name)
	}
	args = append(args, j.TagObjectSHA+":"+tagRef, ":refs/heads/"+j.Source)
	_, err = w.git(ctx, args...)
	return err
}

func (w *Workflows) prepareFinishedRemotePlan(ctx context.Context, path string, j *journal, targets []string, tag string) error {
	if len(j.RemoteTargets) != 0 || j.TagObjectSHA != "" {
		return fmt.Errorf("unfinished workflow has incomplete remote publication plan")
	}
	seen := map[string]struct{}{}
	plan := make([]finishRemoteTarget, 0, len(targets))
	for _, name := range targets {
		if err := w.ensureSafeRef(name); err != nil {
			return err
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("finished workflow has duplicate target %s", name)
		}
		seen[name] = struct{}{}
		before, exists, err := w.remoteBranchSHA(ctx, name)
		if err != nil {
			return fmt.Errorf("observe finished target %s: %w", name, err)
		}
		if !exists {
			return fmt.Errorf("finished target %s is absent on %s", name, w.remote)
		}
		desired, err := w.git(ctx, "rev-parse", "--verify", "refs/heads/"+name+"^{commit}")
		if err != nil {
			return fmt.Errorf("resolve finished target %s: %w", name, err)
		}
		desired = strings.TrimSpace(desired)
		if desired == before {
			return fmt.Errorf("finished target %s is already published; refusing unsafe no-op source deletion", name)
		}
		plan = append(plan, finishRemoteTarget{Name: name, BeforeSHA: before, DesiredSHA: desired})
	}
	tagRef := "refs/tags/" + tag
	tagObject, err := w.git(ctx, "rev-parse", "--verify", tagRef+"^{tag}")
	if err != nil {
		return fmt.Errorf("resolve finished annotated tag %s: %w", tag, err)
	}
	tagObject = strings.TrimSpace(tagObject)
	tagTarget, err := w.git(ctx, "rev-parse", "--verify", tagRef+"^{commit}")
	if err != nil {
		return fmt.Errorf("resolve finished tag target %s: %w", tag, err)
	}
	if strings.TrimSpace(tagTarget) != j.TagTargetSHA {
		return fmt.Errorf("finished annotated tag %s points to %s, not journaled target %s", tag, strings.TrimSpace(tagTarget), j.TagTargetSHA)
	}
	if out, err := w.git(ctx, "ls-remote", "--tags", w.remote, tagRef, tagRef+"^{}"); err != nil {
		return fmt.Errorf("observe finished remote tag %s: %w", tag, err)
	} else if strings.TrimSpace(out) != "" {
		return fmt.Errorf("finished annotated tag %s is already published; refusing unsafe no-op source deletion", tag)
	}
	j.RemoteTargets, j.TagObjectSHA = plan, tagObject
	if err := w.validateFinishedRemotePlan(ctx, j, targets, tag); err != nil {
		return err
	}
	if err := writeAtomic(path, j); err != nil {
		return fmt.Errorf("record finished remote publication plan: %w", err)
	}
	return nil
}

func (w *Workflows) validateFinishedRemotePlan(ctx context.Context, j *journal, targets []string, tag string) error {
	if len(j.RemoteTargets) != len(targets) || j.TagObjectSHA == "" {
		return fmt.Errorf("unfinished workflow remote publication plan does not match required topology")
	}
	tagRef := "refs/tags/" + tag
	tagTarget, err := w.git(ctx, "rev-parse", "--verify", j.TagObjectSHA+"^{commit}")
	if err != nil || strings.TrimSpace(tagTarget) != j.TagTargetSHA {
		return fmt.Errorf("unfinished workflow tag object does not match journaled tag target")
	}
	for i, target := range j.RemoteTargets {
		if target.Name != targets[i] || target.BeforeSHA == "" || target.DesiredSHA == "" {
			return fmt.Errorf("unfinished workflow remote publication plan does not match required topology")
		}
		for _, sha := range []string{target.BeforeSHA, target.DesiredSHA} {
			canonical, err := w.git(ctx, "rev-parse", "--verify", sha+"^{commit}")
			if err != nil || strings.TrimSpace(canonical) != sha {
				return fmt.Errorf("unfinished workflow target %s has invalid immutable identity", target.Name)
			}
		}
		if _, err := w.git(ctx, "merge-base", "--is-ancestor", target.BeforeSHA, target.DesiredSHA); err != nil {
			return fmt.Errorf("finished target %s does not descend from observed remote state: %w", target.Name, err)
		}
		if _, err := w.git(ctx, "merge-base", "--is-ancestor", j.SourceSHA, target.DesiredSHA); err != nil {
			return fmt.Errorf("finish postcondition: %s is not merged into planned %s: %w", j.SourceSHA, target.Name, err)
		}
		if i == 0 {
			if _, err := w.git(ctx, "merge-base", "--is-ancestor", j.TagTargetSHA, target.DesiredSHA); err != nil {
				return fmt.Errorf("finished tag target is not merged into planned %s: %w", target.Name, err)
			}
		}
	}
	localTag, err := w.git(ctx, "rev-parse", "--verify", tagRef+"^{tag}")
	if err != nil || strings.TrimSpace(localTag) != j.TagObjectSHA {
		return fmt.Errorf("unfinished workflow annotated tag %s does not match its publication plan", tag)
	}
	return nil
}

func (w *Workflows) verifyRemotePlanBeforePublish(ctx context.Context, j *journal, tag string) error {
	for _, target := range j.RemoteTargets {
		current, exists, err := w.remoteBranchSHA(ctx, target.Name)
		if err != nil {
			return fmt.Errorf("verify finished target %s: %w", target.Name, err)
		}
		if !exists || current != target.BeforeSHA {
			return fmt.Errorf("finished target %s changed before atomic publication", target.Name)
		}
	}
	tagRef := "refs/tags/" + tag
	out, err := w.git(ctx, "ls-remote", "--tags", w.remote, tagRef, tagRef+"^{}")
	if err != nil {
		return fmt.Errorf("verify finished remote tag %s: %w", tag, err)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("finished annotated tag %s appeared before atomic publication", tag)
	}
	return nil
}

func (w *Workflows) verifyFinishedRemoteReplay(ctx context.Context, j *journal, tag string) error {
	for _, target := range j.RemoteTargets {
		if _, err := w.git(ctx, "fetch", w.remote, "refs/heads/"+target.Name); err != nil {
			return fmt.Errorf("fetch finished target %s after source deletion: %w", target.Name, err)
		}
		current, err := w.git(ctx, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
		if err != nil {
			return fmt.Errorf("resolve finished target %s after source deletion: %w", target.Name, err)
		}
		if _, err = w.git(ctx, "merge-base", "--is-ancestor", target.DesiredSHA, strings.TrimSpace(current)); err != nil {
			return fmt.Errorf("finished target %s no longer contains planned result: %w", target.Name, err)
		}
	}
	object, peeled, err := w.remoteAnnotatedTag(ctx, tag)
	if err != nil {
		return err
	}
	if object != j.TagObjectSHA || peeled != j.TagTargetSHA {
		return fmt.Errorf("remote annotated tag %s does not match completed publication plan", tag)
	}
	return nil
}

func (w *Workflows) remoteAnnotatedTag(ctx context.Context, tag string) (string, string, error) {
	ref := "refs/tags/" + tag
	out, err := w.git(ctx, "ls-remote", "--tags", w.remote, ref, ref+"^{}")
	if err != nil {
		return "", "", fmt.Errorf("verify remote annotated tag %s: %w", tag, err)
	}
	found := map[string]string{}
	for _, line := range strings.FieldsFunc(strings.TrimSpace(out), func(r rune) bool { return r == '\n' || r == '\r' }) {
		fields := strings.Fields(line)
		if len(fields) != 2 || (fields[1] != ref && fields[1] != ref+"^{}") || fields[0] == "" {
			return "", "", fmt.Errorf("verify remote annotated tag %s: unexpected response %q", tag, strings.TrimSpace(out))
		}
		if _, duplicate := found[fields[1]]; duplicate {
			return "", "", fmt.Errorf("verify remote annotated tag %s: ambiguous response", tag)
		}
		found[fields[1]] = fields[0]
	}
	object, hasObject := found[ref]
	peeled, hasPeeled := found[ref+"^{}"]
	if !hasObject || !hasPeeled {
		return "", "", fmt.Errorf("verify remote annotated tag %s: exact annotated tag is missing", tag)
	}
	return object, peeled, nil
}
