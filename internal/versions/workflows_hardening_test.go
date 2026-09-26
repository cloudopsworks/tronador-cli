package versions

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseStartUsesWorkflowSpecificBase(t *testing.T) {
	for _, tc := range []struct {
		name string
		wow  string
		base string
	}{
		{name: "gitflow", wow: "gitflow", base: "develop"},
		{name: "githubflow", wow: "githubflow", base: "main"},
		{name: "trunk alias", wow: "trunk", base: "main"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{replies: map[string]string{
				key("gitversion", "-showvariable", "MajorMinorPatch"):                        "1.2.3\n",
				key("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"): "origin/main\n",
			}}
			w, err := NewWorkflows(WorkflowOptions{WayOfWork: tc.wow, Runner: f})
			if err != nil {
				t.Fatal(err)
			}
			if err := w.ReleaseStart(context.Background(), "minor"); err != nil {
				t.Fatal(err)
			}
			if !f.saw("git", "checkout", "-b", "release/v1.3.0", tc.base) {
				t.Fatalf("release was not created from %s: %#v", tc.base, f.calls)
			}
		})
	}
}

func TestTagExistingAnnotatedTagIsIdempotentlyPublished(t *testing.T) {
	const tag = "v1.2.3-alpha.1+deploy-test"
	f := &fakeRunner{replies: map[string]string{
		key("git", "branch", "--show-current"):                                       "feature/a\n",
		key("git", "rev-parse", "--verify", "feature/a^{commit}"):                    "abc\n",
		key("git", "rev-parse", "--verify", "HEAD^{commit}"):                         "abc\n",
		key("git", "ls-remote", "origin", "refs/heads/feature/a"):                    "abc\trefs/heads/feature/a\n",
		key("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"): "origin/main\n",
		key("gitversion", "-showvariable", "SemVer"):                                 "1.2.3-alpha.1\n",
		key("git", "rev-parse", "--verify", tag+"^{commit}"):                         "abc\n",
	}}
	w, err := NewWorkflows(WorkflowOptions{Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.Tag(context.Background(), "test", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != tag {
		t.Fatalf("tag = %q, want %q", got, tag)
	}
	if f.saw("git", "tag", "-a", tag, "-m", "chore: Version Tagging: "+tag) {
		t.Fatalf("existing tag was recreated: %#v", f.calls)
	}
	if !f.saw("git", "push", "origin", tag) {
		t.Fatalf("existing tag was not published: %#v", f.calls)
	}
}

func TestRequireAnnotatedFinishTagAcceptsAnnotatedTag(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "rev-parse", "--verify", "v1.2.3^{commit}"): "abc\n",
		key("git", "rev-parse", "--verify", "v1.2.3^{tag}"):    "tag-object\n",
	}}
	w, _ := NewWorkflows(WorkflowOptions{Runner: f})
	if err := w.requireAnnotatedFinishTag(context.Background(), "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if !f.saw("git", "rev-parse", "--verify", "v1.2.3^{tag}") {
		t.Fatalf("annotation probe was not consumed: %#v", f.calls)
	}
}

func TestRequireAnnotatedFinishTagAcceptsProvenAbsentTag(t *testing.T) {
	absent := exec.Command("sh", "-c", "exit 1").Run()
	if absent == nil {
		t.Fatal("expected exit status 1")
	}
	f := &fakeRunner{errs: map[string]error{
		key("git", "show-ref", "--verify", "--quiet", "refs/tags/v1.2.3"): absent,
	}}
	w, _ := NewWorkflows(WorkflowOptions{Runner: f})
	if err := w.requireAnnotatedFinishTag(context.Background(), "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if f.saw("git", "rev-parse", "--verify", "v1.2.3^{commit}") {
		t.Fatalf("absent tag incorrectly resolved: %#v", f.calls)
	}
}

func TestLocalFinishRejectsFinishTagProbeOperationalErrorBeforeMutation(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{key("git", "rev-parse", "--git-path", "tronador/versions-journal.json"): filepath.Join(t.TempDir(), "journal")}, errs: map[string]error{
		key("git", "show-ref", "--verify", "--quiet", "refs/tags/v1.2.3"): fmt.Errorf("repository unavailable"),
	}}
	w, _ := NewWorkflows(WorkflowOptions{MainBranch: "main", Runner: f})
	err := w.HotfixFinish(context.Background(), "1.2.3", true)
	if err == nil || !strings.Contains(err.Error(), "verify existing finish tag") || !strings.Contains(err.Error(), "repository unavailable") {
		t.Fatalf("operational probe error = %v", err)
	}
	for _, c := range f.calls {
		if c.name == "git" && len(c.args) > 0 && (c.args[0] == "checkout" || c.args[0] == "merge" || c.args[0] == "push") {
			t.Fatalf("local finish mutated after operational tag probe failure: %#v", f.calls)
		}
	}
}

func TestFinishTagBoundaryDerivesAndRejectsMalformedPlans(t *testing.T) {
	if got, err := finishTagBoundary([]string{"checkout", "merge", "tag", "push"}); err != nil || got != 2 {
		t.Fatalf("tag boundary = %d, %v", got, err)
	}
	for _, steps := range [][]string{{"checkout", "merge"}, {"tag", "merge", "tag"}} {
		if _, err := finishTagBoundary(steps); err == nil {
			t.Fatalf("malformed plan accepted: %#v", steps)
		}
	}
}

func TestLocalFinishRejectsPreexistingLightweightTagBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		finish func(*Workflows) error
	}{
		{name: "hotfix", finish: func(w *Workflows) error { return w.HotfixFinish(context.Background(), "1.2.3", true) }},
		{name: "release", finish: func(w *Workflows) error { return w.ReleaseFinish(context.Background(), "1.2.3", true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{replies: map[string]string{
				key("git", "rev-parse", "--verify", "v1.2.3^{commit}"):                  "abc\n",
				key("git", "rev-parse", "--git-path", "tronador/versions-journal.json"): filepath.Join(t.TempDir(), "journal"),
			}, errs: map[string]error{
				key("git", "rev-parse", "--verify", "v1.2.3^{tag}"): fmt.Errorf("lightweight tag"),
			}}
			w, _ := NewWorkflows(WorkflowOptions{MainBranch: "main", Runner: f})
			err := tc.finish(w)
			if err == nil || !strings.Contains(err.Error(), "lightweight tag") {
				t.Fatalf("local finish error = %v", err)
			}
			for _, c := range f.calls {
				if c.name == "git" && (len(c.args) > 0 && (c.args[0] == "checkout" || c.args[0] == "merge" || c.args[0] == "push")) {
					t.Fatalf("local finish mutated after lightweight-tag preflight: %#v", f.calls)
				}
			}
		})
	}
}

func TestLocalFinishRejectsRemoteOnlyAnnotatedTagBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name, branch string
		finish       func(*Workflows) error
	}{
		{name: "hotfix", branch: "hotfix/v1.2.3", finish: func(w *Workflows) error { return w.HotfixFinish(context.Background(), "1.2.3", true) }},
		{name: "release", branch: "release/v1.2.3", finish: func(w *Workflows) error { return w.ReleaseFinish(context.Background(), "1.2.3", true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, repo := setupWorkflowRemote(t, false)
			gitTest(t, repo, "checkout", "-b", tc.branch)
			writeFile(t, filepath.Join(repo, "source.txt"), tc.name+"\n")
			gitTest(t, repo, "add", "source.txt")
			gitTest(t, repo, "commit", "-m", tc.name)
			gitTest(t, repo, "push", "-u", "origin", tc.branch)

			tagger := filepath.Join(root, "tagger-"+tc.name)
			gitTest(t, root, "clone", filepath.Join(root, "remote.git"), tagger)
			gitTest(t, tagger, "config", "user.email", "test@example.test")
			gitTest(t, tagger, "config", "user.name", "Test")
			gitTest(t, tagger, "checkout", "--track", "origin/"+tc.branch)
			gitTest(t, tagger, "tag", "-a", "v1.2.3", "-m", "remote incompatible tag")
			gitTest(t, tagger, "push", "origin", "v1.2.3")
			if got := gitTest(t, repo, "tag", "-l", "v1.2.3"); got != "" {
				t.Fatalf("working clone unexpectedly already has remote tag: %q", got)
			}

			w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "githubflow", MainBranch: "main"})
			if err != nil {
				t.Fatal(err)
			}
			if err = tc.finish(w); err == nil || !strings.Contains(err.Error(), "existing annotated tag") {
				t.Fatalf("remote tag preflight error = %v", err)
			}
			if got := gitTest(t, repo, "branch", "--show-current"); got != tc.branch+"\n" {
				t.Fatalf("finish changed branch after remote tag rejection: %q", got)
			}
			if _, statErr := os.Stat(filepath.Join(repo, ".git", "tronador", "versions-journal.json")); !os.IsNotExist(statErr) {
				t.Fatalf("remote tag rejection wrote journal: %v", statErr)
			}
			if got := gitTest(t, repo, "log", "--format=%s", "main", "--", "source.txt"); strings.Contains(got, tc.name) {
				t.Fatalf("finish merged source despite remote tag rejection: %q", got)
			}
		})
	}
}

func TestFeatureFinishUsesWorkflowSpecificPullRequestBase(t *testing.T) {
	for _, tc := range []struct {
		name string
		wow  string
		base string
	}{
		{name: "gitflow", wow: "gitflow", base: "develop"},
		{name: "githubflow", wow: "githubflow", base: "main"},
		{name: "trunkbased", wow: "trunkbased", base: "main"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{replies: map[string]string{
				key("git", "branch", "--show-current"):                                       "feature/a\n",
				key("git", "rev-parse", "--verify", "feature/a^{commit}"):                    "abc\n",
				key("git", "ls-remote", "origin", "refs/heads/feature/a"):                    "abc\trefs/heads/feature/a\n",
				key("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"): "origin/main\n",
			}}
			w, err := NewWorkflows(WorkflowOptions{WayOfWork: tc.wow, Runner: f})
			if err != nil {
				t.Fatal(err)
			}
			if err = w.FeatureFinish(context.Background(), ""); err != nil {
				t.Fatal(err)
			}
			if !f.saw("gh", "pr", "create", "--head", "feature/a", "-B", tc.base, "-b", `Feature "feature/a" finish, will merge into "`+tc.base+`".`, "-t", "chore: Feature Finish from feature/a") {
				t.Fatalf("PR base = %s was not used: %#v", tc.base, f.calls)
			}
		})
	}
}

func TestFeaturePurgeGitHubAndTrunkCheckOutMainBeforeDeletion(t *testing.T) {
	for _, wow := range []string{"githubflow", "trunkbased"} {
		t.Run(wow, func(t *testing.T) {
			ctx := context.Background()
			_, repo := setupWorkflowRemote(t, false)
			gitTest(t, repo, "checkout", "-b", "feature/remove-me")
			gitTest(t, repo, "push", "-u", "origin", "feature/remove-me")

			w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: wow, MainBranch: "main"})
			if err != nil {
				t.Fatal(err)
			}
			if err = w.FeaturePurge(ctx, ""); err != nil {
				t.Fatal(err)
			}
			if got := gitTest(t, repo, "branch", "--show-current"); got != "main\n" {
				t.Fatalf("current branch = %q, want main", got)
			}
			assertBranchAbsent(t, repo, "feature/remove-me")
		})
	}
}

func TestHotfixLocalFinishResumesAfterConflictAndRejectsOtherWorkflowJournal(t *testing.T) {
	t.Setenv("GIT_EDITOR", "true")
	ctx := context.Background()
	_, repo := setupWorkflowRemote(t, false)
	writeFile(t, filepath.Join(repo, "conflict.txt"), "base\n")
	gitTest(t, repo, "add", "conflict.txt")
	gitTest(t, repo, "commit", "-m", "add conflict fixture")
	gitTest(t, repo, "push", "origin", "main")

	gitTest(t, repo, "checkout", "-b", "hotfix/v0.1.1")
	writeFile(t, filepath.Join(repo, "conflict.txt"), "hotfix\n")
	gitTest(t, repo, "add", "conflict.txt")
	gitTest(t, repo, "commit", "-m", "hotfix")
	gitTest(t, repo, "push", "-u", "origin", "hotfix/v0.1.1")

	gitTest(t, repo, "checkout", "main")
	writeFile(t, filepath.Join(repo, "conflict.txt"), "main\n")
	gitTest(t, repo, "add", "conflict.txt")
	gitTest(t, repo, "commit", "-m", "main change")
	gitTest(t, repo, "push", "origin", "main")
	gitTest(t, repo, "checkout", "hotfix/v0.1.1")

	w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "githubflow", MainBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.HotfixFinish(ctx, "", true); err == nil || !strings.Contains(err.Error(), "merge") {
		t.Fatalf("first local finish error = %v, want merge conflict", err)
	}
	if err = w.ReleaseFinish(ctx, "0.2.0", true); err == nil || !strings.Contains(err.Error(), "unfinished hotfix-finish workflow") {
		t.Fatalf("different workflow ignored in-progress journal: %v", err)
	}

	writeFile(t, filepath.Join(repo, "conflict.txt"), "resolved\n")
	gitTest(t, repo, "add", "conflict.txt")
	if err = w.HotfixFinish(ctx, "", true); err != nil {
		t.Fatalf("resumed local finish: %v", err)
	}
	if got := gitTest(t, repo, "show", "main:conflict.txt"); got != "resolved\n" {
		t.Fatalf("main merge resolution = %q", got)
	}
	if _, err = os.Stat(filepath.Join(repo, ".git", "tronador", "versions-journal.json")); !os.IsNotExist(err) {
		t.Fatalf("journal remains after successful finish: %v", err)
	}
	assertBranchAbsent(t, repo, "hotfix/v0.1.1")
	if got := gitTest(t, repo, "ls-remote", "origin", "refs/tags/v0.1.1"); !strings.Contains(got, "refs/tags/v0.1.1") {
		t.Fatalf("tag was not pushed: %q", got)
	}
}

func TestReleaseLocalFinishResumesAfterConflict(t *testing.T) {
	t.Setenv("GIT_EDITOR", "true")
	ctx := context.Background()
	_, repo := setupWorkflowRemote(t, true)
	writeFile(t, filepath.Join(repo, "conflict.txt"), "base\n")
	gitTest(t, repo, "add", "conflict.txt")
	gitTest(t, repo, "commit", "-m", "add conflict fixture")
	gitTest(t, repo, "push", "origin", "main")
	gitTest(t, repo, "checkout", "develop")
	gitTest(t, repo, "merge", "--ff-only", "main")
	gitTest(t, repo, "push", "origin", "develop")

	gitTest(t, repo, "checkout", "-b", "release/v0.2.0")
	writeFile(t, filepath.Join(repo, "conflict.txt"), "release\n")
	gitTest(t, repo, "add", "conflict.txt")
	gitTest(t, repo, "commit", "-m", "release")
	gitTest(t, repo, "push", "-u", "origin", "release/v0.2.0")

	gitTest(t, repo, "checkout", "main")
	writeFile(t, filepath.Join(repo, "conflict.txt"), "main\n")
	gitTest(t, repo, "add", "conflict.txt")
	gitTest(t, repo, "commit", "-m", "main change")
	gitTest(t, repo, "push", "origin", "main")
	gitTest(t, repo, "checkout", "release/v0.2.0")

	w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "gitflow", MainBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.ReleaseFinish(ctx, "", true); err == nil || !strings.Contains(err.Error(), "merge") {
		t.Fatalf("first local finish error = %v, want merge conflict", err)
	}
	writeFile(t, filepath.Join(repo, "conflict.txt"), "resolved\n")
	gitTest(t, repo, "add", "conflict.txt")
	if err = w.ReleaseFinish(ctx, "", true); err != nil {
		t.Fatalf("resumed local release finish: %v", err)
	}
	if got := gitTest(t, repo, "branch", "--show-current"); got != "develop\n" {
		t.Fatalf("final branch = %q, want develop", got)
	}
	if got := gitTest(t, repo, "show", "main:conflict.txt"); got != "resolved\n" {
		t.Fatalf("main merge resolution = %q", got)
	}
	if _, err = os.Stat(filepath.Join(repo, ".git", "tronador", "versions-journal.json")); !os.IsNotExist(err) {
		t.Fatalf("journal remains after successful release finish: %v", err)
	}
	assertBranchAbsent(t, repo, "release/v0.2.0")
}

func setupWorkflowRemote(t *testing.T, develop bool) (string, string) {
	t.Helper()
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
	if develop {
		gitTest(t, repo, "checkout", "-b", "develop")
		gitTest(t, repo, "push", "-u", "origin", "develop")
		gitTest(t, repo, "checkout", "main")
	}
	return root, repo
}

func assertBranchAbsent(t *testing.T, repo, branch string) {
	t.Helper()
	c := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	c.Dir = repo
	if err := c.Run(); err == nil {
		t.Fatalf("local branch %s remains", branch)
	}
	if got := gitTest(t, repo, "ls-remote", "origin", "refs/heads/"+branch); got != "" {
		t.Fatalf("remote branch %s remains: %s", branch, got)
	}
}

func TestTagRejectsExistingTagOnWrongCommit(t *testing.T) {
	const tag = "v1.2.3-alpha.1+deploy-test"
	f := &fakeRunner{replies: map[string]string{
		key("git", "branch", "--show-current"):                                       "feature/a\n",
		key("git", "rev-parse", "--verify", "feature/a^{commit}"):                    "source\n",
		key("git", "ls-remote", "origin", "refs/heads/feature/a"):                    "source\trefs/heads/feature/a\n",
		key("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"): "origin/main\n",
		key("gitversion", "-showvariable", "SemVer"):                                 "1.2.3-alpha.1\n",
		key("git", "rev-parse", "--verify", "HEAD^{commit}"):                         "expected\n",
		key("git", "rev-parse", "--verify", tag+"^{commit}"):                         "wrong\n",
	}}
	w, _ := NewWorkflows(WorkflowOptions{Runner: f})
	if _, err := w.Tag(context.Background(), "test", false); err == nil || !strings.Contains(err.Error(), "not expected commit") {
		t.Fatalf("Tag error = %v, want target mismatch", err)
	}
	if f.saw("git", "push", "origin", tag) {
		t.Fatal("wrong tag was pushed")
	}
}

func TestReleaseFinishGitflowCreatesPRsForMainAndDevelop(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "rev-parse", "--verify", "release/v1.2.3^{commit}"):                                                                    "sha\n",
		key("git", "ls-remote", "origin", "refs/heads/release/v1.2.3"):                                                                    "sha\trefs/heads/release/v1.2.3\n",
		key("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"):                                                      "origin/main\n",
		key("gh", "pr", "list", "--head", "release/v1.2.3", "--base", "main", "--state", "open", "--json", "number", "--jq", "length"):    "0\n",
		key("gh", "pr", "list", "--head", "release/v1.2.3", "--base", "develop", "--state", "open", "--json", "number", "--jq", "length"): "0\n",
	}}
	w, _ := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", Runner: f})
	if err := w.ReleaseFinish(context.Background(), "1.2.3", false); err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{"main", "develop"} {
		if !f.saw("gh", "pr", "create", "--head", "release/v1.2.3", "-B", base, "-b", "Release v1.2.3", "-t", "chore: Release v1.2.3 from release/v1.2.3") {
			t.Fatalf("missing %s PR: %#v", base, f.calls)
		}
	}
}

func TestPurgeRejectsUnmergedBranchBeforeCheckoutOrDelete(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "ls-remote", "origin", "refs/heads/feature/a"):                    "sha\trefs/heads/feature/a\n",
		key("git", "rev-parse", "--verify", "feature/a^{commit}"):                    "sha\n",
		key("git", "branch", "--show-current"):                                       "main\n",
		key("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"): "origin/main\n",
	}, errs: map[string]error{key("git", "merge-base", "--is-ancestor", "feature/a", "refs/remotes/origin/main"): fmt.Errorf("not merged")}}
	w, _ := NewWorkflows(WorkflowOptions{WayOfWork: "githubflow", Runner: f})
	if err := w.FeaturePurge(context.Background(), "a"); err == nil || !strings.Contains(err.Error(), "not merged") {
		t.Fatalf("purge error = %v", err)
	}
	if f.saw("git", "checkout", "main") || f.saw("git", "branch", "-d", "feature/a") {
		t.Fatalf("mutated despite failed guard: %#v", f.calls)
	}
}

func TestFeatureStartSynchronizesBaseBeforeCreatingBranch(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"): "origin/main\n",
	}}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "githubflow", Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.FeatureStart(context.Background(), "synchronized"); err != nil {
		t.Fatal(err)
	}
	fetch, pull, create := -1, -1, -1
	for i, c := range f.calls {
		got := strings.Join(append([]string{c.name}, c.args...), " ")
		switch got {
		case "git fetch origin --prune":
			fetch = i
		case "git pull --ff-only origin main":
			pull = i
		case "git checkout -b feature/synchronized main":
			create = i
		}
	}
	if fetch < 0 || pull < 0 || create < 0 || !(fetch < pull && pull < create) {
		t.Fatalf("base was not synchronized before branch creation: %#v", f.calls)
	}
}

func TestNewWorkflowsRejectsUnsafeMainOverride(t *testing.T) {
	if _, err := NewWorkflows(WorkflowOptions{MainBranch: "main; rm -rf /"}); err == nil {
		t.Fatal("unsafe main override was accepted")
	}
}

func TestMainOverrideMustExistOnConfiguredRemote(t *testing.T) {
	f := &fakeRunner{errs: map[string]error{
		key("git", "show-ref", "--verify", "--quiet", "refs/remotes/origin/release-main"): fmt.Errorf("missing"),
	}}
	w, err := NewWorkflows(WorkflowOptions{MainBranch: "release-main", Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Main(context.Background()); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("main override result = %v", err)
	}
}

func TestJournalLockExcludesConcurrentFinishAndIsReleased(t *testing.T) {
	ctx := context.Background()
	_, repo := setupWorkflowRemote(t, false)
	w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "githubflow", MainBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := w.acquireJournalLock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.acquireJournalLock(ctx); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("concurrent lock error = %v", err)
	}
	unlock()
	unlock2, err := w.acquireJournalLock(ctx)
	if err != nil {
		t.Fatalf("lock did not release: %v", err)
	}
	unlock2()
}

func TestJournalRejectsWrongRepositoryIdentity(t *testing.T) {
	ctx := context.Background()
	_, repo := setupWorkflowRemote(t, false)
	w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "githubflow", MainBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := w.journalPath(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := w.journalWorktree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeAtomic(p, &journal{
		Version: journalSchemaVersion, WayOfWork: "githubflow", Repository: "/another/repository", Worktree: worktree,
		Operation: "hotfix-finish", Source: "hotfix/v0.1.1", SourceSHA: "source", Target: "main", Steps: []string{"checkout-target"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = w.startJournal(ctx, "hotfix-finish", "hotfix/v0.1.1", "main", "source", []string{"checkout-target"}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("journal identity error = %v", err)
	}
}

func TestRunnerDryRunLocalFinishCreatesNoJournal(t *testing.T) {
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
	if err = w.HotfixFinish(ctx, "0.1.1", true); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(repo, ".git", "tronador", "versions-journal.json")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created a journal: %v", err)
	}
}

func TestLocalFinishDoesNotCreateJournalWhenSourceIsNotPublished(t *testing.T) {
	ctx := context.Background()
	_, repo := setupWorkflowRemote(t, false)
	gitTest(t, repo, "checkout", "-b", "hotfix/v0.1.1")
	w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "githubflow", MainBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.HotfixFinish(ctx, "0.1.1", true); err == nil || !strings.Contains(err.Error(), "exactly published") {
		t.Fatalf("local finish parity result = %v", err)
	}
	if _, err = os.Stat(filepath.Join(repo, ".git", "tronador", "versions-journal.json")); !os.IsNotExist(err) {
		t.Fatalf("unpublished source created journal: %v", err)
	}
}

func TestPurgeRefusesUnmergedLocalOnlyBranch(t *testing.T) {
	ctx := context.Background()
	_, repo := setupWorkflowRemote(t, false)
	gitTest(t, repo, "checkout", "-b", "feature/local-only")
	writeFile(t, filepath.Join(repo, "local-only.txt"), "unpublished\n")
	gitTest(t, repo, "add", "local-only.txt")
	gitTest(t, repo, "commit", "-m", "unpublished feature")
	w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "githubflow", MainBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.FeaturePurge(ctx, "local-only"); err == nil || !strings.Contains(err.Error(), "not merged") {
		t.Fatalf("local-only purge result = %v", err)
	}
	if got := gitTest(t, repo, "branch", "--show-current"); got != "feature/local-only\n" {
		t.Fatalf("purge changed branch despite rejected source: %q", got)
	}
	check := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/feature/local-only")
	check.Dir = repo
	if err = check.Run(); err != nil {
		t.Fatalf("purge deleted unpublished local branch: %v", err)
	}
}

func TestPurgeDeletesMergedLocalOnlyBranchWithSafeDelete(t *testing.T) {
	ctx := context.Background()
	_, repo := setupWorkflowRemote(t, false)
	gitTest(t, repo, "checkout", "-b", "feature/merged-local")
	gitTest(t, repo, "checkout", "main")
	// The local-only branch points at main, so it is proven merged without
	// publishing any unpublished work. Purge must use git branch -d, not -D.
	w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "githubflow", MainBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.FeaturePurge(ctx, "merged-local"); err != nil {
		t.Fatal(err)
	}
	assertBranchAbsent(t, repo, "feature/merged-local")
}

func TestCheckoutBaseRejectsLocalAheadOfFetchedOriginBeforeCreate(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"): "origin/main\n",
		key("git", "rev-parse", "--verify", "main^{commit}"):                         "local-ahead\n",
		key("git", "rev-parse", "--verify", "refs/remotes/origin/main^{commit}"):     "origin-head\n",
	}}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "githubflow", Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.FeatureStart(context.Background(), "must-not-create"); err == nil || !strings.Contains(err.Error(), "not exactly synchronized") {
		t.Fatalf("FeatureStart error = %v, want local-ahead rejection", err)
	}
	if f.saw("git", "checkout", "-b", "feature/must-not-create", "main") {
		t.Fatalf("feature was created despite local-ahead base: %#v", f.calls)
	}
	fetch, checkout, pull, local, remote := -1, -1, -1, -1, -1
	for i, c := range f.calls {
		got := strings.Join(append([]string{c.name}, c.args...), " ")
		switch got {
		case "git fetch origin --prune":
			fetch = i
		case "git checkout main":
			checkout = i
		case "git pull --ff-only origin main":
			pull = i
		case "git rev-parse --verify main^{commit}":
			local = i
		case "git rev-parse --verify refs/remotes/origin/main^{commit}":
			remote = i
		}
	}
	if !(fetch >= 0 && checkout > fetch && pull > checkout && local > pull && remote > local) {
		t.Fatalf("unexpected base synchronization order: %#v", f.calls)
	}
}

func TestHotfixStartSynchronizesSelectedSupportBeforeDerivingVersion(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "branch", "--show-current"):                                             "support/v1.2.0\n",
		key("git", "rev-parse", "--verify", "support/v1.2.0^{commit}"):                     "support-head\n",
		key("git", "rev-parse", "--verify", "refs/remotes/origin/support/v1.2.0^{commit}"): "support-head\n",
		key("gitversion", "-showvariable", "MajorMinorPatch"):                              "1.2.3\n",
	}}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "main", Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.HotfixStart(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if !f.saw("git", "checkout", "-b", "hotfix/v1.2.4", "support/v1.2.0") {
		t.Fatalf("hotfix was not created from synchronized support line: %#v", f.calls)
	}
	fetch, checkout, pull, remoteParity, gitversion := -1, -1, -1, -1, -1
	for i, c := range f.calls {
		got := strings.Join(append([]string{c.name}, c.args...), " ")
		switch got {
		case "git fetch origin --prune":
			fetch = i
		case "git checkout support/v1.2.0":
			checkout = i
		case "git pull --ff-only origin support/v1.2.0":
			pull = i
		case "git rev-parse --verify refs/remotes/origin/support/v1.2.0^{commit}":
			remoteParity = i
		case "gitversion -showvariable MajorMinorPatch":
			gitversion = i
		}
	}
	if !(fetch >= 0 && checkout > fetch && pull > checkout && remoteParity > pull && gitversion > remoteParity) {
		t.Fatalf("hotfix version was derived before its base was synchronized: %#v", f.calls)
	}
	if countCalls(f, "git fetch origin --prune") != 1 {
		t.Fatalf("hotfix start fetched more than once: %#v", f.calls)
	}
}

func TestHotfixTargetMatchesExactSupportMajorMinor(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "for-each-ref", "--format=%(refname:short)", "refs/heads/support/"): "support/v1.20.0\nsupport/v1.2.9\n",
	}}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "main", Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.hotfixTarget(context.Background(), "v1.2.4")
	if err != nil {
		t.Fatal(err)
	}
	if got != "support/v1.2.9" {
		t.Fatalf("target = %q, want support/v1.2.9", got)
	}
}

func TestHotfixTargetRejectsAmbiguousSupportLines(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "for-each-ref", "--format=%(refname:short)", "refs/heads/support/"): "support/v1.2.0\nsupport/v1.2.9\n",
	}}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "main", Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.hotfixTarget(context.Background(), "v1.2.4"); err == nil || !strings.Contains(err.Error(), "ambiguous support branches") {
		t.Fatalf("hotfixTarget error = %v, want ambiguity rejection", err)
	}
}

func TestHotfixStartUsesRemoteOnlySupportTrackingBase(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/support/"): "origin/support/v1.2.0\n",
		key("git", "rev-parse", "--verify", "refs/remotes/origin/support/v1.2.0^{commit}"):      "support\n",
		key("git", "rev-parse", "--verify", "support/v1.2.0^{commit}"):                          "support\n",
	}}
	f.errs = map[string]error{key("git", "show-ref", "--verify", "--quiet", "refs/heads/support/v1.2.0"): fmt.Errorf("missing local support")}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "main", Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.HotfixStart(context.Background(), "1.2.4"); err != nil {
		t.Fatal(err)
	}
	if !f.saw("git", "checkout", "--track", "-b", "support/v1.2.0", "origin/support/v1.2.0") || !f.saw("git", "checkout", "-b", "hotfix/v1.2.4", "support/v1.2.0") {
		t.Fatalf("remote support base was not tracked and used: %#v", f.calls)
	}
}

func TestHotfixLocalFinishUsesRemoteOnlySupportLine(t *testing.T) {
	ctx := context.Background()
	root, seed := setupWorkflowRemote(t, true)
	gitTest(t, seed, "checkout", "-b", "support/v1.2.0", "main")
	gitTest(t, seed, "push", "-u", "origin", "support/v1.2.0")
	gitTest(t, seed, "checkout", "-b", "hotfix/v1.2.4")
	writeFile(t, filepath.Join(seed, "hotfix.txt"), "maintenance\n")
	gitTest(t, seed, "add", "hotfix.txt")
	gitTest(t, seed, "commit", "-m", "hotfix")
	gitTest(t, seed, "push", "-u", "origin", "hotfix/v1.2.4")

	fresh := filepath.Join(root, "fresh")
	gitTest(t, root, "clone", filepath.Join(root, "remote.git"), fresh)
	gitTest(t, fresh, "config", "user.email", "test@example.test")
	gitTest(t, fresh, "config", "user.name", "Test")
	gitTest(t, fresh, "checkout", "--track", "origin/hotfix/v1.2.4")
	w, err := NewWorkflows(WorkflowOptions{Dir: fresh, WayOfWork: "gitflow", MainBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.HotfixFinish(ctx, "1.2.4", true); err != nil {
		t.Fatal(err)
	}
	if got := gitTest(t, fresh, "show", "support/v1.2.0:hotfix.txt"); got != "maintenance\n" {
		t.Fatalf("remote-only support did not receive hotfix: %q", got)
	}
}

func TestReleaseFinishRetrySkipsExistingMainPRAndCreatesDevelopPR(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "rev-parse", "--verify", "release/v1.2.3^{commit}"):                                                                    "sha\n",
		key("git", "ls-remote", "origin", "refs/heads/release/v1.2.3"):                                                                    "sha\trefs/heads/release/v1.2.3\n",
		key("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"):                                                      "origin/main\n",
		key("gh", "pr", "list", "--head", "release/v1.2.3", "--base", "main", "--state", "open", "--json", "number", "--jq", "length"):    "1\n",
		key("gh", "pr", "list", "--head", "release/v1.2.3", "--base", "develop", "--state", "open", "--json", "number", "--jq", "length"): "0\n",
	}}
	w, _ := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", Runner: f})
	if err := w.ReleaseFinish(context.Background(), "1.2.3", false); err != nil {
		t.Fatal(err)
	}
	if f.saw("gh", "pr", "create", "--head", "release/v1.2.3", "-B", "main", "-b", "Release v1.2.3", "-t", "chore: Release v1.2.3 from release/v1.2.3") || !f.saw("gh", "pr", "create", "--head", "release/v1.2.3", "-B", "develop", "-b", "Release v1.2.3", "-t", "chore: Release v1.2.3 from release/v1.2.3") {
		t.Fatalf("retry did not create only missing PR: %#v", f.calls)
	}
}

func TestPurgeRemoteDeleteUsesLeaseBeforeDeletingLocal(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "ls-remote", "origin", "refs/heads/feature/a"):                    "sha\trefs/heads/feature/a\n",
		key("git", "rev-parse", "--verify", "feature/a^{commit}"):                    "sha\n",
		key("git", "branch", "--show-current"):                                       "main\n",
		key("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"): "origin/main\n",
	}}
	w, _ := NewWorkflows(WorkflowOptions{WayOfWork: "githubflow", Runner: f})
	if err := w.FeaturePurge(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	lease := "--force-with-lease=refs/heads/feature/a:sha"
	if !f.saw("git", "push", lease, "origin", ":refs/heads/feature/a") {
		t.Fatalf("remote deletion did not use lease: %#v", f.calls)
	}
	push, deleteLocal := -1, -1
	for i, c := range f.calls {
		if strings.Join(append([]string{c.name}, c.args...), " ") == "git push "+lease+" origin :refs/heads/feature/a" {
			push = i
		}
		if strings.Join(append([]string{c.name}, c.args...), " ") == "git branch -d feature/a" {
			deleteLocal = i
		}
	}
	if push < 0 || deleteLocal < 0 || push > deleteLocal {
		t.Fatalf("local branch deleted before leased remote delete: %#v", f.calls)
	}
}

func TestPurgeRetainsLocalBranchWhenLeaseRejectsConcurrentAdvance(t *testing.T) {
	lease := "--force-with-lease=refs/heads/feature/a:sha"
	f := &fakeRunner{replies: map[string]string{
		key("git", "ls-remote", "origin", "refs/heads/feature/a"):                    "sha\trefs/heads/feature/a\n",
		key("git", "rev-parse", "--verify", "feature/a^{commit}"):                    "sha\n",
		key("git", "branch", "--show-current"):                                       "main\n",
		key("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"): "origin/main\n",
	}, errs: map[string]error{key("git", "push", lease, "origin", ":refs/heads/feature/a"): fmt.Errorf("stale info")}}
	w, _ := NewWorkflows(WorkflowOptions{WayOfWork: "githubflow", Runner: f})
	if err := w.FeaturePurge(context.Background(), "a"); err == nil || !strings.Contains(err.Error(), "stale info") {
		t.Fatalf("lease failure = %v", err)
	}
	if f.saw("git", "branch", "-d", "feature/a") {
		t.Fatalf("local source deleted after lease rejection: %#v", f.calls)
	}
}

func countCalls(f *fakeRunner, command string) int {
	count := 0
	for _, c := range f.calls {
		if strings.Join(append([]string{c.name}, c.args...), " ") == command {
			count++
		}
	}
	return count
}

func TestFeatureStartRejectsUnpublishedLocalBaseAhead(t *testing.T) {
	ctx := context.Background()
	_, repo := setupWorkflowRemote(t, false)
	writeFile(t, filepath.Join(repo, "local-only-base.txt"), "not pushed\n")
	gitTest(t, repo, "add", "local-only-base.txt")
	gitTest(t, repo, "commit", "-m", "local main ahead of origin")

	w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "githubflow", MainBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.FeatureStart(ctx, "must-not-create"); err == nil || !strings.Contains(err.Error(), "not exactly synchronized") {
		t.Fatalf("FeatureStart error = %v, want local-ahead rejection", err)
	}
	check := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/feature/must-not-create")
	check.Dir = repo
	if err = check.Run(); err == nil {
		t.Fatal("feature branch was created from unpublished main")
	}
}

func TestHotfixStartWithoutVersionUsesSynchronizedMainOutsideSupport(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "branch", "--show-current"):                                   "feature/unrelated\n",
		key("git", "rev-parse", "--verify", "main^{commit}"):                     "main-head\n",
		key("git", "rev-parse", "--verify", "refs/remotes/origin/main^{commit}"): "main-head\n",
		key("gitversion", "-showvariable", "MajorMinorPatch"):                    "1.2.3\n",
	}}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "main", Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.HotfixStart(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if !f.saw("git", "checkout", "-b", "hotfix/v1.2.4", "main") {
		t.Fatalf("hotfix did not use main outside support: %#v", f.calls)
	}
}
