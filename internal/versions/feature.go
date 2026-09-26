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
	base := ""
	var e error
	if w.hasDevelop() {
		base = "develop"
	} else {
		base, e = w.Main(ctx)
		if e != nil {
			return e
		}
	}
	if e = w.checkoutBase(ctx, base); e != nil {
		return e
	}
	if _, e = w.git(ctx, "checkout", "-b", "feature/"+name, base); e != nil {
		return e
	}
	return nil
}
func (w *Workflows) featureName(ctx context.Context, name string) (string, error) {
	if name != "" {
		if e := w.ensureSafeRef(name); e != nil {
			return "", e
		}
		return name, nil
	}
	b, e := w.Current(ctx)
	if e != nil {
		return "", e
	}
	n, ok := branchValue(b, "feature")
	if !ok {
		return "", fmt.Errorf("feature name is required unless current branch is feature/*")
	}
	return n, nil
}
func (w *Workflows) FeaturePublish(ctx context.Context, name string) error {
	n, e := w.featureName(ctx, name)
	if e != nil {
		return e
	}
	branch := "feature/" + n
	if _, e = w.git(ctx, "checkout", branch); e != nil {
		return e
	}
	_, e = w.git(ctx, "push", "--set-upstream", w.remote, branch)
	return e
}

// FeatureFinish creates the same guarded PR the legacy make target created.
func (w *Workflows) FeatureFinish(ctx context.Context, name string) error {
	n, e := w.featureName(ctx, name)
	if e != nil {
		return e
	}
	branch := "feature/" + n
	if e = w.RequireParity(ctx, branch); e != nil {
		return e
	}
	base := ""
	if w.hasDevelop() {
		base = "develop"
	} else {
		base, e = w.Main(ctx)
		if e != nil {
			return e
		}
	}
	_, e = w.gh(ctx, "pr", "create", "--head", branch, "-B", base, "-b", fmt.Sprintf("Feature %q finish, will merge into %q.", branch, base), "-t", fmt.Sprintf("chore: Feature Finish from %s", branch))
	return e
}
func (w *Workflows) FeaturePurge(ctx context.Context, name string) error {
	n, e := w.featureName(ctx, name)
	if e != nil {
		return e
	}
	return w.purge(ctx, "feature/"+n)
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
	localExists := w.branchExists(ctx, branch)
	if len(remoteFields) > 0 {
		if !localExists {
			return fmt.Errorf("cannot safely purge %s: remote branch exists but local branch is unavailable for parity verification", branch)
		}
		if err = w.RequireParity(ctx, branch); err != nil {
			return err
		}
	}
	if !localExists && len(remoteFields) == 0 {
		return nil // already purged
	}
	base, err := w.purgeBase(ctx, branch)
	if err != nil {
		return err
	}
	// Test against the freshly fetched remote base, rather than a possibly stale
	// local tracking branch. This rejects unmerged source work.
	if _, err = w.git(ctx, "merge-base", "--is-ancestor", branch, "refs/remotes/"+w.remote+"/"+base); err != nil {
		return fmt.Errorf("cannot safely purge %s: it is not merged into %s: %w", branch, base, err)
	}
	current, err := w.Current(ctx)
	if err != nil {
		return err
	}
	if current == branch {
		if err = w.checkoutBase(ctx, base); err != nil {
			return err
		}
	}
	if localExists {
		if _, err = w.git(ctx, "branch", "-d", branch); err != nil {
			return err
		}
	}
	if len(remoteFields) > 0 {
		_, err = w.git(ctx, "push", w.remote, "--delete", branch)
	}
	return err
}

func (w *Workflows) purgeBase(ctx context.Context, branch string) (string, error) {
	if w.hasDevelop() && (strings.HasPrefix(branch, "feature/") || strings.HasPrefix(branch, "release/")) {
		return "develop", nil
	}
	return w.Main(ctx)
}
