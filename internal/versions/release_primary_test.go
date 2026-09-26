package versions

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestGitFlowReleaseStartRejectsMissingLiveConfiguredPrimaryBeforeMutation(t *testing.T) {
	f := &fakeRunner{}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "primary", Runner: f})
	if err != nil {
		t.Fatal(err)
	}

	err = w.ReleaseStart(context.Background(), "minor")
	if err == nil || !strings.Contains(err.Error(), "exactly one live branch") {
		t.Fatalf("ReleaseStart error = %v, want live configured primary rejection", err)
	}
	for _, mutation := range [][]string{
		{"git", "fetch", "origin", "--prune"},
		{"git", "checkout"},
		{"gitversion", "-showvariable", "MajorMinorPatch"},
	} {
		if f.sawPrefix(mutation...) {
			t.Fatalf("ReleaseStart continued after missing configured primary: %#v", f.calls)
		}
	}
}

func TestGitFlowReleaseStartPropagatesLiveConfiguredPrimaryProbeFailure(t *testing.T) {
	f := &fakeRunner{errs: map[string]error{
		key("git", "ls-remote", "origin", "refs/heads/primary"): fmt.Errorf("remote unavailable"),
	}}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "primary", Runner: f})
	if err != nil {
		t.Fatal(err)
	}

	err = w.ReleaseStart(context.Background(), "minor")
	if err == nil || !strings.Contains(err.Error(), "remote unavailable") {
		t.Fatalf("ReleaseStart error = %v, want live probe failure", err)
	}
	if f.sawPrefix("git", "fetch") || f.sawPrefix("git", "checkout") || f.sawPrefix("gitversion") {
		t.Fatalf("ReleaseStart continued after live probe failure: %#v", f.calls)
	}
}

func TestGitFlowReleaseStartAcceptsExactLiveConfiguredPrimaryAndUsesDevelop(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "ls-remote", "origin", "refs/heads/primary"): "abc\trefs/heads/primary\n",
		key("gitversion", "-showvariable", "MajorMinorPatch"):   "1.2.3\n",
	}}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "primary", Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.ReleaseStart(context.Background(), "minor"); err != nil {
		t.Fatal(err)
	}
	if !f.saw("git", "checkout", "-b", "release/v1.3.0", "refs/heads/develop") {
		t.Fatalf("ReleaseStart did not retain develop base: %#v", f.calls)
	}
}

func TestGitFlowReleaseStartRejectsStaleTrackingConfiguredPrimary(t *testing.T) {
	ctx := context.Background()
	_, repo := setupWorkflowRemote(t, true)
	gitTest(t, repo, "checkout", "-b", "primary", "main")
	gitTest(t, repo, "push", "-u", "origin", "primary")
	primarySHA := strings.TrimSpace(gitTest(t, repo, "rev-parse", "refs/heads/primary"))
	gitTest(t, repo, "checkout", "--no-guess", "develop")
	gitTest(t, repo, "push", "origin", ":refs/heads/primary")
	gitTest(t, repo, "update-ref", "refs/remotes/origin/primary", primarySHA)

	w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "gitflow", MainBranch: "primary"})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.ReleaseStart(ctx, "minor"); err == nil || !strings.Contains(err.Error(), "exactly one live branch") {
		t.Fatalf("ReleaseStart error = %v, want stale-primary rejection", err)
	}
	if current := gitTest(t, repo, "branch", "--show-current"); current != "develop\n" {
		t.Fatalf("ReleaseStart changed current branch to %q", current)
	}
	if local := gitTest(t, repo, "branch", "--list", "release/v1.3.0"); local != "" {
		t.Fatalf("ReleaseStart created local release branch: %q", local)
	}
	if remote := gitTest(t, repo, "ls-remote", "origin", "refs/heads/release/v1.3.0"); remote != "" {
		t.Fatalf("ReleaseStart published a release branch: %q", remote)
	}
}
