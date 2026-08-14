package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	toolspkg "tronador-cli/internal/tools"
)

func TestDetectsEveryRegisteredMarker(t *testing.T) {
	registry := DefaultRegistry()
	for _, profile := range registry.Profiles {
		profile := profile
		t.Run(profile.ID, func(t *testing.T) {
			workdir := t.TempDir()
			markerDir := filepath.Join(workdir, ".cloudopsworks")
			if err := os.MkdirAll(markerDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(markerDir, profile.Markers[0].Name), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			detection, err := registry.Detect(workdir)
			if err != nil {
				t.Fatal(err)
			}
			if detection.ProfileID != profile.ID || detection.Marker != filepath.ToSlash(filepath.Join(".cloudopsworks", profile.Markers[0].Name)) {
				t.Fatalf("detection = %+v, want %s/%s", detection, profile.ID, profile.Markers[0].Name)
			}
		})
	}
}

func TestDetectRejectsUnknownAmbiguousDirectoryAndSymlinkMarkers(t *testing.T) {
	registry := DefaultRegistry()
	unknown, err := registry.Detect(t.TempDir())
	if err == nil || codeOf(err) != "project_implementation_unknown" || unknown.ProfileID != "" {
		t.Fatalf("unknown detection = %+v, err=%v", unknown, err)
	}

	ambiguous := t.TempDir()
	markerDir := filepath.Join(ambiguous, ".cloudopsworks")
	if err := os.MkdirAll(markerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{".golang", ".python"} {
		if err := os.WriteFile(filepath.Join(markerDir, marker), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := registry.Detect(ambiguous); err == nil || codeOf(err) != "project_implementation_ambiguous" {
		t.Fatalf("ambiguous error = %v", err)
	}

	invalidDir := t.TempDir()
	invalidMarkerDir := filepath.Join(invalidDir, ".cloudopsworks")
	if err := os.MkdirAll(filepath.Join(invalidMarkerDir, ".golang"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Detect(invalidDir); err == nil || codeOf(err) != "project_marker_invalid" {
		t.Fatalf("directory marker error = %v", err)
	}

	if runtime.GOOS != "windows" {
		symlinkDir := t.TempDir()
		symlinkMarkerDir := filepath.Join(symlinkDir, ".cloudopsworks")
		if err := os.MkdirAll(symlinkMarkerDir, 0o755); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(symlinkDir, "target")
		if err := os.WriteFile(target, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(symlinkMarkerDir, ".golang")); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Detect(symlinkDir); err == nil || codeOf(err) != "project_marker_invalid" {
			t.Fatalf("symlink marker error = %v", err)
		}
	}
}

func TestDescribeMatchesCapabilityMatrix(t *testing.T) {
	registry := DefaultRegistry()
	wants := map[string][]string{
		"androidsdk": {"init", "version"}, "docker": {"init", "version"}, "dotnet": {"init", "version"},
		"flutter": {"init", "version"}, "go": {"init", "version"}, "java": {"init", "version"},
		"node": {"init", "version"}, "python": {"init", "version"}, "rust": {"init", "version"}, "xcode": {"init", "version"},
		"terraform-module": {"format", "init", "lint"}, "terragrunt": {"clean", "clean-inputs", "format", "init", "lint"},
	}
	for id, want := range wants {
		profile, ok := registry.Profile(id)
		if !ok {
			t.Fatalf("profile %s missing", id)
		}
		got := profile.CapabilityIDs()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s capabilities = %v, want %v", id, got, want)
		}
	}
}

func TestDescribeIncludesFlagAndArgumentSchemas(t *testing.T) {
	workdir := fixture(t, ".terraform-module")
	runner := mustRunner(t, Options{WorkDir: workdir})
	detection, err := runner.Detect()
	if err != nil {
		t.Fatal(err)
	}
	descriptions, err := runner.Describe(detection)
	if err != nil {
		t.Fatal(err)
	}
	for _, description := range descriptions {
		if description.ID != "init" {
			continue
		}
		if len(description.Arguments) != 1 || description.Arguments[0].Name != "provider" || !description.Arguments[0].Required {
			t.Fatalf("init arguments = %+v", description.Arguments)
		}
		if flagNamed(description.Flags, "engine") || !flagNamed(description.Flags, "allow-network") || !flagNamed(description.Flags, "dry-run") {
			t.Fatalf("init flags = %+v", description.Flags)
		}
		return
	}
	t.Fatal("terraform init description missing")
}

func TestPlanRejectsNamespaceAndProviderInjection(t *testing.T) {
	workdir := fixture(t, ".terraform-module")
	runner := mustRunner(t, Options{WorkDir: workdir})
	if _, _, err := runner.Plan("terraform-module", []string{"init"}); err == nil || codeOf(err) != "project_capability_unsupported" {
		t.Fatalf("namespace error = %v", err)
	} else if !strings.Contains(err.Error(), "do not include an implementation namespace") {
		t.Fatalf("namespace error lacks migration hint: %v", err)
	}
	if _, _, err := runner.Plan("init", []string{"aws/credentials"}); err == nil || codeOf(err) != "project_argument_invalid" {
		t.Fatalf("provider injection error = %v", err)
	}
	detection, plan, err := runner.Plan("init", []string{"aws"})
	if err != nil {
		t.Fatal(err)
	}
	if detection.ProfileID != "terraform-module" || len(plan.ToolCalls) != 2 || plan.ToolCalls[0].ToolName != "boilerplate" || !contains(plan.ToolCalls[0].Arguments, "provider=aws") || plan.ToolCalls[1].ToolName != "git" {
		t.Fatalf("plan = %+v", plan)
	}
	if len(plan.Steps) != 3 || plan.Steps[0].Action != "remove-file" || contains(plan.ToolCalls[0].Arguments, "init") {
		t.Fatalf("terraform init steps = %+v", plan.Steps)
	}
}

func TestDryRunDoesNotResolveOrExecuteTools(t *testing.T) {
	workdir := fixture(t, ".golang")
	called := false
	runner := mustRunner(t, Options{WorkDir: workdir, DryRun: true, ExecuteTool: func(context.Context, ToolCall) (ToolExecution, error) {
		called = true
		return ToolExecution{}, errors.New("must not execute")
	}})
	result, err := runner.Run(context.Background(), "init", nil)
	if err != nil {
		t.Fatal(err)
	}
	if called || !result.DryRun || len(result.Tools) != 0 {
		t.Fatalf("dry-run result = %+v, called=%v", result, called)
	}
}

func TestVersionPreservesGitVersionFullSemVerAndUsesRepoConfig(t *testing.T) {
	for _, configName := range []string{"gitversion.yaml", "gitversion.yml"} {
		t.Run(configName, func(t *testing.T) {
			workdir := fixture(t, ".golang")
			configPath := filepath.Join(workdir, ".cloudopsworks", configName)
			if err := os.WriteFile(configPath, []byte("mode: Mainline\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			initializeGitRepository(t, workdir)
			runGit(t, workdir, "tag", "v2.4.5")
			if err := os.WriteFile(filepath.Join(workdir, "intermediate-change"), []byte("changed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			runGit(t, workdir, "add", "intermediate-change")
			runGit(t, workdir, "commit", "-m", "intermediate change")
			gitversion := executable(t, "gitversion")
			called := []ToolCall{}
			runner := mustRunner(t, Options{
				WorkDir: workdir, ToolPaths: map[string]string{"gitversion": gitversion}, NoInstallTools: true,
				ExecuteTool: func(_ context.Context, call ToolCall) (ToolExecution, error) {
					called = append(called, call)
					return ToolExecution{Stdout: `{"SemVer":"1.8.0-beta.1+69","FullSemVer":"1.8.0-beta.1+69"}`}, nil
				},
			})
			result, err := runner.Run(context.Background(), "version", nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.Version != "v1.8.0-beta.1+69" || result.TagCreated || string(mustRead(t, filepath.Join(workdir, "VERSION"))) != "v1.8.0-beta.1+69\n" {
				t.Fatalf("version result = %+v", result)
			}
			if len(called) != 1 || called[0].ToolName != "gitversion" || !contains(called[0].Arguments, "FullSemVer") || !contains(called[0].Arguments, "-config") || !contains(called[0].Arguments, filepath.Join(".cloudopsworks", configName)) || contains(called[0].Arguments, "tag") || contains(called[0].Arguments, "push") {
				t.Fatalf("typed calls = %+v", called)
			}
		})
	}
}

func TestVersionUsesExactHeadTagWithoutRunningGitVersion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		branch string
		tag    string
		want   string
	}{
		{name: "release branch prerelease tag", branch: "release/1.8", tag: "v1.8.0-beta.1", want: "v1.8.0-beta.1"},
		{name: "deploy metadata tag", tag: "v1.8.0+deploy-proc1", want: "v1.8.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workdir := fixture(t, ".golang")
			initializeGitRepository(t, workdir)
			if tc.branch != "" {
				runGit(t, workdir, "checkout", "-b", tc.branch)
			}
			runGit(t, workdir, "tag", tc.tag)

			called := false
			runner := mustRunner(t, Options{
				WorkDir: workdir, ToolPaths: map[string]string{"gitversion": executable(t, "gitversion")}, NoInstallTools: true,
				ExecuteTool: func(context.Context, ToolCall) (ToolExecution, error) {
					called = true
					return ToolExecution{}, errors.New("GitVersion must not run for an exact HEAD tag")
				},
			})

			result, err := runner.Run(context.Background(), "version", nil)
			if err != nil {
				t.Fatal(err)
			}
			if called || result.Version != tc.want || string(mustRead(t, filepath.Join(workdir, "VERSION"))) != tc.want+"\n" {
				t.Fatalf("tagged version result = %+v, GitVersion called=%v", result, called)
			}
		})
	}
}

func TestVersionSuppressesGitVersionOutputButPreservesCapture(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell process fixture is POSIX-specific")
	}
	workdir := fixture(t, ".golang")
	gitversion := shellExecutable(t, "gitversion", `printf '{"SemVer":"2.4.6"}\n'
printf 'gitversion warning\n' >&2`)
	var stdout, stderr bytes.Buffer
	runner := mustRunner(t, Options{WorkDir: workdir, ToolPaths: map[string]string{"gitversion": gitversion}, NoInstallTools: true, Stdout: &stdout, Stderr: &stderr})

	result, err := runner.Run(context.Background(), "version", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "Generated version v2.4.6 in VERSION (tag_created=false)\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want no GitVersion output", stderr.String())
	}
	if result.Stdout != "{\"SemVer\":\"2.4.6\"}\n" || result.Stderr != "gitversion warning\n" {
		t.Fatalf("captured output = %q/%q", result.Stdout, result.Stderr)
	}
}

func TestVersionFailureReplaysCapturedGitVersionOutput(t *testing.T) {
	workdir := fixture(t, ".golang")
	cases := []struct {
		name      string
		execution ToolExecution
		err       error
	}{
		{name: "process error", execution: ToolExecution{Stdout: "stdout\n", Stderr: "stderr\n"}, err: errors.New("launch failed")},
		{name: "non-zero exit", execution: ToolExecution{Stdout: "stdout\n", Stderr: "stderr\n", ExitStatus: 17}},
		{name: "empty version", execution: ToolExecution{Stderr: "stderr\n"}},
		{name: "invalid semver", execution: ToolExecution{Stdout: `{"SemVer":"not-a-version"}` + "\n", Stderr: "stderr\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var terminal bytes.Buffer
			runner := mustRunner(t, Options{
				WorkDir: workdir, ToolPaths: map[string]string{"gitversion": executable(t, "gitversion")}, NoInstallTools: true, Stdout: &terminal, Stderr: &terminal,
				ExecuteTool: func(context.Context, ToolCall) (ToolExecution, error) { return tc.execution, tc.err },
			})
			_, err := runner.Run(context.Background(), "version", nil)
			if err == nil {
				t.Fatal("version unexpectedly succeeded")
			}
			if got, want := terminal.String(), tc.execution.Stderr+tc.execution.Stdout; got != want {
				t.Fatalf("terminal output = %q, want stderr then stdout %q", got, want)
			}
			var projectErr *Error
			if !errors.As(err, &projectErr) || projectErr.Stdout != tc.execution.Stdout || projectErr.Stderr != tc.execution.Stderr {
				t.Fatalf("error did not preserve capture: %+v", err)
			}
		})
	}
}

func TestVersionJSONFailureDoesNotReplayGitVersionOutput(t *testing.T) {
	workdir := fixture(t, ".golang")
	var terminal bytes.Buffer
	runner := mustRunner(t, Options{
		WorkDir: workdir, ToolPaths: map[string]string{"gitversion": executable(t, "gitversion")}, NoInstallTools: true, JSON: true, Stdout: &terminal, Stderr: &terminal,
		ExecuteTool: func(context.Context, ToolCall) (ToolExecution, error) {
			return ToolExecution{Stdout: "stdout\n", Stderr: "stderr\n", ExitStatus: 1}, nil
		},
	})
	if _, err := runner.Run(context.Background(), "version", nil); err == nil {
		t.Fatal("version unexpectedly succeeded")
	}
	if terminal.Len() != 0 {
		t.Fatalf("JSON mode leaked GitVersion output: %q", terminal.String())
	}
}

func TestGitVersionCallsUseSuppressionAndOptionalConfig(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configName string
	}{
		{name: "absent"},
		{name: "yaml", configName: "gitversion.yaml"},
		{name: "yml", configName: "gitversion.yml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workdir := t.TempDir()
			if tc.configName != "" {
				config := filepath.Join(workdir, ".cloudopsworks", tc.configName)
				if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(config, []byte("mode: Mainline\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for _, binding := range []CapabilityBinding{
				{Operation: "generate-version"},
				{Operation: "application-init"},
			} {
				steps, err := buildOperationSteps(binding, Detection{ProfileID: "docker"}, nil, workdir)
				if err != nil {
					t.Fatal(err)
				}
				for _, step := range steps {
					if step.Call == nil || step.Call.ToolName != "gitversion" {
						continue
					}
					if !step.Call.SuppressOutput {
						t.Fatalf("GitVersion call was not suppressed: %+v", step.Call)
					}
					if tc.configName == "" && contains(step.Call.Arguments, "-config") {
						t.Fatalf("unexpected config flag: %+v", step.Call.Arguments)
					}
					if tc.configName != "" && !contains(step.Call.Arguments, filepath.Join(".cloudopsworks", tc.configName)) {
						t.Fatalf("missing config path in %+v", step.Call.Arguments)
					}
				}
			}
			fullSteps, err := buildOperationSteps(CapabilityBinding{Operation: "application-init"}, Detection{ProfileID: "flutter"}, nil, workdir)
			if err != nil {
				t.Fatal(err)
			}
			if len(fullSteps) < 2 || fullSteps[1].Call == nil || !fullSteps[1].Call.SuppressOutput {
				t.Fatalf("full-version call = %+v", fullSteps)
			}
		})
	}
}

func TestVersionUpdatesProfileMetadata(t *testing.T) {
	cases := []struct {
		profile string
		name    string
		before  string
		after   string
	}{
		{profile: "docker", name: "package.json", before: "{\n  \"version\": \"0.1.0\"\n}\n", after: "{\n  \"version\": \"2.4.6\"\n}\n"},
		{profile: "dotnet", name: "app.csproj", before: "<Project>\n  <Version>0.1.0</Version>\n</Project>\n", after: "<Project>\n  <Version>2.4.6</Version>\n</Project>\n"},
		{profile: "flutter", name: "pubspec.yaml", before: "name: app\nversion: 0.1.0\n", after: "name: app\nversion: 2.4.6\n"},
		{profile: "java", name: "pom.xml", before: "<project>\n  <parent><version>9.9.9</version></parent>\n  <version>0.1.0</version>\n  <dependencies><dependency><version>8.8.8</version></dependency></dependencies>\n</project>\n", after: "<project>\n  <parent><version>9.9.9</version></parent>\n  <version>2.4.6</version>\n  <dependencies><dependency><version>8.8.8</version></dependency></dependencies>\n</project>\n"},
		{profile: "node", name: "package.json", before: "{\n  \"version\": \"0.1.0\",\n  \"name\": \"app\"\n}\n", after: "{\n  \"version\": \"2.4.6\",\n  \"name\": \"app\"\n}\n"},
		{profile: "python", name: "pyproject.toml", before: "[project]\nversion = \"0.1.0\"\n", after: "[project]\nversion = \"2.4.6\"\n"},
		{profile: "rust", name: "Cargo.toml", before: "[package]\nversion = \"0.1.0\"\n\n[dependencies]\nother = { version = \"8.8.8\" }\n", after: "[package]\nversion = \"2.4.6\"\n\n[dependencies]\nother = { version = \"8.8.8\" }\n"},
	}
	for _, tc := range cases {
		t.Run(tc.profile, func(t *testing.T) {
			workdir := fixture(t, markerForProfile(t, tc.profile))
			if err := os.WriteFile(filepath.Join(workdir, tc.name), []byte(tc.before), 0o644); err != nil {
				t.Fatal(err)
			}
			runner := mustRunner(t, Options{
				WorkDir: workdir, ToolPaths: map[string]string{"gitversion": executable(t, "gitversion")}, NoInstallTools: true,
				ExecuteTool: func(_ context.Context, _ ToolCall) (ToolExecution, error) {
					return ToolExecution{Stdout: `{"SemVer":"2.4.6"}`}, nil
				},
			})
			if _, err := runner.Run(context.Background(), "version", nil); err != nil {
				t.Fatal(err)
			}
			if got := string(mustRead(t, filepath.Join(workdir, tc.name))); got != tc.after {
				t.Fatalf("metadata = %q, want %q", got, tc.after)
			}
		})
	}
}

func TestAutoEngineRejectsBothExplicitCandidates(t *testing.T) {
	runner := mustRunner(t, Options{
		WorkDir: fixture(t, ".terraform-module"), NoInstallTools: true,
		Engine: "auto",
		ToolPaths: map[string]string{
			"terraform": executable(t, "terraform"),
			"tofu":      executable(t, "tofu"),
		},
	})
	if _, err := runner.Run(context.Background(), "format", nil); err == nil || codeOf(err) != "project_tool_selection_ambiguous" {
		t.Fatalf("auto engine error = %v", err)
	}
}

func TestToolExitStatusIsPreserved(t *testing.T) {
	runner := mustRunner(t, Options{
		WorkDir: fixture(t, ".terraform-module"), ToolPaths: map[string]string{"terraform": executable(t, "terraform")},
		NoInstallTools: true, Engine: "terraform",
		ExecuteTool: func(_ context.Context, _ ToolCall) (ToolExecution, error) {
			return ToolExecution{ExitStatus: 7, Stderr: "validation failed\n"}, nil
		},
	})
	_, err := runner.Run(context.Background(), "format", nil)
	if err == nil || codeOf(err) != "project_operation_failed" {
		t.Fatalf("operation error = %v", err)
	}
	var projectErr *Error
	if !errors.As(err, &projectErr) || projectErr.ExitStatus != 7 {
		t.Fatalf("operation error status = %+v, want 7", projectErr)
	}
}

func TestExecuteToolPropagatesStdinAndStreamsCapturedOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell process fixture is POSIX-specific")
	}
	var stdout, stderr bytes.Buffer
	workdir := t.TempDir()
	tool := shellExecutable(t, "stdio-tool", `input=$(cat)
printf 'stdout:%s\n' "$input"
printf 'stderr:%s\n' "$input" >&2`)
	runner := mustRunner(t, Options{
		WorkDir: workdir,
		Stdin:   strings.NewReader("payload\n"),
		Stdout:  &stdout,
		Stderr:  &stderr,
	})

	execution, err := runner.executeTool(context.Background(), ToolCall{ResolvedExecutable: tool, WorkingDirectory: workdir})
	if err != nil {
		t.Fatal(err)
	}
	if execution.Stdout != "stdout:payload\n" || execution.Stderr != "stderr:payload\n" || execution.ExitStatus != 0 {
		t.Fatalf("execution = %+v", execution)
	}
	if stdout.String() != execution.Stdout || stderr.String() != execution.Stderr {
		t.Fatalf("streamed output = %q/%q, captured = %q/%q", stdout.String(), stderr.String(), execution.Stdout, execution.Stderr)
	}
}

func TestTerragruntInitStreamsRealToolOutputOnceAndPreservesCaptures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell process fixture is POSIX-specific")
	}
	workdir := fixture(t, ".iac")
	boilerplate := shellExecutable(t, "boilerplate", `input=$(cat)
printf 'boilerplate stdout:%s\n' "$input"
printf 'boilerplate stderr:%s\n' "$input" >&2`)
	terragrunt := shellExecutable(t, "terragrunt", `printf 'terragrunt stdout\n'
printf 'terragrunt stderr\n' >&2`)
	var stdout, stderr bytes.Buffer
	runner := mustRunner(t, Options{
		WorkDir: workdir,
		Stdin:   strings.NewReader("pipeline-input\n"),
		Stdout:  &stdout,
		Stderr:  &stderr,
		ToolPaths: map[string]string{
			"boilerplate": boilerplate,
			"terragrunt":  terragrunt,
		},
		NoInstallTools: true,
	})

	result, err := runner.Run(context.Background(), "init", nil)
	if err != nil {
		t.Fatal(err)
	}
	wantStdout := "boilerplate stdout:pipeline-input\nterragrunt stdout\nproject terragrunt init completed\n"
	wantStderr := "boilerplate stderr:pipeline-input\nterragrunt stderr\n"
	if stdout.String() != wantStdout || stderr.String() != wantStderr {
		t.Fatalf("streamed output = %q/%q, want %q/%q", stdout.String(), stderr.String(), wantStdout, wantStderr)
	}
	if result.Stdout != "boilerplate stdout:pipeline-input\nterragrunt stdout\n" || result.Stderr != wantStderr {
		t.Fatalf("captured result output = %q/%q", result.Stdout, result.Stderr)
	}
	if strings.Count(stdout.String(), "boilerplate stdout:") != 1 || strings.Count(stderr.String(), "boilerplate stderr:") != 1 {
		t.Fatal("child output was duplicated")
	}
}

func TestTerragruntInitPreservesRealToolExitStatusAndOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell process fixture is POSIX-specific")
	}
	workdir := fixture(t, ".iac")
	boilerplate := shellExecutable(t, "boilerplate", `printf 'boilerplate stdout\n'
printf 'boilerplate stderr\n' >&2`)
	terragrunt := shellExecutable(t, "terragrunt", `printf 'terragrunt stdout\n'
printf 'terragrunt stderr\n' >&2
exit 23`)
	var stdout, stderr bytes.Buffer
	runner := mustRunner(t, Options{
		WorkDir: workdir,
		Stdout:  &stdout,
		Stderr:  &stderr,
		ToolPaths: map[string]string{
			"boilerplate": boilerplate,
			"terragrunt":  terragrunt,
		},
		NoInstallTools: true,
	})

	_, err := runner.Run(context.Background(), "init", nil)
	if err == nil {
		t.Fatal("failing Terragrunt unexpectedly succeeded")
	}
	var projectErr *Error
	if !errors.As(err, &projectErr) || projectErr.ExitStatus != 23 {
		t.Fatalf("project error = %+v, want exit status 23", err)
	}
	if projectErr.Stdout != "terragrunt stdout\n" || projectErr.Stderr != "terragrunt stderr\n" {
		t.Fatalf("project error output = %q/%q", projectErr.Stdout, projectErr.Stderr)
	}
	if stdout.String() != "boilerplate stdout\nterragrunt stdout\n" || stderr.String() != "boilerplate stderr\nterragrunt stderr\n" {
		t.Fatalf("streamed failure output = %q/%q", stdout.String(), stderr.String())
	}
}

func TestJSONExecutionDoesNotStreamChildOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell process fixture is POSIX-specific")
	}
	var stdout, stderr bytes.Buffer
	workdir := t.TempDir()
	tool := shellExecutable(t, "json-tool", `printf 'child stdout\n'
printf 'child stderr\n' >&2`)
	runner := mustRunner(t, Options{
		WorkDir: workdir,
		JSON:    true,
		Stdout:  &stdout,
		Stderr:  &stderr,
	})
	execution, err := runner.executeTool(context.Background(), ToolCall{ResolvedExecutable: tool, WorkingDirectory: workdir})
	if err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("JSON execution leaked child output: %q/%q", stdout.String(), stderr.String())
	}
	if execution.Stdout != "child stdout\n" || execution.Stderr != "child stderr\n" {
		t.Fatalf("captured child output = %+v", execution)
	}
}

func TestTerraformPipelineAndTerragruntConfirmation(t *testing.T) {
	workdir := fixture(t, ".terraform-module")
	terraform := executable(t, "terraform")
	called := []ToolCall{}
	runner := mustRunner(t, Options{WorkDir: workdir, ToolPaths: map[string]string{"terraform": terraform}, NoInstallTools: true, Engine: "terraform", ExecuteTool: func(_ context.Context, call ToolCall) (ToolExecution, error) {
		called = append(called, call)
		return ToolExecution{}, nil
	}})
	if _, err := runner.Run(context.Background(), "format", nil); err != nil {
		t.Fatal(err)
	}
	if len(called) != 1 || called[0].ToolName != "terraform" || strings.Join(called[0].Arguments, " ") != "fmt" {
		t.Fatalf("terraform calls = %+v", called)
	}

	terragruntDir := fixture(t, ".iac")
	terragrunt := executable(t, "terragrunt")
	terragruntRunner := mustRunner(t, Options{WorkDir: terragruntDir, ToolPaths: map[string]string{"terragrunt": terragrunt}, NoInstallTools: true})
	if _, err := terragruntRunner.Run(context.Background(), "clean", nil); err == nil || codeOf(err) != "project_confirmation_required" {
		t.Fatalf("confirmation error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(terragruntDir, ".terraform"), 0o755); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(terragruntDir, "nested", ".terragrunt-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"tfplan.out", "plan.tfplan", ".terraform.lock.hcl"} {
		fullPath := filepath.Join(terragruntDir, "nested", path)
		if err := os.WriteFile(fullPath, []byte("generated"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	terragruntRunner.Opts.Yes = true
	if _, err := terragruntRunner.Run(context.Background(), "clean", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(terragruntDir, ".terraform")); err != nil {
		t.Fatalf("non-target Terraform directory was removed, err=%v", err)
	}
	if _, err := os.Stat(cacheDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Terragrunt cache still exists, err=%v", err)
	}
	for _, path := range []string{"tfplan.out", "plan.tfplan", ".terraform.lock.hcl"} {
		if _, err := os.Stat(filepath.Join(terragruntDir, "nested", path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("generated path %s still exists, err=%v", path, err)
		}
	}
}

func TestApplicationInitUsesEachTemplatePipeline(t *testing.T) {
	cases := map[string][]string{
		"androidsdk": {"repository-owner"},
		"docker":     {"repository-owner", "gitversion-major", "package-name", "package-version"},
		"dotnet":     {"repository-owner", "gitversion-major", "rename-solution", "rename-main-project", "rename-test-project", "rename-integration-project", "rename-main-project-file", "rename-test-project-file", "rename-integration-project-file", "dotnet-project-path", "dotnet-assembly-name", "dotnet-assembly-version", "dotnet-version", "dotnet-test-reference", "dotnet-integration-reference", "rewrite-solution"},
		"flutter":    {"repository-owner", "gitversion-full", "flutter-name", "flutter-version"},
		"go":         {"repository-owner", "remove-go-module", "go-mod-init", "go-mod-tidy", "rewrite-go-imports"},
		"java":       {"repository-owner", "gitversion-major", "maven-artifact", "maven-version"},
		"node":       {"repository-owner", "gitversion-major", "package-name", "package-version"},
		"python":     {"repository-owner", "gitversion-major", "python-metadata"},
		"rust":       {"repository-owner", "rust-metadata", "rewrite-rust-imports"},
		"xcode":      {"repository-owner"},
	}
	for profile, wantIDs := range cases {
		t.Run(profile, func(t *testing.T) {
			workdir := fixture(t, markerForProfile(t, profile))
			runner := mustRunner(t, Options{WorkDir: workdir})
			_, plan, err := runner.Plan("init", nil)
			if err != nil {
				t.Fatal(err)
			}
			gotIDs := make([]string, len(plan.Steps))
			for i, step := range plan.Steps {
				gotIDs[i] = step.ID
				if step.Call != nil && (step.Call.ToolName == "boilerplate" || step.Call.ToolName == "terraform" || step.Call.ToolName == "tofu" || step.Call.ToolName == "terragrunt") {
					t.Fatalf("application init unexpectedly dispatches infrastructure tool: %+v", step.Call)
				}
				if step.Call != nil && step.Call.ToolName == "yq" {
					t.Fatalf("application init still dispatches external yq: %+v", step.Call)
				}
			}
			if strings.Join(gotIDs, ",") != strings.Join(wantIDs, ",") {
				t.Fatalf("steps = %v, want %v", gotIDs, wantIDs)
			}
		})
	}
}

func TestApplicationInitExecutesCapturedValuesAndNativeSteps(t *testing.T) {
	workdir := fixture(t, ".docker")
	if err := os.WriteFile(filepath.Join(workdir, "package.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := []ToolCall{}
	project := filepath.Base(workdir)
	runner := mustRunner(t, Options{
		WorkDir: workdir, AllowNetwork: true, NoInstallTools: true,
		ToolPaths: map[string]string{"gh": executable(t, "gh"), "gitversion": executable(t, "gitversion")},
		ExecuteTool: func(_ context.Context, call ToolCall) (ToolExecution, error) {
			calls = append(calls, call)
			switch call.ToolName {
			case "gh":
				return ToolExecution{Stdout: "cloudopsworks\n"}, nil
			case "gitversion":
				return ToolExecution{Stdout: "1.2.3\n"}, nil
			default:
				return ToolExecution{}, nil
			}
		},
	})
	result, err := runner.Run(context.Background(), "init", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].ToolName != "gh" || calls[1].ToolName != "gitversion" {
		t.Fatalf("calls = %+v", calls)
	}
	packageJSON := string(mustRead(t, filepath.Join(workdir, "package.json")))
	if !strings.Contains(packageJSON, `"name":"@cloudopsworks/`+project+`"`) || !strings.Contains(packageJSON, `"version":"1.2.3"`) {
		t.Fatalf("native metadata update = %q", packageJSON)
	}
	if len(result.Steps) != 4 || len(result.Calls) != 2 {
		t.Fatalf("result pipeline = %+v", result)
	}
}

func TestApplicationInitGitVersionFailuresReplayCapture(t *testing.T) {
	cases := []struct {
		name      string
		profile   string
		execution ToolExecution
	}{
		{name: "major non-zero exit", profile: "docker", execution: ToolExecution{Stdout: "stdout\n", Stderr: "stderr\n", ExitStatus: 5}},
		{name: "full capture parse failure", profile: "flutter", execution: ToolExecution{Stdout: "\n", Stderr: "stderr\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			workdir := fixture(t, markerForProfile(t, tc.profile))
			var terminal bytes.Buffer
			runner := mustRunner(t, Options{
				WorkDir: workdir, AllowNetwork: true, NoInstallTools: true, Stdout: &terminal, Stderr: &terminal,
				ToolPaths: map[string]string{"gh": executable(t, "gh"), "gitversion": executable(t, "gitversion")},
				ExecuteTool: func(_ context.Context, call ToolCall) (ToolExecution, error) {
					if call.ToolName == "gh" {
						return ToolExecution{Stdout: "owner\n"}, nil
					}
					if !call.SuppressOutput {
						t.Fatalf("GitVersion call is not output-suppressed: %+v", call)
					}
					return tc.execution, nil
				},
			})
			_, err := runner.Run(context.Background(), "init", nil)
			if err == nil {
				t.Fatal("init unexpectedly succeeded")
			}
			if got, want := terminal.String(), "owner\n"+tc.execution.Stderr+tc.execution.Stdout; got != want {
				t.Fatalf("terminal output = %q, want %q", got, want)
			}
			var projectErr *Error
			if !errors.As(err, &projectErr) || projectErr.Stdout != tc.execution.Stdout || projectErr.Stderr != tc.execution.Stderr {
				t.Fatalf("error did not preserve capture: %+v", err)
			}
		})
	}
}

func TestJavaInitUpdatesOnlyMavenProjectMetadata(t *testing.T) {
	workdir := fixture(t, ".java")
	pom := `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <parent><groupId>example</groupId><version>9.9.9</version></parent>
  <artifactId>template</artifactId>
  <version>0.1.0</version>
  <dependencies><dependency><version>8.8.8</version></dependency></dependencies>
  <build><plugins><plugin><version>7.7.7</version></plugin></plugins></build>
</project>
`
	if err := os.WriteFile(filepath.Join(workdir, "pom.xml"), []byte(pom), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := mustRunner(t, Options{
		WorkDir: workdir, AllowNetwork: true, NoInstallTools: true,
		ToolPaths: map[string]string{"gh": executable(t, "gh"), "gitversion": executable(t, "gitversion")},
		ExecuteTool: func(_ context.Context, call ToolCall) (ToolExecution, error) {
			if call.ToolName == "gh" {
				return ToolExecution{Stdout: "cloudopsworks\n"}, nil
			}
			return ToolExecution{Stdout: "1.2.3\n"}, nil
		},
	})
	if _, err := runner.Run(context.Background(), "init", nil); err != nil {
		t.Fatal(err)
	}
	updated := string(mustRead(t, filepath.Join(workdir, "pom.xml")))
	project := filepath.Base(workdir)
	for _, want := range []string{
		"<artifactId>" + project + "</artifactId>",
		"<version>1.2.3-SNAPSHOT</version>",
		"<parent><groupId>example</groupId><version>9.9.9</version></parent>",
		"<version>8.8.8</version>",
		"<version>7.7.7</version>",
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("pom.xml missing %q after native init: %s", want, updated)
		}
	}
}

func TestApplicationInitEnforcesTheTemplateNetworkStep(t *testing.T) {
	called := false
	runner := mustRunner(t, Options{
		WorkDir: fixture(t, ".android"), NoInstallTools: true,
		ExecuteTool: func(_ context.Context, _ ToolCall) (ToolExecution, error) {
			called = true
			return ToolExecution{}, nil
		},
	})
	_, err := runner.Run(context.Background(), "init", nil)
	if err == nil || codeOf(err) != "project_network_not_allowed" || called {
		t.Fatalf("network policy error = %v, called=%v", err, called)
	}
}

func TestGoInitRunsTypedGoCommandsAndRewritesSources(t *testing.T) {
	workdir := fixture(t, ".golang")
	if err := os.WriteFile(filepath.Join(workdir, "go.mod"), []byte("module hello-service\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(workdir, "main.go")
	if err := os.WriteFile(source, []byte("package main\nimport _ \"hello-service/internal\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := []ToolCall{}
	runner := mustRunner(t, Options{
		WorkDir: workdir, AllowNetwork: true, NoInstallTools: true,
		ToolPaths: map[string]string{"gh": executable(t, "gh"), "gitversion": executable(t, "gitversion"), "go": executable(t, "go")},
		ExecuteTool: func(_ context.Context, call ToolCall) (ToolExecution, error) {
			calls = append(calls, call)
			if call.ToolName == "gh" {
				return ToolExecution{Stdout: "cloudopsworks\n"}, nil
			}
			return ToolExecution{}, nil
		},
	})
	if _, err := runner.Run(context.Background(), "init", nil); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || calls[1].ToolName != "go" || strings.Join(calls[1].Arguments, " ") != "mod init "+filepath.Base(workdir) || strings.Join(calls[2].Arguments, " ") != "mod tidy" {
		t.Fatalf("go calls = %+v", calls)
	}
	if _, err := os.Stat(filepath.Join(workdir, "go.mod")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("go.mod was not removed, err=%v", err)
	}
	if got := string(mustRead(t, source)); !strings.Contains(got, filepath.Base(workdir)+"/internal") {
		t.Fatalf("source was not rewritten: %q", got)
	}
}

func TestPythonAndRustInitUseNativeMetadataActions(t *testing.T) {
	pythonDir := fixture(t, ".python")
	if err := os.WriteFile(filepath.Join(pythonDir, "pyproject.toml"), []byte("[project]\nname = \"template\"\nversion = \"0.1.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pythonRunner := mustRunner(t, Options{
		WorkDir: pythonDir, AllowNetwork: true, NoInstallTools: true,
		ToolPaths: map[string]string{"gh": executable(t, "gh"), "gitversion": executable(t, "gitversion")},
		ExecuteTool: func(_ context.Context, call ToolCall) (ToolExecution, error) {
			if call.ToolName == "gh" {
				return ToolExecution{Stdout: "cloudopsworks\n"}, nil
			}
			return ToolExecution{Stdout: "1.2.3\n"}, nil
		},
	})
	if _, err := pythonRunner.Run(context.Background(), "init", nil); err != nil {
		t.Fatal(err)
	}
	pythonMetadata := string(mustRead(t, filepath.Join(pythonDir, "pyproject.toml")))
	if !strings.Contains(pythonMetadata, "name = \""+filepath.Base(pythonDir)+"\"") || !strings.Contains(pythonMetadata, "version = \"1.2.3\"") {
		t.Fatalf("python metadata = %q", pythonMetadata)
	}

	rustDir := fixture(t, ".rust")
	if err := os.WriteFile(filepath.Join(rustDir, "Cargo.toml"), []byte("[package]\nname = \"hello-api\"\nversion = \"0.1.0\"\n\n[[bin]]\nname = \"hello-api\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rustSource := filepath.Join(rustDir, "main.rs")
	if err := os.WriteFile(rustSource, []byte("fn main() { hello_api::run(); }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rustRunner := mustRunner(t, Options{
		WorkDir: rustDir, AllowNetwork: true, NoInstallTools: true,
		ToolPaths: map[string]string{"gh": executable(t, "gh"), "gitversion": executable(t, "gitversion")},
		ExecuteTool: func(_ context.Context, call ToolCall) (ToolExecution, error) {
			if call.ToolName == "gh" {
				return ToolExecution{Stdout: "cloudopsworks\n"}, nil
			}
			return ToolExecution{}, nil
		},
	})
	if _, err := rustRunner.Run(context.Background(), "init", nil); err != nil {
		t.Fatal(err)
	}
	rustMetadata := string(mustRead(t, filepath.Join(rustDir, "Cargo.toml")))
	if !strings.Contains(rustMetadata, "name = \""+filepath.Base(rustDir)+"\"") || !strings.Contains(string(mustRead(t, rustSource)), strings.ReplaceAll(filepath.Base(rustDir), "-", "_")+"::") {
		t.Fatalf("rust metadata/source = %q / %q", rustMetadata, string(mustRead(t, rustSource)))
	}
}

func TestApplicationInitRejectsSymlinkedMetadataOutsideWorkdir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	workdir := fixture(t, ".python")
	external := filepath.Join(t.TempDir(), "pyproject.toml")
	original := "[project]\nname = \"external\"\nversion = \"0.1.0\"\n"
	if err := os.WriteFile(external, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(workdir, "pyproject.toml")); err != nil {
		t.Fatal(err)
	}
	runner := mustRunner(t, Options{
		WorkDir: workdir, AllowNetwork: true, NoInstallTools: true,
		ToolPaths: map[string]string{"gh": executable(t, "gh"), "gitversion": executable(t, "gitversion")},
		ExecuteTool: func(_ context.Context, call ToolCall) (ToolExecution, error) {
			if call.ToolName == "gh" {
				return ToolExecution{Stdout: "cloudopsworks\n"}, nil
			}
			return ToolExecution{Stdout: "1.2.3\n"}, nil
		},
	})
	if _, err := runner.Run(context.Background(), "init", nil); err == nil || codeOf(err) != "project_operation_failed" {
		t.Fatalf("symlinked metadata error = %v", err)
	}
	if got := string(mustRead(t, external)); got != original {
		t.Fatalf("external metadata was modified: %q", got)
	}
}

func TestTerraformAndTerragruntInitUseTemplateBodies(t *testing.T) {
	terraformDir := fixture(t, ".terraform-module")
	tempProvider := filepath.Join(terraformDir, "provider.temp.tf")
	if err := os.WriteFile(tempProvider, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	boilerplate := executable(t, "boilerplate")
	terraformCalls := []ToolCall{}
	runner := mustRunner(t, Options{WorkDir: terraformDir, ToolPaths: map[string]string{"boilerplate": boilerplate, "git": executable(t, "git")}, NoInstallTools: true, ExecuteTool: func(_ context.Context, call ToolCall) (ToolExecution, error) {
		terraformCalls = append(terraformCalls, call)
		return ToolExecution{}, nil
	}})
	if _, err := runner.Run(context.Background(), "init", []string{"aws"}); err != nil {
		t.Fatal(err)
	}
	if len(terraformCalls) != 2 || terraformCalls[0].ToolName != "boilerplate" || terraformCalls[1].ToolName != "git" || contains(terraformCalls[0].Arguments, "init") || contains(terraformCalls[0].Arguments, "tofu") || contains(terraformCalls[0].Arguments, "terraform") || !contains(terraformCalls[1].Arguments, ".cloudopsworks/.provider") {
		t.Fatalf("terraform init calls = %+v", terraformCalls)
	}
	if _, err := os.Stat(tempProvider); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("provider temp file remains, err=%v", err)
	}

	terragruntDir := fixture(t, ".iac")
	for _, path := range []string{".inputs", ".inputs_mod", ".cloudopsworks/.inputs_cicd", ".inputs_state"} {
		full := filepath.Join(terragruntDir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("inputs"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	terragruntCalls := []ToolCall{}
	terragruntRunner := mustRunner(t, Options{WorkDir: terragruntDir, ToolPaths: map[string]string{"boilerplate": boilerplate, "terragrunt": executable(t, "terragrunt")}, NoInstallTools: true, ExecuteTool: func(_ context.Context, call ToolCall) (ToolExecution, error) {
		terragruntCalls = append(terragruntCalls, call)
		return ToolExecution{}, nil
	}})
	if _, err := terragruntRunner.Run(context.Background(), "init", nil); err != nil {
		t.Fatal(err)
	}
	if len(terragruntCalls) != 2 || terragruntCalls[0].ToolName != "boilerplate" || strings.Join(terragruntCalls[0].Arguments, " ") != strings.Join([]string{"--template-url", ".cloudopsworks/boilerplate/main", "--output-folder", ".", "--var-file=.inputs", "--var-file=.inputs_mod", "--var-file=.cloudopsworks/.inputs_cicd", "--var-file=.inputs_state", "--var=iac_project=" + filepath.Base(terragruntDir), "--disable-dependency-prompt"}, " ") || strings.Join(terragruntCalls[1].Arguments, " ") != "hcl format --exclude-dir .cloudopsworks" {
		t.Fatalf("terragrunt init calls = %+v", terragruntCalls)
	}
}

func TestTerragruntInitStopsBeforeFormattingWhenBoilerplateFails(t *testing.T) {
	workdir := fixture(t, ".iac")
	calls := []ToolCall{}
	runner := mustRunner(t, Options{WorkDir: workdir, ToolPaths: map[string]string{"boilerplate": executable(t, "boilerplate"), "terragrunt": executable(t, "terragrunt")}, NoInstallTools: true, ExecuteTool: func(_ context.Context, call ToolCall) (ToolExecution, error) {
		calls = append(calls, call)
		if call.ToolName == "boilerplate" {
			return ToolExecution{ExitStatus: 9, Stderr: "render failed"}, nil
		}
		return ToolExecution{}, nil
	}})
	_, err := runner.Run(context.Background(), "init", nil)
	if err == nil || codeOf(err) != "project_operation_failed" {
		t.Fatalf("failure = %v", err)
	}
	if len(calls) != 1 || calls[0].ToolName != "boilerplate" {
		t.Fatalf("calls after failure = %+v", calls)
	}
}

func TestAutoEngineDefaultsToOpenTofu(t *testing.T) {
	pathDir := t.TempDir()
	t.Setenv("PATH", pathDir)
	provisioner, err := toolspkg.NewProvisioner(toolspkg.Options{ToolsDir: t.TempDir(), WorkDir: t.TempDir(), SkipInstall: true})
	if err != nil {
		t.Fatal(err)
	}
	runner := mustRunner(t, Options{WorkDir: fixture(t, ".terraform-module"), NoInstallTools: true, Engine: "auto"})
	got, err := runner.selectEngine(provisioner)
	if err != nil || got != "tofu" {
		t.Fatalf("default engine = %q, err=%v", got, err)
	}
	defaultRunner := mustRunner(t, Options{WorkDir: fixture(t, ".terraform-module")})
	if defaultRunner.Opts.Engine != "tofu" {
		t.Fatalf("runner default engine = %q, want tofu", defaultRunner.Opts.Engine)
	}
}

func fixture(t *testing.T, marker string) string {
	t.Helper()
	workdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workdir, ".cloudopsworks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workdir, ".cloudopsworks", marker), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return workdir
}

func markerForProfile(t *testing.T, profileID string) string {
	t.Helper()
	profile, ok := DefaultRegistry().Profile(profileID)
	if !ok {
		t.Fatalf("profile %s missing", profileID)
	}
	return profile.Markers[0].Name
}

func executable(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellExecutable(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	contents := "#!/bin/sh\nset -eu\n" + body + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func initializeGitRepository(t *testing.T, workdir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required to test exact-HEAD tag detection")
	}
	runGit(t, workdir, "init")
	runGit(t, workdir, "config", "user.email", "project-test@example.invalid")
	runGit(t, workdir, "config", "user.name", "Project test")
	runGit(t, workdir, "add", ".")
	runGit(t, workdir, "commit", "-m", "initial project")
}

func runGit(t *testing.T, workdir string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = workdir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}

func mustRunner(t *testing.T, opts Options) *Runner {
	t.Helper()
	runner, err := NewRunner(opts)
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func codeOf(err error) string {
	var projectErr *Error
	if errors.As(err, &projectErr) {
		return projectErr.Code
	}
	return ""
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func flagNamed(values []FlagDefinition, want string) bool {
	for _, value := range values {
		if value.Name == want {
			return true
		}
	}
	return false
}

func TestVersionDryRunPreviewsChangesWithoutMutation(t *testing.T) {
	workdir := fixture(t, ".node")
	packagePath := filepath.Join(workdir, "package.json")
	if err := os.WriteFile(packagePath, []byte("{\n  \"version\": \"0.1.0\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	runner := mustRunner(t, Options{WorkDir: workdir, DryRun: true, Stdout: &stdout, ToolPaths: map[string]string{"gitversion": executable(t, "gitversion")}, NoInstallTools: true,
		ExecuteTool: func(context.Context, ToolCall) (ToolExecution, error) {
			return ToolExecution{Stdout: `{"FullSemVer":"2.4.6"}`}, nil
		},
	})
	result, err := runner.Run(context.Background(), "version", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "v2.4.6" || result.FileChanges == nil || len(*result.FileChanges) != 2 {
		t.Fatalf("preview result = %+v", result)
	}
	if got := string(mustRead(t, packagePath)); got != "{\n  \"version\": \"0.1.0\"\n}\n" {
		t.Fatalf("dry-run mutated metadata: %q", got)
	}
	if _, err := os.Stat(filepath.Join(workdir, "VERSION")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run created VERSION: %v", err)
	}
	if !strings.Contains(stdout.String(), "--- /dev/null\n+++ b/VERSION\n") || strings.Contains(stdout.String(), "DRY-RUN:") {
		t.Fatalf("preview stdout = %q", stdout.String())
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"file_changes"`) || strings.Contains(string(encoded), `"generated_artifacts"`) {
		t.Fatalf("preview JSON = %s", encoded)
	}
}
