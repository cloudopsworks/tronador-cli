package versions

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
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
"branch --show-current") echo main;;
"rev-parse HEAD"|"rev-parse refs/remotes/origin/main"|"rev-parse refs/heads/develop") echo abc123;;
"checkout -b develop main"|"push --set-upstream origin develop") exit 0;;
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
	for _, want := range []string{"status --porcelain", "remote get-url origin", "checkout -b develop main", "push --set-upstream origin develop"} {
		if !strings.Contains(calls, want) {
			t.Fatalf("calls missing %q:\n%s", want, calls)
		}
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
"checkout -b develop primary"|"push --set-upstream origin develop") exit 0;;
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
	if !result.DevelopCreated || !strings.Contains(mustReadFile(t, log), "checkout -b develop primary") {
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
"branch --show-current") echo main;;
"rev-parse HEAD"|"rev-parse refs/remotes/origin/main"|"rev-parse refs/heads/develop") echo abc123;;
"show-ref --verify --quiet refs/heads/develop") exit 0;;
"push --set-upstream origin develop") exit 0;;
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
	if !result.DevelopCreated || !strings.Contains(mustReadFile(t, log), "push --set-upstream origin develop") {
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
