package versions

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func exitStatusOne(t *testing.T) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit 1").Run()
	if err == nil {
		t.Fatal("expected exit status 1")
	}
	return err
}

func TestReleasePurgeGitflowRequiresEveryTargetBeforeDeleting(t *testing.T) {
	ctx := context.Background()
	_, repo := setupWorkflowRemote(t, true)
	branch := "release/v0.2.0"

	gitTest(t, repo, "checkout", "--no-guess", "develop")
	gitTest(t, repo, "checkout", "-b", branch)
	gitTest(t, repo, "commit", "--allow-empty", "-m", "release")
	gitTest(t, repo, "push", "-u", "origin", branch)
	gitTest(t, repo, "checkout", "--no-guess", "develop")
	gitTest(t, repo, "merge", "--no-ff", branch, "-m", "merge release into develop")
	gitTest(t, repo, "push", "origin", "refs/heads/develop:refs/heads/develop")
	gitTest(t, repo, "checkout", "--no-guess", branch)

	w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "gitflow", MainBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.ReleasePurge(ctx, "0.2.0"); err == nil || !strings.Contains(err.Error(), "not merged into main") {
		t.Fatalf("purge error = %v, want missing main integration", err)
	}
	if current := strings.TrimSpace(gitTest(t, repo, "branch", "--show-current")); current != branch {
		t.Fatalf("purge changed branch before all target checks: got %q, want %q", current, branch)
	}
	gitTest(t, repo, "show-ref", "--verify", "refs/heads/"+branch)
	if remote := gitTest(t, repo, "ls-remote", "origin", "refs/heads/"+branch); !strings.Contains(remote, "refs/heads/"+branch) {
		t.Fatalf("purge deleted remote branch despite missing main integration: %q", remote)
	}

	gitTest(t, repo, "checkout", "--no-guess", "main")
	gitTest(t, repo, "merge", "--no-ff", branch, "-m", "merge release into main")
	gitTest(t, repo, "push", "origin", "refs/heads/main:refs/heads/main")
	gitTest(t, repo, "checkout", "--no-guess", branch)
	if err = w.ReleasePurge(ctx, "0.2.0"); err != nil {
		t.Fatalf("purge after every target integration: %v", err)
	}
	check := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	check.Dir = repo
	if err := check.Run(); err == nil {
		t.Fatalf("local branch %s remains", branch)
	}
	if remote := gitTest(t, repo, "ls-remote", "origin", "refs/heads/"+branch); remote != "" {
		t.Fatalf("remote branch %s remains: %q", branch, remote)
	}
}

func TestReleaseFinishRemoteCreatesOnlyTargetsMissingRelease(t *testing.T) {
	ctx := context.Background()
	branch := "release/v1.2.3"
	mainMissing := exitStatusOne(t)
	f := &fakeRunner{replies: map[string]string{
		key("git", "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}"):                                                  "sha\n",
		key("git", "ls-remote", "origin", "refs/heads/"+branch):                                                                "sha\trefs/heads/" + branch + "\n",
		key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"):                                                      "refs/remotes/origin/main\n",
		key("gh", "pr", "list", "--head", branch, "--base", "main", "--state", "open", "--json", "number", "--jq", "length"):   "0\n",
		key("gh", "pr", "list", "--head", branch, "--base", "main", "--state", "merged", "--json", "number", "--jq", "length"): "0\n",
	}, errs: map[string]error{
		key("git", "merge-base", "--is-ancestor", "refs/heads/"+branch, "refs/remotes/origin/main"): mainMissing,
	}}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.ReleaseFinish(ctx, "1.2.3", false); err != nil {
		t.Fatal(err)
	}
	if !f.saw("git", "fetch", "origin", "--prune") {
		t.Fatalf("remote target ancestry was checked without a fresh fetch: %#v", f.calls)
	}
	if !f.saw("gh", "pr", "create", "--head", branch, "-B", "main", "-b", "Release v1.2.3", "-t", "chore: Release v1.2.3 from "+branch) {
		t.Fatalf("missing main PR: %#v", f.calls)
	}
	if f.sawPrefix("gh", "pr", "list", "--head", branch, "--base", "develop") || f.saw("gh", "pr", "create", "--head", branch, "-B", "develop", "-b", "Release v1.2.3", "-t", "chore: Release v1.2.3 from "+branch) {
		t.Fatalf("develop PR was considered despite ancestry proof: %#v", f.calls)
	}
}

func TestReleaseFinishRemoteFailsClosedWhenTargetProbeFails(t *testing.T) {
	branch := "release/v1.2.3"
	f := &fakeRunner{replies: map[string]string{
		key("git", "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}"): "sha\n",
		key("git", "ls-remote", "origin", "refs/heads/"+branch):               "sha\trefs/heads/" + branch + "\n",
		key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"):     "refs/remotes/origin/main\n",
	}, errs: map[string]error{
		key("git", "merge-base", "--is-ancestor", "refs/heads/"+branch, "refs/remotes/origin/main"): fmt.Errorf("target probe failed"),
	}}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.ReleaseFinish(context.Background(), "1.2.3", false); err == nil || !strings.Contains(err.Error(), "check whether release") {
		t.Fatalf("ReleaseFinish error = %v, want failed target probe", err)
	}
	if f.sawPrefix("gh", "pr", "create") {
		t.Fatalf("PR created after target probe failure: %#v", f.calls)
	}
}

type fetchAdvancingReleaseRunner struct {
	calls   []call
	fetched bool
}

func (r *fetchAdvancingReleaseRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, call{name: name, args: append([]string(nil), args...)})
	if key(name, args...) == key("git", "fetch", "origin", "--prune") {
		r.fetched = true
		return "", nil
	}
	branch := "release/v1.2.3"
	switch key(name, args...) {
	case key("git", "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}"):
		return "local-sha\n", nil
	case key("git", "ls-remote", "origin", "refs/heads/"+branch):
		if r.fetched {
			return "remote-advanced\trefs/heads/" + branch + "\n", nil
		}
		return "local-sha\trefs/heads/" + branch + "\n", nil
	}
	return "", nil
}

func (r *fetchAdvancingReleaseRunner) sawPrefix(parts ...string) bool {
	for _, c := range r.calls {
		callParts := append([]string{c.name}, c.args...)
		if len(callParts) < len(parts) {
			continue
		}
		matches := true
		for i, part := range parts {
			if callParts[i] != part {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func TestReleaseFinishRemoteRechecksSourceParityAfterFetch(t *testing.T) {
	r := &fetchAdvancingReleaseRunner{}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", Runner: r})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.ReleaseFinish(context.Background(), "1.2.3", false); err == nil || !strings.Contains(err.Error(), "not exactly published") {
		t.Fatalf("ReleaseFinish error = %v, want source parity rejection", err)
	}
	if !r.fetched {
		t.Fatal("ReleaseFinish did not fetch before source parity validation")
	}
	if r.sawPrefix("git", "merge-base") || r.sawPrefix("gh", "pr") {
		t.Fatalf("ReleaseFinish made ancestry or PR decisions after stale source parity: %#v", r.calls)
	}
}
