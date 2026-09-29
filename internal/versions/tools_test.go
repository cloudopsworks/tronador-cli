package versions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	toolspkg "tronador-cli/internal/tools"
)

type recordingToolEnsurer struct {
	result toolspkg.ResolvedTool
	err    error
	calls  []ensureToolCall
}

type ensureToolCall struct {
	name, path, version string
}

func (e *recordingToolEnsurer) EnsureTool(_ context.Context, name, path, version string) (toolspkg.ResolvedTool, error) {
	e.calls = append(e.calls, ensureToolCall{name: name, path: path, version: version})
	return e.result, e.err
}

func TestCommandToolResolverMemoizesSuccessfulResolution(t *testing.T) {
	workDir := t.TempDir()
	executable := writeToolScript(t, t.TempDir(), "gh", "resolved")
	ensurer := &recordingToolEnsurer{result: toolspkg.ResolvedTool{Name: "gh", Path: executable}}
	factoryCalls := 0
	resolver := newCommandToolResolver(commandToolResolverOptions{
		workDir:  workDir,
		paths:    map[string]string{"gh": "bin/gh"},
		versions: map[string]string{"gh": "9.9.9"},
		newEnsurer: func() (toolEnsurer, error) {
			factoryCalls++
			return ensurer, nil
		},
	})

	first, err := resolver.resolve(context.Background(), "gh")
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	second, err := resolver.resolve(context.Background(), "gh")
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if first != executable || second != executable {
		t.Fatalf("resolved paths = %q, %q; want %q", first, second, executable)
	}
	if factoryCalls != 1 || len(ensurer.calls) != 1 {
		t.Fatalf("factory calls = %d, EnsureTool calls = %d; want 1 each", factoryCalls, len(ensurer.calls))
	}
	wantExplicit := filepath.Join("bin", "gh")
	if call := ensurer.calls[0]; call.name != "gh" || call.path != wantExplicit || call.version != "9.9.9" {
		t.Fatalf("EnsureTool call = %#v; want gh, %q, 9.9.9", call, wantExplicit)
	}
}

func TestCommandToolResolverDoesNotMemoizeFailure(t *testing.T) {
	ensurer := &recordingToolEnsurer{err: errors.New("missing")}
	resolver := newCommandToolResolver(commandToolResolverOptions{
		allowNetwork: false,
		newEnsurer:   func() (toolEnsurer, error) { return ensurer, nil },
	})
	for range 2 {
		_, err := resolver.resolve(context.Background(), "gitversion")
		if err == nil || !strings.Contains(err.Error(), "--allow-network") || !strings.Contains(err.Error(), "gitversion") {
			t.Fatalf("resolve error = %v; want named actionable error", err)
		}
	}
	if len(ensurer.calls) != 2 {
		t.Fatalf("EnsureTool calls = %d; want failures retried", len(ensurer.calls))
	}
}

func TestRunnerWorkflowResolvesOnlyDemandedTools(t *testing.T) {
	workDir := t.TempDir()
	badConfig := filepath.Join(t.TempDir(), "bad-tools.json")
	if err := os.WriteFile(badConfig, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner, err := NewRunner(Options{
		WorkDir: workDir, GitPath: echoExecutable(t), ToolsConfig: badConfig,
		NoInstallTools: true, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	workflow, err := runner.Workflow(WayOfWorkGitHubFlow)
	if err != nil {
		t.Fatalf("Workflow: %v", err)
	}
	if _, err := workflow.git(context.Background(), "status"); err != nil {
		t.Fatalf("Git-only call loaded tool config: %v", err)
	}
	if _, err := workflow.gitVersion(context.Background(), "FullSemVer"); err == nil || !strings.Contains(err.Error(), "parse tool config") {
		t.Fatalf("GitVersion error = %v; want lazy config parse error", err)
	}
}

func TestRunnerWorkflowDryRunBypassesNewResolver(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	bin := t.TempDir()
	writeToolScript(t, bin, "gitversion", "v1.2.3")
	writeToolScript(t, bin, "gh", "[]")
	t.Setenv("PATH", bin)
	badConfig := filepath.Join(t.TempDir(), "bad-tools.json")
	if err := os.WriteFile(badConfig, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner, err := NewRunner(Options{WorkDir: t.TempDir(), DryRun: true, AllowNetwork: true, ToolsConfig: badConfig})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	workflow, err := runner.Workflow(WayOfWorkGitHubFlow)
	if err != nil {
		t.Fatalf("Workflow: %v", err)
	}
	if got, err := workflow.gitVersion(context.Background(), "FullSemVer"); err != nil || strings.TrimSpace(got) != "v1.2.3" {
		t.Fatalf("dry-run GitVersion = %q, %v", got, err)
	}
	if got, err := workflow.gh(context.Background(), "pr", "list"); err != nil || strings.TrimSpace(got) != "[]" {
		t.Fatalf("dry-run gh pr list = %q, %v", got, err)
	}
	if got, err := workflow.gh(context.Background(), "pr", "create"); err != nil || got != "" {
		t.Fatalf("dry-run gh pr create = %q, %v; want suppressed legacy mutation", got, err)
	}
}

func TestRunnerWorkflowExplicitRelativeToolPathUsesCallerDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	callerDir := t.TempDir()
	workDir := t.TempDir()
	t.Chdir(callerDir)
	pathDir := t.TempDir()
	writeToolScript(t, pathDir, "gh", "path-gh")
	writeToolScript(t, pathDir, "gitversion", "path-gitversion")
	t.Setenv("PATH", pathDir)
	for _, tool := range []string{"gh", "gitversion"} {
		t.Run(tool, func(t *testing.T) {
			writeToolScript(t, filepath.Join(callerDir, "bin"), tool, "caller-"+tool)
			runner, err := NewRunner(Options{
				WorkDir: workDir, NoInstallTools: true,
				ToolPaths: map[string]string{tool: filepath.Join("bin", tool)},
			})
			if err != nil {
				t.Fatalf("NewRunner: %v", err)
			}
			workflow, err := runner.Workflow(WayOfWorkGitHubFlow)
			if err != nil {
				t.Fatalf("Workflow: %v", err)
			}
			var got string
			if tool == "gh" {
				got, err = workflow.gh(context.Background(), "pr", "list")
			} else {
				got, err = workflow.gitVersion(context.Background(), "MajorMinorPatch")
			}
			if err != nil || strings.TrimSpace(got) != "caller-"+tool {
				t.Fatalf("relative %s result = %q, %v", tool, got, err)
			}
		})
	}
}

func TestRunnerWorkflowBareExplicitToolNameUsesPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	bin := t.TempDir()
	for _, tool := range []string{"gh", "gitversion"} {
		name := "custom-" + tool
		writeToolScript(t, bin, name, "bare-"+tool)
	}
	t.Setenv("PATH", bin)
	for _, tool := range []string{"gh", "gitversion"} {
		t.Run(tool, func(t *testing.T) {
			runner, err := NewRunner(Options{
				WorkDir: t.TempDir(), NoInstallTools: true,
				ToolPaths: map[string]string{tool: "custom-" + tool},
			})
			if err != nil {
				t.Fatalf("NewRunner: %v", err)
			}
			workflow, err := runner.Workflow(WayOfWorkGitHubFlow)
			if err != nil {
				t.Fatalf("Workflow: %v", err)
			}
			var got string
			if tool == "gh" {
				got, err = workflow.gh(context.Background(), "pr", "list")
			} else {
				got, err = workflow.gitVersion(context.Background(), "MajorMinorPatch")
			}
			if err != nil || strings.TrimSpace(got) != "bare-"+tool {
				t.Fatalf("bare %s result = %q, %v", tool, got, err)
			}
		})
	}
}

func TestRunnerWorkflowUsesCachedToolWithoutNetwork(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	toolsDir := t.TempDir()
	writeToolScript(t, toolsDir, "gh", "from-cache")
	t.Setenv("PATH", t.TempDir())
	runner, err := NewRunner(Options{WorkDir: t.TempDir(), ToolsDir: toolsDir, NoInstallTools: true})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	workflow, err := runner.Workflow(WayOfWorkGitHubFlow)
	if err != nil {
		t.Fatalf("Workflow: %v", err)
	}
	got, err := workflow.gh(context.Background(), "pr", "list")
	if err != nil || strings.TrimSpace(got) != "from-cache" {
		t.Fatalf("cached gh result = %q, %v", got, err)
	}
}

func TestRunnerWorkflowPATHWinsOverConfiguredVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	bin := t.TempDir()
	writeToolScript(t, bin, "gitversion", "from-path")
	t.Setenv("PATH", bin)
	toolsDir := filepath.Join(t.TempDir(), "cache")
	runner, err := NewRunner(Options{
		WorkDir: t.TempDir(), ToolsDir: toolsDir, AllowNetwork: true,
		ToolVersions: map[string]string{"gitversion": "99.0.0"},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	workflow, err := runner.Workflow(WayOfWorkGitHubFlow)
	if err != nil {
		t.Fatalf("Workflow: %v", err)
	}
	got, err := workflow.gitVersion(context.Background(), "FullSemVer")
	if err != nil || strings.TrimSpace(got) != "from-path" {
		t.Fatalf("GitVersion = %q, %v; want PATH executable", got, err)
	}
	if _, err := os.Stat(toolsDir); !os.IsNotExist(err) {
		t.Fatalf("tools cache was created despite PATH hit: %v", err)
	}
}

func TestRunnerWorkflowProvisionsMissingToolWithConfiguredVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	requested := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested <- r.URL.Path
		_, _ = w.Write([]byte("#!/bin/sh\nprintf 'downloaded-gh\\n'\n"))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "tools.json")
	downloadURL := server.URL + "/{version}/gh"
	config := fmt.Sprintf(`{"tools":[{"name":"gh","executable":"gh","default_version":"1.0.0","url_template":%q,"format":"binary","platform_overrides":{%q:{"url_template":%q,"format":"binary"}}}]}`,
		downloadURL, runtime.GOOS+"/"+runtime.GOARCH, downloadURL)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	toolsDir := filepath.Join(t.TempDir(), "cache")
	var stderr bytes.Buffer
	runner, err := NewRunner(Options{
		WorkDir: t.TempDir(), ToolsDir: toolsDir, ToolsConfig: configPath, AllowNetwork: true,
		ToolVersions: map[string]string{"gh": "9.8.7"}, Stderr: &stderr,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	workflow, err := runner.Workflow(WayOfWorkGitHubFlow)
	if err != nil {
		t.Fatalf("Workflow: %v", err)
	}
	got, err := workflow.gh(context.Background(), "pr", "list")
	if err != nil || strings.TrimSpace(got) != "downloaded-gh" {
		t.Fatalf("downloaded gh result = %q, %v", got, err)
	}
	if path := <-requested; path != "/9.8.7/gh" {
		t.Fatalf("download path = %q; want /9.8.7/gh", path)
	}
	if !strings.Contains(stderr.String(), "Installing gh 9.8.7") {
		t.Fatalf("stderr = %q; want installation diagnostic", stderr.String())
	}
	if !toolspkg.ExecutableExists(filepath.Join(toolsDir, toolspkg.ExecutableName("gh"))) {
		t.Fatal("provisioned gh executable is missing")
	}
}

func TestResolverProvisioningFailuresPreserveContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("downloaded shell fixture")
	}
	for _, tc := range []struct {
		name       string
		cancel     bool
		status     int
		want       string
		wantCancel bool
	}{
		{name: "failed HTTP", status: http.StatusServiceUnavailable, want: "HTTP 503"},
		{name: "cancelled", cancel: true, status: http.StatusOK, want: "context canceled", wantCancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				if tc.status == http.StatusOK {
					_, _ = w.Write([]byte("#!/bin/sh\nexit 0\n"))
				}
			}))
			defer server.Close()
			configPath := filepath.Join(t.TempDir(), "tools.json")
			downloadURL := server.URL + "/{version}/gh"
			config := fmt.Sprintf(`{"tools":[{"name":"gh","executable":"gh","default_version":"1.0.0","url_template":%q,"format":"binary","platform_overrides":{%q:{"url_template":%q,"format":"binary"}}}]}`,
				downloadURL, runtime.GOOS+"/"+runtime.GOARCH, downloadURL)
			if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", t.TempDir())
			t.Setenv("PATH", t.TempDir())
			resolver := newCommandToolResolver(commandToolResolverOptions{
				toolsDir: filepath.Join(t.TempDir(), "cache"), toolsConfig: configPath, allowNetwork: true, stderr: io.Discard,
			})
			ctx := context.Background()
			if tc.cancel {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			_, err := resolver.resolve(ctx, "gh")
			if err == nil || !strings.Contains(err.Error(), "resolve gh tool") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("resolve error = %v; want contextual %q", err, tc.want)
			}
			if tc.wantCancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("resolve error does not preserve context cancellation: %v", err)
			}
		})
	}
}

func TestResolverInstallGateCannotBeEnabledByEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name, install, skip          string
		allowNetwork, noInstallTools bool
	}{
		{name: "install true", install: "true"},
		{name: "skip false", skip: "false"},
		{name: "both", install: "true", skip: "false"},
		{name: "no-install vetoes install true", install: "true", allowNetwork: true, noInstallTools: true},
		{name: "environment skip remains authoritative", skip: "true", allowNetwork: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("PATH", t.TempDir())
			t.Setenv("TRONADOR_TOOLS_INSTALL", tc.install)
			t.Setenv("TRONADOR_TOOLS_SKIP_INSTALL", tc.skip)
			toolsDir := filepath.Join(t.TempDir(), "missing-cache")
			resolver := newCommandToolResolver(commandToolResolverOptions{
				toolsDir: toolsDir, allowNetwork: tc.allowNetwork, noInstallTools: tc.noInstallTools,
			})
			_, err := resolver.resolve(context.Background(), "gh")
			if err == nil {
				t.Fatal("resolve unexpectedly succeeded")
			}
			if !tc.allowNetwork && !strings.Contains(err.Error(), "--allow-network") {
				t.Fatalf("resolve error = %v; want network-gate hint", err)
			}
			if tc.noInstallTools && !strings.Contains(err.Error(), "--no-install-tools") {
				t.Fatalf("resolve error = %v; want no-install hint", err)
			}
			if _, statErr := os.Stat(toolsDir); !os.IsNotExist(statErr) {
				t.Fatalf("tools dir exists after gated resolution: %v", statErr)
			}
		})
	}
}

func writeToolScript(t *testing.T, dir, name, output string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' '"+output+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func echoExecutable(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{"/bin/echo", "/usr/bin/echo"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Skip("echo executable unavailable")
	return ""
}
