package repos

import (
	"context"
	"errors"
	"fmt"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

type goGitClient struct{}

func (goGitClient) Clone(ctx context.Context, url, destination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := git.PlainCloneContext(ctx, destination, false, &git.CloneOptions{URL: url})
	if err != nil {
		return fmt.Errorf("clone %s: %w", url, err)
	}
	return nil
}

func (goGitClient) RemoveRemote(ctx context.Context, workdir, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	repo, err := git.PlainOpen(workdir)
	if err != nil {
		return fmt.Errorf("open git repository %s: %w", workdir, err)
	}
	if err := repo.DeleteRemote(name); err != nil && !errors.Is(err, git.ErrRemoteNotFound) {
		return fmt.Errorf("remove git remote %s: %w", name, err)
	}
	return nil
}

func (goGitClient) Checkout(ctx context.Context, workdir, ref string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	repo, err := git.PlainOpen(workdir)
	if err != nil {
		return "", fmt.Errorf("open git repository %s: %w", workdir, err)
	}
	hash, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return "", fmt.Errorf("resolve revision %s: %w", ref, err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("open git worktree %s: %w", workdir, err)
	}
	if err := worktree.Checkout(&git.CheckoutOptions{Hash: *hash}); err != nil {
		return "", fmt.Errorf("checkout %s: %w", ref, err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return hash.String(), nil
}

func (goGitClient) OriginOwnerRepo(ctx context.Context, workdir string) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	repo, err := git.PlainOpen(workdir)
	if err != nil {
		return "", "", fmt.Errorf("open git repository %s: %w", workdir, err)
	}
	remote, err := repo.Remote("origin")
	if err != nil {
		return "", "", fmt.Errorf("read origin remote: %w", err)
	}
	urls := remote.Config().URLs
	if len(urls) == 0 {
		return "", "", fmt.Errorf("origin remote has no URLs")
	}
	owner, repoName, err := parseOwnerRepo(urls[0])
	if err != nil {
		return "", "", err
	}
	return owner, repoName, nil
}
