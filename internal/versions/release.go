package versions

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

func (w *Workflows) ReleaseStart(ctx context.Context, kind string) error {
	if err := w.validateConfiguredGitFlowTopology(); err != nil {
		return err
	}
	if w.hasDevelop() {
		if err := w.validateConfiguredMainLive(ctx); err != nil {
			return err
		}
	}
	base, e := w.Main(ctx)
	if e != nil {
		return e
	}
	if w.hasDevelop() {
		base = "develop"
	}
	// Version calculation must happen on the synchronized release base.
	if e = w.checkoutBase(ctx, base); e != nil {
		return e
	}
	v, e := w.CurrentVersion(ctx)
	if e != nil {
		return e
	}
	v, e = bump(v, kind)
	if e != nil {
		return e
	}
	_, e = w.git(ctx, "checkout", "-b", "release/"+v, "refs/heads/"+base)
	return e
}
func (w *Workflows) releaseName(ctx context.Context, name string) (string, error) {
	if name != "" {
		name = normalizeVersion(name)
		if e := w.ensureSafeRef(name); e != nil {
			return "", e
		}
		return name, nil
	}
	b, e := w.Current(ctx)
	if e != nil {
		return "", e
	}
	n, ok := branchValue(b, "release")
	if !ok {
		return "", fmt.Errorf("release version is required unless current branch is release/*")
	}
	return n, nil
}
func (w *Workflows) ReleasePublish(ctx context.Context, name string) error {
	if err := w.validateConfiguredGitFlowTopology(); err != nil {
		return err
	}
	n, e := w.releaseName(ctx, name)
	if e != nil {
		return e
	}
	b := "release/" + n
	if _, e = w.git(ctx, "checkout", "--no-guess", b); e != nil {
		return e
	}
	_, e = w.git(ctx, "push", "--set-upstream", w.remote, "refs/heads/"+b+":refs/heads/"+b)
	return e
}
func (w *Workflows) ReleaseFinish(ctx context.Context, name string, local bool) error {
	if err := w.validateConfiguredGitFlowTopology(); err != nil {
		return err
	}
	if local && name == "" {
		if resumed, found, err := w.resumeLocalFinishName(ctx, "release-finish", "release"); err != nil {
			return err
		} else if found {
			name = resumed
		}
	}
	n, e := w.releaseName(ctx, name)
	if e != nil {
		return e
	}
	branch := "release/" + n
	if !local {
		// Source parity and target ancestry must share one fetched view. Checking
		// parity first could leave a source branch stale after fetch while gh
		// resolves the newer remote head for PR creation.
		if _, e = w.git(ctx, "fetch", w.remote, "--prune"); e != nil {
			return e
		}
		if e = w.RequireParity(ctx, branch); e != nil {
			return e
		}
		main, e := w.Main(ctx)
		if e != nil {
			return e
		}
		if e = w.ensureReleasePR(ctx, branch, main, n); e != nil {
			return e
		}
		if w.hasDevelop() && main != "develop" {
			e = w.ensureReleasePR(ctx, branch, "develop", n)
		}
		return e
	}
	return w.finishReleaseLocal(ctx, branch, n)
}

func (w *Workflows) ensureReleasePR(ctx context.Context, branch, base, version string) error {
	contained, err := w.releaseTargetContains(ctx, branch, base)
	if err != nil {
		return err
	}
	if contained {
		return nil
	}
	open, err := w.releasePRCount(ctx, branch, base, "open")
	if err != nil {
		return err
	}
	if open > 0 {
		return nil
	}
	if _, err = w.gh(ctx, "pr", "create", "--head", branch, "-B", base, "-b", fmt.Sprintf("Release %s", version), "-t", fmt.Sprintf("chore: Release %s from %s", version, branch)); err != nil {
		return fmt.Errorf("create release PR %s -> %s: %w", branch, base, err)
	}
	return nil
}

// releaseTargetContains proves whether the exact local release source is in a
// freshly fetched remote target. Exit status 1 is Git's documented "not an
// ancestor" result; every other failure is unsafe to treat as absence.
func (w *Workflows) releaseTargetContains(ctx context.Context, branch, base string) (bool, error) {
	_, err := w.git(ctx, "merge-base", "--is-ancestor", "refs/heads/"+branch, "refs/remotes/"+w.remote+"/"+base)
	if err == nil {
		return true, nil
	}
	if isExitStatus(err, 1) {
		return false, nil
	}
	return false, fmt.Errorf("check whether release %s is merged into %s: %w", branch, base, err)
}

func (w *Workflows) releasePRCount(ctx context.Context, branch, base, state string) (int, error) {
	out, err := w.gh(ctx, "pr", "list", "--head", branch, "--base", base, "--state", state, "--json", "number", "--jq", "length")
	if err != nil {
		return 0, fmt.Errorf("check existing %s release PR %s -> %s: %w", state, branch, base, err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || count < 0 {
		return 0, fmt.Errorf("check existing %s release PR %s -> %s: expected numeric count, got %q", state, branch, base, strings.TrimSpace(out))
	}
	return count, nil
}
func (w *Workflows) finishReleaseLocal(ctx context.Context, branch, version string) error {
	if w.isDryRun() {
		return nil
	}
	unlock, e := w.acquireJournalLock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	if _, e = w.git(ctx, "fetch", w.remote, "--prune", "--tags"); e != nil {
		return e
	}
	target, e := w.Main(ctx)
	if e != nil {
		return e
	}
	steps := w.localFinishSteps("release-finish")
	j, p, e := w.startLocalFinishJournal(ctx, "release-finish", branch, target, version, steps)
	if e != nil {
		return e
	}
	for j.Done < len(j.Steps) {
		s := j.Steps[j.Done]
		switch s {
		case "checkout-main":
			e = w.checkoutFetchedBase(ctx, target)
		case "merge-main":
			e = w.checkoutFinishBranch(ctx, target)
			if e == nil {
				e = w.mergeContinue(ctx)
			}
			if e == nil {
				_, e = w.git(ctx, "merge", "--no-ff", j.SourceSHA, "-m", fmt.Sprintf("chore: Release %s", version))
			}
		case "merge-develop":
			e = w.checkoutFinishBranch(ctx, "develop")
			if e == nil {
				e = w.mergeContinue(ctx)
			}
			if e == nil {
				_, e = w.git(ctx, "merge", "--no-ff", j.SourceSHA, "-m", fmt.Sprintf("chore: Release %s", version))
			}
		case "tag":
			e = w.checkoutFinishBranch(ctx, target)
			if e == nil {
				e = w.recordFinishTagTarget(ctx, p, j)
			}
			if e == nil {
				e = w.ensureAnnotatedTag(ctx, version, fmt.Sprintf("chore: Release %s", version), j.TagTargetSHA)
			}
		case "checkout-develop":
			e = w.checkoutFetchedBase(ctx, "develop")
		case "publish-and-delete-remote":
			targets := []string{target}
			if w.hasDevelop() {
				targets = append(targets, "develop")
			}
			e = w.publishFinishedAndDeleteRemote(ctx, p, j, targets, version)
		case "delete-local":
			e = w.deleteLocalBranch(ctx, branch)
		}
		if e != nil {
			return fmt.Errorf("%s: %w", s, e)
		}
		if e = w.advanceJournal(p, j); e != nil {
			return e
		}
	}
	return clearJournal(p)
}

func (w *Workflows) verifyReleaseFinished(ctx context.Context, target, sourceSHA, tag string) error {
	if err := w.verifyFinished(ctx, target, sourceSHA, tag); err != nil {
		return err
	}
	if w.hasDevelop() {
		if _, err := w.git(ctx, "merge-base", "--is-ancestor", sourceSHA, "refs/heads/develop"); err != nil {
			return fmt.Errorf("finish postcondition: %s is not merged into develop: %w", sourceSHA, err)
		}
	}
	return nil
}
func (w *Workflows) ReleasePurge(ctx context.Context, name string) error {
	if err := w.validateConfiguredGitFlowTopology(); err != nil {
		return err
	}
	n, e := w.releaseName(ctx, name)
	if e != nil {
		return e
	}
	return w.purge(ctx, "release/"+n)
}
func (w *Workflows) verifyFinished(ctx context.Context, target, sourceSHA, tag string) error {
	if _, e := w.git(ctx, "merge-base", "--is-ancestor", sourceSHA, "refs/heads/"+target); e != nil {
		return fmt.Errorf("finish postcondition: %s is not merged into %s: %w", sourceSHA, target, e)
	}
	if _, e := w.git(ctx, "rev-parse", "--verify", "refs/tags/"+tag+"^{tag}"); e != nil {
		return fmt.Errorf("finish postcondition: annotated tag %s missing: %w", tag, e)
	}
	return nil
}
