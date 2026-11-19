package tools

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExtractVersionFromRealToolOutputs(t *testing.T) {
	cases := map[string]struct {
		output string
		want   string
	}{
		"gomplate":       {"gomplate version 3.10.0\n", "3.10.0"},
		"gh":             {"gh version 2.105.0 (2026-11-17)\nhttps://github.com/cli/cli/releases/tag/v2.105.0\n", "2.105.0"},
		"boilerplate":    {"boilerplate version v0.15.0\n", "v0.15.0"},
		"gitversion":     {"6.7.0+Branch.main.Sha.00e59b5f45fe97e2e610a480a860b3a009db9602\n", "6.7.0"},
		"yq":             {"yq (https://github.com/mikefarah/yq/) version v4.54.3\n", "v4.54.3"},
		"python-yq":      {"yq 3.4.3\n", "3.4.3"},
		"terraform":      {"Terraform v1.11.1\non darwin_arm64\n", "v1.11.1"},
		"terraform-docs": {"terraform-docs version v0.25.1 darwin/arm64\n", "v0.25.1"},
		"tofu":           {"OpenTofu v1.11.4\non darwin_arm64\n", "v1.11.4"},
		"terragrunt":     {"terragrunt version 1.1.2\n", "1.1.2"},
		"ignores-go":     {"built with go1.26.5\nfoo version 4.2.0\n", "4.2.0"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ExtractVersion(tc.output, "")
			if err != nil {
				t.Fatalf("ExtractVersion() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("ExtractVersion() = %q, want %q", got, tc.want)
			}
		})
	}
	if _, err := ExtractVersion("no version here", ""); err == nil {
		t.Fatalf("expected error for output without version")
	}
	if got, err := ExtractVersion("release=9.1.0 build=1.2.3", `release=(\S+)`); err != nil || got != "9.1.0" {
		t.Fatalf("custom regex = %q, %v", got, err)
	}
	if _, err := ExtractVersion("1.2.3", `(`); err == nil {
		t.Fatalf("expected invalid regex error")
	}
}

func TestCompatibilityKeyIsMajorVersion(t *testing.T) {
	for version, want := range map[string]string{
		"6.7.0": "6", "v2.95.0": "2", "0.24.0": "0", "v0.15.0": "0", "10": "10",
	} {
		got, ok := CompatibilityKey(version)
		if !ok || got != want {
			t.Fatalf("CompatibilityKey(%q) = %q, %v; want %q", version, got, ok, want)
		}
	}
	if _, ok := CompatibilityKey("latest"); ok {
		t.Fatalf("CompatibilityKey(latest) should not parse")
	}
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtures")
	}
}

func versionedTool(url string) Tool {
	return Tool{
		Name:        "demo-tool",
		Executable:  "demo-tool",
		VersionArgs: []string{"--version"},
		Download: DownloadSpec{
			URLTemplate:    url,
			Format:         "binary",
			DefaultVersion: "2.4.0",
		},
	}
}

func fixedDownload(body string, hits *int) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		*hits++
		return response(200, body), nil
	})}
}

func newCompatProvisioner(t *testing.T, toolsDir string, skipInstall bool, client *http.Client, stderr io.Writer) *Provisioner {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TRONADOR_TOOLS_VERSION_CHECK", "")
	t.Setenv("TRONADOR_TOOLS_INSTALL", "")
	t.Setenv("TRONADOR_TOOLS_SKIP_INSTALL", "")
	p, err := NewProvisioner(Options{WorkDir: t.TempDir(), ToolsDir: toolsDir, SkipInstall: skipInstall, HTTPClient: client, Stdout: io.Discard, Stderr: stderr})
	if err != nil {
		t.Fatalf("NewProvisioner() error = %v", err)
	}
	return p
}

func TestEnsureUsesPathToolWithSupportedMajor(t *testing.T) {
	skipOnWindows(t)
	bin := t.TempDir()
	local := writeToolExecutable(t, bin, "demo-tool", "#!/bin/sh\necho 'demo-tool version v2.1.9'\n")
	t.Setenv("PATH", bin)
	hits := 0
	p := newCompatProvisioner(t, t.TempDir(), false, fixedDownload("x", &hits), io.Discard)
	resolved, err := p.Ensure(context.Background(), versionedTool("https://example.invalid/{version}"))
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if resolved.Path != local || resolved.Source != "path" || resolved.Version != "v2.1.9" || hits != 0 {
		t.Fatalf("resolved = %+v hits=%d; want PATH tool without download", resolved, hits)
	}
}

func TestEnsureIgnoresPathToolWithDifferentMajorAndProvisions(t *testing.T) {
	skipOnWindows(t)
	bin := t.TempDir()
	writeToolExecutable(t, bin, "demo-tool", "#!/bin/sh\necho 'demo-tool version v1.9.0'\n")
	t.Setenv("PATH", bin)
	toolsDir := t.TempDir()
	hits := 0
	var stderr bytes.Buffer
	p := newCompatProvisioner(t, toolsDir, false, fixedDownload("#!/bin/sh\necho 'demo-tool version v2.4.0'\n", &hits), &stderr)
	resolved, err := p.Ensure(context.Background(), versionedTool("https://example.invalid/{version}"))
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if resolved.Source != "download" || resolved.Path != filepath.Join(toolsDir, "demo-tool") || hits != 1 {
		t.Fatalf("resolved = %+v hits=%d; want provisioned supported tool", resolved, hits)
	}
	if !strings.Contains(stderr.String(), "demo-tool v1.9.0") || !strings.Contains(stderr.String(), "supported demo-tool 2.4.0 (2.x)") {
		t.Fatalf("stderr = %q; want incompatible notice", stderr.String())
	}
}

func TestEnsureIgnoresNewerMajorToo(t *testing.T) {
	skipOnWindows(t)
	bin := t.TempDir()
	writeToolExecutable(t, bin, "demo-tool", "#!/bin/sh\necho '3.0.0'\n")
	t.Setenv("PATH", bin)
	hits := 0
	p := newCompatProvisioner(t, t.TempDir(), false, fixedDownload("#!/bin/sh\necho 2.4.0\n", &hits), io.Discard)
	resolved, err := p.Ensure(context.Background(), versionedTool("https://example.invalid/{version}"))
	if err != nil || resolved.Source != "download" || hits != 1 {
		t.Fatalf("resolved = %+v err=%v hits=%d; want download", resolved, err, hits)
	}
}

func TestEnsurePrefersCompatibleCacheOverIncompatiblePath(t *testing.T) {
	skipOnWindows(t)
	bin := t.TempDir()
	writeToolExecutable(t, bin, "demo-tool", "#!/bin/sh\necho 1.0.0\n")
	t.Setenv("PATH", bin)
	toolsDir := t.TempDir()
	cached := writeToolExecutable(t, toolsDir, "demo-tool", "#!/bin/sh\necho 2.0.1\n")
	hits := 0
	p := newCompatProvisioner(t, toolsDir, true, fixedDownload("x", &hits), io.Discard)
	resolved, err := p.Ensure(context.Background(), versionedTool("https://example.invalid/{version}"))
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if resolved.Path != cached || resolved.Source != "tools-dir" || hits != 0 {
		t.Fatalf("resolved = %+v hits=%d; want compatible cache", resolved, hits)
	}
}

func TestEnsureReplacesIncompatibleCachedTool(t *testing.T) {
	skipOnWindows(t)
	t.Setenv("PATH", t.TempDir())
	toolsDir := t.TempDir()
	writeToolExecutable(t, toolsDir, "demo-tool", "#!/bin/sh\necho 'gomplate version 1.10.0'\n")
	hits := 0
	p := newCompatProvisioner(t, toolsDir, false, fixedDownload("#!/bin/sh\necho 2.4.0\n", &hits), io.Discard)
	resolved, err := p.Ensure(context.Background(), versionedTool("https://example.invalid/{version}"))
	if err != nil || resolved.Source != "download" || hits != 1 {
		t.Fatalf("resolved = %+v err=%v hits=%d; want reinstall", resolved, err, hits)
	}
	if v, err := probeToolVersion(context.Background(), "", resolved.Path, []string{"--version"}, ""); err != nil || v != "2.4.0" {
		t.Fatalf("installed version = %q, %v", v, err)
	}
}

func TestEnsureIncompatibleWithoutInstallFailsActionably(t *testing.T) {
	skipOnWindows(t)
	bin := t.TempDir()
	writeToolExecutable(t, bin, "demo-tool", "#!/bin/sh\necho 1.0.0\n")
	t.Setenv("PATH", bin)
	hits := 0
	p := newCompatProvisioner(t, t.TempDir(), true, fixedDownload("x", &hits), io.Discard)
	_, err := p.Ensure(context.Background(), versionedTool("https://example.invalid/{version}"))
	if err == nil || !strings.Contains(err.Error(), "demo-tool 1.0.0 at") || !strings.Contains(err.Error(), "TRONADOR_TOOLS_VERSION_CHECK=false") || hits != 0 {
		t.Fatalf("err = %v hits=%d; want actionable incompatible error", err, hits)
	}
}

func TestEnsureVersionCheckCanBeDisabled(t *testing.T) {
	skipOnWindows(t)
	bin := t.TempDir()
	local := writeToolExecutable(t, bin, "demo-tool", "#!/bin/sh\necho 1.0.0\n")
	t.Setenv("PATH", bin)
	hits := 0
	p := newCompatProvisioner(t, t.TempDir(), true, fixedDownload("x", &hits), io.Discard)
	t.Setenv("TRONADOR_TOOLS_VERSION_CHECK", "false")
	p, err := NewProvisioner(p.Opts)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := p.Ensure(context.Background(), versionedTool("https://example.invalid/{version}"))
	if err != nil || resolved.Path != local {
		t.Fatalf("resolved = %+v err=%v; want PATH tool when check disabled", resolved, err)
	}
}

func TestEnsureRejectsLocalToolWithUnreadableVersion(t *testing.T) {
	skipOnWindows(t)
	for name, body := range map[string]string{
		"no output":    "#!/bin/sh\nexit 0\n",
		"garbage only": "#!/bin/sh\necho dev-build\n",
		"probe fails":  "#!/bin/sh\necho 2.4.0\nexit 3\n",
	} {
		t.Run(name, func(t *testing.T) {
			bin := t.TempDir()
			writeToolExecutable(t, bin, "demo-tool", body)
			t.Setenv("PATH", bin)
			hits := 0
			p := newCompatProvisioner(t, t.TempDir(), true, fixedDownload("x", &hits), io.Discard)
			_, err := p.Ensure(context.Background(), versionedTool("https://example.invalid/{version}"))
			if !errors.Is(err, ErrIncompatibleVersion) || !strings.Contains(err.Error(), "(version unreadable)") || hits != 0 {
				t.Fatalf("err = %v hits=%d; want unreadable local tool rejected", err, hits)
			}
		})
	}
}

func TestEnsureSkipsCheckWhenSupportedVersionUnparseable(t *testing.T) {
	skipOnWindows(t)
	bin := t.TempDir()
	local := writeToolExecutable(t, bin, "demo-tool", "#!/bin/sh\necho 9.0.0\n")
	t.Setenv("PATH", bin)
	hits := 0
	p := newCompatProvisioner(t, t.TempDir(), true, fixedDownload("x", &hits), io.Discard)
	tool := versionedTool("https://example.invalid/{version}")
	tool.Download.DefaultVersion = "latest"
	resolved, err := p.Ensure(context.Background(), tool)
	if err != nil || resolved.Path != local {
		t.Fatalf("resolved = %+v err=%v; want PATH tool when supported version is not a semver", resolved, err)
	}
}

func TestEnsureRejectsPathToolThatCannotAnswerProbe(t *testing.T) {
	skipOnWindows(t)
	bin := t.TempDir()
	writeToolExecutable(t, bin, "demo-tool", "#!/bin/sh\necho 'unknown flag'\nexit 3\n")
	t.Setenv("PATH", bin)
	hits := 0
	p := newCompatProvisioner(t, t.TempDir(), true, fixedDownload("x", &hits), io.Discard)
	_, err := p.Ensure(context.Background(), versionedTool("https://example.invalid/{version}"))
	if !errors.Is(err, ErrIncompatibleVersion) || !strings.Contains(err.Error(), "(version unreadable)") || hits != 0 {
		t.Fatalf("err = %v hits=%d; want unreadable PATH tool rejected", err, hits)
	}
}

// GitVersion 7 treats "/version" as a target path and exits 1, but answers
// "--version"; the fallback probe must detect and reject it.
func TestEnsureFallbackProbeRejectsChangedCLIMajor(t *testing.T) {
	skipOnWindows(t)
	bin := t.TempDir()
	writeToolExecutable(t, bin, "gitversion", "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 7.0.0; exit 0; fi\necho \"The working directory '$1' does not exist.\"\nexit 1\n")
	t.Setenv("PATH", bin)
	toolsDir := t.TempDir()
	cached := writeToolExecutable(t, toolsDir, "gitversion", "#!/bin/sh\nif [ \"$1\" = /version ]; then echo 6.7.0+Branch.main; exit 0; fi\nexit 134\n")
	hits := 0
	p := newCompatProvisioner(t, toolsDir, true, fixedDownload("x", &hits), io.Discard)
	tool := versionedTool("https://example.invalid/{version}")
	tool.Name, tool.Executable, tool.VersionArgs = "gitversion", "gitversion", []string{"/version"}
	tool.Download.DefaultVersion = "6.7.0"
	resolved, err := p.Ensure(context.Background(), tool)
	if err != nil || resolved.Path != cached || resolved.Source != "tools-dir" || resolved.Version != "6.7.0" {
		t.Fatalf("resolved = %+v err=%v; want cached 6.7.0 over PATH 7.0.0", resolved, err)
	}
	p.Opts.ToolsDir = t.TempDir()
	_, err = p.Ensure(context.Background(), tool)
	if !errors.Is(err, ErrIncompatibleVersion) || !strings.Contains(err.Error(), "gitversion 7.0.0 at") {
		t.Fatalf("err = %v; want 7.0.0 reported incompatible", err)
	}
}

func TestEnsureNeverVersionChecksExplicitPath(t *testing.T) {
	skipOnWindows(t)
	t.Setenv("PATH", t.TempDir())
	explicit := writeToolExecutable(t, t.TempDir(), "demo-tool", "#!/bin/sh\necho 1.0.0\n")
	hits := 0
	p := newCompatProvisioner(t, t.TempDir(), true, fixedDownload("x", &hits), io.Discard)
	tool := versionedTool("https://example.invalid/{version}")
	tool.ConfiguredPath = explicit
	resolved, err := p.Ensure(context.Background(), tool)
	if err != nil || resolved.Path != explicit || resolved.Source != "configured" {
		t.Fatalf("resolved = %+v err=%v; want explicit path untouched", resolved, err)
	}
}

func TestEmbeddedDownloadableToolsDeclareVersionProbe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TRONADOR_TOOLS_CONFIG", "")
	t.Setenv("TRONADOR_TOOLS_CONFIG_PATH", "")
	config, err := LoadConfig(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range config.Tools {
		if tool.Format == "unavailable" {
			continue
		}
		if len(tool.VersionArgs) == 0 {
			t.Errorf("%s has no version_args; major-version fallback would be skipped", tool.Name)
		}
	}
}

func TestProjectScopedConfigCannotSetVersionProbe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TRONADOR_TOOLS_CONFIG", "")
	t.Setenv("TRONADOR_TOOLS_CONFIG_PATH", "")
	workDir := t.TempDir()
	mustWriteFile(t, filepath.Join(workDir, ".tronador", "tools.json"),
		`{"tools":[{"name":"gh","version_args":["api","-X","PUT","x"],"version_regex":"(9)","platform_overrides":{"darwin":{"version_args":["evil"]}}}]}`)
	config, err := LoadConfig(workDir, "")
	if err != nil {
		t.Fatal(err)
	}
	gh, _ := config.Tool("gh")
	if len(gh.VersionArgs) != 1 || gh.VersionArgs[0] != "--version" || gh.VersionRegex != "" {
		t.Fatalf("project config changed probe: args=%v regex=%q", gh.VersionArgs, gh.VersionRegex)
	}
	explicit := filepath.Join(t.TempDir(), "tools.json")
	mustWriteFile(t, explicit, `{"tools":[{"name":"gh","version_args":["version"]}]}`)
	config, err = LoadConfig(workDir, explicit)
	if err != nil {
		t.Fatal(err)
	}
	if gh, _ = config.Tool("gh"); len(gh.VersionArgs) != 1 || gh.VersionArgs[0] != "version" {
		t.Fatalf("trusted explicit config should set probe, got %v", gh.VersionArgs)
	}
}

func TestIgnoreProjectConfigSkipsProjectOverrides(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TRONADOR_TOOLS_CONFIG", "")
	t.Setenv("TRONADOR_TOOLS_CONFIG_PATH", "")
	workDir := t.TempDir()
	mustWriteFile(t, filepath.Join(workDir, ".tronador", "tools.json"), `{"tools":[{"name":"gh","executable":"./evil"}]}`)
	p, err := NewProvisioner(Options{WorkDir: workDir, ToolsDir: t.TempDir(), IgnoreProjectConfig: true, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if gh, _ := p.Config.Tool("gh"); gh.executable() != "gh" {
		t.Fatalf("project executable override applied: %q", gh.executable())
	}
}

func resolveTrustedFixture(t *testing.T, server string) (bin, toolsDir string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"TRONADOR_TOOLS_INSTALL", "TRONADOR_TOOLS_SKIP_INSTALL", "TRONADOR_TOOLS_VERSION_CHECK", "TRONADOR_TOOLS_CONFIG_PATH"} {
		t.Setenv(name, "")
	}
	config := filepath.Join(t.TempDir(), "tools.json")
	mustWriteFile(t, config, `{"tools":[{"name":"demo-tool","default_version":"2.4.0","version_args":["--version"],"url_template":"`+server+`/{version}","format":"binary","platform_overrides":{}}]}`)
	t.Setenv("TRONADOR_TOOLS_CONFIG", config)
	toolsDir = t.TempDir()
	t.Setenv("TRONADOR_TOOLS_DIR", toolsDir)
	bin = t.TempDir()
	writeToolExecutable(t, bin, "demo-tool", "#!/bin/sh\necho 1.0.0\n")
	t.Setenv("PATH", bin)
	return bin, toolsDir
}

func TestResolveTrustedNeverRunsRejectedToolOrInstallsWithoutConsent(t *testing.T) {
	skipOnWindows(t)
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("#!/bin/sh\necho 2.4.0\n"))
	}))
	defer server.Close()
	_, toolsDir := resolveTrustedFixture(t, server.URL)

	if _, err := ResolveTrusted(context.Background(), "demo-tool", "DEMO", t.TempDir(), false, io.Discard); err == nil || !strings.Contains(err.Error(), "TRONADOR_TOOLS_INSTALL=true") || hits != 0 {
		t.Fatalf("err = %v hits=%d; want refusal without consent", err, hits)
	}
	t.Setenv("TRONADOR_TOOLS_INSTALL", "true")
	if _, err := ResolveTrusted(context.Background(), "demo-tool", "DEMO", t.TempDir(), true, io.Discard); err == nil || hits != 0 {
		t.Fatalf("err = %v hits=%d; dry-run must never install", err, hits)
	}
	path, err := ResolveTrusted(context.Background(), "demo-tool", "DEMO", t.TempDir(), false, io.Discard)
	if err != nil || path != filepath.Join(toolsDir, "demo-tool") || hits != 1 {
		t.Fatalf("path = %q err = %v hits=%d; want consented install", path, err, hits)
	}
}

func TestProjectOverriddenDefinitionKeepsLocalFirstBehavior(t *testing.T) {
	skipOnWindows(t)
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"TRONADOR_TOOLS_CONFIG", "TRONADOR_TOOLS_CONFIG_PATH", "TRONADOR_TOOLS_VERSION_CHECK", "TRONADOR_TOOLS_INSTALL", "TRONADOR_TOOLS_SKIP_INSTALL"} {
		t.Setenv(name, "")
	}
	workDir := t.TempDir()
	mustWriteFile(t, filepath.Join(workDir, ".tronador", "tools.json"),
		`{"tools":[{"name":"gomplate","default_version":"99.0.0","url_template":"https://attacker.invalid/{version}"}]}`)
	bin := t.TempDir()
	local := writeToolExecutable(t, bin, "gomplate", "#!/bin/sh\necho 'gomplate version 5.1.0'\n")
	t.Setenv("PATH", bin)
	hits := 0
	p, err := NewProvisioner(Options{WorkDir: workDir, ToolsDir: t.TempDir(), HTTPClient: fixedDownload("evil", &hits), Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := p.EnsureTool(context.Background(), "gomplate", "", "")
	if err != nil || resolved.Path != local || hits != 0 {
		t.Fatalf("resolved = %+v err=%v hits=%d; project config must not force a download", resolved, err, hits)
	}
}

func TestIncompatibleErrorIsSentinelAndProbeRunsInWorkDir(t *testing.T) {
	skipOnWindows(t)
	bin := t.TempDir()
	// Reports the version found in the current directory, like tfenv/asdf shims.
	writeToolExecutable(t, bin, "demo-tool", "#!/bin/sh\n/bin/cat .demo-version 2>/dev/null || echo 1.0.0\n")
	t.Setenv("PATH", bin)
	hits := 0
	p := newCompatProvisioner(t, t.TempDir(), true, fixedDownload("x", &hits), io.Discard)
	_, err := p.Ensure(context.Background(), versionedTool("https://example.invalid/{version}"))
	if !errors.Is(err, ErrIncompatibleVersion) {
		t.Fatalf("err = %v; want ErrIncompatibleVersion", err)
	}
	mustWriteFile(t, filepath.Join(p.Opts.WorkDir, ".demo-version"), "2.3.0\n")
	if resolved, err := p.Ensure(context.Background(), versionedTool("https://example.invalid/{version}")); err != nil || resolved.Version != "2.3.0" {
		t.Fatalf("resolved = %+v err=%v; probe must run in workdir", resolved, err)
	}
}

func TestResolveTrustedIgnoresSkipInstallFalse(t *testing.T) {
	skipOnWindows(t)
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("#!/bin/sh\necho 2.4.0\n"))
	}))
	defer server.Close()
	resolveTrustedFixture(t, server.URL)
	t.Setenv("TRONADOR_TOOLS_SKIP_INSTALL", "false")
	if _, err := ResolveTrusted(context.Background(), "demo-tool", "DEMO", t.TempDir(), false, io.Discard); err == nil || hits != 0 {
		t.Fatalf("err = %v hits=%d; only TRONADOR_TOOLS_INSTALL=true may opt in", err, hits)
	}
}
