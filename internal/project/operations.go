package project

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// applicationInitSteps is the profile polymorphism boundary. The registry
// keeps the public capability stable while this builder translates each
// template's code/init target into the same inspectable step representation.
func applicationInitSteps(
	profile, workdir string,
	call func(string, ...string) ToolCall,
	tool func(string, string, ToolCall, string, string, bool) OperationStep,
	native func(string, string, string, map[string]string) OperationStep,
) []OperationStep {
	steps := []OperationStep{
		tool("repository-owner", "resolve the GitHub repository owner", call("gh", "repo", "view", "--json", "name,owner", "-q", ".owner.login"), "owner", "plain", true),
	}
	project := projectNameForProfile(profile, workdir)
	version := "{{version}}"
	majorVersion := tool("gitversion-major", "calculate the template's MajorMinorPatch version", gitVersionToolCall(workdir, "-output", "json", "-showvariable", "MajorMinorPatch"), "version", "version", false)
	fullVersion := tool("gitversion-full", "calculate the template's FullSemVer version", gitVersionToolCall(workdir, "-output", "json", "-showvariable", "FullSemVer"), "version", "version", false)

	switch profile {
	case "docker", "node":
		steps = append(steps, majorVersion,
			native("package-name", "set the scoped package name", "update-metadata", map[string]string{"format": "json", "path": "package.json", "selector": "name", "value": "@{{owner}}/{{project}}"}),
			native("package-version", "set the package version", "update-metadata", map[string]string{"format": "json", "path": "package.json", "selector": "version", "value": "{{version}}"}),
		)
	case "dotnet":
		steps = append(steps, majorVersion,
			native("rename-solution", "rename the .NET solution", "rename", map[string]string{"source": "HelloWorldApi.sln", "destination": project + ".sln"}),
			native("rename-main-project", "rename the .NET application directory", "rename", map[string]string{"source": "HelloWorldApi", "destination": project}),
			native("rename-test-project", "rename the .NET test directory", "rename", map[string]string{"source": "HelloWorldApi.Tests", "destination": project + ".Tests"}),
			native("rename-integration-project", "rename the .NET integration-test directory", "rename", map[string]string{"source": "HelloWorldApi.Tests.Integration", "destination": project + ".Tests.Integration"}),
			native("rename-main-project-file", "rename the .NET application project file", "rename", map[string]string{"source": project + "/HelloWorldApi.csproj", "destination": project + "/" + project + ".csproj"}),
			native("rename-test-project-file", "rename the .NET test project file", "rename", map[string]string{"source": project + ".Tests/HelloWorldApi.Tests.csproj", "destination": project + ".Tests/" + project + ".Tests.csproj"}),
			native("rename-integration-project-file", "rename the .NET integration project file", "rename", map[string]string{"source": project + ".Tests.Integration/HelloWorldApi.Tests.Integration.csproj", "destination": project + ".Tests.Integration/" + project + ".Tests.Integration.csproj"}),
			native("dotnet-project-path", "update the shared .NET project path", "update-metadata", map[string]string{"format": "yaml", "path": ".github/vars/inputs-global.yaml", "selector": "dotnet.project_path", "value": "{{project}}"}),
			native("dotnet-assembly-name", "set the .NET assembly name", "update-metadata", map[string]string{"format": "xml", "path": project + "/" + project + ".csproj", "selector": "Project/PropertyGroup/AssemblyName", "value": "{{project}}"}),
			native("dotnet-assembly-version", "set the .NET assembly version", "update-metadata", map[string]string{"format": "xml", "path": project + "/" + project + ".csproj", "selector": "Project/PropertyGroup/AssemblyVersion", "value": "{{version}}"}),
			native("dotnet-version", "set the .NET package version", "update-metadata", map[string]string{"format": "xml", "path": project + "/" + project + ".csproj", "selector": "Project/PropertyGroup/Version", "value": "{{version}}"}),
			native("dotnet-test-reference", "update the .NET test project reference", "update-metadata", map[string]string{"format": "xml", "path": project + ".Tests/" + project + ".Tests.csproj", "selector": "Project/ItemGroup[1]/ProjectReference", "attribute": "Include", "value": "../{{project}}/{{project}}.csproj"}),
			native("dotnet-integration-reference", "update the .NET integration project reference", "update-metadata", map[string]string{"format": "xml", "path": project + ".Tests.Integration/" + project + ".Tests.Integration.csproj", "selector": "Project/ItemGroup[1]/ProjectReference", "attribute": "Include", "value": "../{{project}}/{{project}}.csproj"}),
			native("rewrite-solution", "rewrite solution project names", "replace-text", map[string]string{"path": project + ".sln", "old": "HelloWorldApi", "new": project}),
		)
	case "flutter":
		steps = append(steps, fullVersion,
			native("flutter-name", "set the Flutter package name", "update-metadata", map[string]string{"format": "yaml", "path": "pubspec.yaml", "selector": "name", "value": "{{project}}"}),
			native("flutter-version", "set the Flutter package version", "update-metadata", map[string]string{"format": "yaml", "path": "pubspec.yaml", "selector": "version", "value": "{{version}}"}),
		)
	case "go":
		steps = append(steps,
			native("remove-go-module", "remove the template Go module", "remove-file", map[string]string{"path": "go.mod", "force": "false"}),
			tool("go-mod-init", "initialize the Go module", call("go", "mod", "init", project), "", "", false),
			tool("go-mod-tidy", "resolve Go module dependencies", call("go", "mod", "tidy"), "", "", false),
			native("rewrite-go-imports", "rewrite template Go imports", "replace-text-recursive", map[string]string{"suffix": ".go", "old": "hello-service", "new": project}),
		)
	case "java":
		steps = append(steps, majorVersion,
			native("maven-artifact", "set the Maven artifact identifier", "update-metadata", map[string]string{"format": "xml", "path": "pom.xml", "selector": "project/artifactId", "value": "{{project}}"}),
			native("maven-version", "set the Maven snapshot version", "update-metadata", map[string]string{"format": "xml", "path": "pom.xml", "selector": "project/version", "value": "{{version}}-SNAPSHOT"}),
		)
	case "python":
		steps = append(steps, majorVersion,
			native("python-metadata", "set Python project metadata", "update-python-metadata", map[string]string{"path": "pyproject.toml", "name": project, "version": version}),
		)
	case "rust":
		steps = append(steps,
			native("rust-metadata", "set Rust package and binary names", "update-rust-metadata", map[string]string{"path": "Cargo.toml", "name": project}),
			native("rewrite-rust-imports", "rewrite template Rust crate references", "replace-text-recursive", map[string]string{"suffix": ".rs", "old": "hello_api::", "new": strings.ReplaceAll(project, "-", "_") + "::"}),
		)
	case "androidsdk", "xcode":
		// The template target performs its prerequisites, assertions, and owner
		// lookup but has no profile-specific mutation after that lookup.
	default:
		return nil
	}
	return steps
}

func terragruntInitArguments(workdir string) ([]string, error) {
	args := []string{"--template-url", ".cloudopsworks/boilerplate/main", "--output-folder", "."}
	for _, path := range []string{".inputs", ".inputs_mod", ".cloudopsworks/.inputs_cicd", ".inputs_state"} {
		candidate, err := safeProjectPath(workdir, path)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		args = append(args, "--var-file="+path)
	}
	project := filepath.Base(filepath.Clean(workdir))
	if project == "" || project == "." || project == string(filepath.Separator) {
		return nil, fmt.Errorf("working directory has no project basename")
	}
	args = append(args, "--var=iac_project="+project, "--disable-dependency-prompt")
	return args, nil
}

func projectNameForProfile(profile, workdir string) string {
	name := filepath.Base(filepath.Clean(workdir))
	if profile != "dotnet" {
		return name
	}
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '-' })
	for i, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(part)
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	return strings.Join(parts, "")
}

func projectCode(workdir string) string {
	return strings.ReplaceAll(projectNameForProfile("", workdir), "-", "_")
}

func hasNetworkStep(steps []OperationStep) bool {
	for _, step := range steps {
		if step.Network {
			return true
		}
	}
	return false
}

func replaceEngineCalls(plan *OperationPlan, engine string) {
	for i := range plan.Steps {
		if plan.Steps[i].Call != nil && plan.Steps[i].Call.ToolName == "iac-engine" {
			plan.Steps[i].Call.ToolName = engine
		}
	}
	plan.ToolCalls = toolCallsFromSteps(plan.Steps)
}

func setResolvedStepPaths(plan *OperationPlan, resolved []ResolvedToolResult) {
	for i := range plan.Steps {
		if plan.Steps[i].Call == nil {
			continue
		}
		if path, ok := resolvedPathByName(resolved, plan.Steps[i].Call.ToolName); ok {
			plan.Steps[i].Call.ResolvedExecutable = path
		}
	}
	plan.ToolCalls = toolCallsFromSteps(plan.Steps)
}

func (r *Runner) executeSteps(ctx context.Context, detection Detection, plan OperationPlan, result Result) (Result, error) {
	steps := cloneSteps(plan.Steps)
	vars := map[string]string{
		"project":      projectNameForProfile(detection.ProfileID, r.Opts.WorkDir),
		"project_code": projectCode(r.Opts.WorkDir),
	}
	actualCalls := make([]ToolCall, 0, len(steps))
	for i := range steps {
		step := &steps[i]
		if step.Kind == StepNative {
			if err := executeNativeAction(r.Opts.WorkDir, step.Action, renderParameters(step.Parameters, vars)); err != nil {
				return Result{}, withDetection(operationStepError(plan.Capability, *step, err, "", "", 1), detection)
			}
			continue
		}
		if step.Kind != StepTool || step.Call == nil {
			return Result{}, withDetection(projectError("project_operation_failed", fmt.Sprintf("step %q is not a valid typed operation", step.ID)), detection)
		}
		call := *step.Call
		call.Arguments = renderArguments(call.Arguments, vars)
		call.Arguments = expandDynamicArguments(r.Opts.WorkDir, call.Arguments)
		step.Call = &call
		actualCalls = append(actualCalls, call)
		execution, err := r.executeTool(ctx, call)
		result.Stdout += execution.Stdout
		result.Stderr += execution.Stderr
		if err == nil && execution.ExitStatus != 0 {
			err = fmt.Errorf("%s exited with status %d", call.ToolName, execution.ExitStatus)
		}
		if err != nil {
			status := execution.ExitStatus
			if status == 0 {
				status = 1
			}
			if call.SuppressOutput {
				r.printToolFailure(execution)
			}
			return Result{}, withDetection(operationStepError(plan.Capability, *step, err, execution.Stdout, execution.Stderr, status), detection)
		}
		if step.Capture != "" {
			value, parseErr := parseStepCapture(step.CaptureParser, execution.Stdout)
			if parseErr != nil {
				if call.SuppressOutput {
					r.printToolFailure(execution)
				}
				return Result{}, withDetection(operationStepError(plan.Capability, *step, parseErr, execution.Stdout, execution.Stderr, 1), detection)
			}
			vars[step.Capture] = value
		}
	}
	result.Calls = actualCalls
	result.Steps = steps
	return result, nil
}

func gitVersionToolCall(workdir string, args ...string) ToolCall {
	arguments := append([]string(nil), args...)
	if configPath := gitVersionConfigPath(workdir); configPath != "" {
		arguments = append(arguments, "-config", configPath)
	}
	return ToolCall{ToolName: "gitversion", Arguments: arguments, WorkingDirectory: workdir, SuppressOutput: true}
}

// gitVersionConfigPath reports the repo-local GitVersion config as a path
// relative to workdir. GitVersion runs with workdir as its working directory,
// so the relative form resolves identically and keeps command lines portable.
func gitVersionConfigPath(workdir string) string {
	for _, name := range []string{"gitversion.yaml", "gitversion.yml"} {
		relative := filepath.Join(".cloudopsworks", name)
		info, err := os.Stat(filepath.Join(workdir, relative))
		if err == nil && info.Mode().IsRegular() {
			return relative
		}
	}
	return ""
}

func operationStepError(capability string, step OperationStep, cause error, stdout, stderr string, status int) *Error {
	tool := ""
	if step.Call != nil {
		tool = step.Call.ToolName
	}
	return &Error{Code: "project_operation_failed", Command: "project", Capability: capability, Tool: tool, Stdout: stdout, Stderr: stderr, ExitStatus: status, Cause: fmt.Errorf("step %s: %w", step.ID, cause)}
}

func renderArguments(arguments []string, vars map[string]string) []string {
	result := make([]string, len(arguments))
	for i, argument := range arguments {
		result[i] = renderValue(argument, vars)
	}
	return result
}

func renderParameters(parameters map[string]string, vars map[string]string) map[string]string {
	result := make(map[string]string, len(parameters))
	for key, value := range parameters {
		result[key] = renderValue(value, vars)
	}
	return result
}

func renderValue(value string, vars map[string]string) string {
	for key, replacement := range vars {
		value = strings.ReplaceAll(value, "{{"+key+"}}", replacement)
	}
	return value
}

func expandDynamicArguments(workdir string, arguments []string) []string {
	result := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		if argument != "{{terraform-files}}" {
			result = append(result, argument)
			continue
		}
		matches, err := filepath.Glob(filepath.Join(workdir, "*.tf"))
		if err != nil {
			continue
		}
		for _, match := range matches {
			if info, statErr := os.Lstat(match); statErr == nil && info.Mode().IsRegular() {
				result = append(result, filepath.Base(match))
			}
		}
	}
	return result
}

func parseStepCapture(parser, output string) (string, error) {
	value := strings.TrimSpace(output)
	if value == "" {
		return "", fmt.Errorf("capture returned no output")
	}
	if parser == "plain" {
		return strings.TrimSpace(strings.Split(value, "\n")[0]), nil
	}
	if parser == "version" {
		value = parseGitVersionOutput(value)
		if value == "" {
			return "", fmt.Errorf("version capture returned no version")
		}
		return strings.ReplaceAll(value, "+", "-"), nil
	}
	return value, nil
}

func executeNativeAction(workdir, action string, params map[string]string) error {
	switch action {
	case "update-metadata":
		return updateMetadataFile(workdir, metadataUpdate{
			Format: params["format"], Path: params["path"], Selector: params["selector"], Attribute: params["attribute"], Value: params["value"],
		})
	case "remove-file":
		path, err := safeProjectPath(workdir, params["path"])
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) && params["force"] == "true" {
				return nil
			}
			return err
		}
		return nil
	case "rename":
		source, err := safeProjectPath(workdir, params["source"])
		if err != nil {
			return err
		}
		destination, err := safeProjectPath(workdir, params["destination"])
		if err != nil {
			return err
		}
		return os.Rename(source, destination)
	case "replace-text":
		return replaceTextFile(workdir, params["path"], params["old"], params["new"])
	case "replace-text-recursive":
		return replaceTextRecursive(workdir, params["suffix"], params["old"], params["new"])
	case "update-python-metadata":
		return updateTOMLFields(workdir, params["path"], "[project]", []tomlField{{"name", params["name"]}, {"version", params["version"]}})
	case "update-rust-metadata":
		if err := updateTOMLFields(workdir, params["path"], "[package]", []tomlField{{"name", params["name"]}}); err != nil {
			return err
		}
		return updateTOMLFields(workdir, params["path"], "[[bin]]", []tomlField{{"name", params["name"]}})
	default:
		return fmt.Errorf("unknown native action %q", action)
	}
}

func safeProjectPath(workdir, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("unsafe project path %q", relative)
	}
	relative = filepath.Clean(filepath.FromSlash(relative))
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe project path %q", relative)
	}
	root, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	path := filepath.Join(workdir, relative)
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("project path %q is a symlink", relative)
		}
		realPath, evalErr := filepath.EvalSymlinks(path)
		if evalErr != nil {
			return "", fmt.Errorf("resolve project path %q: %w", relative, evalErr)
		}
		if !withinPath(root, realPath) {
			return "", fmt.Errorf("project path %q escapes the workdir", relative)
		}
		return path, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}
	parent := filepath.Dir(path)
	for {
		realParent, evalErr := filepath.EvalSymlinks(parent)
		if evalErr == nil {
			if !withinPath(root, realParent) {
				return "", fmt.Errorf("project path %q escapes the workdir", relative)
			}
			return path, nil
		}
		if !errors.Is(evalErr, os.ErrNotExist) || parent == filepath.Dir(parent) {
			return "", fmt.Errorf("resolve project path %q: %w", relative, evalErr)
		}
		parent = filepath.Dir(parent)
	}
}

func withinPath(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func replaceTextFile(workdir, relative, old, replacement string) error {
	path, err := safeProjectPath(workdir, relative)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated := strings.ReplaceAll(string(data), old, replacement)
	if updated == string(data) {
		return nil
	}
	return os.WriteFile(path, []byte(updated), fileMode(path))
}

func replaceTextRecursive(workdir, suffix, old, replacement string) error {
	return filepath.WalkDir(workdir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != workdir && entry.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), suffix) {
			return nil
		}
		relative, err := filepath.Rel(workdir, path)
		if err != nil {
			return err
		}
		return replaceTextFile(workdir, relative, old, replacement)
	})
}

func fileMode(path string) os.FileMode {
	info, err := os.Stat(path)
	if err != nil {
		return 0o644
	}
	return info.Mode().Perm()
}
