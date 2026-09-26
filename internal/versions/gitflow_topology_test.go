package versions

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitFlowConfiguredDevelopPrimaryRejectsEveryWorkflowMutation(t *testing.T) {
	operations := []struct {
		name string
		run  func(*Workflows) error
	}{
		{"feature start", func(w *Workflows) error { return w.FeatureStart(context.Background(), "blocked") }},
		{"feature publish", func(w *Workflows) error { return w.FeaturePublish(context.Background(), "blocked") }},
		{"feature finish", func(w *Workflows) error { return w.FeatureFinish(context.Background(), "blocked") }},
		{"feature purge", func(w *Workflows) error { return w.FeaturePurge(context.Background(), "blocked") }},
		{"hotfix start", func(w *Workflows) error { return w.HotfixStart(context.Background(), "1.2.3") }},
		{"hotfix publish", func(w *Workflows) error { return w.HotfixPublish(context.Background(), "1.2.3") }},
		{"hotfix finish", func(w *Workflows) error { return w.HotfixFinish(context.Background(), "1.2.3", false) }},
		{"hotfix purge", func(w *Workflows) error { return w.HotfixPurge(context.Background(), "1.2.3") }},
		{"release start", func(w *Workflows) error { return w.ReleaseStart(context.Background(), "patch") }},
		{"release publish", func(w *Workflows) error { return w.ReleasePublish(context.Background(), "1.2.3") }},
		{"release finish", func(w *Workflows) error { return w.ReleaseFinish(context.Background(), "1.2.3", false) }},
		{"release purge", func(w *Workflows) error { return w.ReleasePurge(context.Background(), "1.2.3") }},
		{"support start", func(w *Workflows) error { return w.SupportStart(context.Background(), "1.2.3") }},
		{"support publish", func(w *Workflows) error { return w.SupportPublish(context.Background(), "1.2.3") }},
		{"support purge", func(w *Workflows) error { return w.SupportPurge(context.Background(), "1.2.3") }},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			f := &fakeRunner{}
			w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", MainBranch: "develop", Runner: f})
			if err != nil {
				t.Fatal(err)
			}
			err = operation.run(w)
			if err == nil || !strings.Contains(err.Error(), "primary branch must not be develop") {
				t.Fatalf("operation error = %v, want topology rejection", err)
			}
			if len(f.calls) != 0 {
				t.Fatalf("operation performed Git work before topology rejection: %#v", f.calls)
			}
		})
	}
}

func TestGitFlowDiscoveredDevelopPrimaryRejectsBeforeFeatureMutation(t *testing.T) {
	f := &fakeRunner{replies: map[string]string{
		key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"): "refs/remotes/origin/develop\n",
	}}
	w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", Runner: f})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.FeatureStart(context.Background(), "blocked"); err == nil || !strings.Contains(err.Error(), "primary branch must not be develop") {
		t.Fatalf("FeatureStart error = %v, want topology rejection", err)
	}
	if f.sawPrefix("git", "fetch") || f.sawPrefix("git", "checkout") {
		t.Fatalf("FeatureStart mutated after discovered invalid topology: %#v", f.calls)
	}
}

func TestGitFlowDanglingDiscoveredPrimaryRejectsFeatureAndReleaseStartBeforeMutation(t *testing.T) {
	operations := []struct {
		name string
		run  func(*Workflows) error
	}{
		{"feature", func(w *Workflows) error { return w.FeatureStart(context.Background(), "blocked") }},
		{"release", func(w *Workflows) error { return w.ReleaseStart(context.Background(), "patch") }},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			f := &fakeRunner{
				replies: map[string]string{
					key("git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"): "refs/remotes/origin/main\n",
				},
				errs: map[string]error{
					key("git", "ls-remote", "--exit-code", "origin", "refs/heads/main"): errors.New("missing origin/main"),
				},
			}
			w, err := NewWorkflows(WorkflowOptions{WayOfWork: "gitflow", Runner: f})
			if err != nil {
				t.Fatal(err)
			}
			if err = operation.run(w); err == nil || !strings.Contains(err.Error(), "resolved unavailable live branch") {
				t.Fatalf("operation error = %v, want dangling primary rejection", err)
			}
			for _, mutation := range [][]string{
				{"git", "fetch"},
				{"git", "checkout"},
				{"git", "push"},
			} {
				if f.sawPrefix(mutation...) {
					t.Fatalf("operation mutated after dangling primary rejection: %#v", f.calls)
				}
			}
		})
	}
}

func TestGitFlowDiscoveredDevelopPrimaryLeavesTemporaryRepositoryUnchanged(t *testing.T) {
	ctx := context.Background()
	root, repo := setupWorkflowRemote(t, true)
	gitTest(t, root, "--git-dir="+root+"/remote.git", "symbolic-ref", "HEAD", "refs/heads/develop")
	gitTest(t, repo, "remote", "set-head", "origin", "-a")
	before := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD"))
	w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "gitflow"})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.FeatureStart(ctx, "blocked"); err == nil || !strings.Contains(err.Error(), "primary branch must not be develop") {
		t.Fatalf("FeatureStart error = %v, want topology rejection", err)
	}
	if got := strings.TrimSpace(gitTest(t, repo, "branch", "--show-current")); got != "main" {
		t.Fatalf("current branch = %q, want main", got)
	}
	if got := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD")); got != before {
		t.Fatalf("HEAD changed from %s to %s", before, got)
	}
	if out := strings.TrimSpace(gitTest(t, repo, "branch", "--list", "feature/blocked")); out != "" {
		t.Fatalf("feature branch was created: %s", out)
	}
}

func TestInitGitFlowRejectsConfiguredDevelopPrimaryBeforeConfigOrGitMutation(t *testing.T) {
	dir := workflowFixture(t)
	target := filepath.Join(dir, cloudOpsWorksDir, "gitversion.yaml")
	before := mustReadFile(t, target)
	git := fakeGit(t, `echo "$*" >&2; exit 2`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, MainBranch: "develop", Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow}); err == nil || !strings.Contains(err.Error(), "primary branch must not be develop") {
		t.Fatalf("Init error = %v, want topology rejection", err)
	}
	if got := mustReadFile(t, target); got != before {
		t.Fatalf("gitversion config changed before topology rejection:\n%s", got)
	}
}

func TestInitGitFlowRejectsDiscoveredDevelopPrimaryBeforeExistingDevelopConfigMutation(t *testing.T) {
	dir := workflowFixture(t)
	target := filepath.Join(dir, cloudOpsWorksDir, "gitversion.yaml")
	before := mustReadFile(t, target)
	log := filepath.Join(dir, "git.log")
	git := fakeGit(t, `echo "$*" >> "$GIT_LOG"
case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"symbolic-ref --quiet refs/remotes/origin/HEAD") echo refs/remotes/origin/develop;;
"show-ref --verify --quiet refs/remotes/origin/develop") exit 0;;
"show-ref --verify --quiet refs/heads/develop") exit 1;;
"checkout "*|"push "*) echo "unexpected mutation path $*" >&2; exit 2;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	t.Setenv("GIT_LOG", log)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow}); err == nil || !strings.Contains(err.Error(), "primary branch must not be develop") {
		t.Fatalf("Init error = %v, want topology rejection", err)
	}
	if got := mustReadFile(t, target); got != before {
		t.Fatalf("gitversion config changed before topology rejection:\n%s", got)
	}
	calls := mustReadFile(t, log)
	if strings.Contains(calls, "checkout ") || strings.Contains(calls, "push ") {
		t.Fatalf("Init continued into a branch mutation path: %s", calls)
	}
}

func TestNonGitFlowAllowsDevelopAsPrimary(t *testing.T) {
	for _, wow := range []string{"githubflow", "trunkbased"} {
		t.Run(wow, func(t *testing.T) {
			f := &fakeRunner{}
			w, err := NewWorkflows(WorkflowOptions{WayOfWork: wow, MainBranch: "develop", Runner: f})
			if err != nil {
				t.Fatal(err)
			}
			if err = w.FeatureStart(context.Background(), "allowed"); err != nil {
				t.Fatalf("FeatureStart rejected non-GitFlow develop primary: %v", err)
			}
			if !f.saw("git", "checkout", "-b", "feature/allowed", "refs/heads/develop") {
				t.Fatalf("FeatureStart did not retain develop primary: %#v", f.calls)
			}
		})
	}
}

func TestInitGitFlowRejectsDanglingDiscoveredPrimaryBeforeExistingDevelopConfigMutation(t *testing.T) {
	dir := workflowFixture(t)
	target := filepath.Join(dir, cloudOpsWorksDir, "gitversion.yaml")
	before := mustReadFile(t, target)
	log := filepath.Join(dir, "git.log")
	git := fakeGit(t, `echo "$*" >> "$GIT_LOG"
case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"show-ref --verify --quiet refs/remotes/origin/develop") exit 0;;
"show-ref --verify --quiet refs/heads/develop") exit 1;;
"symbolic-ref --quiet refs/remotes/origin/HEAD") echo refs/remotes/origin/main;;
"show-ref --verify --quiet refs/remotes/origin/main") exit 1;;
"checkout "*|"push "*) echo "unexpected mutation path $*" >&2; exit 2;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	t.Setenv("GIT_LOG", log)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow}); err == nil || !strings.Contains(err.Error(), "resolved unavailable branch") {
		t.Fatalf("Init error = %v, want dangling primary rejection", err)
	}
	if got := mustReadFile(t, target); got != before {
		t.Fatalf("gitversion config changed before dangling-primary rejection:\n%s", got)
	}
	calls := mustReadFile(t, log)
	if strings.Contains(calls, "checkout ") || strings.Contains(calls, "push ") {
		t.Fatalf("Init continued into a branch mutation path: %s", calls)
	}
}

func TestGitFlowDiscoveredPrimaryRejectsStaleTrackingRefBeforeFeatureOrReleaseMutation(t *testing.T) {
	ctx := context.Background()
	operations := []struct {
		name   string
		branch string
		run    func(*Workflows) error
	}{
		{"feature", "feature/stale-primary", func(w *Workflows) error { return w.FeatureStart(ctx, "stale-primary") }},
		{"release", "release/v0.1.1", func(w *Workflows) error { return w.ReleaseStart(ctx, "patch") }},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			root, repo := setupWorkflowRemote(t, true)
			gitTest(t, root, "--git-dir="+root+"/remote.git", "symbolic-ref", "HEAD", "refs/heads/main")
			gitTest(t, repo, "remote", "set-head", "origin", "-a")
			primarySHA := strings.TrimSpace(gitTest(t, repo, "rev-parse", "refs/remotes/origin/main"))
			gitTest(t, repo, "checkout", "--no-guess", "develop")
			gitTest(t, root, "--git-dir="+root+"/remote.git", "update-ref", "-d", "refs/heads/main")
			if got := strings.TrimSpace(gitTest(t, repo, "rev-parse", "refs/remotes/origin/main")); got != primarySHA {
				t.Fatalf("stale local primary = %s, want %s", got, primarySHA)
			}

			w, err := NewWorkflows(WorkflowOptions{Dir: repo, WayOfWork: "gitflow"})
			if err != nil {
				t.Fatal(err)
			}
			if err = operation.run(w); err == nil || !strings.Contains(err.Error(), "resolved unavailable live branch") {
				t.Fatalf("operation error = %v, want stale discovered-primary rejection", err)
			}
			if current := strings.TrimSpace(gitTest(t, repo, "branch", "--show-current")); current != "develop" {
				t.Fatalf("operation changed current branch to %q", current)
			}
			if got := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD")); got != primarySHA {
				t.Fatalf("operation changed HEAD to %s, want %s", got, primarySHA)
			}
			if got := strings.TrimSpace(gitTest(t, repo, "branch", "--list", operation.branch)); got != "" {
				t.Fatalf("operation created branch %s", got)
			}
		})
	}
}
