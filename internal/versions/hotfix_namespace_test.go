package versions

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestHotfixFinishTargetsSupportBranchDespiteSameNamedTags(t *testing.T) {
	ctx := context.Background()
	_, repo := setupWorkflowRemote(t, true)
	gitTest(t, repo, "config", "core.warnAmbiguousRefs", "false")
	mainBefore := strings.TrimSpace(gitTest(t, repo, "rev-parse", "refs/heads/main"))

	const support = "support/v1.2.0"
	gitTest(t, repo, "checkout", "-b", support)
	gitTest(t, repo, "push", "-u", "origin", support)
	gitTest(t, repo, "tag", "-a", support, "-m", "shadow support branch")
	gitTest(t, repo, "push", "origin", "refs/tags/"+support+":refs/tags/"+support)
	gitTest(t, repo, "checkout", "-b", "hotfix/v1.2.4")
	gitTest(t, repo, "commit", "--allow-empty", "-m", "hotfix")
	gitTest(t, repo, "push", "-u", "origin", "hotfix/v1.2.4")
	sourceSHA := strings.TrimSpace(gitTest(t, repo, "rev-parse", "refs/heads/hotfix/v1.2.4"))

	w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "gitflow", MainBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.HotfixFinish(ctx, "1.2.4", true); err != nil {
		t.Fatalf("hotfix finish with support tag collision: %v", err)
	}
	if got := strings.TrimSpace(gitTest(t, repo, "rev-parse", "refs/heads/main")); got != mainBefore {
		t.Fatalf("main changed: got %s, want %s", got, mainBefore)
	}
	gitTest(t, repo, "merge-base", "--is-ancestor", sourceSHA, "refs/heads/"+support)
	gitTest(t, repo, "merge-base", "--is-ancestor", sourceSHA, "refs/remotes/origin/"+support)
	check := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/hotfix/v1.2.4")
	check.Dir = repo
	if err := check.Run(); err == nil {
		t.Fatal("local hotfix branch remains after finish")
	}
	if got := gitTest(t, repo, "ls-remote", "origin", "refs/heads/hotfix/v1.2.4"); got != "" {
		t.Fatalf("remote hotfix branch remains after finish: %q", got)
	}
}
