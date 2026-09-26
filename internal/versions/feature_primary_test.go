package versions

import (
	"context"
	"strings"
	"testing"
)

func TestGitFlowFeatureStartRejectsMissingConfiguredPrimaryBeforeSync(t *testing.T) {
	ctx := context.Background()
	f := &fakeRunner{}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "primary", Runner: f})
	if err != nil {
		t.Fatal(err)
	}

	err = w.FeatureStart(ctx, "blocked")
	if err == nil || !strings.Contains(err.Error(), `configured main branch "primary" is not available as exactly one live branch on origin`) {
		t.Fatalf("FeatureStart error = %v, want unavailable configured primary", err)
	}
	for _, mutation := range [][]string{
		{"git", "fetch", "origin", "--prune"},
		{"git", "checkout", "--no-guess", "develop"},
		{"git", "checkout", "-b", "feature/blocked", "refs/heads/develop"},
	} {
		if f.saw(mutation...) {
			t.Fatalf("FeatureStart mutated after invalid configured primary: %#v", f.calls)
		}
	}
}

func TestGitFlowFeaturePurgeRejectsMissingConfiguredPrimaryBeforeFetchOrDeletion(t *testing.T) {
	ctx := context.Background()
	f := &fakeRunner{}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "primary", Runner: f})
	if err != nil {
		t.Fatal(err)
	}

	err = w.FeaturePurge(ctx, "blocked")
	if err == nil || !strings.Contains(err.Error(), `configured main branch "primary" is not available as exactly one live branch on origin`) {
		t.Fatalf("FeaturePurge error = %v, want unavailable configured primary", err)
	}
	for _, mutation := range [][]string{
		{"git", "fetch", "origin", "--prune"},
		{"git", "checkout"},
		{"git", "push"},
		{"git", "branch", "-d", "feature/blocked"},
	} {
		if f.sawPrefix(mutation...) {
			t.Fatalf("FeaturePurge mutated after invalid configured primary: %#v", f.calls)
		}
	}
}

func TestGitFlowFeaturePublishAndFinishRejectMissingConfiguredPrimaryBeforeMutation(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		run  func(*Workflows) error
	}{
		{name: "publish", run: func(w *Workflows) error { return w.FeaturePublish(ctx, "blocked") }},
		{name: "finish", run: func(w *Workflows) error { return w.FeatureFinish(ctx, "blocked") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{}
			w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "primary", Runner: f})
			if err != nil {
				t.Fatal(err)
			}
			if err = tc.run(w); err == nil || !strings.Contains(err.Error(), `configured main branch "primary" is not available as exactly one live branch on origin`) {
				t.Fatalf("operation error = %v, want unavailable configured primary", err)
			}
			if f.sawPrefix("git", "checkout") || f.sawPrefix("git", "push") || f.sawPrefix("gh", "pr", "create") {
				t.Fatalf("operation mutated after invalid configured primary: %#v", f.calls)
			}
		})
	}
}

func TestGitFlowFeaturePrimaryOverrideRequiresOneExactLiveBranch(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, output string
	}{
		{name: "wrong-ref", output: "abc\trefs/heads/other\n"},
		{name: "ambiguous", output: "abc\trefs/heads/primary\ndef\trefs/heads/primary\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{replies: map[string]string{
				key("git", "ls-remote", "origin", "refs/heads/primary"): tc.output,
			}}
			w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "primary", Runner: f})
			if err != nil {
				t.Fatal(err)
			}
			if err = w.FeatureStart(ctx, "blocked"); err == nil || !strings.Contains(err.Error(), "exactly one live branch") {
				t.Fatalf("FeatureStart error = %v, want exact live branch rejection", err)
			}
			if f.sawPrefix("git", "fetch") || f.sawPrefix("git", "checkout") {
				t.Fatalf("FeatureStart mutated after malformed live result: %#v", f.calls)
			}
		})
	}
}

func TestGitFlowFeaturePrimaryOverridePropagatesLiveProbeFailure(t *testing.T) {
	probeErr := purgeExitStatusOne()
	f := &fakeRunner{errs: map[string]error{
		key("git", "ls-remote", "origin", "refs/heads/primary"): probeErr,
	}}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "primary", Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.FeaturePublish(context.Background(), "blocked"); err == nil || !strings.Contains(err.Error(), "verify configured main branch") {
		t.Fatalf("FeaturePublish error = %v, want live probe failure", err)
	}
	if f.sawPrefix("git", "checkout") || f.sawPrefix("git", "push") {
		t.Fatalf("FeaturePublish mutated after live probe failure: %#v", f.calls)
	}
}

func TestGitFlowFeaturePrimaryOverrideAcceptsExactLiveBranch(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "ls-remote", "origin", "refs/heads/primary"): "abc\trefs/heads/primary\n",
	}}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "primary", Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.FeatureStart(context.Background(), "allowed"); err != nil {
		t.Fatal(err)
	}
	if !f.saw("git", "checkout", "-b", "feature/allowed", "refs/heads/develop") {
		t.Fatalf("FeatureStart did not retain develop base: %#v", f.calls)
	}
}

func TestGitFlowFeatureOperationsRejectStaleTrackingPrimaryOverride(t *testing.T) {
	ctx := context.Background()
	_, repo := setupWorkflowRemote(t, true)
	gitTest(t, repo, "checkout", "-b", "primary", "main")
	gitTest(t, repo, "push", "-u", "origin", "primary")
	primarySHA := strings.TrimSpace(gitTest(t, repo, "rev-parse", "refs/heads/primary"))
	gitTest(t, repo, "checkout", "--no-guess", "develop")
	gitTest(t, repo, "push", "origin", ":refs/heads/primary")
	// Keep the clone's tracking ref to model a remote branch deleted after the
	// last fetch. The live ls-remote probe must reject it for every operation.
	gitTest(t, repo, "update-ref", "refs/remotes/origin/primary", primarySHA)

	for _, tc := range []struct {
		name string
		run  func(*Workflows) error
	}{
		{name: "start", run: func(w *Workflows) error { return w.FeatureStart(ctx, "stale") }},
		{name: "publish", run: func(w *Workflows) error { return w.FeaturePublish(ctx, "stale") }},
		{name: "finish", run: func(w *Workflows) error { return w.FeatureFinish(ctx, "stale") }},
		{name: "purge", run: func(w *Workflows) error { return w.FeaturePurge(ctx, "stale") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "gitflow", MainBranch: "primary"})
			if err != nil {
				t.Fatal(err)
			}
			if err = tc.run(w); err == nil || !strings.Contains(err.Error(), "exactly one live branch") {
				t.Fatalf("operation error = %v, want stale primary rejection", err)
			}
			if current := gitTest(t, repo, "branch", "--show-current"); current != "develop\n" {
				t.Fatalf("operation changed current branch to %q", current)
			}
			if local := gitTest(t, repo, "branch", "--list", "feature/stale"); local != "" {
				t.Fatalf("operation created feature branch: %q", local)
			}
			if remote := gitTest(t, repo, "ls-remote", "origin", "refs/heads/feature/stale"); remote != "" {
				t.Fatalf("operation published or retained feature branch: %q", remote)
			}
		})
	}
}
