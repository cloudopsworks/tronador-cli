package versions

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

type checkoutRefreshRewriteRunner struct {
	calls         []call
	branch        string
	rewrittenBase string
	fetches       int
}

func (r *checkoutRefreshRewriteRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, call{name: name, args: append([]string(nil), args...)})
	if key(name, args...) == key("git", "fetch", "origin", "--prune") {
		r.fetches++
		return "", nil
	}
	switch key(name, args...) {
	case key("git", "ls-remote", "origin", "refs/heads/"+r.branch):
		return "source-sha\trefs/heads/" + r.branch + "\n", nil
	case key("git", "rev-parse", "--verify", "refs/heads/"+r.branch+"^{commit}"):
		return "source-sha\n", nil
	case key("git", "branch", "--show-current"):
		return r.branch + "\n", nil
	case key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"):
		return "refs/remotes/origin/main\n", nil
	}
	if len(args) == 4 && args[0] == "merge-base" && args[1] == "--is-ancestor" && args[2] == "refs/heads/"+r.branch {
		if r.fetches >= 2 && args[3] == "refs/remotes/origin/"+r.rewrittenBase {
			return "", purgeExitStatusOne()
		}
		return "", nil
	}
	return "", nil
}

func (r *checkoutRefreshRewriteRunner) sawPrefix(parts ...string) bool {
	for _, c := range r.calls {
		all := append([]string{c.name}, c.args...)
		if len(all) < len(parts) {
			continue
		}
		matched := true
		for i, part := range parts {
			if all[i] != part {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func (r *checkoutRefreshRewriteRunner) count(parts ...string) int {
	count := 0
	for _, c := range r.calls {
		all := append([]string{c.name}, c.args...)
		if len(all) != len(parts) {
			continue
		}
		matched := true
		for i, part := range parts {
			if all[i] != part {
				matched = false
				break
			}
		}
		if matched {
			count++
		}
	}
	return count
}

func purgeExitStatusOne() error {
	return exec.Command("sh", "-c", "exit 1").Run()
}

func TestPurgeRevalidatesTargetsAfterCheckoutRefresh(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, wow, branch, rewrittenBase string
		purge                            func(*Workflows) error
	}{
		{name: "feature", wow: "githubflow", branch: "feature/a", rewrittenBase: "main", purge: func(w *Workflows) error { return w.FeaturePurge(ctx, "a") }},
		{name: "hotfix", wow: "githubflow", branch: "hotfix/v1.2.3", rewrittenBase: "main", purge: func(w *Workflows) error { return w.HotfixPurge(ctx, "1.2.3") }},
		{name: "support", wow: "gitflow", branch: "support/v1.2.3", rewrittenBase: "main", purge: func(w *Workflows) error { return w.SupportPurge(ctx, "1.2.3") }},
		{name: "release", wow: "gitflow", branch: "release/v1.2.3", rewrittenBase: "develop", purge: func(w *Workflows) error { return w.ReleasePurge(ctx, "1.2.3") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &checkoutRefreshRewriteRunner{branch: tc.branch, rewrittenBase: tc.rewrittenBase}
			w, err := NewWorkflows(WorkflowOptions{WayOfWork: tc.wow, Runner: r})
			if err != nil {
				t.Fatal(err)
			}
			if err = tc.purge(w); err == nil || !strings.Contains(err.Error(), "not merged") {
				t.Fatalf("purge error = %v, want refreshed-target rejection", err)
			}
			if r.fetches < 2 {
				t.Fatalf("purge did not refresh while moving off source: %#v", r.calls)
			}
			if tc.name == "release" {
				mainProbe := []string{"git", "merge-base", "--is-ancestor", "refs/heads/" + tc.branch, "refs/remotes/origin/main"}
				developProbe := []string{"git", "merge-base", "--is-ancestor", "refs/heads/" + tc.branch, "refs/remotes/origin/develop"}
				if r.count(mainProbe...) != 2 || r.count(developProbe...) != 2 {
					t.Fatalf("release did not revalidate refreshed main before rejecting develop: %#v", r.calls)
				}
			}
			remoteDelete := []string{"git", "push", "--force-with-lease=refs/heads/" + tc.branch + ":source-sha", "origin", ":refs/heads/" + tc.branch}
			if r.sawPrefix(remoteDelete...) || r.sawPrefix("git", "branch", "-d") {
				t.Fatalf("purge deleted after target rewrite: %#v", r.calls)
			}
		})
	}
}
