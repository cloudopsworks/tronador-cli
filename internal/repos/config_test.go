package repos

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestArgoCDTemplateConfigSupportsWorkInProgressRepository(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	tmpl, ok := cfg.FindTemplate("argocd")
	if !ok {
		t.Fatalf("expected argocd template in default config")
	}
	if tmpl.Marker != ".argocd" {
		t.Fatalf("argocd marker = %q, want .argocd", tmpl.Marker)
	}
	if tmpl.Repository != "cloudopsworks/argocd-project-template" {
		t.Fatalf("argocd repository = %q, want cloudopsworks/argocd-project-template", tmpl.Repository)
	}
	if !tmpl.Merge || !tmpl.Versioned {
		t.Fatalf("argocd template must support versioned upgrades: %+v", tmpl)
	}
	if tmpl.CICD || tmpl.Boilerplate {
		t.Fatalf("argocd template must not enable CICD footer or boilerplate handling: %+v", tmpl)
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

func TestDetectActiveArgoCDTemplate(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v0.1.0")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", ".argocd"), "")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	tmpl, state, err := runner.ActiveTemplate()
	if err != nil {
		t.Fatalf("ActiveTemplate() error = %v", err)
	}
	if tmpl.Name != "argocd" {
		t.Fatalf("template = %s, want argocd", tmpl.Name)
	}
	if state.Version != "v0.1.0" || state.Pre510 {
		t.Fatalf("state = %+v, want versioned .cloudopsworks layout", state)
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
	mustWrite(t, filepath.Join(dir, ".github", "auto-assign.yml"), "reviewers: []")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Migrate("go", "510"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	for _, path := range []string{
		".cloudopsworks/.golang",
		".cloudopsworks/vars/inputs.yaml",
	} {
		if !exists(filepath.Join(dir, path)) {
			t.Fatalf("expected %s to exist after migration", path)
		}
	}
	if !exists(filepath.Join(dir, ".github", "_VERSION")) {
		t.Fatal("migration must defer legacy release marker removal to Stack")
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
	mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "build.yml"), "uses: cloudopsworks/blueprints/cd/checkout@v5.10\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "dependabot.yml"), "updates: []\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "secret_scanning.yml"), "secret-scanning: enabled\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "codeql", "codeql-config.yml"), "name: default\n")
	mustWrite(t, filepath.Join(dir, ".template", "Makefile"), "new\n")
	mustWrite(t, filepath.Join(dir, ".template", ".gitignore"), "new\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v1.6.31\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "gitversion_trunkbased.yaml"), "mode: ContinuousDeployment\n")

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
	if got := mustRead(t, filepath.Join(dir, ".github", "dependabot.yml")); got != "updates: []\n" {
		t.Fatalf("dependabot config = %q, want template configuration", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "secret_scanning.yml")); got != "secret-scanning: enabled\n" {
		t.Fatalf("secret scanning config = %q, want template configuration", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "codeql", "codeql-config.yml")); got != "name: default\n" {
		t.Fatalf("CodeQL config = %q, want template configuration", got)
	}
	trunkBasedConfig := filepath.Join(dir, ".cloudopsworks", "gitversion_trunkbased.yaml")
	if got := mustRead(t, trunkBasedConfig); got != "mode: ContinuousDeployment\n" {
		t.Fatalf("trunk-based GitVersion config = %q, want template configuration", got)
	}
}

func TestArgoCDUpgradeRouteCopiesVersionedTemplate(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")

	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v0.1.0\n")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", ".argocd"), "")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "scan.yml"), "name: scan\n")
	mustWrite(t, filepath.Join(dir, ".template", ".gitignore"), "*.tmp\n")
	mustWrite(t, filepath.Join(dir, ".template", "Makefile"), "-include .tronador\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v0.1.1\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", ".argocd"), "")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	tmpl, state, err := runner.ActiveTemplate()
	if err != nil {
		t.Fatalf("ActiveTemplate() error = %v", err)
	}
	if err := runner.applyVersionedTemplate(tmpl, state, "v0.1.1"); err != nil {
		t.Fatalf("applyVersionedTemplate() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "_VERSION")); got != "v0.1.1\n" {
		t.Fatalf(".cloudopsworks/_VERSION = %q, want v0.1.1", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "workflows", "scan.yml")); got != "name: scan\n" {
		t.Fatalf("workflow = %q, want ArgoCD template workflow", got)
	}
	if got := mustRead(t, filepath.Join(dir, "Makefile")); got != "-include .tronador\n" {
		t.Fatalf("Makefile = %q, want ArgoCD template Makefile", got)
	}
	staged := runGit(t, dir, "diff", "--cached", "--name-only")
	if !strings.Contains(staged, "Makefile\n") {
		t.Fatalf("new ArgoCD Makefile was not staged: %s", staged)
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

func TestCopyDependabotIfExistsCopiesMissingConfiguration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "dependabot.yml"), "updates:\n  - package-ecosystem: gomod\n")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.copyDependabotIfExists(context.Background()); err != nil {
		t.Fatalf("copyDependabotIfExists() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "dependabot.yml")); got != "updates:\n  - package-ecosystem: gomod\n" {
		t.Fatalf("copied dependabot config = %q", got)
	}
	tracked := runGit(t, dir, "diff", "--cached", "--name-only")
	if !strings.Contains(tracked, ".github/dependabot.yml\n") {
		t.Fatalf("copied dependabot config was not staged: %s", tracked)
	}
}

func TestCopyDependabotIfExistsPreservesExistingConfiguration(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".template", ".github", "dependabot.yml"), "template\n")
	mustWrite(t, filepath.Join(dir, ".github", "dependabot.yml"), "local\n\x00")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.copyDependabotIfExists(context.Background()); err != nil {
		t.Fatalf("copyDependabotIfExists() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "dependabot.yml")); got != "local\n\x00" {
		t.Fatalf("existing dependabot config changed: %q", got)
	}
}

func TestCopyGitHubSecurityConfigsIfExistsCopiesMissingConfigurations(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "secret_scanning.yml"), "secret-scanning: template\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "codeql", "codeql-config.yml"), "name: template\n")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.copyGitHubSecurityConfigsIfExists(context.Background()); err != nil {
		t.Fatalf("copyGitHubSecurityConfigsIfExists() error = %v", err)
	}

	wants := map[string]string{
		".github/secret_scanning.yml":      "secret-scanning: template\n",
		".github/codeql/codeql-config.yml": "name: template\n",
	}
	tracked := runGit(t, dir, "diff", "--cached", "--name-only")
	for path, want := range wants {
		if got := mustRead(t, filepath.Join(dir, filepath.FromSlash(path))); got != want {
			t.Fatalf("copied %s = %q, want %q", path, got, want)
		}
		if !strings.Contains(tracked, path+"\n") {
			t.Fatalf("copied %s was not staged: %s", path, tracked)
		}
	}
}

func TestCopyGitHubSecurityConfigsIfExistsPreservesExistingConfigurations(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".template", ".github", "secret_scanning.yml"), "template secret scanning\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "codeql", "codeql-config.yml"), "template codeql\n")
	mustWrite(t, filepath.Join(dir, ".github", "secret_scanning.yml"), "local secret scanning\n")
	mustWrite(t, filepath.Join(dir, ".github", "codeql", "codeql-config.yml"), "local codeql\n")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.copyGitHubSecurityConfigsIfExists(context.Background()); err != nil {
		t.Fatalf("copyGitHubSecurityConfigsIfExists() error = %v", err)
	}

	if got := mustRead(t, filepath.Join(dir, ".github", "secret_scanning.yml")); got != "local secret scanning\n" {
		t.Fatalf("existing secret scanning config changed: %q", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "codeql", "codeql-config.yml")); got != "local codeql\n" {
		t.Fatalf("existing CodeQL config changed: %q", got)
	}
}

func TestCopyDependabotIfExistsSkipsMissingTemplateConfiguration(t *testing.T) {
	dir := t.TempDir()
	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.copyDependabotIfExists(context.Background()); err != nil {
		t.Fatalf("copyDependabotIfExists() error = %v", err)
	}
	if exists(filepath.Join(dir, ".github", "dependabot.yml")) {
		t.Fatal("dependabot config was created without a template source")
	}
}

func TestCopyDependabotIfExistsReportsCopyFailureWithoutPartialOutput(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".template", ".github", "dependabot.yml"), "template\n")
	if err := os.WriteFile(filepath.Join(dir, ".github"), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write blocking .github path: %v", err)
	}

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	err = runner.copyDependabotIfExists(context.Background())
	if err == nil || !strings.Contains(err.Error(), "copy .github/dependabot.yml") {
		t.Fatalf("copyDependabotIfExists() error = %v, want copy failure", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".github")); got != "not a directory" {
		t.Fatalf("blocking path changed after copy failure: %q", got)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".github", ".tronador-copy-*"))
	if err != nil {
		t.Fatalf("glob temporary files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("copy failure left temporary files: %v", matches)
	}
}

func TestCopyAutoAssignIfExistsCopiesMissingConfigurations(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "auto-assign.yml"), "current\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "auto-assign.yml"), "legacy\n")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.copyAutoAssignIfExists(context.Background()); err != nil {
		t.Fatalf("copyAutoAssignIfExists() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "auto-assign.yml")); got != "current\n" {
		t.Fatalf("current auto-assign config = %q", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "auto-assign.yml")); got != "legacy\n" {
		t.Fatalf("legacy auto-assign config = %q", got)
	}
	tracked := runGit(t, dir, "diff", "--cached", "--name-only")
	for _, path := range []string{".cloudopsworks/auto-assign.yml", ".github/auto-assign.yml"} {
		if !strings.Contains(tracked, path+"\n") {
			t.Fatalf("copied %s was not staged: %s", path, tracked)
		}
	}
}

func TestCopyAutoAssignIfExistsPreservesExistingConfigurations(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "auto-assign.yml"), "template current\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "auto-assign.yml"), "template legacy\n")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "auto-assign.yml"), "user current\n\x00")
	mustWrite(t, filepath.Join(dir, ".github", "auto-assign.yml"), "user legacy\n\x00")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.copyAutoAssignIfExists(context.Background()); err != nil {
		t.Fatalf("copyAutoAssignIfExists() error = %v", err)
	}
	for _, test := range []struct {
		path string
		want string
	}{
		{path: ".cloudopsworks/auto-assign.yml", want: "user current\n\x00"},
		{path: ".github/auto-assign.yml", want: "user legacy\n\x00"},
	} {
		if got := mustRead(t, filepath.Join(dir, test.path)); got != test.want {
			t.Fatalf("%s changed: %q", test.path, got)
		}
	}
}

func TestMergeGitignoreReplacesManagedBlockAndPreservesUserBytes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	mustWrite(t, filepath.Join(dir, ".gitignore"), "before\r\n"+string(formatGitignoreManagedBlock([]byte("old\n")))+"after\n")
	mustWrite(t, filepath.Join(dir, ".template", ".gitignore"), "new\n")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.mergeGitignoreIfExists(context.Background()); err != nil {
		t.Fatalf("mergeGitignoreIfExists() error = %v", err)
	}
	want := "before\r\n" + string(formatGitignoreManagedBlock([]byte("new\n"))) + "after\n"
	if got := mustRead(t, filepath.Join(dir, ".gitignore")); got != want {
		t.Fatalf("merged .gitignore = %q, want %q", got, want)
	}
	tracked := runGit(t, dir, "diff", "--cached", "--name-only")
	if !strings.Contains(tracked, ".gitignore\n") {
		t.Fatalf("changed .gitignore was not staged: %s", tracked)
	}
}

func TestMergeGitignoreAcceptsMarkedTemplateAndIsIdempotent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	oldBlock := formatGitignoreManagedBlock([]byte("old\n"))
	mustWrite(t, filepath.Join(dir, ".gitignore"), "before\n"+string(oldBlock)+"after\n")
	mustWrite(t, filepath.Join(dir, ".template", ".gitignore"), string(formatGitignoreManagedBlock([]byte("new\n"))))

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.mergeGitignoreIfExists(context.Background()); err != nil {
		t.Fatalf("first mergeGitignoreIfExists() error = %v", err)
	}
	want := "before\n" + string(formatGitignoreManagedBlock([]byte("new\n"))) + "after\n"
	if got := mustRead(t, filepath.Join(dir, ".gitignore")); got != want {
		t.Fatalf("marked template merge = %q, want %q", got, want)
	}
	if err := runner.mergeGitignoreIfExists(context.Background()); err != nil {
		t.Fatalf("second mergeGitignoreIfExists() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".gitignore")); got != want {
		t.Fatalf("repeated marked template merge = %q, want %q", got, want)
	}
}

func TestMergeGitignoreAppendsManagedBlockToUnmarkedFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	mustWrite(t, filepath.Join(dir, ".gitignore"), "user custom content")
	mustWrite(t, filepath.Join(dir, ".template", ".gitignore"), "template defaults\n")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.mergeGitignoreIfExists(context.Background()); err != nil {
		t.Fatalf("mergeGitignoreIfExists() error = %v", err)
	}

	if got := mustRead(t, filepath.Join(dir, ".gitignore")); !strings.HasPrefix(got, "user custom content") || !strings.Contains(got, gitignoreManagedStartMarker) {
		t.Fatalf("merged .gitignore did not preserve and append content: %q", got)
	}
}

func TestMergeGitignorePreservesMalformedMarkers(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	existing := "user\n" + gitignoreManagedStartMarker + "\npartial\n"
	mustWrite(t, filepath.Join(dir, ".gitignore"), existing)
	mustWrite(t, filepath.Join(dir, ".template", ".gitignore"), "template\n")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.mergeGitignoreIfExists(context.Background()); err != nil {
		t.Fatalf("mergeGitignoreIfExists() error = %v", err)
	}
	want := existing + string(formatGitignoreManagedBlock([]byte("template\n")))
	if got := mustRead(t, filepath.Join(dir, ".gitignore")); got != want {
		t.Fatalf("malformed .gitignore = %q, want %q", got, want)
	}
	if err := runner.mergeGitignoreIfExists(context.Background()); err != nil {
		t.Fatalf("second mergeGitignoreIfExists() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".gitignore")); got != want {
		t.Fatalf("repeated malformed .gitignore = %q, want %q", got, want)
	}
}

func TestMergeGitignoreTreatsEmbeddedMarkersAsUserContent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	existing := "user # " + gitignoreManagedStartMarker + "\nuser content\n"
	mustWrite(t, filepath.Join(dir, ".gitignore"), existing)
	mustWrite(t, filepath.Join(dir, ".template", ".gitignore"), "template\n")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.mergeGitignoreIfExists(context.Background()); err != nil {
		t.Fatalf("mergeGitignoreIfExists() error = %v", err)
	}
	want := existing + string(formatGitignoreManagedBlock([]byte("template\n")))
	if got := mustRead(t, filepath.Join(dir, ".gitignore")); got != want {
		t.Fatalf("embedded marker content was not preserved: %q", got)
	}
}

func TestMergeGitignoreRefusesSymlinkDestination(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.gitignore")
	mustWrite(t, target, "user\n")
	if err := os.Symlink(target, filepath.Join(dir, ".gitignore")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	mustWrite(t, filepath.Join(dir, ".template", ".gitignore"), "template\n")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.mergeGitignoreIfExists(context.Background()); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("mergeGitignoreIfExists() error = %v, want symlink refusal", err)
	}
	if got := mustRead(t, target); got != "user\n" {
		t.Fatalf("symlink target changed: %q", got)
	}
}

func TestMergeGitignoreNoOpLeavesFileUnstaged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "tronador-cli-test@example.com")
	runGit(t, dir, "config", "user.name", "tronador-cli test")
	existing := "user\n" + string(formatGitignoreManagedBlock([]byte("template\n"))) + "tail\n"
	mustWrite(t, filepath.Join(dir, ".gitignore"), existing)
	runGit(t, dir, "add", ".gitignore")
	runGit(t, dir, "commit", "-m", "initial")
	mustWrite(t, filepath.Join(dir, ".template", ".gitignore"), "template\n")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.mergeGitignoreIfExists(context.Background()); err != nil {
		t.Fatalf("mergeGitignoreIfExists() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".gitignore")); got != existing {
		t.Fatalf("no-op merge changed .gitignore: %q", got)
	}
	if status := runGit(t, dir, "status", "--short", "--", ".gitignore"); status != "" {
		t.Fatalf("no-op merge changed Git status: %s", status)
	}
}

func TestGitAddCloudopsworksExcludesPreservedAutoAssign(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "auto-assign.yml"), "user\n")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v1.0.0\n")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.gitAddCloudopsworks(context.Background()); err != nil {
		t.Fatalf("gitAddCloudopsworks() error = %v", err)
	}
	tracked := runGit(t, dir, "diff", "--cached", "--name-only")
	if strings.Contains(tracked, ".cloudopsworks/auto-assign.yml\n") {
		t.Fatalf("preserved auto-assign configuration was staged: %s", tracked)
	}
	if !strings.Contains(tracked, ".cloudopsworks/_VERSION\n") {
		t.Fatalf("upgrade-owned cloudopsworks file was not staged: %s", tracked)
	}
}

func TestUnversionedTemplateUpgradeRouteCopiesMissingGitHubConfigurations(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "build.yml"), "name: build\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "dependabot.yml"), "updates: []\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "secret_scanning.yml"), "secret-scanning: enabled\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "codeql", "codeql-config.yml"), "name: default\n")
	mustWrite(t, filepath.Join(dir, ".template", "Makefile"), "all:\n\t@true\n")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.applyUnversionedTemplate(); err != nil {
		t.Fatalf("applyUnversionedTemplate() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "dependabot.yml")); got != "updates: []\n" {
		t.Fatalf("dependabot config = %q", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "secret_scanning.yml")); got != "secret-scanning: enabled\n" {
		t.Fatalf("secret scanning config = %q", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "codeql", "codeql-config.yml")); got != "name: default\n" {
		t.Fatalf("CodeQL config = %q", got)
	}
}

func TestMigrateTerragrunt510MovesRecursiveConfigurationAndStagesEffects(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "tronador-cli-test@example.com")
	runGit(t, dir, "config", "user.name", "tronador-cli test")
	for path, content := range map[string]string{
		".github/.iac":                        "",
		".github/.inputs_cicd":                "",
		".github/LICENSE":                     "license\n",
		".github/vars/custom/local-only.yaml": "local: true\n",
		".github/vars/custom/script.txt":      "preserve\n",
		".github/values/prod/values.yaml":     "replicas: 2\n",
		".github/cloudopsworks-ci.yaml":       "pipeline: legacy\n",
		".github/gitversion_trunkbased.yaml":  "mode: Mainline\n",
		".github/_VERSION":                    "v5.9.0\n",
	} {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(path)), content)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Migrate("terragrunt", "510"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	for _, path := range []string{
		".cloudopsworks/.iac",
		".cloudopsworks/.inputs_cicd",
		".cloudopsworks/LICENSE",
		".cloudopsworks/vars/custom/local-only.yaml",
		".cloudopsworks/vars/custom/script.txt",
		".cloudopsworks/values/prod/values.yaml",
	} {
		if !exists(filepath.Join(dir, filepath.FromSlash(path))) {
			t.Fatalf("migration did not move %s", path)
		}
	}
	staged := runGit(t, dir, "diff", "--cached", "--name-status", "--no-renames")
	for _, effect := range []string{
		"D\t.github/vars/custom/local-only.yaml",
		"A\t.cloudopsworks/vars/custom/local-only.yaml",
		"D\t.github/vars/custom/script.txt",
		"A\t.cloudopsworks/vars/custom/script.txt",
		"D\t.github/.iac",
		"A\t.cloudopsworks/.iac",
		"D\t.github/.inputs_cicd",
		"A\t.cloudopsworks/.inputs_cicd",
	} {
		if !strings.Contains(staged, effect+"\n") {
			t.Fatalf("migration effect %s was not staged:\n%s", effect, staged)
		}
	}
}

func TestMigrateTerraform510MovesLegacyConfigurationAndProvider(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	for path, content := range map[string]string{
		".github/.terraform-module":           "",
		".github/.provider":                   "aws\n",
		".github/vars/custom/local-only.yaml": "local: true\n",
		".github/values/prod/values.yaml":     "replicas: 2\n",
		".github/cloudopsworks-ci.yaml":       "pipeline: legacy\n",
		".github/gitversion_trunkbased.yaml":  "mode: Mainline\n",
		".github/LICENSE":                     "license\n",
		".github/Makefile":                    "legacy\n",
		".github/labeler.yml":                 "labels: {}\n",
		".github/auto-assign.yml":             "reviewers: []\n",
		".github/_VERSION":                    "v5.9.0\n",
	} {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(path)), content)
	}
	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Migrate("terraform-module", "510"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	for _, path := range []string{
		".cloudopsworks/.terraform-module",
		".cloudopsworks/.provider",
		".cloudopsworks/vars/custom/local-only.yaml",
		".cloudopsworks/values/prod/values.yaml",
		".cloudopsworks/LICENSE",
		".cloudopsworks/Makefile",
	} {
		if !exists(filepath.Join(dir, filepath.FromSlash(path))) {
			t.Fatalf("migration did not move %s", path)
		}
	}
}

func TestStackV510MigrationCommitsOnlyUpgradeEffects(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "tronador-cli-test@example.com")
	runGit(t, dir, "config", "user.name", "tronador-cli test")
	for path, content := range map[string]string{
		".github/_VERSION":                "v5.9.0\n",
		".github/.golang":                 "",
		".github/workflows/build.yml":     "name: old\n",
		".github/workflows/stale.yml":     "name: stale\n",
		".github/vars/inputs-global.yaml": "cloud: aws\ncloud_type: lambda\n",
		".github/cloudopsworks-ci.yaml":   "pipeline: local\n",
		"README.local":                    "base\n",
	} {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(path)), content)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	mustWrite(t, filepath.Join(dir, "README.local"), "unrelated tracked work\n")
	runGit(t, dir, "add", "README.local")
	mustWrite(t, filepath.Join(dir, "scratch.txt"), "unrelated untracked work\n")
	for path, content := range map[string]string{
		".template/.github/workflows/build.yml":            "uses: cloudopsworks/blueprints/cd/checkout@v5.10\n",
		".template/Makefile":                               "all:\n\t@true\n",
		".template/.cloudopsworks/_VERSION":                "v5.10.2\n",
		".template/.cloudopsworks/vars/inputs-global.yaml": "cloud: aws\ncloud_type: lambda\ntarget_default: kept\n",
		".template/.cloudopsworks/cloudopsworks-ci.yaml":   "pipeline: template\n",
		".template/.cloudopsworks/boilerplate/target.sh":   "target boilerplate\n",
	} {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(path)), content)
	}

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	state := RepositoryState{WorkDir: dir, BlueprintPath: ".github", VersionFile: ".github/_VERSION", Pre510: true, Version: "v5.9.0"}
	tmpl := Template{Name: "go", Merge: true, Versioned: true, CICD: true, Boilerplate: true, BoilerplatePathPre510: ".cloudopsworks/legacy", BoilerplatePathV510Plus: ".cloudopsworks/boilerplate"}
	if err := runner.Stack(context.Background(), StackOptions{Template: tmpl, State: state, PullBranch: "test", TemplateHash: "target-hash", V510Plus: "v5.10"}); err != nil {
		t.Fatalf("Stack() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "_VERSION")); got != "v5.10.2\n" {
		t.Fatalf("target version = %q", got)
	}
	assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks", "cloudopsworks-ci.yaml")), "pipeline: local")
	assertContainsAll(t, mustRead(t, filepath.Join(dir, ".cloudopsworks", "vars", "inputs-global.yaml")), "target_default: kept")
	if content := mustRead(t, filepath.Join(dir, ".cloudopsworks", "cloudopsworks-ci.yaml")); !strings.Contains(content, "#workflow-version-tag: v5.10.2 - hash: target-hash") {
		t.Fatalf("CICD footer did not use target version:\n%s", content)
	}
	if content := runGit(t, dir, "show", "HEAD:.cloudopsworks/cloudopsworks-ci.yaml"); !strings.Contains(content, "#workflow-version-tag: v5.10.2 - hash: target-hash") {
		t.Fatalf("committed CICD footer did not use target version:\n%s", content)
	}
	committed := runGit(t, dir, "show", "--format=", "--name-only", "HEAD")
	for _, path := range []string{".cloudopsworks/boilerplate/target.sh", ".github/workflows/stale.yml"} {
		if !strings.Contains(committed, path+"\n") {
			t.Fatalf("expected upgrade effect %s was not committed:\n%s", path, committed)
		}
	}
	if strings.Contains(committed, "README.local\n") || strings.Contains(committed, "scratch.txt\n") {
		t.Fatalf("unrelated work was committed:\n%s", committed)
	}
	status := runGit(t, dir, "status", "--short")
	if !strings.Contains(status, "M  README.local") || !strings.Contains(status, "?? scratch.txt") {
		t.Fatalf("unrelated work was not preserved:\n%s", status)
	}
	if staged := runGit(t, dir, "diff", "--cached", "--name-only"); staged != "README.local\n" {
		t.Fatalf("pre-staged unrelated work changed: %q", staged)
	}
}

func TestStackPreV510AndUnversionedRoutesCommitRecordedEffects(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	for _, test := range []struct {
		name      string
		versioned bool
		want      []string
	}{
		{name: "versioned", versioned: true, want: []string{".github/_VERSION", ".github/workflows/build.yml", "Makefile"}},
		{name: "unversioned", want: []string{".github/workflows/build.yml", "Makefile"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			runGit(t, dir, "init")
			runGit(t, dir, "config", "user.email", "tronador-cli-test@example.com")
			runGit(t, dir, "config", "user.name", "tronador-cli test")
			mustWrite(t, filepath.Join(dir, ".github", "_VERSION"), "v5.8.0\n")
			mustWrite(t, filepath.Join(dir, ".github", "workflows", "build.yml"), "name: old\n")
			mustWrite(t, filepath.Join(dir, "Makefile"), "old\n")
			runGit(t, dir, "add", ".")
			runGit(t, dir, "commit", "-m", "initial")
			mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "build.yml"), "uses: cloudopsworks/blueprints/cd/checkout@v5.9\n")
			mustWrite(t, filepath.Join(dir, ".template", "Makefile"), "target\n")
			if test.versioned {
				mustWrite(t, filepath.Join(dir, ".template", ".github", "_VERSION"), "v5.9.1\n")
			}

			runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			state := RepositoryState{WorkDir: dir, BlueprintPath: ".github", VersionFile: ".github/_VERSION", Version: "v5.8.0"}
			tmpl := Template{Name: "go", Merge: true, Versioned: test.versioned}
			if err := runner.Stack(context.Background(), StackOptions{Template: tmpl, State: state, PullBranch: "test", TemplateHash: "target-hash"}); err != nil {
				t.Fatalf("Stack() error = %v", err)
			}
			committed := runGit(t, dir, "show", "--format=", "--name-only", "HEAD")
			for _, path := range test.want {
				if !strings.Contains(committed, path+"\n") {
					t.Fatalf("Stack() did not commit %s:\n%s", path, committed)
				}
			}
		})
	}
}

func TestStackV510DryRunUsesPostMigrationState(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".github", "_VERSION"), "v5.9.0\n")
	mustWrite(t, filepath.Join(dir, ".github", ".golang"), "")
	mustWrite(t, filepath.Join(dir, ".github", "cloudopsworks-ci.yaml"), "pipeline: local\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "build.yml"), "uses: cloudopsworks/blueprints/cd/checkout@v5.10\n")
	mustWrite(t, filepath.Join(dir, ".template", "Makefile"), "all:\n\t@true\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v5.10.2\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "cloudopsworks-ci.yaml"), "pipeline: target\n")
	out := &bytes.Buffer{}
	runner, err := NewRunner(Options{WorkDir: dir, DryRun: true, Stdout: out, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	state := RepositoryState{WorkDir: dir, BlueprintPath: ".github", VersionFile: ".github/_VERSION", Pre510: true, Version: "v5.9.0"}
	tmpl := Template{Name: "go", Merge: true, Versioned: true, CICD: true}
	if err := runner.Stack(context.Background(), StackOptions{Template: tmpl, State: state, PullBranch: "test", TemplateHash: "target-hash", V510Plus: "v5.10"}); err != nil {
		t.Fatalf("Stack() error = %v", err)
	}
	if exists(filepath.Join(dir, ".cloudopsworks")) {
		t.Fatal("dry-run created the post-migration layout")
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "_VERSION")); got != "v5.9.0\n" {
		t.Fatalf("dry-run changed legacy version: %q", got)
	}
	if !strings.Contains(out.String(), "DRY-RUN write "+filepath.Join(dir, ".cloudopsworks", "cloudopsworks-ci.yaml")) {
		t.Fatalf("dry-run did not use the deterministic post-migration CICD path:\n%s", out.String())
	}
	if count := strings.Count(out.String(), "--literal-pathspecs add -A --"); count != 1 {
		t.Fatalf("dry-run staged effects %d times, want one transaction boundary:\n%s", count, out.String())
	}
	if !strings.Contains(out.String(), "--literal-pathspecs commit --only") {
		t.Fatalf("dry-run did not print exact-path commit intent:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "version: v5.10.2") || strings.Contains(out.String(), "Repository upgraded to version: v5.9.0") {
		t.Fatalf("dry-run did not report validated target version:\n%s", out.String())
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

func TestCommitUpgradeEffectsDropsEphemeralUntrackedPaths(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "tronador-cli-test@example.com")
	runGit(t, dir, "config", "user.name", "tronador-cli test")
	mustWrite(t, filepath.Join(dir, "README.local"), "initial\n")
	runGit(t, dir, "add", "README.local")
	runGit(t, dir, "commit", "-m", "initial")
	mustWrite(t, filepath.Join(dir, "Makefile"), "target\n")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	runner.effects = newUpgradeEffects()
	runner.effects.add("Makefile", ".github/workflows/ephemeral.yml")
	if err := runner.commitUpgradeEffects(context.Background(), RepositoryState{Version: "v5.10.0"}); err != nil {
		t.Fatalf("commitUpgradeEffects() error = %v", err)
	}
	committed := runGit(t, dir, "show", "--format=", "--name-only", "HEAD")
	if !strings.Contains(committed, "Makefile\n") || strings.Contains(committed, "ephemeral.yml\n") {
		t.Fatalf("commit paths = %q, want only existing effect", committed)
	}
}

func TestRecoverLeavesGitIndexUntouched(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "tronador-cli-test@example.com")
	runGit(t, dir, "config", "user.name", "tronador-cli test")
	for path, content := range map[string]string{
		".cloudopsworks/_VERSION":              "v5.10.1\n",
		".cloudopsworks/.golang":               "",
		".cloudopsworks/cloudopsworks-ci.yaml": "pipeline: local\n",
		".github/workflows/old.yml":            "name: old\n",
	} {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(path)), content)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	before := runGit(t, dir, "write-tree")

	runner, err := NewRunner(Options{WorkDir: dir, PullBranch: "main", Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for index := range runner.Config.Templates {
		if runner.Config.Templates[index].Name == "go" {
			runner.Config.Templates[index].Boilerplate = true
			runner.Config.Templates[index].BoilerplatePathV510Plus = ".cloudopsworks/boilerplate"
		}
	}
	runner.gitClient = recoverFixtureGitClient{}
	runner.suppressStaging = true
	if err := runner.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() with prior suppression error = %v", err)
	}
	if !runner.suppressStaging {
		t.Fatal("Recover() did not restore prior suppressStaging=true")
	}
	runner.suppressStaging = false
	if err := runner.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() with prior staging enabled error = %v", err)
	}
	if runner.suppressStaging {
		t.Fatal("Recover() did not restore prior suppressStaging=false")
	}
	if after := runGit(t, dir, "write-tree"); after != before {
		t.Fatalf("Recover() changed the index: before=%s after=%s", before, after)
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "workflows", "target.yml")); got != "name: target\n" {
		t.Fatalf("Recover() did not overlay fixture workflow: %q", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "boilerplate", "target.sh")); got != "target boilerplate\n" {
		t.Fatalf("Recover() did not overlay boilerplate: %q", got)
	}
	if !strings.Contains(mustRead(t, filepath.Join(dir, ".cloudopsworks", "cloudopsworks-ci.yaml")), "#workflow-version-tag: v5.10.1 - hash:") {
		t.Fatal("Recover() did not update CICD footer")
	}
}

func TestPushRejectsNonUpgradeStagedPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	mustWrite(t, filepath.Join(dir, "README.local"), "unrelated\n")
	runGit(t, dir, "add", "README.local")
	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Push(context.Background(), Template{}, RepositoryState{}); err == nil || !strings.Contains(err.Error(), "non-upgrade") {
		t.Fatalf("Push() error = %v, want staged non-upgrade path rejection", err)
	}
	if staged := runGit(t, dir, "diff", "--cached", "--name-only"); staged != "README.local\n" {
		t.Fatalf("Push() changed caller index: %q", staged)
	}
}

func TestPushCommitsCallerStagedUpgradeBytesNotLaterWorktreeBytes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "tronador-cli-test@example.com")
	runGit(t, dir, "config", "user.name", "Tronador CLI Test")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "vars.yaml"), "value: base\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "vars.yaml"), "value: staged\n")
	runGit(t, dir, "add", ".cloudopsworks/vars.yaml")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "vars.yaml"), "value: worktree\n")
	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Push(context.Background(), Template{}, RepositoryState{}); err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if got := runGit(t, dir, "show", "HEAD:.cloudopsworks/vars.yaml"); got != "value: staged\n" {
		t.Fatalf("committed bytes = %q, want staged bytes", got)
	}
}

func TestDefaultV510MigrationMovesOnlyAutoAssign(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "auto*.yml") {
		t.Fatalf("migration still uses broad auto YAML glob: %s", data)
	}
}

func TestGitStageExactRejectsTraversalAndDirectories(t *testing.T) {
	dir, runner, _ := configUpgradeRunner(t, false)
	mustWrite(t, filepath.Join(dir, "nested", "file.txt"), "content\n")
	for _, path := range []string{"../outside", "nested"} {
		if err := runner.gitStageExact(context.Background(), path); err == nil {
			t.Fatalf("gitStageExact(%q) accepted unsafe path", path)
		}
	}
}

func TestEffectPathValidationFailsAtStagingCall(t *testing.T) {
	_, runner, _ := configUpgradeRunner(t, false)
	runner.effects = newUpgradeEffects()
	if err := runner.gitAdd(context.Background(), "../outside"); err == nil {
		t.Fatal("gitAdd accepted unsafe effect path")
	}
	if err := runner.gitStageExact(context.Background(), "../outside"); err == nil {
		t.Fatal("gitStageExact accepted unsafe effect path")
	}
}

func TestConfigValidateRejectsUnsafeTemplatePaths(t *testing.T) {
	for _, path := range []string{".", ".cloudopsworks/..", "../template", "/tmp/template"} {
		cfg := &Config{SchemaVersion: "1", TemplateDirectory: path}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("Validate accepted unsafe templateDirectory %q", path)
		}
	}
}

func TestStackPreflightRejectsManagedSymlinkBeforeMutation(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	for _, hazard := range []string{"Makefile", ".cloudopsworks/hooks", ".cloudopsworks"} {
		t.Run(hazard, func(t *testing.T) {
			dir := t.TempDir()
			runGit(t, dir, "init")
			runGit(t, dir, "config", "user.email", "tronador-cli-test@example.com")
			runGit(t, dir, "config", "user.name", "Tronador CLI Test")
			mustWrite(t, filepath.Join(dir, ".github", "_VERSION"), "v5.9.0\n")
			mustWrite(t, filepath.Join(dir, ".github", "workflows", "old.yml"), "name: old\n")
			mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "build.yml"), "uses: cloudopsworks/blueprints/cd/checkout@v5.10\n")
			mustWrite(t, filepath.Join(dir, ".template", "Makefile"), "target\n")
			mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v5.10.2\n")
			mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "hooks", "target.sh"), "target\n")
			runGit(t, dir, "add", ".")
			runGit(t, dir, "commit", "-m", "initial")
			outside := filepath.Join(t.TempDir(), "sentinel")
			mustWrite(t, outside, "outside\n")
			dest := filepath.Join(dir, hazard)
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, dest); err != nil {
				t.Fatal(err)
			}
			index := runGit(t, dir, "write-tree")
			runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			state := RepositoryState{WorkDir: dir, BlueprintPath: ".github", VersionFile: ".github/_VERSION", Pre510: true, Version: "v5.9.0"}
			err = runner.Stack(context.Background(), StackOptions{Template: Template{Name: "go", Merge: true, Versioned: true}, State: state, TemplateHash: "hash"})
			if err == nil {
				t.Fatal("Stack accepted managed symlink")
			}
			if got := mustRead(t, filepath.Join(dir, ".github", "workflows", "old.yml")); got != "name: old\n" {
				t.Fatalf("workflow mutated: %q", got)
			}
			if got := mustRead(t, filepath.Join(dir, ".github", "_VERSION")); got != "v5.9.0\n" {
				t.Fatalf("legacy version mutated: %q", got)
			}
			if got := mustRead(t, outside); got != "outside\n" {
				t.Fatalf("outside bytes mutated: %q", got)
			}
			if got := runGit(t, dir, "write-tree"); got != index {
				t.Fatalf("index mutated: %s != %s", got, index)
			}
		})
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

type recoverFixtureGitClient struct{}

func (recoverFixtureGitClient) Clone(_ context.Context, _ string, destination string) error {
	for path, content := range map[string]string{
		".github/workflows/target.yml":                   "name: target\n",
		".cloudopsworks/boilerplate/target.sh":           "target boilerplate\n",
		".cloudopsworks/boilerplate/stale-template.yaml": "template: true\n",
	} {
		if err := os.MkdirAll(filepath.Join(destination, filepath.Dir(path)), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(destination, path), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (recoverFixtureGitClient) RemoveRemote(context.Context, string, string) error { return nil }
func (recoverFixtureGitClient) Checkout(context.Context, string, string) (string, error) {
	return "fixture-hash", nil
}
func (recoverFixtureGitClient) OriginOwnerRepo(context.Context, string) (string, string, error) {
	return "", "", errors.New("fixture has no origin")
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

func TestDetectSupportsLegacyFlutterMobileMarker(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v5.10.1\n")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", ".fluttermobile"), "")
	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	tmpl, _, err := runner.ActiveTemplate()
	if err != nil {
		t.Fatalf("ActiveTemplate() error = %v", err)
	}
	if tmpl.Name != "flutter" {
		t.Fatalf("template = %q, want flutter", tmpl.Name)
	}
}

func TestCommitUpgradeEffectsNoOpPreservesUnrelatedIndex(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "tronador-cli-test@example.com")
	runGit(t, dir, "config", "user.name", "tronador-cli test")
	mustWrite(t, filepath.Join(dir, "Makefile"), "baseline\n")
	mustWrite(t, filepath.Join(dir, "README.local"), "baseline\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	// This staged edit is unrelated to the recorded effect path and must survive
	// both no-op attempts and a later exact-path upgrade commit.
	mustWrite(t, filepath.Join(dir, "README.local"), "unrelated staged work\n")
	runGit(t, dir, "add", "README.local")
	head := runGit(t, dir, "rev-parse", "HEAD")

	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		runner.effects = newUpgradeEffects()
		runner.effects.add("Makefile")
		if err := runner.commitUpgradeEffects(context.Background(), RepositoryState{Version: "v5.10.0"}); err != nil {
			t.Fatalf("no-op attempt %d: commitUpgradeEffects() error = %v", attempt+1, err)
		}
		if got := runGit(t, dir, "rev-parse", "HEAD"); got != head {
			t.Fatalf("no-op attempt %d created a commit: got %s, want %s", attempt+1, got, head)
		}
		if staged := runGit(t, dir, "diff", "--cached", "--name-only"); staged != "README.local\n" {
			t.Fatalf("no-op attempt %d changed unrelated index: %q", attempt+1, staged)
		}
	}

	mustWrite(t, filepath.Join(dir, "Makefile"), "upgraded\n")
	runner.effects = newUpgradeEffects()
	runner.effects.add("Makefile")
	if err := runner.commitUpgradeEffects(context.Background(), RepositoryState{Version: "v5.10.0"}); err != nil {
		t.Fatalf("changed effect: commitUpgradeEffects() error = %v", err)
	}
	if got := runGit(t, dir, "show", "--format=", "--name-only", "HEAD"); got != "Makefile\n" {
		t.Fatalf("changed effect commit paths = %q, want Makefile only", got)
	}
	if staged := runGit(t, dir, "diff", "--cached", "--name-only"); staged != "README.local\n" {
		t.Fatalf("changed effect altered unrelated index: %q", staged)
	}
}
