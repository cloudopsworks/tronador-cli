package versions

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInitValidatesEverySelectorBeforeMutation(t *testing.T) {
	dir := workflowFixture(t)
	target := filepath.Join(dir, cloudOpsWorksDir, "gitversion.yaml")
	before := mustReadFile(t, target)
	if err := os.Remove(filepath.Join(dir, cloudOpsWorksDir, "gitversion_trunkbased.yaml")); err != nil {
		t.Fatal(err)
	}
	called := false
	r := newInitRunner(t, dir, func(_ io.Reader, _ io.Writer) (WayOfWork, error) { called = true; return WayOfWorkGitHubFlow, nil })
	_, err := r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitHubFlow})
	if err == nil || !strings.Contains(err.Error(), "gitversion_trunkbased.yaml") {
		t.Fatalf("Init error = %v", err)
	}
	if called {
		t.Fatal("selector called before selector-set validation")
	}
	if got := mustReadFile(t, target); got != before {
		t.Fatalf("target mutated before validation:\n%s", got)
	}
}

func TestInitSelectsDisabledConfigAndPreservesScalarLayout(t *testing.T) {
	dir := workflowFixture(t)
	ci := "config:\n    gitFlow:\n        enabled: false # keep this comment\n        supportBranches: false\n"
	writeFile(t, filepath.Join(dir, cloudOpsWorksDir, "cloudopsworks-ci.yaml"), ci)
	var prompt bytes.Buffer
	r, err := NewRunner(Options{WorkDir: dir, Stdin: strings.NewReader("1\n"), Stdout: &prompt, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	// Selection 1 requires no git when the remote develop branch is already present.
	git := fakeGit(t, `case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"show-ref --verify --quiet refs/remotes/origin/develop") exit 0;;
"show-ref --verify --quiet refs/heads/develop") exit 1;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	r.gitPath = git
	result, err := r.Init(context.Background(), InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.WayOfWork != WayOfWorkGitFlow {
		t.Fatalf("WayOfWork = %q", result.WayOfWork)
	}
	if !strings.Contains(prompt.String(), "1) gitflow") {
		t.Fatalf("prompt = %q", prompt.String())
	}
	if got := mustReadFile(t, filepath.Join(dir, cloudOpsWorksDir, "cloudopsworks-ci.yaml")); got != "config:\n    gitFlow:\n        enabled: true # keep this comment\n        supportBranches: false\n" {
		t.Fatalf("ci = %q", got)
	}
	if got := mustReadFile(t, filepath.Join(dir, cloudOpsWorksDir, "gitversion.yaml")); !strings.Contains(got, "WayOfWork=gitflow") {
		t.Fatalf("target = %q", got)
	}
}

func TestInitExplicitGitHubFlowDoesNotRequireGitAndWarnsCurrentHeader(t *testing.T) {
	dir := workflowFixture(t)
	var stderr bytes.Buffer
	r, err := NewRunner(Options{WorkDir: dir, Stdout: &bytes.Buffer{}, Stderr: &stderr})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitHubFlow})
	if err != nil {
		t.Fatal(err)
	}
	if result.DevelopCreated {
		t.Fatal("GitHub Flow unexpectedly created develop")
	}
	if !strings.Contains(stderr.String(), "current WayOfWork=gitflow") {
		t.Fatalf("warning = %q", stderr.String())
	}
	if got := mustReadFile(t, filepath.Join(dir, cloudOpsWorksDir, "gitversion.yaml")); !strings.Contains(got, "WayOfWork=githubflow") {
		t.Fatalf("target = %q", got)
	}
}

func TestInitGitFlowCreatesDevelopOnlyFromCleanParityPrimary(t *testing.T) {
	dir := workflowFixture(t)
	log := filepath.Join(dir, "git.log")
	git := fakeGit(t, `echo "$*" >> "$GIT_LOG"
case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"show-ref --verify --quiet refs/remotes/origin/develop"|"show-ref --verify --quiet refs/heads/develop") exit 1;;
"symbolic-ref --quiet refs/remotes/origin/HEAD") exit 1;;
"branch --show-current") echo main;;
"rev-parse HEAD"|"rev-parse refs/remotes/origin/main"|"rev-parse refs/heads/develop") echo abc123;;
"checkout -b develop refs/heads/main"|"push --set-upstream origin refs/heads/develop:refs/heads/develop") exit 0;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	var stderr bytes.Buffer
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, Stdout: &bytes.Buffer{}, Stderr: &stderr})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_LOG", log)
	result, err := r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DevelopCreated {
		t.Fatal("develop was not created")
	}
	calls := mustReadFile(t, log)
	for _, want := range []string{"status --porcelain", "remote get-url origin", "checkout -b develop refs/heads/main", "push --set-upstream origin refs/heads/develop:refs/heads/develop"} {
		if !strings.Contains(calls, want) {
			t.Fatalf("calls missing %q:\n%s", want, calls)
		}
	}
}

func TestInitGitFlowCreatesDevelopFromOriginHeadPrimary(t *testing.T) {
	dir := workflowFixture(t)
	log := filepath.Join(dir, "git.log")
	git := fakeGit(t, `echo "$*" >> "$GIT_LOG"
case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"show-ref --verify --quiet refs/remotes/origin/develop"|"show-ref --verify --quiet refs/heads/develop") exit 1;;
"symbolic-ref --quiet refs/remotes/origin/HEAD") echo refs/remotes/origin/primary;;
"branch --show-current") echo primary;;
"rev-parse HEAD"|"rev-parse refs/remotes/origin/primary") echo abc123;;
"checkout -b develop refs/heads/primary"|"push --set-upstream origin refs/heads/develop:refs/heads/develop") exit 0;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_LOG", log)
	result, err := r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DevelopCreated {
		t.Fatalf("develop was not created: %+v", result)
	}
	calls := mustReadFile(t, log)
	for _, want := range []string{"symbolic-ref --quiet refs/remotes/origin/HEAD", "checkout -b develop refs/heads/primary", "push --set-upstream origin refs/heads/develop:refs/heads/develop"} {
		if !strings.Contains(calls, want) {
			t.Fatalf("calls missing %q:\n%s", want, calls)
		}
	}
}

func TestInitGitFlowRejectsOriginHeadPrimaryOnWrongCurrentBranch(t *testing.T) {
	dir := workflowFixture(t)
	git := fakeGit(t, `case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"show-ref --verify --quiet refs/remotes/origin/develop") exit 1;;
"symbolic-ref --quiet refs/remotes/origin/HEAD") echo refs/remotes/origin/primary;;
"rev-parse refs/remotes/origin/primary") echo abc123;;
"branch --show-current") echo main;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow}); err == nil || !strings.Contains(err.Error(), `check out origin/HEAD branch "primary"`) {
		t.Fatalf("wrong-current error = %v", err)
	}
}

func TestInitGitFlowRejectsDivergentOriginHeadPrimary(t *testing.T) {
	dir := workflowFixture(t)
	git := fakeGit(t, `case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"show-ref --verify --quiet refs/remotes/origin/develop") exit 1;;
"symbolic-ref --quiet refs/remotes/origin/HEAD") echo refs/remotes/origin/primary;;
"branch --show-current") echo primary;;
"rev-parse HEAD") echo local;;
"rev-parse refs/remotes/origin/primary") echo remote;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow}); err == nil || !strings.Contains(err.Error(), "primary is not equal to origin/primary") {
		t.Fatalf("divergent origin/HEAD error = %v", err)
	}
}

func TestInitGitFlowFallsBackToMainWhenOriginHeadIsAbsent(t *testing.T) {
	for name, originHead := range map[string]string{
		"missing": `exit 1;;`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := workflowFixture(t)
			git := fakeGit(t, `case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"show-ref --verify --quiet refs/remotes/origin/develop"|"show-ref --verify --quiet refs/heads/develop") exit 1;;
"symbolic-ref --quiet refs/remotes/origin/HEAD") `+originHead+`
"branch --show-current") echo main;;
"rev-parse HEAD"|"rev-parse refs/remotes/origin/main") echo abc123;;
"checkout -b develop refs/heads/main"|"push --set-upstream origin refs/heads/develop:refs/heads/develop") exit 0;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
			r, err := NewRunner(Options{WorkDir: dir, GitPath: git, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow})
			if err != nil {
				t.Fatal(err)
			}
			if !result.DevelopCreated {
				t.Fatalf("main fallback did not create develop: %+v", result)
			}
		})
	}
}

func TestInitGitFlowRejectsOutsideOriginHeadTargetWithoutMutation(t *testing.T) {
	dir := workflowFixture(t)
	target := filepath.Join(dir, cloudOpsWorksDir, "gitversion.yaml")
	before := mustReadFile(t, target)
	git := fakeGit(t, `case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"show-ref --verify --quiet refs/remotes/origin/develop") exit 1;;
"symbolic-ref --quiet refs/remotes/origin/HEAD") echo refs/heads/production;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow}); err == nil || !strings.Contains(err.Error(), "invalid origin/HEAD target") {
		t.Fatalf("outside origin/HEAD error = %v", err)
	}
	if got := mustReadFile(t, target); got != before {
		t.Fatalf("config changed after outside origin/HEAD target:\n%s", got)
	}
}

func TestInitGitFlowRejectsOperationalOriginHeadErrorWithoutMutation(t *testing.T) {
	dir := workflowFixture(t)
	target := filepath.Join(dir, cloudOpsWorksDir, "gitversion.yaml")
	before := mustReadFile(t, target)
	log := filepath.Join(dir, "git.log")
	git := fakeGit(t, `echo "$*" >> "$GIT_LOG"
case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"show-ref --verify --quiet refs/remotes/origin/develop") exit 1;;
"symbolic-ref --quiet refs/remotes/origin/HEAD") echo "repository error" >&2; exit 2;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_LOG", log)
	if _, err = r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow}); err == nil || !strings.Contains(err.Error(), "resolve origin/HEAD") {
		t.Fatalf("operational origin/HEAD error = %v", err)
	}
	if got := mustReadFile(t, target); got != before {
		t.Fatalf("config changed after origin/HEAD error:\n%s", got)
	}
	calls := mustReadFile(t, log)
	if strings.Contains(calls, "checkout ") || strings.Contains(calls, "push ") {
		t.Fatalf("mutating git call after origin/HEAD error:\n%s", calls)
	}
}

func TestInitGitFlowRejectsUnresolvedOriginHeadBranch(t *testing.T) {
	dir := workflowFixture(t)
	target := filepath.Join(dir, cloudOpsWorksDir, "gitversion.yaml")
	before := mustReadFile(t, target)
	log := filepath.Join(dir, "git.log")
	git := fakeGit(t, `echo "$*" >> "$GIT_LOG"
case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"show-ref --verify --quiet refs/remotes/origin/develop") exit 1;;
"symbolic-ref --quiet refs/remotes/origin/HEAD") echo refs/remotes/origin/primary;;
"rev-parse refs/remotes/origin/primary") echo "missing primary" >&2; exit 1;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_LOG", log)
	if _, err = r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow}); err == nil || !strings.Contains(err.Error(), "resolve origin/HEAD branch origin/primary") {
		t.Fatalf("unresolved origin/HEAD branch error = %v", err)
	}
	if got := mustReadFile(t, target); got != before {
		t.Fatalf("config changed after unresolved origin/HEAD branch:\n%s", got)
	}
	calls := mustReadFile(t, log)
	if strings.Contains(calls, "checkout ") || strings.Contains(calls, "push ") {
		t.Fatalf("mutating git call after unresolved origin/HEAD branch:\n%s", calls)
	}
}

func TestInitGitFlowUsesOriginHeadBranchDespiteSameNamedTag(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	gitTest(t, root, "init", "--bare", remote)
	repo := filepath.Join(root, "repo")
	gitTest(t, root, "clone", remote, repo)
	gitTest(t, repo, "config", "user.email", "test@example.test")
	gitTest(t, repo, "config", "user.name", "Test")
	gitTest(t, repo, "checkout", "-b", "main")
	gitTest(t, repo, "commit", "--allow-empty", "-m", "main")
	gitTest(t, repo, "push", "-u", "origin", "main")
	gitTest(t, repo, "tag", "-a", "origin/production", "-m", "shadow production remote name")
	gitTest(t, repo, "push", "origin", "refs/tags/origin/production")
	gitTest(t, repo, "checkout", "-b", "production")

	config := filepath.Join(repo, cloudOpsWorksDir)
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, wow := range []WayOfWork{WayOfWorkGitFlow, WayOfWorkGitHubFlow, WayOfWorkTrunkBased} {
		writeFile(t, filepath.Join(config, wow.selectorFileName()), "# Agents: WayOfWork="+string(wow)+"\nmode: test\n")
	}
	writeFile(t, filepath.Join(config, "gitversion.yaml"), "# Agents: WayOfWork=gitflow\nmode: old\n")
	gitTest(t, repo, "add", cloudOpsWorksDir)
	gitTest(t, repo, "commit", "-m", "production config")
	production := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD"))
	gitTest(t, repo, "push", "-u", "origin", "production")
	gitTest(t, root, "--git-dir="+remote, "symbolic-ref", "HEAD", "refs/heads/production")
	gitTest(t, repo, "remote", "set-head", "origin", "-a")

	r, err := NewRunner(Options{WorkDir: repo, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DevelopCreated {
		t.Fatalf("develop was not created: %+v", result)
	}
	if got := strings.TrimSpace(gitTest(t, repo, "rev-parse", "refs/heads/develop")); got != production {
		t.Fatalf("develop commit = %s, want origin/HEAD production %s", got, production)
	}
	if tag := strings.TrimSpace(gitTest(t, repo, "rev-parse", "refs/tags/origin/production")); tag == production {
		t.Fatal("same-named tag unexpectedly resolves to production branch commit")
	}
}

func TestInitGitFlowCreatesDevelopFromConfiguredPrimary(t *testing.T) {
	dir := workflowFixture(t)
	log := filepath.Join(dir, "git.log")
	git := fakeGit(t, `echo "$*" >> "$GIT_LOG"
case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"show-ref --verify --quiet refs/remotes/origin/develop"|"show-ref --verify --quiet refs/heads/develop") exit 1;;
"branch --show-current") echo primary;;
"rev-parse HEAD"|"rev-parse refs/remotes/origin/primary") echo abc123;;
"checkout -b develop refs/heads/primary"|"push --set-upstream origin refs/heads/develop:refs/heads/develop") exit 0;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, MainBranch: "primary", Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_LOG", log)
	result, err := r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DevelopCreated || !strings.Contains(mustReadFile(t, log), "checkout -b develop refs/heads/primary") {
		t.Fatalf("configured primary was not used: %+v\n%s", result, mustReadFile(t, log))
	}
}

func TestInitGitFlowRejectsConfiguredPrimaryOriginMismatch(t *testing.T) {
	dir := workflowFixture(t)
	git := fakeGit(t, `case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"show-ref --verify --quiet refs/remotes/origin/develop") exit 1;;
"branch --show-current") echo primary;;
"rev-parse HEAD") echo local;;
"rev-parse refs/remotes/origin/primary") echo remote;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, MainBranch: "primary", Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow}); err == nil || !strings.Contains(err.Error(), "primary is not equal to origin/primary") {
		t.Fatalf("origin mismatch error = %v", err)
	}
}

func TestInitGitFlowRejectsWrongCurrentConfiguredPrimary(t *testing.T) {
	dir := workflowFixture(t)
	git := fakeGit(t, `case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"show-ref --verify --quiet refs/remotes/origin/develop") exit 1;;
"branch --show-current") echo main;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, MainBranch: "primary", Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow}); err == nil || !strings.Contains(err.Error(), `check out configured main branch "primary"`) {
		t.Fatalf("wrong-current error = %v", err)
	}
}

func TestInitRejectsSymlinkedCloudOpsWorksDirectoryBeforeReadOrGitMutation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	dir := workflowFixture(t)
	external := t.TempDir()
	for _, wow := range []WayOfWork{WayOfWorkGitFlow, WayOfWorkGitHubFlow, WayOfWorkTrunkBased} {
		writeFile(t, filepath.Join(external, wow.selectorFileName()), "# Agents: WayOfWork="+string(wow)+"\nexternal: true\n")
	}
	gitVersion := filepath.Join(external, "gitversion.yaml")
	writeFile(t, gitVersion, "# Agents: WayOfWork=gitflow\nexternal: unchanged\n")
	if err := os.RemoveAll(filepath.Join(dir, cloudOpsWorksDir)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(dir, cloudOpsWorksDir)); err != nil {
		t.Fatal(err)
	}
	gitCalled := filepath.Join(t.TempDir(), "git-called")
	git := fakeGit(t, `touch "`+gitCalled+`"
exit 0`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow}); err == nil || !strings.Contains(err.Error(), "non-symlink directory") {
		t.Fatalf("Init error = %v", err)
	}
	if got := mustReadFile(t, gitVersion); got != "# Agents: WayOfWork=gitflow\nexternal: unchanged\n" {
		t.Fatalf("external gitversion.yaml mutated:\n%s", got)
	}
	if _, err := os.Stat(gitCalled); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Git mutation began before layout validation: %v", err)
	}
}

func TestInitRejectsDirectorySwapBeforeConfigReadOrGitMutation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	dir := workflowFixture(t)
	writeFile(t, filepath.Join(dir, cloudOpsWorksDir, "cloudopsworks-ci.yaml"), "config:\n  gitFlow:\n    enabled: false\n")
	external := t.TempDir()
	writeFile(t, filepath.Join(external, "gitversion.yaml"), "# Agents: WayOfWork=gitflow\nexternal: unchanged\n")
	gitCalled := filepath.Join(t.TempDir(), "git-called")
	git := fakeGit(t, `touch "`+gitCalled+`"
exit 0`)
	r, err := NewRunner(Options{
		WorkDir: dir,
		GitPath: git,
		SelectWayOfWork: func(io.Reader, io.Writer) (WayOfWork, error) {
			swapCloudOpsWorksDir(t, dir, external)
			return WayOfWorkGitFlow, nil
		},
		Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Init(context.Background(), InitOptions{}); err == nil || !strings.Contains(err.Error(), "changed during initialization") {
		t.Fatalf("Init error = %v", err)
	}
	if got := mustReadFile(t, filepath.Join(external, "gitversion.yaml")); got != "# Agents: WayOfWork=gitflow\nexternal: unchanged\n" {
		t.Fatalf("external gitversion.yaml mutated:\n%s", got)
	}
	if _, err := os.Stat(gitCalled); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Git mutation began after directory swap: %v", err)
	}
}

func TestInitRootAnchorsAtomicWriteAcrossDirectorySwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	dir := workflowFixture(t)
	external := t.TempDir()
	gitVersion := filepath.Join(external, "gitversion.yaml")
	writeFile(t, gitVersion, "# Agents: WayOfWork=gitflow\nexternal: unchanged\n")
	oldHook := beforeRootAtomicTempCreate
	beforeRootAtomicTempCreate = func() { swapCloudOpsWorksDir(t, dir, external) }
	t.Cleanup(func() { beforeRootAtomicTempCreate = oldHook })
	r := newInitRunner(t, dir, SelectWayOfWork)
	if _, err := r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitHubFlow}); err == nil || !strings.Contains(err.Error(), "changed during initialization") {
		t.Fatalf("Init error = %v", err)
	}
	if got := mustReadFile(t, gitVersion); got != "# Agents: WayOfWork=gitflow\nexternal: unchanged\n" {
		t.Fatalf("external gitversion.yaml mutated:\n%s", got)
	}
}

func swapCloudOpsWorksDir(t *testing.T, workDir, external string) {
	t.Helper()
	config := filepath.Join(workDir, cloudOpsWorksDir)
	if err := os.Rename(config, config+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, config); err != nil {
		t.Fatal(err)
	}
}

func TestInitRejectsSymlinkSelector(t *testing.T) {
	dir := workflowFixture(t)
	path := filepath.Join(dir, cloudOpsWorksDir, "gitversion_trunkbased.yaml")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("gitversion_gitflow.yaml", path); err != nil {
		t.Fatal(err)
	}
	r, err := NewRunner(Options{WorkDir: dir, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitHubFlow}); err == nil || !strings.Contains(err.Error(), "regular non-symlink") {
		t.Fatalf("Init error = %v", err)
	}
}

func TestSelectWayOfWorkRejectsNonNumericValue(t *testing.T) {
	if _, err := SelectWayOfWork(strings.NewReader("gitflow\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("non-numeric selection accepted")
	}
}

func workflowFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	config := filepath.Join(dir, cloudOpsWorksDir)
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, wow := range []WayOfWork{WayOfWorkGitFlow, WayOfWorkGitHubFlow, WayOfWorkTrunkBased} {
		writeFile(t, filepath.Join(config, wow.selectorFileName()), "# Agents: WayOfWork="+string(wow)+"\nmode: test\n")
	}
	writeFile(t, filepath.Join(config, "gitversion.yaml"), "# Agents: WayOfWork=gitflow\nmode: old\n")
	return dir
}
func newInitRunner(t *testing.T, dir string, selectWOW func(io.Reader, io.Writer) (WayOfWork, error)) *Runner {
	t.Helper()
	r, err := NewRunner(Options{WorkDir: dir, SelectWayOfWork: selectWOW, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func fakeGit(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunnerWorkflowCarriesMainBranchOverride(t *testing.T) {
	dir := workflowFixture(t)
	// Main overrides are accepted only when the configured remote advertises
	// the branch. Keep this runner wiring test hermetic while asserting that
	// the workflow performs that guard.
	git := fakeGit(t, `if [ "$1" = "show-ref" ]; then exit 0; fi
exit 1`)
	runner, err := NewRunner(Options{WorkDir: dir, GitPath: git, MainBranch: "primary"})
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := runner.Workflow(WayOfWorkGitHubFlow)
	if err != nil {
		t.Fatal(err)
	}
	main, err := workflow.Main(context.Background())
	if err != nil || main != "primary" {
		t.Fatalf("workflow main = %q, %v", main, err)
	}
}

func TestGitFlowConfigFindsReorderedNestedMappingAndPreservesBytes(t *testing.T) {
	dir := workflowFixture(t)
	ci := "# unrelated matching names must not participate\ngitFlow:\n  enabled: true\nconfig: # root settings\n  runner_set: shared\n\n  gitFlow: # keep mapping comment\n    supportBranches: false\n    # preserve this comment\n    enabled: false # preserve inline\n  trailing: value\nother:\n  config:\n    gitFlow:\n      enabled: true\n"
	path := filepath.Join(dir, cloudOpsWorksDir, "cloudopsworks-ci.yaml")
	writeFile(t, path, ci)
	r := newInitRunner(t, dir, SelectWayOfWork)
	enabled, supported, err := r.gitFlowConfig()
	if err != nil || !supported || enabled {
		t.Fatalf("gitFlowConfig = enabled %v, supported %v, err %v", enabled, supported, err)
	}
	changed, err := r.setGitFlowEnabled(true)
	if err != nil || !changed {
		t.Fatalf("setGitFlowEnabled = changed %v, err %v", changed, err)
	}
	want := strings.Replace(ci, "    enabled: false # preserve inline", "    enabled: true # preserve inline", 1)
	if got := mustReadFile(t, path); got != want {
		t.Fatalf("CI changed outside exact scalar:\nwant %q\n got %q", want, got)
	}
}

func TestGitFlowConfigRejectsNestedOrSimilarKeys(t *testing.T) {
	dir := workflowFixture(t)
	path := filepath.Join(dir, cloudOpsWorksDir, "cloudopsworks-ci.yaml")
	ci := "config:\n  child:\n    gitFlow:\n      enabled: true\n  gitflow:\n    enabled: true\n  gitFlowEnabled: true\n"
	writeFile(t, path, ci)
	r := newInitRunner(t, dir, SelectWayOfWork)
	if enabled, supported, err := r.gitFlowConfig(); err != nil || supported || enabled {
		t.Fatalf("gitFlowConfig = enabled %v, supported %v, err %v", enabled, supported, err)
	}
	changed, err := r.setGitFlowEnabled(false)
	if err != nil || changed {
		t.Fatalf("setGitFlowEnabled = changed %v, err %v", changed, err)
	}
	if got := mustReadFile(t, path); got != ci {
		t.Fatalf("unsupported CI changed: %q", got)
	}
}

func TestGitFlowConfigIgnoresNestedMappingBeforeRootConfig(t *testing.T) {
	dir := workflowFixture(t)
	path := filepath.Join(dir, cloudOpsWorksDir, "cloudopsworks-ci.yaml")
	ci := "other:\n  config:\n    gitFlow:\n      enabled: true # nested must stay\n\nconfig: # root configuration\n  gitFlow:\n    enabled: false # root must change\n"
	writeFile(t, path, ci)
	r := newInitRunner(t, dir, SelectWayOfWork)
	enabled, supported, err := r.gitFlowConfig()
	if err != nil || !supported || enabled {
		t.Fatalf("gitFlowConfig = enabled %v, supported %v, err %v", enabled, supported, err)
	}
	changed, err := r.setGitFlowEnabled(true)
	if err != nil || !changed {
		t.Fatalf("setGitFlowEnabled = changed %v, err %v", changed, err)
	}
	want := strings.Replace(ci, "    enabled: false # root must change", "    enabled: true # root must change", 1)
	if got := mustReadFile(t, path); got != want {
		t.Fatalf("nested mapping was read or changed:\nwant %q\n got %q", want, got)
	}
}

func TestGitFlowConfigNestedOnlyDoesNotDriveSelectionOrMutation(t *testing.T) {
	dir := workflowFixture(t)
	path := filepath.Join(dir, cloudOpsWorksDir, "cloudopsworks-ci.yaml")
	ci := "other:\n  config:\n    gitFlow:\n      enabled: false # nested only\n"
	writeFile(t, path, ci)
	selected := false
	r := newInitRunner(t, dir, func(io.Reader, io.Writer) (WayOfWork, error) {
		selected = true
		return WayOfWorkGitHubFlow, nil
	})
	if enabled, supported, err := r.gitFlowConfig(); err != nil || supported || enabled {
		t.Fatalf("gitFlowConfig = enabled %v, supported %v, err %v", enabled, supported, err)
	}
	wow, err := r.chooseWayOfWork("")
	if err != nil || wow != WayOfWorkGitFlow || selected {
		t.Fatalf("nested-only selection = %q, selected %v, err %v", wow, selected, err)
	}
	changed, err := r.setGitFlowEnabled(true)
	if err != nil || changed {
		t.Fatalf("setGitFlowEnabled = changed %v, err %v", changed, err)
	}
	if got := mustReadFile(t, path); got != ci {
		t.Fatalf("nested-only config mutated: %q", got)
	}
}

func TestInitIdenticalSelectionIsNoOp(t *testing.T) {
	dir := workflowFixture(t)
	selector := mustReadFile(t, filepath.Join(dir, cloudOpsWorksDir, "gitversion_githubflow.yaml"))
	writeFile(t, filepath.Join(dir, cloudOpsWorksDir, "gitversion.yaml"), selector)
	r := newInitRunner(t, dir, SelectWayOfWork)
	result, err := r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitHubFlow})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatalf("identical init reported changed: %+v", result)
	}
}

func TestInitPublishesExistingLocalDevelopWhenRemoteMissing(t *testing.T) {
	dir := workflowFixture(t)
	log := filepath.Join(dir, "git.log")
	git := fakeGit(t, `echo "$*" >> "$GIT_LOG"
case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"show-ref --verify --quiet refs/remotes/origin/develop") exit 1;;
"symbolic-ref --quiet refs/remotes/origin/HEAD") exit 1;;
"branch --show-current") echo main;;
"rev-parse HEAD"|"rev-parse refs/remotes/origin/main"|"rev-parse refs/heads/develop") echo abc123;;
"show-ref --verify --quiet refs/heads/develop") exit 0;;
"push --set-upstream origin refs/heads/develop:refs/heads/develop") exit 0;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_LOG", log)
	result, err := r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DevelopCreated || !strings.Contains(mustReadFile(t, log), "push --set-upstream origin refs/heads/develop:refs/heads/develop") {
		t.Fatalf("existing local develop was not published: %+v\n%s", result, mustReadFile(t, log))
	}
}

func TestInitRejectsDivergentLocalAndRemoteDevelop(t *testing.T) {
	dir := workflowFixture(t)
	git := fakeGit(t, `case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"show-ref --verify --quiet refs/remotes/origin/develop"|"show-ref --verify --quiet refs/heads/develop") exit 0;;
"rev-parse refs/heads/develop") echo local-develop;;
"rev-parse refs/remotes/origin/develop") echo remote-develop;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow})
	if err == nil || !strings.Contains(err.Error(), "local develop differs from origin/develop") {
		t.Fatalf("Init divergent develop error = %v", err)
	}
}

type failingAtomicTempFile struct {
	*os.File
	chmodErr error
	writeErr error
}

func (f failingAtomicTempFile) Chmod(mode os.FileMode) error {
	if f.chmodErr != nil {
		return f.chmodErr
	}
	return f.File.Chmod(mode)
}

func (f failingAtomicTempFile) Write(data []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.File.Write(data)
}

func TestRootedAtomicWritePreservesDestinationOnTemporaryFailure(t *testing.T) {
	for _, test := range []struct {
		name     string
		chmodErr error
		writeErr error
	}{
		{name: "chmod", chmodErr: errors.New("chmod failed")},
		{name: "write", writeErr: errors.New("write failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := workflowFixture(t)
			target := filepath.Join(dir, cloudOpsWorksDir, "gitversion.yaml")
			writeFile(t, target, "original\n")
			r := newInitRunner(t, dir, SelectWayOfWork)
			layout, err := r.validateCloudOpsWorksDir()
			if err != nil {
				t.Fatal(err)
			}
			defer layout.root.Close()
			oldFactory := createRootAtomicTempFile
			createRootAtomicTempFile = func(root *os.Root) (string, atomicTempFile, error) {
				name, file, err := newRootAtomicTempFile(root)
				if err != nil {
					return "", nil, err
				}
				return name, failingAtomicTempFile{File: file.(*os.File), chmodErr: test.chmodErr, writeErr: test.writeErr}, nil
			}
			t.Cleanup(func() { createRootAtomicTempFile = oldFactory })

			if err := writeAtomicallyInLayout(layout, "gitversion.yaml", []byte("replacement\n"), 0o644); err == nil {
				t.Fatal("writeAtomicallyInLayout unexpectedly succeeded")
			}
			if got := mustReadFile(t, target); got != "original\n" {
				t.Fatalf("destination replaced after temporary failure: %q", got)
			}
			if temporary, err := filepath.Glob(filepath.Join(filepath.Dir(target), ".tronador-*")); err != nil || len(temporary) != 0 {
				t.Fatalf("temporary files = %v, %v", temporary, err)
			}
		})
	}
}

func TestRootedAtomicWriteCleansTemporaryOnReplacementFailure(t *testing.T) {
	dir := workflowFixture(t)
	target := filepath.Join(dir, cloudOpsWorksDir, "gitversion.yaml")
	writeFile(t, target, "original\n")
	r := newInitRunner(t, dir, SelectWayOfWork)
	layout, err := r.validateCloudOpsWorksDir()
	if err != nil {
		t.Fatal(err)
	}
	defer layout.root.Close()
	originalReplace := replaceAtomicFileInRoot
	replaceAtomicFileInRoot = func(*os.Root, string, string) error { return errors.New("replace failed") }
	t.Cleanup(func() { replaceAtomicFileInRoot = originalReplace })

	if err := writeAtomicallyInLayout(layout, "gitversion.yaml", []byte("replacement\n"), 0o644); err == nil {
		t.Fatal("writeAtomicallyInLayout unexpectedly succeeded")
	}
	if got := mustReadFile(t, target); got != "original\n" {
		t.Fatalf("destination replaced after replacement failure: %q", got)
	}
	if temporary, err := filepath.Glob(filepath.Join(filepath.Dir(target), ".tronador-*")); err != nil || len(temporary) != 0 {
		t.Fatalf("temporary files = %v, %v", temporary, err)
	}
}

func TestInitExistingDevelopRejectsInvalidConfiguredPrimary(t *testing.T) {
	dir := workflowFixture(t)
	git := fakeGit(t, `case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"show-ref --verify --quiet refs/remotes/origin/develop") exit 0;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, MainBranch: "bad branch", Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow}); err == nil || !strings.Contains(err.Error(), "invalid configured main branch") {
		t.Fatalf("invalid override error = %v", err)
	}
}

func TestInitExistingDevelopRejectsWrongCurrentConfiguredPrimary(t *testing.T) {
	dir := workflowFixture(t)
	git := fakeGit(t, `case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"branch --show-current") echo main;;
"show-ref --verify --quiet refs/remotes/origin/develop") exit 0;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, MainBranch: "primary", Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow}); err == nil || !strings.Contains(err.Error(), `check out configured main branch "primary"`) {
		t.Fatalf("wrong current override error = %v", err)
	}
}

func TestInitExistingDevelopRejectsDivergentConfiguredPrimary(t *testing.T) {
	dir := workflowFixture(t)
	git := fakeGit(t, `case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"branch --show-current") echo primary;;
"rev-parse HEAD") echo local;;
"rev-parse refs/remotes/origin/primary") echo remote;;
"show-ref --verify --quiet refs/remotes/origin/develop") exit 0;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, MainBranch: "primary", Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow}); err == nil || !strings.Contains(err.Error(), "primary is not equal to origin/primary") {
		t.Fatalf("divergent override error = %v", err)
	}
}

func TestInitExistingDevelopAllowsMatchingConfiguredPrimary(t *testing.T) {
	dir := workflowFixture(t)
	git := fakeGit(t, `case "$*" in
"status --porcelain") exit 0;;
"remote get-url origin") echo https://example.test/acme/repo.git;;
"fetch origin --prune") exit 0;;
"branch --show-current") echo primary;;
"rev-parse HEAD"|"rev-parse refs/remotes/origin/primary") echo same;;
"show-ref --verify --quiet refs/remotes/origin/develop") exit 0;;
"show-ref --verify --quiet refs/heads/develop") exit 1;;
*) echo "unexpected git $*" >&2; exit 2;; esac`)
	r, err := NewRunner(Options{WorkDir: dir, GitPath: git, MainBranch: "primary", Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Init(context.Background(), InitOptions{WayOfWork: WayOfWorkGitFlow})
	if err != nil {
		t.Fatal(err)
	}
	if result.DevelopCreated {
		t.Fatalf("existing remote develop unexpectedly reported created: %+v", result)
	}
}
