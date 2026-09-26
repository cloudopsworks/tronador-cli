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

// validateFeaturePrimaryOverride keeps GitFlow feature operations on develop
// while still enforcing a supplied primary override before mutation.
func (w *Workflows) validateFeaturePrimaryOverride(ctx context.Context) error {
	if !w.hasDevelop() {
		return nil
	}
	return w.validateConfiguredMainLive(ctx)
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
	if err = w.verifyPurgeBases(ctx, branch, "refs/heads/"+branch, bases); err != nil {
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
	// checkoutBase fetches and pulls. Snapshot the source after that network
	// boundary, then prove every target contains that immutable commit. Using
	// refs/heads/<branch> here would allow a concurrent local advance after the
	// proof to be leased and deleted without itself being proven merged.
	source := "refs/heads/" + branch
	remoteSHA := ""
	if remoteExists {
		remoteSHA, err = w.remoteParitySHA(ctx, branch)
		if err != nil {
			return err
		}
		source = remoteSHA
	} else {
		source, err = w.git(ctx, "rev-parse", "--verify", source+"^{commit}")
		if err != nil {
			return fmt.Errorf("resolve local purge source %s: %w", branch, err)
		}
		source = strings.TrimSpace(source)
	}
	if err = w.verifyPurgeBases(ctx, branch, source, bases); err != nil {
		return err
	}
	if remoteExists {
		if err = w.deleteRemoteBranch(ctx, branch, remoteSHA); err != nil {
			return err
		}
	}
	if localExists {
		_, err = w.git(ctx, "branch", "-d", branch)
	}
	return err
}

func (w *Workflows) verifyPurgeBases(ctx context.Context, branch, source string, bases []string) error {
	// Test every required freshly fetched remote base. GitFlow releases require
	// both main and develop; the other workflows retain their single base.
	for _, base := range bases {
		if _, err := w.git(ctx, "merge-base", "--is-ancestor", source, "refs/remotes/"+w.remote+"/"+base); err != nil {
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
