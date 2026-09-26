package versions

import (
	"context"
	"strings"
	"testing"
)

func TestGitFlowFeatureStartRejectsMissingConfiguredPrimaryBeforeSync(t *testing.T) {
	ctx := context.Background()
	f := &fakeRunner{errs: map[string]error{
		key("git", "show-ref", "--verify", "--quiet", "refs/remotes/origin/primary"): purgeExitStatusOne(),
	}}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "primary", Runner: f})
	if err != nil {
		t.Fatal(err)
	}

	err = w.FeatureStart(ctx, "blocked")
	if err == nil || !strings.Contains(err.Error(), `configured main branch "primary" is not available on origin`) {
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
	f := &fakeRunner{errs: map[string]error{
		key("git", "show-ref", "--verify", "--quiet", "refs/remotes/origin/primary"): purgeExitStatusOne(),
	}}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "primary", Runner: f})
	if err != nil {
		t.Fatal(err)
	}

	err = w.FeaturePurge(ctx, "blocked")
	if err == nil || !strings.Contains(err.Error(), `configured main branch "primary" is not available on origin`) {
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
			f := &fakeRunner{errs: map[string]error{
				key("git", "show-ref", "--verify", "--quiet", "refs/remotes/origin/primary"): purgeExitStatusOne(),
			}}
			w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "primary", Runner: f})
			if err != nil {
				t.Fatal(err)
			}
			if err = tc.run(w); err == nil || !strings.Contains(err.Error(), `configured main branch "primary" is not available on origin`) {
				t.Fatalf("operation error = %v, want unavailable configured primary", err)
			}
			if f.sawPrefix("git", "checkout") || f.sawPrefix("git", "push") || f.sawPrefix("gh", "pr", "create") {
				t.Fatalf("operation mutated after invalid configured primary: %#v", f.calls)
			}
		})
	}
}
