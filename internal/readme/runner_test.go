package readme

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesReadmeYAMLFromEmbeddedAsset(t *testing.T) {
	dir := t.TempDir()
	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "README.yaml"))
	if err != nil {
		t.Fatalf("README.yaml not created: %v", err)
	}
	if !strings.Contains(string(data), "name: <project name>") {
		t.Fatalf("README.yaml did not come from embedded template: %q", data[:min(len(data), 80)])
	}
}

func TestInitDoesNotOverwriteExistingReadmeYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.yaml")
	if err := os.WriteFile(path, []byte("name: local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "name: local\n" {
		t.Fatalf("README.yaml overwritten: %q", got)
	}
}

func TestBuildInvokesGomplateWithMakefileCompatibleEnv(t *testing.T) {
	dir := t.TempDir()
	fake := writeExecutable(t, dir, "gomplate", `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--out" ]; then
    shift
    out="$1"
  fi
  shift || true
done
printf 'yaml=%s\nincludes=%s\nfile=%s\n' "$README_YAML" "$README_INCLUDES" "$README_TEMPLATE_FILE" > "$out"
`)
	if err := os.WriteFile(filepath.Join(dir, "README.yaml"), []byte("name: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner, err := NewRunner(Options{WorkDir: dir, GomplatePath: fake, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Build(context.Background()); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "yaml=README.yaml") || !strings.Contains(string(got), "includes=file://") {
		t.Fatalf("unexpected gomplate output/env: %s", got)
	}
}

func TestDetectTerraformModuleUsesMarkerOrTerraformFiles(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "canonical marker", path: filepath.Join(".cloudopsworks", ".terraform-module"), want: true},
		{name: "terraform file", path: "variables.tf", want: true},
		{name: "terraform json file", path: "main.tf.json", want: true},
		{name: "unrelated file", path: "main.go", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			mustWrite(t, filepath.Join(dir, tt.path), "")
			runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
			if err != nil {
				t.Fatalf("NewRunner() error = %v", err)
			}
			got, err := runner.DetectTerraformModule()
			if err != nil {
				t.Fatalf("DetectTerraformModule() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("DetectTerraformModule() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildGeneratesTerraformDocsBeforeReadmeForDetectedModule(t *testing.T) {
	dir := t.TempDir()
	terraformDocs := writeExecutable(t, dir, "terraform-docs", `#!/bin/sh
printf '## Terraform inputs\n'
`)
	gomplate := writeExecutable(t, dir, "gomplate", `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--out" ]; then
    shift
    out="$1"
  fi
  shift || true
done
cat docs/terraform.md > "$out"
`)
	mustWrite(t, filepath.Join(dir, "README.yaml"), "name: test\ninclude:\n  - docs/terraform.md\n")
	mustWrite(t, filepath.Join(dir, "variables.tf"), "variable \"name\" {}\n")
	runner, err := NewRunner(Options{
		WorkDir:           dir,
		GomplatePath:      gomplate,
		TerraformDocsPath: terraformDocs,
		Stdout:            io.Discard,
		Stderr:            io.Discard,
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Build(context.Background()); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "## Terraform inputs\n" {
		t.Fatalf("README.md = %q", got)
	}
}

func TestBuildTerraformOnlyDoesNotRequireReadmeInputs(t *testing.T) {
	dir := t.TempDir()
	terraformDocs := writeExecutable(t, dir, "terraform-docs", `#!/bin/sh
printf '## Terraform only\n'
`)
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", ".terraform-module"), "")
	runner, err := NewRunner(Options{
		WorkDir:           dir,
		TerraformDocsPath: terraformDocs,
		Stdout:            io.Discard,
		Stderr:            io.Discard,
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.BuildTerraform(context.Background()); err != nil {
		t.Fatalf("BuildTerraform() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "docs", "terraform.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "## Terraform only\n" {
		t.Fatalf("docs/terraform.md = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); !os.IsNotExist(err) {
		t.Fatalf("README.md should not be generated, stat error = %v", err)
	}
}

func TestBuildSkipsTerraformDocsOutsideTerraformModule(t *testing.T) {
	dir := t.TempDir()
	gomplate := writeExecutable(t, dir, "gomplate", `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--out" ]; then shift; out="$1"; fi
  shift || true
done
printf 'readme only\n' > "$out"
`)
	mustWrite(t, filepath.Join(dir, "README.yaml"), "name: test\n")
	runner, err := NewRunner(Options{
		WorkDir:           dir,
		GomplatePath:      gomplate,
		TerraformDocsPath: filepath.Join(dir, "missing-terraform-docs"),
		Stdout:            io.Discard,
		Stderr:            io.Discard,
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Build(context.Background()); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
}

func TestBuildUsesProvisionedGomplateFromToolsDir(t *testing.T) {
	t.Setenv("PATH", "")
	dir := t.TempDir()
	toolsDir := t.TempDir()
	writeExecutable(t, toolsDir, "gomplate", `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--out" ]; then
    shift
    out="$1"
  fi
  shift || true
done
printf 'from-tools-dir\n' > "$out"
`)
	if err := os.WriteFile(filepath.Join(dir, "README.yaml"), []byte("name: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner, err := NewRunner(Options{WorkDir: dir, ToolsDir: toolsDir, SkipGomplateInstall: true, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Build(context.Background()); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "from-tools-dir\n" {
		t.Fatalf("README.md = %q", got)
	}
}

func TestDepsDryRunProvisionsGomplateIntoToolsDir(t *testing.T) {
	t.Setenv("PATH", "")
	var stdout strings.Builder
	toolsDir := t.TempDir()
	runner, err := NewRunner(Options{
		WorkDir:         t.TempDir(),
		ToolsDir:        toolsDir,
		DryRun:          true,
		GomplateVersion: "9.9.9",
		Stdout:          &stdout,
		Stderr:          io.Discard,
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Deps(context.Background()); err != nil {
		t.Fatalf("Deps() error = %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "DRY-RUN: mkdir -p "+toolsDir) ||
		!strings.Contains(out, "download gomplate 9.9.9 from https://github.com/hairyhenderson/gomplate/releases/download/v9.9.9/gomplate_") ||
		!strings.Contains(out, "gomplate: "+filepath.Join(toolsDir, "gomplate")) {
		t.Fatalf("unexpected deps dry-run output:\n%s", out)
	}
}

func TestLintDetectsOutdatedReadme(t *testing.T) {
	dir := t.TempDir()
	fake := writeExecutable(t, dir, "gomplate", `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--out" ]; then
    shift
    out="$1"
  fi
  shift || true
done
printf 'generated\n' > "$out"
`)
	mustWrite(t, filepath.Join(dir, "README.yaml"), "name: test\n")
	mustWrite(t, filepath.Join(dir, "README.md"), "stale\n")
	runner, err := NewRunner(Options{WorkDir: dir, GomplatePath: fake, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Lint(context.Background()); err == nil {
		t.Fatalf("Lint() succeeded for stale README")
	}
}

func TestResolveTemplatePrefersProjectOverride(t *testing.T) {
	dir := t.TempDir()
	override := filepath.Join(dir, ".tronador", "readme", "README.md.gotmpl")
	mustWrite(t, override, "custom")
	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	asset, err := runner.ResolveTemplate()
	if err != nil {
		t.Fatalf("ResolveTemplate() error = %v", err)
	}
	if asset.Path != override || asset.Source != "project:.tronador" {
		t.Fatalf("asset = %+v, want project override %s", asset, override)
	}
}

func TestSyncAssetsDownloadsGitHubAssetsIntoCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/owner/repo/v1/templates/README.yaml":
			return response(200, "name: synced\n"), nil
		case "/owner/repo/v1/templates/README.md.gotmpl":
			return response(200, "template"), nil
		default:
			return response(404, "missing"), nil
		}
	})}
	if err := SyncAssets(context.Background(), SyncOptions{Repo: "owner/repo", Ref: "v1", BasePath: "templates", HTTPClient: client, Stdout: io.Discard, Force: true}); err != nil {
		t.Fatalf("SyncAssets() error = %v", err)
	}
	cache, err := CacheDir("owner/repo", "v1")
	if err != nil {
		t.Fatalf("CacheDir() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(cache, "README.yaml"))
	if err != nil {
		t.Fatalf("synced README.yaml missing: %v", err)
	}
	if string(got) != "name: synced\n" {
		t.Fatalf("README.yaml = %q", got)
	}
}

func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
