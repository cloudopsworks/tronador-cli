package repos

import (
	"context"
	"fmt"
	"strings"
)

// GitClient is the native Git surface used by repos workflows. Shell Git is
// retained only as a fallback for credential-helper parity and for staging /
// commit operations that intentionally mimic the Makefile workflow.
type GitClient interface {
	Clone(ctx context.Context, url, destination string) error
	RemoveRemote(ctx context.Context, workdir, name string) error
	Checkout(ctx context.Context, workdir, ref string) (string, error)
	OriginOwnerRepo(ctx context.Context, workdir string) (string, string, error)
}

// GitHubClient is the GitHub API surface used by repos workflows.
type GitHubClient interface {
	ListTags(ctx context.Context, repository string) ([]string, error)
}

func (r *Runner) git() GitClient {
	if r.gitClient == nil {
		r.gitClient = goGitClient{}
	}
	return r.gitClient
}

func (r *Runner) github() GitHubClient {
	if r.githubClient == nil {
		r.githubClient = goGHClient{}
	}
	return r.githubClient
}

func (r *Runner) cloneTemplateRepository(ctx context.Context, url string) error {
	if r.Opts.DryRun {
		return r.run(ctx, r.gitPath(), "clone", url, r.Config.TemplateDirectory)
	}
	if err := r.git().Clone(ctx, url, r.path(r.Config.TemplateDirectory)); err != nil {
		fmt.Fprintf(r.Opts.Stderr, "Native git clone failed, falling back to git: %v\n", err)
		return r.run(ctx, r.gitPath(), "clone", url, r.Config.TemplateDirectory)
	}
	return nil
}

func (r *Runner) checkoutTemplateRepository(ctx context.Context, ref string) (string, error) {
	if r.Opts.DryRun {
		return r.checkoutTemplateRepositoryFromShell(ctx, ref)
	}
	hash, err := r.git().Checkout(ctx, r.path(r.Config.TemplateDirectory), ref)
	if err == nil {
		return hash, nil
	}
	fmt.Fprintf(r.Opts.Stderr, "Native git checkout failed, falling back to git: %v\n", err)
	return r.checkoutTemplateRepositoryFromShell(ctx, ref)
}

func (r *Runner) checkoutTemplateRepositoryFromShell(ctx context.Context, ref string) (string, error) {
	templateDir := r.path(r.Config.TemplateDirectory)
	if err := r.runInDir(ctx, templateDir, r.gitPath(), "checkout", ref); err != nil {
		return "", err
	}
	out, err := r.outputInDir(ctx, templateDir, r.gitPath(), "rev-parse", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Runner) setDefaultRepository(ctx context.Context, owner, repo string) error {
	// gh repo set-default writes gh CLI state. Keep the shell call here for
	// compatibility while the Git/GitHub read paths move to native clients.
	return r.run(ctx, r.ghPath(), "repo", "set-default", fmt.Sprintf("%s/%s", owner, repo))
}
