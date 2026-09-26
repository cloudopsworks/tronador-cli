package versions

import (
	"context"
	"fmt"
)

func (w *Workflows) requireGitFlow() error {
	if !w.hasDevelop() {
		return fmt.Errorf("support branches are available only with gitflow WayOfWork")
	}
	return nil
}
func (w *Workflows) SupportStart(ctx context.Context, tag string) error {
	if e := w.requireGitFlow(); e != nil {
		return e
	}
	tag = normalizeVersion(tag)
	if e := w.ensureSafeRef(tag); e != nil {
		return e
	}
	// Validate only after fetching: a tag may exist solely on origin.
	if _, err := w.git(ctx, "fetch", w.remote, "--tags"); err != nil {
		return err
	}
	if _, e := w.git(ctx, "rev-parse", "--verify", tag+"^{commit}"); e != nil {
		return fmt.Errorf("support start requires existing tag %s: %w", tag, e)
	}
	_, err := w.git(ctx, "checkout", "-b", "support/"+tag, tag)
	return err
}
func (w *Workflows) supportName(ctx context.Context, name string) (string, error) {
	if e := w.requireGitFlow(); e != nil {
		return "", e
	}
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
	n, ok := branchValue(b, "support")
	if !ok {
		return "", fmt.Errorf("support version is required unless current branch is support/*")
	}
	return n, nil
}
func (w *Workflows) SupportPublish(ctx context.Context, name string) error {
	n, e := w.supportName(ctx, name)
	if e != nil {
		return e
	}
	b := "support/" + n
	if _, e = w.git(ctx, "checkout", b); e != nil {
		return e
	}
	_, e = w.git(ctx, "push", "--set-upstream", w.remote, b)
	return e
}
func (w *Workflows) SupportPurge(ctx context.Context, name string) error {
	n, e := w.supportName(ctx, name)
	if e != nil {
		return e
	}
	return w.purge(ctx, "support/"+n)
}
