package versions

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v: %s", args, e, b)
	}
	return string(b)
}
func TestFeatureAliasPurgeChecksOutSafeBaseWithoutDeletingCanonicalCoexistence(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	gitTest(t, root, "init", "--bare", remote)
	repo := filepath.Join(root, "repo")
	gitTest(t, root, "clone", remote, repo)
	gitTest(t, repo, "config", "user.email", "test@example.test")
	gitTest(t, repo, "config", "user.name", "Test")
	gitTest(t, repo, "checkout", "-b", "main")
	gitTest(t, repo, "commit", "--allow-empty", "-m", "initial")
	gitTest(t, repo, "push", "-u", "origin", "main")
	gitTest(t, repo, "checkout", "-b", "develop")
	gitTest(t, repo, "push", "-u", "origin", "develop")
	gitTest(t, repo, "checkout", "-b", "feat/remove-me")
	gitTest(t, repo, "push", "-u", "origin", "feat/remove-me")
	gitTest(t, repo, "checkout", "develop")
	gitTest(t, repo, "checkout", "-b", "feature/remove-me")
	gitTest(t, repo, "push", "-u", "origin", "feature/remove-me")
	gitTest(t, repo, "checkout", "feat/remove-me")
	w, e := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "gitflow"})
	if e != nil {
		t.Fatal(e)
	}
	if e = w.FeaturePurge(ctx, ""); e != nil {
		t.Fatal(e)
	}
	if got := gitTest(t, repo, "branch", "--show-current"); got != "develop\n" {
		t.Fatalf("current branch %q", got)
	}
	c := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/feat/remove-me")
	c.Dir = repo
	if e := c.Run(); e == nil {
		t.Fatal("local branch remains")
	}
	out := gitTest(t, repo, "ls-remote", "origin", "refs/heads/feat/remove-me")
	if out != "" {
		t.Fatalf("remote branch remains: %s", out)
	}
	if got := gitTest(t, repo, "show-ref", "--verify", "refs/heads/feature/remove-me"); got == "" {
		t.Fatal("canonical coexisting feature branch was deleted")
	}
	if out := gitTest(t, repo, "ls-remote", "origin", "refs/heads/feature/remove-me"); out == "" {
		t.Fatal("remote canonical coexisting feature branch was deleted")
	}
}

func TestRunnerWorkflowDryRunPerformsNoGitMutation(t *testing.T) {
	ctx := context.Background()
	_, repo := setupWorkflowRemote(t, false)
	r, err := NewRunner(Options{WorkDir: repo, MainBranch: "main", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	w, err := r.Workflow(WayOfWorkGitHubFlow)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.FeatureStart(ctx, "dry-run"); err != nil {
		t.Fatal(err)
	}
	c := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/feature/dry-run")
	c.Dir = repo
	if err = c.Run(); err == nil {
		t.Fatal("dry-run created a feature branch")
	}
}

func TestBranchWorkflowRefsIgnoreSameNamedAnnotatedTags(t *testing.T) {
	ctx := context.Background()
	_, repo := setupWorkflowRemote(t, true)
	gitTest(t, repo, "config", "core.warnAmbiguousRefs", "false")
	gitTest(t, repo, "tag", "-a", "main", "-m", "shadow main")
	gitTest(t, repo, "checkout", "develop")
	gitTest(t, repo, "tag", "-a", "develop", "-m", "shadow develop")

	w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "gitflow", MainBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.FeatureStart(ctx, "collision"); err != nil {
		t.Fatalf("feature start with develop tag collision: %v", err)
	}
	gitTest(t, repo, "tag", "-a", "feature/collision", "-m", "shadow feature")
	if err = w.FeaturePublish(ctx, "collision"); err != nil {
		t.Fatalf("feature publish with branch/tag collision: %v", err)
	}
	if got := gitTest(t, repo, "ls-remote", "origin", "refs/heads/feature/collision"); !strings.Contains(got, "refs/heads/feature/collision") {
		t.Fatalf("feature was not published through qualified refspec: %q", got)
	}

	gitTest(t, repo, "checkout", "develop")
	gitTest(t, repo, "checkout", "-b", "release/v0.2.0")
	gitTest(t, repo, "commit", "--allow-empty", "-m", "release")
	gitTest(t, repo, "push", "-u", "origin", "release/v0.2.0")
	sourceSHA := strings.TrimSpace(gitTest(t, repo, "rev-parse", "refs/heads/release/v0.2.0"))
	if err = w.ReleaseFinish(ctx, "0.2.0", true); err != nil {
		t.Fatalf("release finish with main/develop tag collisions: %v", err)
	}
	for _, branch := range []string{"main", "develop"} {
		gitTest(t, repo, "merge-base", "--is-ancestor", sourceSHA, "refs/remotes/origin/"+branch)
	}
}
