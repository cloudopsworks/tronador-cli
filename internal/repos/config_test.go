package repos

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDefaultConfigIncludesFutureMigrationSlots(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	for _, version := range []string{"510", "5.11", "v5.12"} {
		if _, ok := cfg.FindMigrationPlan(version); !ok {
			t.Fatalf("expected migration plan for %s", version)
		}
	}
	if _, ok := cfg.FindTemplate("go"); !ok {
		t.Fatalf("expected go template in default config")
	}
}

func TestTerraformModuleTemplateConfigMatchesMakefileVersionedBehavior(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	tmpl, ok := cfg.FindTemplate("terraform-module")
	if !ok {
		t.Fatalf("expected terraform-module template in default config")
	}
	if !tmpl.Versioned {
		t.Fatalf("terraform-module template must be versioned so upgrades copy .cloudopsworks/_VERSION")
	}
	if tmpl.CICD {
		t.Fatalf("terraform-module template must not enable CICD footer updates")
	}
	if !tmpl.Boilerplate {
		t.Fatalf("terraform-module template must keep boilerplate handling enabled")
	}
}

func TestDetectActiveTemplateUsesBlueprintLayout(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v5.10.1")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", ".golang"), "")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	tmpl, state, err := runner.ActiveTemplate()
	if err != nil {
		t.Fatalf("ActiveTemplate() error = %v", err)
	}
	if tmpl.Name != "go" {
		t.Fatalf("template = %s, want go", tmpl.Name)
	}
	if state.BlueprintPath != ".cloudopsworks" || state.Pre510 {
		t.Fatalf("state = %+v, want .cloudopsworks non-pre510", state)
	}
}

func TestMigrateGo510MovesPre510Layout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	mustWrite(t, filepath.Join(dir, ".github", "_VERSION"), "v5.9.9")
	mustWrite(t, filepath.Join(dir, ".github", ".golang"), "")
	mustWrite(t, filepath.Join(dir, ".github", "cloudopsworks-ci.yaml"), "name: ci")
	mustWrite(t, filepath.Join(dir, ".github", "vars", "inputs.yaml"), "x: y")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Migrate("go", "510"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	for _, path := range []string{
		".cloudopsworks/_VERSION",
		".cloudopsworks/.golang",
		".cloudopsworks/cloudopsworks-ci.yaml",
		".cloudopsworks/vars/inputs.yaml",
	} {
		if !exists(filepath.Join(dir, path)) {
			t.Fatalf("expected %s to exist after migration", path)
		}
	}
	if exists(filepath.Join(dir, ".github", "_VERSION")) {
		t.Fatalf("expected .github/_VERSION to move")
	}
}

func TestLatestMatchingTagSortsSemver(t *testing.T) {
	tags := []string{"v5.10.2", "v5.10.10", "v5.10.9", "v5.11.0"}
	got := latestMatchingTag(tags, regexp.MustCompile(`^v?5\.10\.[0-9]+$`))
	if got != "v5.10.10" {
		t.Fatalf("latestMatchingTag() = %s, want v5.10.10", got)
	}
}

func TestCurrentMinorTagPatternMirrorsMakefileDefaultUpgrade(t *testing.T) {
	major, minor, err := parseMajorMinor("v5.10.1")
	if err != nil {
		t.Fatalf("parseMajorMinor() error = %v", err)
	}
	tags := []string{"v5.9.9", "v5.10.2", "v5.10.10", "v5.11.0", "v6.0.0"}
	got := latestMatchingTag(tags, currentMinorTagPattern(major, minor))
	if got != "v5.10.10" {
		t.Fatalf("default upgrade tag = %s, want v5.10.10 from current major/minor", got)
	}
}

func TestResolveDefaultUpgradeTargetUsesCurrentMinorLine(t *testing.T) {
	runner := &Runner{
		Opts:         Options{WorkDir: t.TempDir(), Stdout: io.Discard, Stderr: io.Discard},
		githubClient: &fakeGitHubClient{tags: []string{"v5.10.2", "v5.10.10", "v5.11.3", "v6.0.0"}},
	}
	tmpl := Template{Repository: "cloudopsworks/go-app-template"}
	state := RepositoryState{Version: "v5.10.1"}

	got, major, minor, err := runner.resolveDefaultUpgradeTarget(context.Background(), tmpl, state)
	if err != nil {
		t.Fatalf("resolveDefaultUpgradeTarget() error = %v", err)
	}
	if got != "v5.10.10" || major != "5" || minor != "10" {
		t.Fatalf("default target = %s (%s.%s), want v5.10.10 (5.10)", got, major, minor)
	}
}

func TestResolveExplicitUpgradeTargetMajorUsesCurrentMajorLine(t *testing.T) {
	runner := &Runner{
		Opts:         Options{WorkDir: t.TempDir(), Stdout: io.Discard, Stderr: io.Discard},
		githubClient: &fakeGitHubClient{tags: []string{"v5.10.10", "v5.11.3", "v5.12.1", "v6.0.0"}},
	}
	tmpl := Template{Repository: "cloudopsworks/go-app-template"}
	state := RepositoryState{Version: "v5.10.1"}

	got, err := runner.resolveExplicitUpgradeTarget(context.Background(), tmpl, state, "major")
	if err != nil {
		t.Fatalf("resolveExplicitUpgradeTarget(major) error = %v", err)
	}
	if got != "v5.12.1" {
		t.Fatalf("major target = %s, want latest same-major tag v5.12.1", got)
	}
}

func TestResolveExplicitUpgradeTargetMasterBypassesVersionLookup(t *testing.T) {
	client := &fakeGitHubClient{err: errors.New("tag lookup should not run")}
	runner := &Runner{
		Opts:         Options{WorkDir: t.TempDir(), Stdout: io.Discard, Stderr: io.Discard},
		githubClient: client,
	}
	tmpl := Template{Repository: "cloudopsworks/go-app-template"}
	state := RepositoryState{Version: "v5.10.1"}

	got, err := runner.resolveExplicitUpgradeTarget(context.Background(), tmpl, state, "master")
	if err != nil {
		t.Fatalf("resolveExplicitUpgradeTarget(master) error = %v", err)
	}
	if got != "master" {
		t.Fatalf("master target = %s, want master", got)
	}
	if client.repository != "" {
		t.Fatalf("master target queried tags for %s; want no version lookup", client.repository)
	}
}

func TestAvailableCleansStaleTemplateWorkspace(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v5.10.1")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", ".golang"), "")
	mustWrite(t, filepath.Join(dir, ".template", "stale.txt"), "stale")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	runner.githubClient = &fakeGitHubClient{tags: []string{"v5.10.10", "v5.11.1"}}

	if err := runner.Available(context.Background()); err != nil {
		t.Fatalf("Available() error = %v", err)
	}
	if exists(filepath.Join(dir, ".template")) {
		t.Fatalf("Available() left stale .template workspace behind")
	}
}

func TestUpgradeVersionCleansTemplateWorkspaceAfterResolutionFailure(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v5.10.1")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", ".golang"), "")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	runner.gitClient = cloneWritingGitClient{}
	runner.githubClient = &fakeGitHubClient{tags: []string{"v6.0.0"}}

	if err := runner.UpgradeVersion(context.Background(), "major"); err == nil {
		t.Fatalf("UpgradeVersion(major) succeeded; want no same-major tag error")
	}
	if exists(filepath.Join(dir, ".template")) {
		t.Fatalf("UpgradeVersion() left .template workspace after failure")
	}
}

func TestTerraformModuleUpgradeRouteCopiesTemplateVersion(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")

	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v1.6.27\n")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", ".terraform-module"), "")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "build.yml"), "name: new\n")
	mustWrite(t, filepath.Join(dir, ".template", "Makefile"), "new\n")
	mustWrite(t, filepath.Join(dir, ".template", ".gitignore"), "new\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v1.6.31\n")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	tmpl, state, err := runner.ActiveTemplate()
	if err != nil {
		t.Fatalf("ActiveTemplate() error = %v", err)
	}
	templateVersion, err := runner.EvalTemplateVersion()
	if err != nil {
		t.Fatalf("EvalTemplateVersion() error = %v", err)
	}
	if tmpl.Versioned {
		err = runner.applyVersionedTemplate(tmpl, state, templateVersion)
	} else {
		err = runner.applyUnversionedTemplate()
	}
	if err != nil {
		t.Fatalf("apply template route error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "_VERSION")); got != "v1.6.31\n" {
		t.Fatalf(".cloudopsworks/_VERSION = %q, want template version v1.6.31", got)
	}
}

func TestTemplateDryRunSkipsWhenMarkerMissing(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v5.10.1")
	runner, err := NewRunner(Options{WorkDir: dir, DryRun: true, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Template(context.Background(), "go"); err != nil {
		t.Fatalf("Template() error = %v", err)
	}
}

func TestCopyIssueTemplatesOnlyCopiesMissingImplementationForms(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	mustWrite(t, filepath.Join(dir, ".template", ".github", "ISSUE_TEMPLATE", "config.yml"), "blank_issues_enabled: false\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "ISSUE_TEMPLATE", "01_bug_report.yml.disabled"), "name: Bug\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "ISSUE_TEMPLATE", "20_custom.yml"), "name: Upstream custom\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "ISSUE_TEMPLATE", "98_template_bug_report.yml"), "name: Template bug\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "ISSUE_TEMPLATE", "99_template_feature_request.yml"), "name: Template feature\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "PULL_REQUEST_TEMPLATE.md"), "## Summary\n")
	mustWrite(t, filepath.Join(dir, ".github", "ISSUE_TEMPLATE", "config.yml"), "blank_issues_enabled: true\n")
	mustWrite(t, filepath.Join(dir, ".github", "ISSUE_TEMPLATE", "20_custom.yml"), "name: Local custom\n")
	mustWrite(t, filepath.Join(dir, ".github", "ISSUE_TEMPLATE", "98_existing.yml"), "name: Existing reserved\n")
	mustWrite(t, filepath.Join(dir, ".github", "PULL_REQUEST_TEMPLATE.md"), "## Local PR\n")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.copyIssueTemplatesIfExists(context.Background()); err != nil {
		t.Fatalf("copyIssueTemplatesIfExists() error = %v", err)
	}
	if err := runner.copyPullRequestTemplateIfExists(context.Background()); err != nil {
		t.Fatalf("copyPullRequestTemplateIfExists() error = %v", err)
	}

	for _, path := range []string{
		".github/ISSUE_TEMPLATE/01_bug_report.yml",
		".github/ISSUE_TEMPLATE/config.yml",
		".github/ISSUE_TEMPLATE/20_custom.yml",
		".github/ISSUE_TEMPLATE/98_existing.yml",
		".github/PULL_REQUEST_TEMPLATE.md",
	} {
		if !exists(filepath.Join(dir, path)) {
			t.Fatalf("expected %s to exist after issue template copy", path)
		}
	}
	for _, path := range []string{
		".github/ISSUE_TEMPLATE/98_template_bug_report.yml",
		".github/ISSUE_TEMPLATE/99_template_feature_request.yml",
		".github/ISSUE_TEMPLATE/01_bug_report.yml.disabled",
	} {
		if exists(filepath.Join(dir, path)) {
			t.Fatalf("expected %s not to be copied", path)
		}
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "ISSUE_TEMPLATE", "config.yml")); got != "blank_issues_enabled: true\n" {
		t.Fatalf("config.yml was overwritten: %q", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "ISSUE_TEMPLATE", "20_custom.yml")); got != "name: Local custom\n" {
		t.Fatalf("20_custom.yml was overwritten: %q", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "PULL_REQUEST_TEMPLATE.md")); got != "## Local PR\n" {
		t.Fatalf("PULL_REQUEST_TEMPLATE.md was overwritten: %q", got)
	}
}

func TestVersionedTemplateCommitIncludesRootTemplateFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "tronador-cli-test@example.com")
	runGit(t, dir, "config", "user.name", "tronador-cli test")

	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v5.10.1\n")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", ".golang"), "")
	mustWrite(t, filepath.Join(dir, ".github", "workflows", "build.yml"), "name: old\n")
	mustWrite(t, filepath.Join(dir, "Makefile"), "old\n")
	mustWrite(t, filepath.Join(dir, ".gitignore"), "old\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "build.yml"), "name: new\n")
	mustWrite(t, filepath.Join(dir, ".template", "Makefile"), "new\n")
	mustWrite(t, filepath.Join(dir, ".template", ".gitignore"), "new\n")
	mustWrite(t, filepath.Join(dir, ".template", "AGENTS.md"), "agents\n")
	mustWrite(t, filepath.Join(dir, ".template", "CLAUDE.md"), "claude\n")
	mustWrite(t, filepath.Join(dir, ".template", "README-TEMPLATE.md"), "readme template\n")
	mustWrite(t, filepath.Join(dir, ".template", ".helmignore"), "helm\n")
	mustWrite(t, filepath.Join(dir, ".template", ".dockerignore"), "docker\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v5.10.2\n")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	tmpl := Template{Name: "go", Versioned: true}
	state := RepositoryState{
		WorkDir:       dir,
		BlueprintPath: ".cloudopsworks",
		VersionFile:   ".cloudopsworks/_VERSION",
		Version:       "v5.10.1",
	}
	if err := runner.applyVersionedTemplate(tmpl, state, "v5.10.2"); err != nil {
		t.Fatalf("applyVersionedTemplate() error = %v", err)
	}
	if err := runner.Push(context.Background(), tmpl, state); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	tracked := runGit(t, dir, "ls-tree", "-r", "--name-only", "HEAD")
	for _, path := range []string{"AGENTS.md", "CLAUDE.md", "README-TEMPLATE.md", ".helmignore", ".dockerignore"} {
		if !strings.Contains(tracked, path+"\n") {
			t.Fatalf("HEAD does not track copied template root file %s; tracked files:\n%s", path, tracked)
		}
	}
	status := runGit(t, dir, "status", "--short")
	for _, path := range []string{"AGENTS.md", "CLAUDE.md", "README-TEMPLATE.md", ".helmignore", ".dockerignore"} {
		if strings.Contains(status, "?? "+path) {
			t.Fatalf("copied template root file %s was left untracked; status:\n%s", path, status)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

type cloneWritingGitClient struct{}

func (cloneWritingGitClient) Clone(_ context.Context, _ string, destination string) error {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(destination, "README.md"), []byte("template\n"), 0o644)
}

func (cloneWritingGitClient) RemoveRemote(context.Context, string, string) error { return nil }

func (cloneWritingGitClient) Checkout(context.Context, string, string) (string, error) {
	return "", errors.New("unused")
}

func (cloneWritingGitClient) OriginOwnerRepo(context.Context, string) (string, string, error) {
	return "", "", errors.New("unused")
}
