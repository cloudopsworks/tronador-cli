package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	toolspkg "tronador-cli/internal/tools"
)

// Options controls project detection, planning, tool resolution, and execution.
type Options struct {
	WorkDir        string
	ToolsDir       string
	ToolsConfig    string
	NoInstallTools bool
	AllowNetwork   bool
	ToolVersions   map[string]string
	ToolPaths      map[string]string
	Engine         string
	Yes            bool
	DryRun         bool
	JSON           bool
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
	Registry       Registry
	ExecuteTool    ExecuteToolFunc
}

// ExecuteToolFunc is injectable so acceptance tests can prove plans without
// starting a child process or downloading an external tool.
type ExecuteToolFunc func(context.Context, ToolCall) (ToolExecution, error)

// ToolExecution contains captured child output and its exit status.
type ToolExecution struct {
	Stdout     string
	Stderr     string
	ExitStatus int
}

// ToolCall is a typed executable invocation. It is never converted to a shell
// command string.
type ToolCall struct {
	ToolName           string   `json:"tool"`
	ResolvedExecutable string   `json:"resolved_executable"`
	Arguments          []string `json:"arguments"`
	WorkingDirectory   string   `json:"working_directory"`
	SuppressOutput     bool     `json:"suppress_output,omitempty"`
}

const (
	StepTool   = "tool"
	StepNative = "native"
)

// OperationStep is one ordered, typed action in a polymorphic capability.
// Tool steps use an argument vector; native steps use a named action and
// validated parameters. Neither form is converted to a shell command.
type OperationStep struct {
	ID            string            `json:"id"`
	Kind          string            `json:"kind"`
	Description   string            `json:"description,omitempty"`
	Call          *ToolCall         `json:"tool_call,omitempty"`
	Action        string            `json:"action,omitempty"`
	Parameters    map[string]string `json:"parameters,omitempty"`
	Capture       string            `json:"capture,omitempty"`
	CaptureParser string            `json:"capture_parser,omitempty"`
	Network       bool              `json:"network,omitempty"`
}

// OperationPlan is built completely before dependency resolution or mutation.
type OperationPlan struct {
	Implementation     string            `json:"implementation"`
	Marker             string            `json:"detected_marker"`
	Capability         string            `json:"capability"`
	Arguments          map[string]string `json:"arguments,omitempty"`
	Executor           string            `json:"executor"`
	Operation          string            `json:"operation"`
	ToolRequirements   []ToolRequirement `json:"tool_requirements,omitempty"`
	ToolCalls          []ToolCall        `json:"calls,omitempty"`
	Steps              []OperationStep   `json:"steps,omitempty"`
	MutationClass      string            `json:"mutation_class"`
	GeneratedArtifacts []string          `json:"generated_artifacts,omitempty"`
	NetworkPolicy      string            `json:"network_policy"`
	ConfirmationPolicy string            `json:"confirmation_policy"`
	DryRun             string            `json:"dry_run"`
	NativePaths        []string          `json:"native_paths,omitempty"`
}

// ResolvedToolResult is the stable result representation for a resolved tool.
type ResolvedToolResult struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Path   string `json:"path,omitempty"`
}

// Result is emitted for successful project operations.
type Result struct {
	Command            string               `json:"command"`
	Implementation     string               `json:"implementation"`
	DetectedMarker     string               `json:"detected_marker"`
	Capability         string               `json:"capability"`
	Arguments          map[string]string    `json:"arguments,omitempty"`
	Executor           string               `json:"executor"`
	Tools              []ResolvedToolResult `json:"tools,omitempty"`
	Calls              []ToolCall           `json:"calls,omitempty"`
	Steps              []OperationStep      `json:"steps,omitempty"`
	MutationClass      string               `json:"mutation_class"`
	GeneratedArtifacts []string             `json:"generated_artifacts,omitempty"`
	Removed            []string             `json:"removed,omitempty"`
	Version            string               `json:"version,omitempty"`
	TagCreated         bool                 `json:"tag_created"`
	DryRun             bool                 `json:"dry_run,omitempty"`
	Plan               *OperationPlan       `json:"plan,omitempty"`
	Stdout             string               `json:"stdout,omitempty"`
	Stderr             string               `json:"stderr,omitempty"`
	ExitStatus         int                  `json:"exit_status"`
}

// CapabilityDescription joins a shared definition to its profile binding for
// the authoritative capabilities discovery surface.
type CapabilityDescription struct {
	ID                 string               `json:"id"`
	Aliases            []string             `json:"aliases,omitempty"`
	Arguments          []ArgumentDefinition `json:"arguments,omitempty"`
	Flags              []FlagDefinition     `json:"flags,omitempty"`
	Semantics          string               `json:"semantics"`
	ResultFields       []string             `json:"result_fields,omitempty"`
	Executor           string               `json:"executor"`
	Operation          string               `json:"operation"`
	Tools              []ToolRequirement    `json:"tools,omitempty"`
	Prerequisites      []string             `json:"prerequisites,omitempty"`
	MutationClass      string               `json:"mutation_class"`
	GeneratedArtifacts []string             `json:"generated_artifacts,omitempty"`
	NetworkPolicy      string               `json:"network_policy"`
	ConfirmationPolicy string               `json:"confirmation_policy"`
	DryRun             string               `json:"dry_run"`
}

// Runner executes project operations.
type Runner struct {
	Opts     Options
	Registry Registry
}

// NewRunner normalizes options and validates the profile registry.
func NewRunner(opts Options) (*Runner, error) {
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	abs, err := filepath.Abs(opts.WorkDir)
	if err != nil {
		return nil, wrapProjectError("project_implementation_unknown", "resolve workdir", err)
	}
	opts.WorkDir = abs
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Registry.Profiles == nil {
		opts.Registry = DefaultRegistry()
	}
	if err := opts.Registry.Validate(); err != nil {
		return nil, err
	}
	if opts.Engine == "" {
		opts.Engine = "tofu"
	}
	opts.Engine = strings.ToLower(strings.TrimSpace(opts.Engine))
	if opts.Engine != "auto" && opts.Engine != "terraform" && opts.Engine != "tofu" {
		return nil, projectError("project_argument_invalid", "--engine must be terraform, tofu, or auto")
	}
	return &Runner{Opts: opts, Registry: opts.Registry}, nil
}

// Detect returns the implementation selected by the marker contract.
func (r *Runner) Detect() (Detection, error) { return r.Registry.Detect(r.Opts.WorkDir) }

// Describe returns the exact capabilities advertised by a detected profile.
func (r *Runner) Describe(detection Detection) ([]CapabilityDescription, error) {
	profile, ok := r.Registry.Profile(detection.ProfileID)
	if !ok {
		return nil, withDetection(projectError("project_registry_invalid", "detected profile is not registered"), detection)
	}
	descriptions := make([]CapabilityDescription, 0, len(profile.Capabilities))
	for _, binding := range profile.Capabilities {
		definition := r.Registry.Definitions[binding.Capability]
		arguments := binding.Arguments
		if arguments == nil {
			arguments = definition.Arguments
		}
		descriptions = append(descriptions, CapabilityDescription{
			ID: definition.ID, Aliases: append([]string(nil), definition.Aliases...), Arguments: append([]ArgumentDefinition(nil), arguments...),
			Flags:     capabilityFlags(binding),
			Semantics: definition.Semantics, ResultFields: append([]string(nil), definition.ResultFields...), Executor: binding.Executor,
			Operation: binding.Operation, Tools: cloneRequirements(binding.Tools), Prerequisites: append([]string(nil), binding.Prerequisites...),
			MutationClass: binding.MutationClass, GeneratedArtifacts: append([]string(nil), binding.GeneratedArtifacts...), NetworkPolicy: binding.NetworkPolicy,
			ConfirmationPolicy: binding.ConfirmationPolicy, DryRun: binding.DryRun,
		})
	}
	sort.Slice(descriptions, func(i, j int) bool { return descriptions[i].ID < descriptions[j].ID })
	return descriptions, nil
}

func capabilityFlags(binding CapabilityBinding) []FlagDefinition {
	flags := []FlagDefinition{
		{Name: "workdir", Type: "string", Description: "target project directory", Default: "."},
		{Name: "json", Type: "bool", Description: "emit stable JSON output", Default: "false"},
		{Name: "dry-run", Type: "bool", Description: "show the operation plan without mutation or tool resolution", Default: "false"},
	}
	if len(binding.Tools) > 0 {
		flags = append(flags,
			FlagDefinition{Name: "tools-dir", Type: "string", Description: "override the provisioned tool cache"},
			FlagDefinition{Name: "tools-config", Type: "string", Description: "tool catalog override file"},
			FlagDefinition{Name: "no-install-tools", Type: "bool", Description: "resolve tools without installation", Default: "false"},
			FlagDefinition{Name: "allow-network", Type: "bool", Description: "permit direct tool provisioning", Default: "false"},
			FlagDefinition{Name: "tool-version", Type: "stringArray", Description: "pin a tool version as name=version"},
			FlagDefinition{Name: "tool-path", Type: "stringArray", Description: "use an explicit executable as name=path"},
		)
	}
	if binding.Executor == ExecutorNative && binding.ConfirmationPolicy == "yes_for_noninteractive" {
		flags = append(flags, FlagDefinition{Name: "yes", Type: "bool", Description: "confirm destructive operations", Default: "false"})
	}
	if hasEngineRequirement(binding.Tools) {
		flags = append(flags, FlagDefinition{Name: "engine", Type: "string", Description: "select Terraform or OpenTofu", Default: "tofu"})
	}
	return flags
}

func hasEngineRequirement(requirements []ToolRequirement) bool {
	for _, requirement := range requirements {
		if requirement.Name == "iac-engine" || len(requirement.Alternatives) > 0 {
			return true
		}
	}
	return false
}

// Plan resolves detection, capability identity, and positional arguments. It
// does not resolve tools, invoke child processes, or mutate the workspace.
func (r *Runner) Plan(capability string, args []string) (Detection, OperationPlan, error) {
	detection, err := r.Detect()
	if err != nil {
		return Detection{}, OperationPlan{}, err
	}
	profile, ok := r.Registry.Profile(detection.ProfileID)
	if !ok {
		return detection, OperationPlan{}, withDetection(projectError("project_registry_invalid", "detected profile is not registered"), detection)
	}
	capability = strings.ToLower(strings.TrimSpace(capability))
	if capability == "" {
		return detection, OperationPlan{}, withDetection(projectError("project_capability_unsupported", "a project capability is required"), detection)
	}
	if namespace, exists := r.Registry.Profile(capability); exists {
		return detection, OperationPlan{}, withDetection(&Error{Code: "project_capability_unsupported", Command: "project", Capability: capability, Hint: "Use `tronador project capabilities` to list valid capabilities for this detected implementation; do not include an implementation namespace.", ExitStatus: 1, Cause: fmt.Errorf("implementation namespace %q is not part of the project command grammar", namespace.ID)}, detection)
	}
	binding, definition, ok := profile.Capability(capability, r.Registry.Definitions)
	if !ok {
		valid := strings.Join(profile.CapabilityIDs(), ", ")
		return detection, OperationPlan{}, withDetection(&Error{Code: "project_capability_unsupported", Command: "project", Capability: capability, Hint: "valid capabilities: " + valid, ExitStatus: 1, Cause: fmt.Errorf("capability %q is not supported", capability)}, detection)
	}
	arguments, argumentErr := validateArguments(binding, definition, args)
	if argumentErr != nil {
		argumentErr.Capability = binding.Capability
		argumentErr.RequestedArguments = append([]string(nil), args...)
		return detection, OperationPlan{}, withDetection(argumentErr, detection)
	}
	plan := OperationPlan{
		Implementation: detection.ProfileID, Marker: detection.Marker, Capability: binding.Capability,
		Arguments: arguments, Executor: binding.Executor, Operation: binding.Operation,
		ToolRequirements: cloneRequirements(binding.Tools),
		MutationClass:    binding.MutationClass, GeneratedArtifacts: append([]string(nil), binding.GeneratedArtifacts...),
		NetworkPolicy: binding.NetworkPolicy, ConfirmationPolicy: binding.ConfirmationPolicy, DryRun: binding.DryRun,
	}
	var steps []OperationStep
	if binding.Executor != ExecutorNative {
		var stepErr error
		steps, stepErr = buildOperationSteps(binding, detection, arguments, r.Opts.WorkDir)
		if stepErr != nil {
			return detection, OperationPlan{}, withDetection(stepErr, detection)
		}
	}
	plan.Steps = steps
	plan.ToolCalls = toolCallsFromSteps(steps)
	if binding.Executor == ExecutorNative {
		plan.NativePaths, err = nativePaths(binding.Operation, r.Opts.WorkDir)
		if err != nil {
			return detection, OperationPlan{}, withDetection(wrapProjectError("project_operation_failed", "inspect native operation paths", err), detection)
		}
	}
	return detection, plan, nil
}

func validateArguments(binding CapabilityBinding, definition CapabilityDefinition, args []string) (map[string]string, *Error) {
	schema := binding.Arguments
	if schema == nil {
		schema = definition.Arguments
	}
	minimum := countRequired(schema)
	if len(args) < minimum || len(args) > len(schema) {
		return nil, projectError("project_argument_invalid", fmt.Sprintf("%s expects between %d and %d positional argument(s), got %d", binding.Capability, minimum, len(schema), len(args)))
	}
	result := map[string]string{}
	for i, argument := range schema {
		value := argument.Default
		if i < len(args) {
			value = strings.TrimSpace(args[i])
		}
		if argument.Required && value == "" {
			return nil, projectError("project_argument_invalid", fmt.Sprintf("argument %q is required", argument.Name))
		}
		if argument.Pattern != "" {
			matched, err := regexp.MatchString(argument.Pattern, value)
			if err != nil || !matched {
				return nil, projectError("project_argument_invalid", fmt.Sprintf("argument %q has an invalid value", argument.Name))
			}
		}
		if len(argument.Allowed) > 0 && !hasString(argument.Allowed, value) {
			return nil, projectError("project_argument_invalid", fmt.Sprintf("argument %q must be one of %s", argument.Name, strings.Join(argument.Allowed, ", ")))
		}
		result[argument.Name] = value
	}
	return result, nil
}

func countRequired(schema []ArgumentDefinition) int {
	count := 0
	for _, argument := range schema {
		if argument.Required {
			count++
		}
	}
	return count
}

func buildOperationSteps(binding CapabilityBinding, detection Detection, arguments map[string]string, workdir string) ([]OperationStep, error) {
	call := func(tool string, args ...string) ToolCall {
		return ToolCall{ToolName: tool, Arguments: append([]string(nil), args...), WorkingDirectory: workdir}
	}
	tool := func(id, description string, call ToolCall, capture, parser string, network bool) OperationStep {
		return OperationStep{ID: id, Kind: StepTool, Description: description, Call: &call, Capture: capture, CaptureParser: parser, Network: network}
	}
	native := func(id, description, action string, params map[string]string) OperationStep {
		return OperationStep{ID: id, Kind: StepNative, Description: description, Action: action, Parameters: params}
	}
	switch binding.Operation {
	case "application-init":
		steps := applicationInitSteps(detection.ProfileID, workdir, call, tool, native)
		if len(steps) == 0 {
			return nil, fmt.Errorf("application profile %q has no registered init pipeline", detection.ProfileID)
		}
		return steps, nil
	case "generate-version":
		return []OperationStep{tool("gitversion", "calculate project version", gitVersionToolCall(workdir, "-output", "json"), "", "", false)}, nil
	case "terraform-init":
		return []OperationStep{
			native("remove-provider-temp", "remove stale provider template", "remove-file", map[string]string{"path": "provider.temp.tf", "force": "true"}),
			tool("boilerplate-init", "render the provider-specific module template", call("boilerplate", "--template-url", ".cloudopsworks/boilerplate/main", "--output-folder", ".", "--var", "provider="+arguments["provider"], "--disable-dependency-prompt", "--non-interactive"), "", "", false),
			tool("stage-generated-terraform", "stage the provider marker and generated Terraform files", call("git", "add", ".cloudopsworks/.provider", "{{terraform-files}}"), "", "", false),
		}, nil
	case "terraform-lint":
		return []OperationStep{
			tool("iac-validate", "validate Terraform/OpenTofu configuration", call("iac-engine", "validate"), "", "", false),
			tool("iac-format-check", "check Terraform/OpenTofu formatting", call("iac-engine", "fmt", "-check"), "", "", false),
		}, nil
	case "terraform-format":
		return []OperationStep{tool("iac-format", "format Terraform/OpenTofu configuration", call("iac-engine", "fmt"), "", "", false)}, nil
	case "terragrunt-init":
		args, err := terragruntInitArguments(workdir)
		if err != nil {
			return nil, wrapProjectError("project_operation_failed", "inspect Terragrunt init inputs", err)
		}
		return []OperationStep{
			tool("boilerplate-init", "render the Terragrunt project template", call("boilerplate", args...), "", "", false),
			tool("terragrunt-format", "format rendered Terragrunt configuration", call("terragrunt", "hcl", "format", "--exclude-dir", ".cloudopsworks"), "", "", false),
		}, nil
	case "terragrunt-lint":
		return []OperationStep{
			tool("terragrunt-validate", "validate Terragrunt configuration", call("terragrunt", "validate"), "", "", false),
			tool("iac-validate", "validate the underlying Terraform/OpenTofu configuration", call("iac-engine", "validate"), "", "", false),
		}, nil
	case "terragrunt-format":
		return []OperationStep{tool("terragrunt-format", "format Terragrunt HCL", call("terragrunt", "hcl", "format"), "", "", false)}, nil
	default:
		return nil, fmt.Errorf("operation %q has no registered step builder", binding.Operation)
	}
}

func toolCallsFromSteps(steps []OperationStep) []ToolCall {
	calls := make([]ToolCall, 0, len(steps))
	for _, step := range steps {
		if step.Call != nil {
			calls = append(calls, *step.Call)
		}
	}
	return calls
}

func cloneRequirements(requirements []ToolRequirement) []ToolRequirement {
	result := make([]ToolRequirement, len(requirements))
	copy(result, requirements)
	for i := range result {
		result[i].Alternatives = append([]ToolCandidate(nil), result[i].Alternatives...)
	}
	return result
}

func nativePaths(operation, workdir string) ([]string, error) {
	switch operation {
	case "terragrunt-clean":
		return terragruntCleanPaths(workdir)
	case "terragrunt-clean-inputs":
		return []string{
			filepath.Join(workdir, ".inputs"),
			filepath.Join(workdir, ".inputs_mod"),
			filepath.Join(workdir, ".cloudopsworks", ".inputs_cicd"),
		}, nil
	default:
		return nil, nil
	}
}

func terragruntCleanPaths(workdir string) ([]string, error) {
	paths := []string{
		filepath.Join(workdir, ".inputs"),
		filepath.Join(workdir, ".inputs_mod"),
		filepath.Join(workdir, ".cloudopsworks", ".inputs_cicd"),
	}
	terragruntPaths := map[string]bool{}
	walkErr := filepath.WalkDir(workdir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != workdir && entry.Name() == ".git" {
				return fs.SkipDir
			}
			if path != workdir && entry.Name() == ".terragrunt-cache" {
				if !terragruntPaths[path] {
					paths = append(paths, path)
					terragruntPaths[path] = true
				}
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.Type().IsRegular() && (entry.Name() == "tfplan.out" || strings.HasSuffix(entry.Name(), ".tfplan") || entry.Name() == ".terraform.lock.hcl") {
			paths = append(paths, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return paths, nil
}

// Run plans, resolves all declared dependencies, enforces policy, and executes
// the typed plan. Dry-run stops before tool resolution as required by DESIGN.md.
func (r *Runner) Run(ctx context.Context, capability string, args []string) (Result, error) {
	detection, plan, err := r.Plan(capability, args)
	if err != nil {
		return Result{}, err
	}
	if plan.ConfirmationPolicy == "yes_for_noninteractive" && !r.Opts.Yes && !r.Opts.DryRun {
		return Result{}, withDetection(&Error{Code: "project_confirmation_required", Command: "project", Capability: plan.Capability, Hint: "rerun with --yes", ExitStatus: 1, Cause: errors.New("destructive operation requires --yes")}, detection)
	}
	if r.Opts.DryRun && r.Opts.Engine != "auto" {
		plan.ToolRequirements = replaceEngineRequirement(plan.ToolRequirements, r.Opts.Engine)
		replaceEngineCalls(&plan, r.Opts.Engine)
	}
	result := resultFromPlan(plan, detection, r.Opts.DryRun)
	if r.Opts.DryRun {
		if r.Opts.JSON {
			result.Plan = &plan
		} else {
			fmt.Fprintf(r.Opts.Stdout, "DRY-RUN: %s\n", formatPlan(plan))
		}
		return result, nil
	}
	if hasNetworkStep(plan.Steps) && !r.Opts.AllowNetwork {
		return Result{}, withDetection(&Error{Code: "project_network_not_allowed", Command: "project", Capability: plan.Capability, Hint: "pass --allow-network to permit the template's declared network operation", ExitStatus: 1, Cause: errors.New("operation contains a direct network step")}, detection)
	}
	resolved, err := r.resolveTools(ctx, &plan)
	if err != nil {
		return Result{}, withDetection(err, detection)
	}
	result.Tools = resolved
	setResolvedStepPaths(&plan, resolved)
	result.Calls = append([]ToolCall(nil), plan.ToolCalls...)
	result.Steps = cloneSteps(plan.Steps)
	if plan.Executor == ExecutorNative {
		removed, err := executeNative(plan)
		if err != nil {
			return Result{}, withDetection(err, detection)
		}
		result.Removed = removed
		if !r.Opts.JSON {
			fmt.Fprintf(r.Opts.Stdout, "project %s %s completed\n", detection.ProfileID, plan.Capability)
		}
		return result, nil
	}
	if plan.Capability == "version" {
		return r.runVersion(ctx, detection, plan, result)
	}
	result, err = r.executeSteps(ctx, detection, plan, result)
	if err != nil {
		return Result{}, err
	}
	if !r.Opts.JSON {
		fmt.Fprintf(r.Opts.Stdout, "project %s %s completed\n", detection.ProfileID, plan.Capability)
	}
	return result, nil
}

func resultFromPlan(plan OperationPlan, detection Detection, dryRun bool) Result {
	return Result{Command: "project", Implementation: detection.ProfileID, DetectedMarker: detection.Marker, Capability: plan.Capability, Arguments: cloneStringMap(plan.Arguments), Executor: plan.Executor, Calls: append([]ToolCall(nil), plan.ToolCalls...), Steps: cloneSteps(plan.Steps), MutationClass: plan.MutationClass, GeneratedArtifacts: append([]string(nil), plan.GeneratedArtifacts...), DryRun: dryRun, ExitStatus: 0}
}

func cloneSteps(steps []OperationStep) []OperationStep {
	result := make([]OperationStep, len(steps))
	for i, step := range steps {
		result[i] = step
		if step.Call != nil {
			call := *step.Call
			call.Arguments = append([]string(nil), step.Call.Arguments...)
			result[i].Call = &call
		}
		result[i].Parameters = cloneStringMap(step.Parameters)
	}
	return result
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func formatPlan(plan OperationPlan) string {
	data, _ := json.Marshal(plan)
	return string(data)
}

func (r *Runner) resolveTools(ctx context.Context, plan *OperationPlan) ([]ResolvedToolResult, error) {
	if len(plan.ToolRequirements) == 0 {
		return nil, nil
	}
	stdout, stderr := r.Opts.Stdout, r.Opts.Stderr
	if r.Opts.JSON {
		stdout, stderr = io.Discard, io.Discard
	}
	provisioner, err := toolspkg.NewProvisioner(toolspkg.Options{ToolsDir: r.Opts.ToolsDir, WorkDir: r.Opts.WorkDir, ConfigPath: r.Opts.ToolsConfig, SkipInstall: r.Opts.NoInstallTools || !r.Opts.AllowNetwork, Stdout: stdout, Stderr: stderr})
	if err != nil {
		return nil, &Error{Code: "project_tool_catalog_invalid", Command: "project", Capability: plan.Capability, ExitStatus: 1, Cause: err}
	}
	selectedEngine := ""
	if hasEngineRequirement(plan.ToolRequirements) {
		selectedEngine, err = r.selectEngine(provisioner)
		if err != nil {
			return nil, err
		}
		plan.ToolRequirements = replaceEngineRequirement(plan.ToolRequirements, selectedEngine)
		replaceEngineCalls(plan, selectedEngine)
	}
	results := make([]ResolvedToolResult, 0, len(plan.ToolRequirements))
	seen := map[string]bool{}
	for _, requirement := range plan.ToolRequirements {
		name := requirement.Name
		if name == "iac-engine" {
			name = selectedEngine
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		path := explicitToolPath(r.Opts.ToolPaths, name)
		if path == "" && selectedEngine != "" {
			path = explicitToolPath(r.Opts.ToolPaths, "iac-engine")
		}
		version := r.Opts.ToolVersions[name]
		if version == "" && selectedEngine != "" {
			version = r.Opts.ToolVersions["iac-engine"]
		}
		resolved, ensureErr := provisioner.EnsureTool(ctx, name, path, version)
		if ensureErr != nil {
			return nil, r.toolError(plan.Capability, name, ensureErr)
		}
		results = append(results, ResolvedToolResult{Name: name, Source: resolved.Source, Path: resolved.Path})
	}
	return results, nil
}

func replaceEngineRequirement(requirements []ToolRequirement, engine string) []ToolRequirement {
	result := cloneRequirements(requirements)
	for i := range result {
		if result[i].Name == "iac-engine" {
			result[i].Name, result[i].Executable, result[i].Alternatives = engine, engine, nil
		}
	}
	return result
}

func (r *Runner) selectEngine(provisioner *toolspkg.Provisioner) (string, error) {
	if r.Opts.Engine != "auto" {
		return r.Opts.Engine, nil
	}
	available := []string{}
	for _, candidate := range []string{"tofu", "terraform"} {
		configured := explicitToolPath(r.Opts.ToolPaths, candidate)
		configuredAvailable := configured != "" && toolspkg.ExecutableExists(configured)
		if configuredAvailable || func() bool {
			_, err := exec.LookPath(candidate)
			return err == nil
		}() || toolspkg.ExecutableExists(provisioner.ToolPath(candidate)) {
			available = append(available, candidate)
		}
	}
	if len(available) > 1 {
		return "", &Error{Code: "project_tool_selection_ambiguous", Command: "project", Tool: "terraform/tofu", Hint: "pass --engine terraform or --engine tofu", ExitStatus: 1, Cause: fmt.Errorf("both Terraform and OpenTofu are available")}
	}
	if len(available) == 1 {
		return available[0], nil
	}
	return "tofu", nil
}

func explicitToolPath(paths map[string]string, name string) string {
	path := paths[name]
	if path == "" {
		return ""
	}
	if expanded, err := toolspkg.ExpandHomePath(path); err == nil {
		return expanded
	}
	return path
}

func (r *Runner) toolError(capability, name string, err error) *Error {
	code := "project_tool_unavailable"
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "is not configured") {
		code = "project_tool_catalog_invalid"
	} else if strings.Contains(lower, "checksum") || strings.Contains(lower, "download") || strings.Contains(lower, "extract") {
		code = "project_tool_install_failed"
	} else if !r.Opts.NoInstallTools && !r.Opts.AllowNetwork {
		code = "project_network_not_allowed"
	}
	return &Error{Code: code, Command: "project", Capability: capability, Tool: name, Hint: "resolve the tool on PATH/cache, or pass --allow-network to permit direct provisioning", ExitStatus: 1, Cause: err}
}

func (r *Runner) executeTool(ctx context.Context, call ToolCall) (ToolExecution, error) {
	if r.Opts.ExecuteTool != nil {
		execution, err := r.Opts.ExecuteTool(ctx, call)
		if !call.SuppressOutput {
			r.streamExecution(execution)
		}
		return execution, err
	}
	var stdout, stderr bytes.Buffer
	command := call.ResolvedExecutable
	if command == "" {
		command = call.ToolName
	}
	cmd := exec.CommandContext(ctx, command, call.Arguments...)
	cmd.Dir = call.WorkingDirectory
	cmd.Stdin = r.Opts.Stdin
	cmd.Stdout = io.MultiWriter(&stdout, r.toolOutput(call, r.Opts.Stdout))
	cmd.Stderr = io.MultiWriter(&stderr, r.toolOutput(call, r.Opts.Stderr))
	err := cmd.Run()
	status := 0
	if err != nil {
		status = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			status = exitErr.ExitCode()
		}
	}
	return ToolExecution{Stdout: stdout.String(), Stderr: stderr.String(), ExitStatus: status}, err
}

func (r *Runner) toolOutput(call ToolCall, writer io.Writer) io.Writer {
	if r.Opts.JSON || call.SuppressOutput {
		return io.Discard
	}
	return writer
}

// printToolFailure exposes output deliberately withheld from a suppressed tool
// invocation. JSON mode keeps stdout machine-readable, including on failure.
func (r *Runner) printToolFailure(execution ToolExecution) {
	if r.Opts.JSON {
		return
	}
	if execution.Stderr != "" {
		_, _ = fmt.Fprint(r.Opts.Stderr, execution.Stderr)
	}
	if execution.Stdout != "" {
		_, _ = fmt.Fprint(r.Opts.Stdout, execution.Stdout)
	}
}

func (r *Runner) streamExecution(execution ToolExecution) {
	if r.Opts.JSON {
		return
	}
	if execution.Stdout != "" {
		_, _ = fmt.Fprint(r.Opts.Stdout, execution.Stdout)
	}
	if execution.Stderr != "" {
		_, _ = fmt.Fprint(r.Opts.Stderr, execution.Stderr)
	}
}

func resolvedPathByName(results []ResolvedToolResult, name string) (string, bool) {
	for _, result := range results {
		if result.Name == name {
			return result.Path, true
		}
	}
	return "", false
}

func executeNative(plan OperationPlan) ([]string, error) {
	removed := []string{}
	for _, path := range plan.NativePaths {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, wrapProjectError("project_operation_failed", "inspect cleanup path", err)
		}
		if err := os.RemoveAll(path); err != nil {
			return nil, wrapProjectError("project_operation_failed", "remove "+path, err)
		}
		removed = append(removed, path)
	}
	return removed, nil
}

func (r *Runner) runVersion(ctx context.Context, detection Detection, plan OperationPlan, result Result) (Result, error) {
	call := plan.ToolCalls[0]
	execution, err := r.executeTool(ctx, call)
	result.Stdout = execution.Stdout
	result.Stderr = execution.Stderr
	if err != nil {
		status := execution.ExitStatus
		if status == 0 {
			status = 1
		}
		r.printToolFailure(execution)
		return Result{}, withDetection(&Error{Code: "project_operation_failed", Command: "project", Capability: "version", Stdout: execution.Stdout, Stderr: execution.Stderr, ExitStatus: status, Cause: fmt.Errorf("gitversion: %w", err)}, detection)
	}
	if execution.ExitStatus != 0 {
		r.printToolFailure(execution)
		return Result{}, withDetection(&Error{Code: "project_operation_failed", Command: "project", Capability: "version", Stdout: execution.Stdout, Stderr: execution.Stderr, ExitStatus: execution.ExitStatus, Cause: fmt.Errorf("gitversion exited with status %d", execution.ExitStatus)}, detection)
	}
	version := parseGitVersionOutput(execution.Stdout)
	if version == "" {
		r.printToolFailure(execution)
		return Result{}, withDetection(&Error{Code: "project_operation_failed", Command: "project", Capability: "version", Stdout: execution.Stdout, Stderr: execution.Stderr, ExitStatus: 1, Cause: fmt.Errorf("gitversion returned no version")}, detection)
	}
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`).MatchString(version) {
		r.printToolFailure(execution)
		return Result{}, withDetection(&Error{Code: "project_operation_failed", Command: "project", Capability: "version", Stdout: execution.Stdout, Stderr: execution.Stderr, ExitStatus: 1, Cause: fmt.Errorf("gitversion returned an invalid semantic version")}, detection)
	}
	versionPath, err := safeProjectPath(r.Opts.WorkDir, "VERSION")
	if err != nil {
		return Result{}, withDetection(wrapProjectError("project_operation_failed", "resolve VERSION", err), detection)
	}
	if err := os.WriteFile(versionPath, []byte(version+"\n"), 0o644); err != nil {
		return Result{}, withDetection(wrapProjectError("project_operation_failed", "write VERSION", err), detection)
	}
	if err := updateProfileMetadata(r.Opts.WorkDir, detection.ProfileID, version); err != nil {
		return Result{}, withDetection(err, detection)
	}
	result.Version = version
	result.GeneratedArtifacts = append([]string(nil), plan.GeneratedArtifacts...)
	result.TagCreated = false
	if !r.Opts.JSON {
		fmt.Fprintf(r.Opts.Stdout, "Generated version %s in VERSION (tag_created=false)\n", version)
	}
	return result, nil
}

func updateProfileMetadata(workdir, profile, version string) error {
	updates := []metadataUpdate{}
	switch profile {
	case "docker", "node":
		updates = append(updates, metadataUpdate{Format: "json", Path: "package.json", Selector: "version", Value: version, IgnoreMissing: true})
	case "flutter":
		updates = append(updates, metadataUpdate{Format: "yaml", Path: "pubspec.yaml", Selector: "version", Value: version, IgnoreMissing: true})
	case "java":
		// Maven has many version elements (parent, dependencies, plugins). The
		// project version is the direct /project/version element only.
		updates = append(updates, metadataUpdate{Format: "xml", Path: "pom.xml", Selector: "project/version", Value: version, IgnoreMissing: true})
	case "python":
		err := updateTOMLFields(workdir, "pyproject.toml", "[project]", []tomlField{{name: "version", value: version}})
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	case "rust":
		err := updateTOMLFields(workdir, "Cargo.toml", "[package]", []tomlField{{name: "version", value: version}})
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	case "dotnet":
		matches, err := filepath.Glob(filepath.Join(workdir, "*.csproj"))
		if err != nil {
			return wrapProjectError("project_operation_failed", "find .csproj metadata", err)
		}
		for _, path := range matches {
			relative, err := filepath.Rel(workdir, path)
			if err != nil {
				return wrapProjectError("project_operation_failed", "resolve metadata path", err)
			}
			updates = append(updates, metadataUpdate{Format: "xml", Path: relative, Selector: "Project/PropertyGroup/Version", Value: version, IgnoreMissing: true})
			// Minimal project fixtures and older templates may place Version
			// directly under Project; support that shape without broad matching.
			updates = append(updates, metadataUpdate{Format: "xml", Path: relative, Selector: "Project/Version", Value: version, IgnoreMissing: true})
		}
	default:
		return nil
	}
	for _, update := range updates {
		if err := updateMetadataFile(workdir, update); err != nil {
			return wrapProjectError("project_operation_failed", "update metadata "+update.Path, err)
		}
	}
	return nil
}

func parseGitVersionOutput(output string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return ""
	}
	var value struct {
		SemVer     string `json:"SemVer"`
		FullSemVer string `json:"FullSemVer"`
	}
	if json.Unmarshal([]byte(trimmed), &value) == nil {
		if value.SemVer != "" {
			return value.SemVer
		}
		return value.FullSemVer
	}
	return strings.TrimSpace(strings.Split(trimmed, "\n")[0])
}
