package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewProvisionerDefaultsToolsDirUnderUserCloudOpsWorks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	provisioner, err := NewProvisioner(Options{WorkDir: t.TempDir(), Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewProvisioner() error = %v", err)
	}
	want := filepath.Join(home, ".cloudopsworks", "tronador")
	if provisioner.Opts.ToolsDir != want {
		t.Fatalf("ToolsDir = %q, want %q", provisioner.Opts.ToolsDir, want)
	}
}

func TestEmbeddedConfigIncludesDefaultTools(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	provisioner, err := NewProvisioner(Options{WorkDir: t.TempDir(), Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewProvisioner() error = %v", err)
	}
	got := strings.Join(provisioner.Config.ToolNames(), ",")
	for _, want := range []string{"boilerplate", "gh", "gitversion", "gomplate", "terraform-docs", "yq"} {
		if !strings.Contains(got, want) {
			t.Fatalf("tool %s missing from config names %s", want, got)
		}
	}
}

func TestConfigOverrideMergesFieldByField(t *testing.T) {
	home := t.TempDir()
	workDir := t.TempDir()
	t.Setenv("HOME", home)
	mustWriteFile(t, filepath.Join(home, ".cloudopsworks", "tronador", "tools.json"), `{
  "tools": [
    {"name": "gomplate", "default_version": "4.0.0"},
    {"name": "custom", "executable": "custom", "default_version": "1.0.0", "url_template": "https://example.invalid/{version}", "format": "binary"}
  ]
}`)
	explicit := filepath.Join(t.TempDir(), "tools.json")
	mustWriteFile(t, explicit, `{
  "tools": [
    {"name": "gomplate", "version_env_vars": ["CUSTOM_GOMPLATE_VERSION"], "platform_overrides": {"`+platformKey()+`": {"url_template": "https://override/{version}/{os}/{arch}"}}},
    {"name": "custom", "default_version": "2.0.0"}
  ]
}`)
	config, err := LoadConfig(workDir, explicit)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	gomplate, ok := config.Tool("gomplate")
	if !ok {
		t.Fatalf("gomplate missing")
	}
	if gomplate.DefaultVersion != "4.0.0" {
		t.Fatalf("DefaultVersion = %q, want inherited user override", gomplate.DefaultVersion)
	}
	if got := strings.Join(gomplate.VersionEnvVars, ","); got != "CUSTOM_GOMPLATE_VERSION" {
		t.Fatalf("VersionEnvVars = %q", got)
	}
	if gomplate.URLTemplate != "https://override/{version}/{os}/{arch}" {
		t.Fatalf("platform override URLTemplate = %q", gomplate.URLTemplate)
	}
	custom, ok := config.Tool("custom")
	if !ok {
		t.Fatalf("custom missing")
	}
	if custom.DefaultVersion != "2.0.0" || custom.URLTemplate != "https://example.invalid/{version}" {
		t.Fatalf("custom merge = %+v", custom)
	}
}

func TestEnsureUsesCachedToolFromToolsDir(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("HOME", t.TempDir())
	toolsDir := t.TempDir()
	cached := writeToolExecutable(t, toolsDir, "terraform-docs", "#!/bin/sh\nexit 0\n")
	provisioner, err := NewProvisioner(Options{WorkDir: t.TempDir(), ToolsDir: toolsDir, SkipInstall: true, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewProvisioner() error = %v", err)
	}
	resolved, err := provisioner.Ensure(context.Background(), Tool{Name: "terraform-docs"})
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if resolved.Path != cached || resolved.Source != "tools-dir" {
		t.Fatalf("resolved = %+v, want tools-dir path %s", resolved, cached)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureDryRunPrintsDirectDownload(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("HOME", t.TempDir())
	var stdout bytes.Buffer
	toolsDir := t.TempDir()
	provisioner, err := NewProvisioner(Options{WorkDir: t.TempDir(), ToolsDir: toolsDir, DryRun: true, Stdout: &stdout, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewProvisioner() error = %v", err)
	}
	resolved, err := provisioner.EnsureTool(context.Background(), "gomplate", "", "9.9.9")
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if resolved.Path != filepath.Join(toolsDir, ExecutableName("gomplate")) {
		t.Fatalf("resolved path = %q", resolved.Path)
	}
	out := stdout.String()
	for _, want := range []string{
		"DRY-RUN: mkdir -p " + toolsDir,
		"download gomplate 9.9.9 from https://github.com/hairyhenderson/gomplate/releases/download/v9.9.9/gomplate_",
		"to " + filepath.Join(toolsDir, ExecutableName("gomplate")),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, out)
		}
	}
}

func TestConfiguredToolsResolveFromJSONDryRun(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"gomplate", "gh", "boilerplate", "gitversion", "terraform-docs", "yq"} {
		t.Run(name, func(t *testing.T) {
			var stdout bytes.Buffer
			toolsDir := t.TempDir()
			provisioner, err := NewProvisioner(Options{WorkDir: t.TempDir(), ToolsDir: toolsDir, DryRun: true, Stdout: &stdout, Stderr: io.Discard})
			if err != nil {
				t.Fatalf("NewProvisioner() error = %v", err)
			}
			resolved, err := provisioner.EnsureTool(context.Background(), name, "", "9.9.9")
			if err != nil {
				t.Fatalf("EnsureTool(%s) error = %v", name, err)
			}
			if resolved.Path != filepath.Join(toolsDir, ExecutableName(name)) {
				t.Fatalf("resolved path = %q", resolved.Path)
			}
			out := stdout.String()
			if !strings.Contains(out, "download "+name+" 9.9.9") || !strings.Contains(out, " to "+filepath.Join(toolsDir, ExecutableName(name))) {
				t.Fatalf("unexpected dry-run output for %s:\n%s", name, out)
			}
		})
	}
}

func TestConfiguredVersionEnvVarsComeFromJSON(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YQ_VERSION", "8.8.8")
	var stdout bytes.Buffer
	provisioner, err := NewProvisioner(Options{WorkDir: t.TempDir(), ToolsDir: t.TempDir(), DryRun: true, Stdout: &stdout, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewProvisioner() error = %v", err)
	}
	if _, err := provisioner.EnsureTool(context.Background(), "yq", "", ""); err != nil {
		t.Fatalf("EnsureTool(yq) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "download yq 8.8.8") {
		t.Fatalf("expected YQ_VERSION from config to drive version, got:\n%s", stdout.String())
	}
}

func TestEnsureInstallsOnlyRequestedToolFromDirectDownload(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("HOME", t.TempDir())
	toolsDir := t.TempDir()
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.String(), "/demo/1.2.3/") {
			return response(404, "missing"), nil
		}
		return response(200, "#!/bin/sh\nprintf installed\\n\n"), nil
	})}
	provisioner, err := NewProvisioner(Options{WorkDir: t.TempDir(), ToolsDir: toolsDir, HTTPClient: client, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewProvisioner() error = %v", err)
	}
	resolved, err := provisioner.Ensure(context.Background(), Tool{
		Name:       "demo-tool",
		Executable: "demo-tool",
		Download: DownloadSpec{
			URLTemplate:    "https://example.invalid/demo/{version}/{os}-{arch}",
			Format:         "binary",
			DefaultVersion: "1.2.3",
		},
	})
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if !resolved.Installed || resolved.Source != "download" {
		t.Fatalf("resolved = %+v, want installed from direct download", resolved)
	}
	if !ExecutableExists(filepath.Join(toolsDir, "demo-tool")) {
		t.Fatalf("demo-tool was not installed")
	}
	if ExecutableExists(filepath.Join(toolsDir, "other-tool")) {
		t.Fatalf("unexpected other tool installation")
	}
}

func writeToolExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, ExecutableName(name))
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
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
