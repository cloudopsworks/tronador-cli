package versions

import (
	"context"
	"fmt"
	"strings"
)

// Tag creates the legacy gitflow version tag. Stable branches use MajorMinorPatch;
// other branches preserve the GitVersion qualifier. qualifier maps to +deploy-X.
func (w *Workflows) Tag(ctx context.Context, qualifier string, publish bool) (string, error) {
	branch, e := w.Current(ctx)
	if e != nil {
		return "", e
	}
	if e = w.RequireParity(ctx, branch); e != nil {
		return "", e
	}
	// GitVersion can advance its calculated version after a local tag is
	// created. In publish mode, prefer the unique version tag already on HEAD;
	// this makes `tag` followed by `tag --publish` publish the same tag instead
	// of silently creating the next calculated version. A tag already on the
	// remote is an idempotent no-op.
	if publish {
		if tag, found, err := w.currentVersionTagAtHead(ctx, qualifier); err != nil {
			return "", err
		} else if found {
			return tag, nil
		}
	}
	main, e := w.Main(ctx)
	if e != nil {
		return "", e
	}
	variable := "SemVer"
	if branch == main {
		variable = "MajorMinorPatch"
	}
	o, e := w.gitVersion(ctx, variable)
	if e != nil {
		return "", e
	}
	v := normalizeVersion(strings.TrimSpace(o))
	if qualifier != "" {
		if e = w.ensureSafeRef(qualifier); e != nil {
			return "", e
		}
		v += "+deploy-" + qualifier
	}
	if e = w.ensureSafeRef(v); e != nil {
		return "", e
	}
	// A clone may not have fetched tags even though the calculated tag already
	// exists on origin. Check its advertised target before creating a local tag:
	// independently-created annotated tags have different tag-object hashes.
	remoteTarget, remoteExists, err := w.remoteTagTarget(ctx, v)
	if err != nil {
		return "", err
	}
	if remoteExists {
		head, err := w.git(ctx, "rev-parse", "--verify", "HEAD^{commit}")
		if err != nil {
			return "", fmt.Errorf("resolve HEAD for remote tag validation: %w", err)
		}
		if remoteTarget != strings.TrimSpace(head) {
			return "", fmt.Errorf("remote tag %s already exists but does not point at HEAD", v)
		}
		return v, nil
	}
	if e = w.rejectDifferentVersionAtHead(ctx, v); e != nil {
		return "", e
	}
	if e = w.ensureAnnotatedTag(ctx, v, fmt.Sprintf("chore: Version Tagging: %s", v), "HEAD"); e != nil {
		return "", e
	}
	if publish {
		if e = w.publishTagIfNeeded(ctx, v); e != nil {
			return "", e
		}
	}
	return v, nil
}

// remoteTagTarget returns the commit named by an exact advertised remote tag.
// Annotated tags include both the tag-object ref and a peeled ^{} ref; lightweight
// tags advertise the commit directly.
func (w *Workflows) remoteTagTarget(ctx context.Context, tag string) (string, bool, error) {
	ref := "refs/tags/" + tag
	out, err := w.git(ctx, "ls-remote", "--tags", w.remote, ref)
	if err != nil {
		return "", false, fmt.Errorf("check remote tag %s: %w", tag, err)
	}
	var object, peeled string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return "", false, fmt.Errorf("unexpected remote tag response for %s: %q", tag, line)
		}
		switch fields[1] {
		case ref:
			if object != "" {
				return "", false, fmt.Errorf("duplicate remote tag response for %s", tag)
			}
			object = fields[0]
		case ref + "^{}":
			if peeled != "" {
				return "", false, fmt.Errorf("duplicate peeled remote tag response for %s", tag)
			}
			peeled = fields[0]
		}
	}
	if object == "" {
		if peeled != "" {
			return "", false, fmt.Errorf("peeled remote tag %s has no tag ref", tag)
		}
		return "", false, nil
	}
	if peeled != "" {
		return peeled, true, nil
	}
	return object, true, nil
}

// currentVersionTagAtHead returns the sole supported SemVer tag at HEAD.
// Multiple candidates are ambiguous: publishing must not guess which local
// version the user intended.
func (w *Workflows) currentVersionTagAtHead(ctx context.Context, qualifier string) (string, bool, error) {
	out, err := w.git(ctx, "tag", "--points-at", "HEAD")
	if err != nil {
		return "", false, fmt.Errorf("list version tags at HEAD: %w", err)
	}
	var unpublished, published []string
	for _, line := range strings.Split(out, "\n") {
		tag := strings.TrimSpace(line)
		if tag == "" || !semverRE.MatchString(tag) {
			continue
		}
		if qualifier != "" && !strings.HasSuffix(tag, "+deploy-"+qualifier) {
			continue
		}
		if err := w.ensureSafeRef(tag); err != nil {
			return "", false, err
		}
		localObject, err := w.git(ctx, "rev-parse", "--verify", "refs/tags/"+tag)
		if err != nil {
			return "", false, fmt.Errorf("resolve local tag %s: %w", tag, err)
		}
		remoteObject, exists, err := w.remoteTagObject(ctx, tag)
		if err != nil {
			return "", false, err
		}
		if exists && remoteObject != strings.TrimSpace(localObject) {
			return "", false, fmt.Errorf("remote tag %s points to a different tag object", tag)
		}
		if exists {
			published = append(published, tag)
		} else {
			unpublished = append(unpublished, tag)
		}
	}
	if len(unpublished) > 1 {
		return "", false, fmt.Errorf("multiple unpushed version tags point at HEAD (%s); publish the intended tag explicitly", strings.Join(unpublished, ", "))
	}
	if len(unpublished) == 0 && len(published) == 0 {
		return "", false, nil
	}
	if len(unpublished) == 0 && len(published) > 1 {
		return "", false, fmt.Errorf("multiple version tags point at HEAD and are already published (%s); specify a qualifier to select one", strings.Join(published, ", "))
	}
	tag := ""
	if len(unpublished) == 1 {
		tag = unpublished[0]
	} else {
		tag = published[0]
	}
	if err := w.rejectDifferentVersionAtHead(ctx, tag); err != nil {
		return "", false, err
	}
	if len(unpublished) == 1 {
		if err := w.publishTagIfNeeded(ctx, tag); err != nil {
			return "", false, err
		}
	}
	return tag, true, nil
}

func (w *Workflows) remoteTagObject(ctx context.Context, tag string) (string, bool, error) {
	ref := "refs/tags/" + tag
	out, err := w.git(ctx, "ls-remote", "--refs", w.remote, ref)
	if err != nil {
		return "", false, fmt.Errorf("check remote tag %s: %w", tag, err)
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return "", false, nil
	}
	fields := strings.Fields(trimmed)
	if len(fields) != 2 || fields[1] != ref || strings.Contains(trimmed, "\n") {
		return "", false, fmt.Errorf("unexpected remote tag response for %s: %q", tag, trimmed)
	}
	return fields[0], true, nil
}

func (w *Workflows) publishTagIfNeeded(ctx context.Context, tag string) error {
	localObject, err := w.git(ctx, "rev-parse", "--verify", "refs/tags/"+tag)
	if err != nil {
		return fmt.Errorf("resolve local tag %s: %w", tag, err)
	}
	remoteObject, exists, err := w.remoteTagObject(ctx, tag)
	if err != nil {
		return err
	}
	if exists {
		if remoteObject != strings.TrimSpace(localObject) {
			return fmt.Errorf("remote tag %s points to a different tag object", tag)
		}
		return nil
	}
	ref := "refs/tags/" + tag
	if _, err := w.git(ctx, "push", w.remote, ref+":"+ref); err != nil {
		return err
	}
	return nil
}

// rejectDifferentVersionAtHead permits deployment-qualified aliases of
// one version, but prevents a distinct release/prerelease version from being
// assigned to the same immutable commit.
func (w *Workflows) rejectDifferentVersionAtHead(ctx context.Context, candidate string) error {
	candidateIdentity, valid := versionIdentity(candidate)
	if !valid {
		return fmt.Errorf("invalid semantic version tag %q", candidate)
	}
	out, err := w.git(ctx, "tag", "--points-at", "HEAD")
	if err != nil {
		return fmt.Errorf("list version tags at HEAD: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		existing := strings.TrimSpace(line)
		existingIdentity, valid := versionIdentity(existing)
		if !valid || existingIdentity == candidateIdentity {
			continue
		}
		return fmt.Errorf("commit already has version tag %s; refusing different version %s on the same commit", existing, candidate)
	}
	commit, err := w.git(ctx, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return fmt.Errorf("resolve HEAD for version-tag validation: %w", err)
	}
	remoteTags, err := w.git(ctx, "ls-remote", "--tags", w.remote)
	if err != nil {
		return fmt.Errorf("list remote version tags: %w", err)
	}
	remoteTargets := make(map[string]string)
	for _, line := range strings.Split(remoteTags, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !strings.HasPrefix(fields[1], "refs/tags/") {
			continue
		}
		ref := strings.TrimPrefix(fields[1], "refs/tags/")
		if strings.HasSuffix(ref, "^{}") {
			remoteTargets[strings.TrimSuffix(ref, "^{}")] = fields[0]
		} else if _, exists := remoteTargets[ref]; !exists {
			remoteTargets[ref] = fields[0]
		}
	}
	for tag, target := range remoteTargets {
		if target != strings.TrimSpace(commit) || tag == candidate {
			continue
		}
		tagIdentity, valid := versionIdentity(tag)
		if valid && tagIdentity != candidateIdentity {
			return fmt.Errorf("commit already has remote version tag %s; refusing different version %s on the same commit", tag, candidate)
		}
	}
	return nil
}

// versionIdentity removes only this command's deployment qualifier. SemVer
// prerelease/build identifiers remain part of the version identity (so beta.3
// and beta.4 cannot label the same commit).
func versionIdentity(tag string) (string, bool) {
	if i := strings.Index(tag, "+deploy-"); i >= 0 {
		tag = tag[:i]
	}
	if !semverRE.MatchString(tag) {
		return "", false
	}
	return tag, true
}
