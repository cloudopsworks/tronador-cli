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
	if len(args) == 4 && args[0] == "merge-base" && args[1] == "--is-ancestor" {
		if r.fetches >= 2 && args[2] == "source-sha" && args[3] == "refs/remotes/origin/"+r.rewrittenBase {
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
				mainProbe := []string{"git", "merge-base", "--is-ancestor", "source-sha", "refs/remotes/origin/main"}
				developProbe := []string{"git", "merge-base", "--is-ancestor", "source-sha", "refs/remotes/origin/develop"}
				if r.count(mainProbe...) != 1 || r.count(developProbe...) != 1 {
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

type purgeABARunner struct {
	calls             []call
	branch            string
	remoteProbes      int
	localProbes       int
	rejectFinalSource bool
}

func (r *purgeABARunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, call{name: name, args: append([]string(nil), args...)})
	if key(name, args...) == key("git", "ls-remote", "origin", "refs/heads/"+r.branch) {
		r.remoteProbes++
		sha := "a"
		if r.remoteProbes == 3 {
			sha = "b"
		}
		return sha + "\trefs/heads/" + r.branch + "\n", nil
	}
	if key(name, args...) == key("git", "rev-parse", "--verify", "refs/heads/"+r.branch+"^{commit}") {
		r.localProbes++
		if r.localProbes == 2 {
			return "b\n", nil
		}
		return "a\n", nil
	}
	if key(name, args...) == key("git", "branch", "--show-current") {
		return "main\n", nil
	}
	if key(name, args...) == key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD") {
		return "refs/remotes/origin/main\n", nil
	}
	if len(args) == 4 && args[0] == "merge-base" && args[1] == "--is-ancestor" {
		if args[2] == "refs/heads/"+r.branch || args[2] == "b" {
			if args[2] == "b" && r.rejectFinalSource {
				return "", purgeExitStatusOne()
			}
			return "", nil
		}
		return "", purgeExitStatusOne()
	}
	if len(args) == 4 && args[0] == "push" && args[2] == "origin" && args[3] == ":refs/heads/"+r.branch {
		if args[1] == "--force-with-lease=refs/heads/"+r.branch+":b" {
			// The final delete-side remote observation is A, so a lease for B
			// must be rejected by the server. A stale A lease would succeed.
			return "", exec.Command("sh", "-c", "exit 1").Run()
		}
		return "", nil
	}
	return "", nil
}

func (r *purgeABARunner) saw(parts ...string) bool {
	for _, c := range r.calls {
		if strings.Join(append([]string{c.name}, c.args...), " ") == strings.Join(parts, " ") {
			return true
		}
	}
	return false
}

func (r *purgeABARunner) count(parts ...string) int {
	count := 0
	for _, c := range r.calls {
		if strings.Join(append([]string{c.name}, c.args...), " ") == strings.Join(parts, " ") {
			count++
		}
	}
	return count
}

// TestPurgeUsesFinalParitySHAForLease covers the generic purge path used by
// all entrypoints. The remote changes A->B->A while the local source reaches
// B: deletion must lease B, so the final A rejects it and preserves local work.
func TestPurgeUsesFinalParitySHAForLease(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, wow, branch string
		purge             func(*Workflows) error
	}{
		{name: "feature", wow: "githubflow", branch: "feature/a", purge: func(w *Workflows) error { return w.FeaturePurge(ctx, "a") }},
		{name: "hotfix", wow: "githubflow", branch: "hotfix/v1.2.3", purge: func(w *Workflows) error { return w.HotfixPurge(ctx, "1.2.3") }},
		{name: "support", wow: "gitflow", branch: "support/v1.2.3", purge: func(w *Workflows) error { return w.SupportPurge(ctx, "1.2.3") }},
		{name: "release", wow: "gitflow", branch: "release/v1.2.3", purge: func(w *Workflows) error { return w.ReleasePurge(ctx, "1.2.3") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &purgeABARunner{branch: tc.branch}
			w, err := NewWorkflows(WorkflowOptions{WayOfWork: tc.wow, Runner: r})
			if err != nil {
				t.Fatal(err)
			}
			if err = tc.purge(w); err == nil {
				t.Fatal("purge succeeded after remote ABA change")
			}
			lease := "--force-with-lease=refs/heads/" + tc.branch + ":b"
			if !r.saw("git", "push", lease, "origin", ":refs/heads/"+tc.branch) {
				t.Fatalf("purge did not use final parity SHA %q: %#v", lease, r.calls)
			}
			if r.remoteProbes != 4 {
				t.Fatalf("remote observations = %d, want A/A/B/A sequence: %#v", r.remoteProbes, r.calls)
			}
			for _, base := range purgeTestBases(tc.name) {
				if r.count("git", "merge-base", "--is-ancestor", "b", "refs/remotes/origin/"+base) != 1 {
					t.Fatalf("purge did not verify immutable source b against %s: %#v", base, r.calls)
				}
			}
			if r.saw("git", "branch", "-d", tc.branch) {
				t.Fatalf("purge deleted local branch after leased remote rejection: %#v", r.calls)
			}
		})
	}
}

func TestPurgeRejectsUnmergedFinalSourceBeforeDelete(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, wow, branch string
		purge             func(*Workflows) error
	}{
		{name: "feature", wow: "githubflow", branch: "feature/a", purge: func(w *Workflows) error { return w.FeaturePurge(ctx, "a") }},
		{name: "hotfix", wow: "githubflow", branch: "hotfix/v1.2.3", purge: func(w *Workflows) error { return w.HotfixPurge(ctx, "1.2.3") }},
		{name: "support", wow: "gitflow", branch: "support/v1.2.3", purge: func(w *Workflows) error { return w.SupportPurge(ctx, "1.2.3") }},
		{name: "release", wow: "gitflow", branch: "release/v1.2.3", purge: func(w *Workflows) error { return w.ReleasePurge(ctx, "1.2.3") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &purgeABARunner{branch: tc.branch, rejectFinalSource: true}
			w, err := NewWorkflows(WorkflowOptions{WayOfWork: tc.wow, Runner: r})
			if err != nil {
				t.Fatal(err)
			}
			if err = tc.purge(w); err == nil || !strings.Contains(err.Error(), "not merged") {
				t.Fatalf("purge error = %v, want final-source merge rejection", err)
			}
			if r.count("git", "merge-base", "--is-ancestor", "b", "refs/remotes/origin/main") != 1 {
				t.Fatalf("purge did not reject immutable final source b: %#v", r.calls)
			}
			for _, c := range r.calls {
				if c.name == "git" && (c.args[0] == "push" || (c.args[0] == "branch" && len(c.args) > 1 && c.args[1] == "-d")) {
					t.Fatalf("purge mutated after final-source rejection: %#v", r.calls)
				}
			}
		})
	}
}

func purgeTestBases(entrypoint string) []string {
	if entrypoint == "release" {
		return []string{"main", "develop"}
	}
	return []string{"main"}
}
