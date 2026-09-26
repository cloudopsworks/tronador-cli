package versions

import (
	"context"
	"fmt"
)

func (w *Workflows) ReleaseStart(ctx context.Context, kind string) error {
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
	_, e = w.git(ctx, "checkout", "-b", "release/"+v, base)
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
	n, e := w.releaseName(ctx, name)
	if e != nil {
		return e
	}
	b := "release/" + n
	if _, e = w.git(ctx, "checkout", b); e != nil {
		return e
	}
	_, e = w.git(ctx, "push", "--set-upstream", w.remote, b)
	return e
}
func (w *Workflows) ReleaseFinish(ctx context.Context, name string, local bool) error {
	n, e := w.releaseName(ctx, name)
	if e != nil {
		return e
	}
	branch := "release/" + n
	if !local {
		if e = w.RequireParity(ctx, branch); e != nil {
			return e
		}
		main, e := w.Main(ctx)
		if e != nil {
			return e
		}
		if _, e = w.gh(ctx, "pr", "create", "--head", branch, "-B", main, "-b", fmt.Sprintf("Release %s", n), "-t", fmt.Sprintf("chore: Release %s from %s", n, branch)); e != nil {
			return e
		}
		if w.hasDevelop() && main != "develop" {
			_, e = w.gh(ctx, "pr", "create", "--head", branch, "-B", "develop", "-b", fmt.Sprintf("Release %s", n), "-t", fmt.Sprintf("chore: Release %s from %s", n, branch))
		}
		return e
	}
	return w.finishReleaseLocal(ctx, branch, n)
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
	target, e := w.Main(ctx)
	if e != nil {
		return e
	}
	steps := []string{"checkout-main", "merge-main", "tag", "push-main", "push-tag"}
	if w.hasDevelop() {
		steps = append(steps, "checkout-develop", "merge-develop", "push-develop")
	}
	steps = append(steps, "delete-local", "delete-remote")
	j, p, e := w.startLocalFinishJournal(ctx, "release-finish", branch, target, steps)
	if e != nil {
		return e
	}
	for j.Done < len(j.Steps) {
		s := j.Steps[j.Done]
		if s == "delete-local" {
			if err := w.verifyFinished(ctx, target, branch, version); err != nil {
				return err
			}
		}
		switch s {
		case "checkout-main":
			e = w.checkoutBase(ctx, target)
		case "merge-main", "merge-develop":
			e = w.mergeContinue(ctx)
			if e == nil {
				_, e = w.git(ctx, "merge", "--no-ff", branch, "-m", fmt.Sprintf("chore: Release %s", version))
			}
		case "tag":
			e = w.ensureAnnotatedTag(ctx, version, fmt.Sprintf("chore: Release %s", version), target)
		case "push-main":
			_, e = w.git(ctx, "push", w.remote, target)
		case "push-tag":
			_, e = w.git(ctx, "push", w.remote, version)
		case "checkout-develop":
			e = w.checkoutBase(ctx, "develop")
		case "push-develop":
			_, e = w.git(ctx, "push", w.remote, "develop")
		case "delete-local":
			e = w.deleteLocalBranch(ctx, branch)
		case "delete-remote":
			e = w.deleteRemoteBranch(ctx, branch)
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
func (w *Workflows) ReleasePurge(ctx context.Context, name string) error {
	n, e := w.releaseName(ctx, name)
	if e != nil {
		return e
	}
	return w.purge(ctx, "release/"+n)
}
func (w *Workflows) verifyFinished(ctx context.Context, target, branch, tag string) error {
	if _, e := w.git(ctx, "merge-base", "--is-ancestor", branch, target); e != nil {
		return fmt.Errorf("finish postcondition: %s is not merged into %s: %w", branch, target, e)
	}
	if _, e := w.git(ctx, "rev-parse", "--verify", tag+"^{tag}"); e != nil {
		return fmt.Errorf("finish postcondition: annotated tag %s missing: %w", tag, e)
	}
	return nil
}
