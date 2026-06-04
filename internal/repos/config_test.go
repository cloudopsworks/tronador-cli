package repos

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
