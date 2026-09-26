package versions

import (
	"context"
	"fmt"
	"strings"
)

func (w *Workflows) FeatureStart(ctx context.Context, name string) error {
	if e := w.ensureSafeRef(name); e != nil {
		return e
	}
	base, e := w.featureBase(ctx)
	if e != nil {
		return e
	}
	if e = w.checkoutBase(ctx, base); e != nil {
		return e
	}
	if _, e = w.git(ctx, "checkout", "-b", "feature/"+name, "refs/heads/"+base); e != nil {
		return e
	}
	return nil
}

// featureBranch resolves an explicit short name to the canonical feature/*
// branch, but keeps the complete current branch for omitted names. This
// matters because the documented feat/* alias can coexist with feature/*.
func (w *Workflows) featureBranch(ctx context.Context, name string) (string, error) {
	if name != "" {
		if e := w.ensureSafeRef(name); e != nil {
			return "", e
		}
		return "feature/" + name, nil
	}
	b, e := w.Current(ctx)
	if e != nil {
		return "", e
	}
	_, ok := branchValue(b, "feature")
	if !ok {
		return "", fmt.Errorf("feature name is required unless current branch is feature/* or feat/*")
	}
	return b, nil
}
func (w *Workflows) FeaturePublish(ctx context.Context, name string) error {
	if e := w.validateFeaturePrimaryOverride(ctx); e != nil {
		return e
	}
	branch, e := w.featureBranch(ctx, name)
	if e != nil {
		return e
	}
	if _, e = w.git(ctx, "checkout", "--no-guess", branch); e != nil {
		return e
	}
	_, e = w.git(ctx, "push", "--set-upstream", w.remote, "refs/heads/"+branch+":refs/heads/"+branch)
	return e
}

// FeatureFinish creates the same guarded PR the legacy make target created.
func (w *Workflows) FeatureFinish(ctx context.Context, name string) error {
	base, e := w.featureBase(ctx)
	if e != nil {
		return e
	}
	branch, e := w.featureBranch(ctx, name)
	if e != nil {
		return e
	}
	if e = w.RequireParity(ctx, branch); e != nil {
		return e
	}
	_, e = w.gh(ctx, "pr", "create", "--head", branch, "-B", base, "-b", fmt.Sprintf("Feature %q finish, will merge into %q.", branch, base), "-t", fmt.Sprintf("chore: Feature Finish from %s", branch))
	return e
}
func (w *Workflows) FeaturePurge(ctx context.Context, name string) error {
	if _, e := w.featureBase(ctx); e != nil {
		return e
	}
	branch, e := w.featureBranch(ctx, name)
	if e != nil {
		return e
	}
	return w.purge(ctx, branch)
}

// validateFeaturePrimaryOverride ensures an explicitly supplied primary stays
// valid even when GitFlow feature operations use develop as their direct base.
// The main value is only populated without a user override after Main has
// already resolved it, so validating that cached value is also conservative.
func (w *Workflows) validateFeaturePrimaryOverride(ctx context.Context) error {
	if w.hasDevelop() && w.main != "" {
		_, err := w.Main(ctx)
		return err
	}
	return nil
}

func (w *Workflows) featureBase(ctx context.Context) (string, error) {
	if w.hasDevelop() {
		if err := w.validateFeaturePrimaryOverride(ctx); err != nil {
			return "", err
		}
		return "develop", nil
	}
	return w.Main(ctx)
}
func (w *Workflows) purge(ctx context.Context, branch string) error {
	if err := w.ensureSafeRef(branch); err != nil {
		return err
	}
	// Fetch before evaluating destructive preconditions. No checkout or delete
	// occurs until both publication parity and merge safety have been proven.
	if _, err := w.git(ctx, "fetch", w.remote, "--prune"); err != nil {
		return err
	}
	remoteOut, err := w.git(ctx, "ls-remote", w.remote, "refs/heads/"+branch)
	if err != nil {
		return err
	}
	remoteFields := strings.Fields(remoteOut)
	remoteExists := len(remoteFields) > 0
	localExists := w.branchExists(ctx, branch)
	if remoteExists {
		if !localExists {
			return fmt.Errorf("cannot safely purge %s: remote branch exists but local branch is unavailable for parity verification", branch)
		}
		if err = w.RequireParity(ctx, branch); err != nil {
			return err
		}
	}
	if !localExists && !remoteExists {
		return nil // already purged
	}
	bases, err := w.purgeBases(ctx, branch)
	if err != nil {
		return err
	}
	// Prove merge safety before checking out of the source branch. This keeps an
	// unmerged branch checked out instead of moving the user to a base branch.
	if err = w.verifyPurgeBases(ctx, branch, bases); err != nil {
		return err
	}
	current, err := w.Current(ctx)
	if err != nil {
		return err
	}
	if current == branch {
		if err = w.checkoutBase(ctx, bases[0]); err != nil {
			return err
		}
	}
	// checkoutBase fetches and pulls. Re-prove every target after that network
	// boundary and immediately before deletion, otherwise a rewritten target can
	// invalidate the earlier ancestry proof while the source lease still passes.
	if err = w.verifyPurgeBases(ctx, branch, bases); err != nil {
		return err
	}
	if remoteExists {
		// Re-observe source publication after every destructive precondition.
		// The SHA returned here is the compare-and-swap expectation for deletion:
		// retaining the earlier observation could accept an A->B->A ABA change.
		remoteSHA, err := w.remoteParitySHA(ctx, branch)
		if err != nil {
			return err
		}
		if err = w.deleteRemoteBranch(ctx, branch, remoteSHA); err != nil {
			return err
		}
	}
	if localExists {
		_, err = w.git(ctx, "branch", "-d", branch)
	}
	return err
}

func (w *Workflows) verifyPurgeBases(ctx context.Context, branch string, bases []string) error {
	// Test every required freshly fetched remote base. GitFlow releases require
	// both main and develop; the other workflows retain their single base.
	for _, base := range bases {
		if _, err := w.git(ctx, "merge-base", "--is-ancestor", "refs/heads/"+branch, "refs/remotes/"+w.remote+"/"+base); err != nil {
			return fmt.Errorf("cannot safely purge %s: it is not merged into %s: %w", branch, base, err)
		}
	}
	return nil
}

func (w *Workflows) purgeBases(ctx context.Context, branch string) ([]string, error) {
	if w.hasDevelop() && strings.HasPrefix(branch, "release/") {
		main, err := w.Main(ctx)
		if err != nil {
			return nil, err
		}
		if main == "develop" {
			return []string{main}, nil
		}
		return []string{main, "develop"}, nil
	}
	base, err := w.purgeBase(ctx, branch)
	if err != nil {
		return nil, err
	}
	return []string{base}, nil
}

func (w *Workflows) purgeBase(ctx context.Context, branch string) (string, error) {
	if w.hasDevelop() && (strings.HasPrefix(branch, "feature/") || strings.HasPrefix(branch, "feat/")) {
		return "develop", nil
	}
	return w.Main(ctx)
}
