package project

import (
	"fmt"
	"sort"
	"strings"
)

const (
	ExecutorNative      = "native"
	ExecutorToolPipe    = "tool_pipeline"
	MutationReadOnly    = "read_only"
	MutationWorkspace   = "workspace_files"
	MutationGenerated   = "generated_files"
	MutationDestructive = "destructive"
	NetworkForbidden    = "forbidden"
	NetworkDeclared     = "declared"
)

// MarkerDefinition identifies an implementation using a regular file beneath
// .cloudopsworks. Marker contents are deliberately not interpreted.
type MarkerDefinition struct {
	Name      string `json:"name"`
	Canonical bool   `json:"canonical"`
}

// ArgumentDefinition describes a positional capability argument.
type ArgumentDefinition struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Default     string   `json:"default,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	Allowed     []string `json:"allowed,omitempty"`
}

// FlagDefinition describes a shared project-command flag in capabilities
// discovery output.
type FlagDefinition struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
}

// ToolCandidate is an alternative executable for a logical tool requirement.
type ToolCandidate struct {
	Name       string `json:"name"`
	Executable string `json:"executable"`
}

// ToolRequirement declares the only external tools a binding may resolve.
type ToolRequirement struct {
	Name               string          `json:"name"`
	Executable         string          `json:"executable"`
	Alternatives       []ToolCandidate `json:"alternatives,omitempty"`
	Version            string          `json:"version,omitempty"`
	ConfiguredPathFlag string          `json:"configured_path_flag,omitempty"`
	InstallPolicy      string          `json:"install_policy"`
	RequiredFor        []string        `json:"required_for,omitempty"`
}

// CapabilityDefinition is the shared logical user-facing contract.
type CapabilityDefinition struct {
	ID           string               `json:"id"`
	Aliases      []string             `json:"aliases,omitempty"`
	Arguments    []ArgumentDefinition `json:"arguments,omitempty"`
	Semantics    string               `json:"semantics"`
	ResultFields []string             `json:"result_fields,omitempty"`
}

// CapabilityBinding connects one logical capability to an implementation.
type CapabilityBinding struct {
	Capability         string               `json:"capability"`
	Executor           string               `json:"executor"`
	Operation          string               `json:"operation"`
	Arguments          []ArgumentDefinition `json:"arguments,omitempty"`
	Tools              []ToolRequirement    `json:"tools,omitempty"`
	Prerequisites      []string             `json:"prerequisites,omitempty"`
	MutationClass      string               `json:"mutation_class"`
	GeneratedArtifacts []string             `json:"generated_artifacts,omitempty"`
	NetworkPolicy      string               `json:"network_policy"`
	ConfirmationPolicy string               `json:"confirmation_policy"`
	DryRun             string               `json:"dry_run"`
}

// Profile is a complete detected implementation descriptor.
type Profile struct {
	ID           string              `json:"id"`
	DisplayName  string              `json:"display_name"`
	Version      string              `json:"version"`
	Markers      []MarkerDefinition  `json:"markers"`
	Capabilities []CapabilityBinding `json:"capabilities"`
}

// Registry is intentionally data-driven: adding a profile does not add a
// Cobra namespace or a dispatcher switch.
type Registry struct {
	Definitions map[string]CapabilityDefinition
	Profiles    []Profile
}

// DefaultRegistry returns the first approved profile matrix.
func DefaultRegistry() Registry {
	definitions := map[string]CapabilityDefinition{
		"init": {
			ID: "init", Semantics: "initialize the detected project using its typed profile pipeline",
			ResultFields: []string{"implementation", "capability", "tools", "calls", "mutation_class"},
		},
		"version": {
			ID: "version", Semantics: "generate a project version without tag management",
			ResultFields: []string{"version", "generated_artifacts", "tag_created", "file_changes"},
		},
		"lint": {
			ID: "lint", Aliases: []string{"validate"}, Semantics: "validate the detected infrastructure project",
			ResultFields: []string{"implementation", "tools", "calls", "exit_status"},
		},
		"format": {
			ID: "format", Aliases: []string{"fmt"}, Semantics: "format the detected infrastructure project",
			ResultFields: []string{"implementation", "tools", "calls", "exit_status"},
		},
		"clean": {
			ID: "clean", Semantics: "remove generated Terragrunt/IaC caches",
			ResultFields: []string{"removed", "mutation_class"},
		},
		"clean-inputs": {
			ID: "clean-inputs", Aliases: []string{"clean_inputs"}, Semantics: "remove generated Terragrunt input files",
			ResultFields: []string{"removed", "mutation_class"},
		},
	}
	appProfiles := []Profile{
		{ID: "androidsdk", DisplayName: "Android SDK", Version: "1", Markers: []MarkerDefinition{{Name: ".android", Canonical: true}}},
		{ID: "docker", DisplayName: "Docker", Version: "1", Markers: []MarkerDefinition{{Name: ".docker", Canonical: true}}},
		{ID: "dotnet", DisplayName: ".NET", Version: "1", Markers: []MarkerDefinition{{Name: ".dotnet", Canonical: true}}},
		{ID: "flutter", DisplayName: "Flutter", Version: "1", Markers: []MarkerDefinition{{Name: ".fluttermobile", Canonical: true}}},
		{ID: "go", DisplayName: "Go", Version: "1", Markers: []MarkerDefinition{{Name: ".golang", Canonical: true}}},
		{ID: "java", DisplayName: "Java", Version: "1", Markers: []MarkerDefinition{{Name: ".java", Canonical: true}}},
		{ID: "node", DisplayName: "Node", Version: "1", Markers: []MarkerDefinition{{Name: ".node", Canonical: true}}},
		{ID: "python", DisplayName: "Python", Version: "1", Markers: []MarkerDefinition{{Name: ".python", Canonical: true}}},
		{ID: "rust", DisplayName: "Rust", Version: "1", Markers: []MarkerDefinition{{Name: ".rust", Canonical: true}}},
		{ID: "xcode", DisplayName: "Xcode", Version: "1", Markers: []MarkerDefinition{{Name: ".xcode", Canonical: true}}},
	}
	for i := range appProfiles {
		appProfiles[i].Capabilities = []CapabilityBinding{
			applicationInitBinding(appProfiles[i].ID), applicationVersionBinding(appProfiles[i].ID),
		}
	}
	profiles := append(appProfiles,
		terraformProfile(), terragruntProfile(),
	)
	return Registry{Definitions: definitions, Profiles: profiles}
}

func applicationInitBinding(profile string) CapabilityBinding {
	artifacts := []string{}
	switch profile {
	case "docker", "node":
		artifacts = []string{"package.json"}
	case "dotnet":
		artifacts = []string{"*.sln", "*.csproj", ".github/vars/inputs-global.yaml"}
	case "flutter":
		artifacts = []string{"pubspec.yaml"}
	case "java":
		artifacts = []string{"pom.xml"}
	case "python":
		artifacts = []string{"pyproject.toml"}
	case "rust":
		artifacts = []string{"Cargo.toml", "*.rs"}
	}
	tools := []ToolRequirement{
		{Name: "gitversion", Executable: "gitversion", InstallPolicy: "provision", RequiredFor: []string{"init"}},
		{Name: "gh", Executable: "gh", InstallPolicy: "provision", RequiredFor: []string{"init"}},
	}
	if profile == "go" {
		// The template invokes the system Go tool directly; it does not install
		// Go from the Makefile, so this requirement is declared but unavailable
		// to the Tronador provisioner.
		tools = append(tools, ToolRequirement{Name: "go", Executable: "go", InstallPolicy: "unavailable", RequiredFor: []string{"init"}})
	}
	return CapabilityBinding{
		Capability: "init", Executor: ExecutorToolPipe, Operation: "application-init",
		Tools:         tools,
		MutationClass: MutationWorkspace, GeneratedArtifacts: artifacts, NetworkPolicy: NetworkDeclared,
		ConfirmationPolicy: "none", DryRun: "required",
	}
}

func applicationVersionBinding(profile string) CapabilityBinding {
	artifacts := []string{"VERSION"}
	switch profile {
	case "docker", "node":
		artifacts = append(artifacts, "package.json")
	case "dotnet":
		artifacts = append(artifacts, "*.csproj")
	case "flutter":
		artifacts = append(artifacts, "pubspec.yaml")
	case "java":
		artifacts = append(artifacts, "pom.xml")
	case "python":
		artifacts = append(artifacts, "pyproject.toml")
	case "rust":
		artifacts = append(artifacts, "Cargo.toml")
	}
	return CapabilityBinding{
		Capability: "version", Executor: ExecutorToolPipe, Operation: "generate-version",
		Tools:         []ToolRequirement{{Name: "gitversion", Executable: "gitversion", InstallPolicy: "provision", RequiredFor: []string{"version"}}},
		MutationClass: MutationGenerated, GeneratedArtifacts: artifacts, NetworkPolicy: NetworkForbidden,
		ConfirmationPolicy: "none", DryRun: "required",
	}
}

func terraformProfile() Profile {
	return Profile{
		ID: "terraform-module", DisplayName: "Terraform module", Version: "1",
		Markers: []MarkerDefinition{{Name: ".terraform-module", Canonical: true}},
		Capabilities: []CapabilityBinding{
			{Capability: "init", Executor: ExecutorToolPipe, Operation: "terraform-init", Arguments: []ArgumentDefinition{{Name: "provider", Description: "provider identifier", Required: true, Allowed: []string{"aws", "azurerm", "gcp", "mongodb", "github", "hoop"}}}, Tools: []ToolRequirement{{Name: "boilerplate", Executable: "boilerplate", InstallPolicy: "provision", RequiredFor: []string{"init"}}, {Name: "git", Executable: "git", InstallPolicy: "unavailable", RequiredFor: []string{"init"}}}, MutationClass: MutationWorkspace, NetworkPolicy: NetworkDeclared, ConfirmationPolicy: "none", DryRun: "required"},
			{Capability: "lint", Executor: ExecutorToolPipe, Operation: "terraform-lint", Tools: []ToolRequirement{{Name: "iac-engine", Executable: "tofu", Alternatives: []ToolCandidate{{Name: "tofu", Executable: "tofu"}, {Name: "terraform", Executable: "terraform"}}, InstallPolicy: "provision", RequiredFor: []string{"lint"}}}, MutationClass: MutationReadOnly, NetworkPolicy: NetworkForbidden, ConfirmationPolicy: "none", DryRun: "metadata_only"},
			{Capability: "format", Executor: ExecutorToolPipe, Operation: "terraform-format", Tools: []ToolRequirement{{Name: "iac-engine", Executable: "tofu", Alternatives: []ToolCandidate{{Name: "tofu", Executable: "tofu"}, {Name: "terraform", Executable: "terraform"}}, InstallPolicy: "provision", RequiredFor: []string{"format"}}}, MutationClass: MutationWorkspace, NetworkPolicy: NetworkForbidden, ConfirmationPolicy: "none", DryRun: "required"},
		},
	}
}

func terragruntProfile() Profile {
	return Profile{
		ID: "terragrunt", DisplayName: "Terragrunt project", Version: "1",
		Markers: []MarkerDefinition{{Name: ".iac", Canonical: true}},
		Capabilities: []CapabilityBinding{
			{Capability: "init", Executor: ExecutorToolPipe, Operation: "terragrunt-init", Tools: []ToolRequirement{{Name: "boilerplate", Executable: "boilerplate", InstallPolicy: "provision", RequiredFor: []string{"init"}}, {Name: "terragrunt", Executable: "terragrunt", InstallPolicy: "provision", RequiredFor: []string{"init"}}}, MutationClass: MutationWorkspace, NetworkPolicy: NetworkDeclared, ConfirmationPolicy: "none", DryRun: "required"},
			{Capability: "lint", Executor: ExecutorToolPipe, Operation: "terragrunt-lint", Tools: []ToolRequirement{{Name: "terragrunt", Executable: "terragrunt", InstallPolicy: "provision", RequiredFor: []string{"lint"}}, {Name: "iac-engine", Executable: "tofu", Alternatives: []ToolCandidate{{Name: "tofu", Executable: "tofu"}, {Name: "terraform", Executable: "terraform"}}, InstallPolicy: "provision", RequiredFor: []string{"lint"}}}, MutationClass: MutationReadOnly, NetworkPolicy: NetworkForbidden, ConfirmationPolicy: "none", DryRun: "metadata_only"},
			{Capability: "format", Executor: ExecutorToolPipe, Operation: "terragrunt-format", Tools: []ToolRequirement{{Name: "terragrunt", Executable: "terragrunt", InstallPolicy: "provision", RequiredFor: []string{"format"}}}, MutationClass: MutationWorkspace, NetworkPolicy: NetworkForbidden, ConfirmationPolicy: "none", DryRun: "required"},
			{Capability: "clean", Executor: ExecutorNative, Operation: "terragrunt-clean", MutationClass: MutationDestructive, NetworkPolicy: NetworkForbidden, ConfirmationPolicy: "yes_for_noninteractive", DryRun: "required"},
			{Capability: "clean-inputs", Executor: ExecutorNative, Operation: "terragrunt-clean-inputs", MutationClass: MutationDestructive, NetworkPolicy: NetworkForbidden, ConfirmationPolicy: "yes_for_noninteractive", DryRun: "required"},
		},
	}
}

// Validate checks registry identity and capability references before detection.
func (r Registry) Validate() error {
	if len(r.Profiles) == 0 {
		return projectError("project_registry_invalid", "profile registry is empty")
	}
	definitionIDs := map[string]bool{}
	aliases := map[string]string{}
	for key, definition := range r.Definitions {
		id := strings.ToLower(strings.TrimSpace(definition.ID))
		if key == "" || id == "" || key != definition.ID || definitionIDs[id] || aliases[id] != "" {
			return projectError("project_registry_invalid", fmt.Sprintf("invalid capability definition %q", key))
		}
		definitionIDs[id] = true
		for _, alias := range definition.Aliases {
			alias = strings.ToLower(strings.TrimSpace(alias))
			if alias == "" || alias == id || definitionIDs[alias] {
				return projectError("project_registry_invalid", fmt.Sprintf("invalid capability alias %q", alias))
			}
			if previous, ok := aliases[alias]; ok && previous != id {
				return projectError("project_registry_invalid", fmt.Sprintf("capability alias %q belongs to %s and %s", alias, previous, id))
			}
			aliases[alias] = id
		}
	}
	profiles := map[string]bool{}
	markers := map[string]string{}
	for _, profile := range r.Profiles {
		id := strings.TrimSpace(profile.ID)
		if id == "" || profiles[id] || profile.Version == "" || len(profile.Markers) == 0 {
			return projectError("project_registry_invalid", fmt.Sprintf("duplicate or empty profile %q", profile.ID))
		}
		profiles[id] = true
		canonicalMarkers := 0
		profileMarkers := map[string]bool{}
		for _, marker := range profile.Markers {
			if marker.Name == "" || profileMarkers[marker.Name] || !strings.HasPrefix(marker.Name, ".") || strings.Contains(marker.Name, "/") || strings.Contains(marker.Name, `\`) {
				return projectError("project_registry_invalid", fmt.Sprintf("invalid marker %q for %s", marker.Name, id))
			}
			profileMarkers[marker.Name] = true
			if marker.Canonical {
				canonicalMarkers++
			}
			if previous, ok := markers[marker.Name]; ok && previous != id {
				return projectError("project_registry_invalid", fmt.Sprintf("marker %q belongs to %s and %s", marker.Name, previous, id))
			}
			markers[marker.Name] = id
		}
		if canonicalMarkers != 1 {
			return projectError("project_registry_invalid", fmt.Sprintf("profile %q must define exactly one canonical marker", id))
		}
		seenCaps := map[string]bool{}
		for _, binding := range profile.Capabilities {
			definition, ok := r.Definitions[binding.Capability]
			if !ok || seenCaps[binding.Capability] || binding.Capability != definition.ID {
				return projectError("project_registry_invalid", fmt.Sprintf("invalid capability %q in %s", binding.Capability, id))
			}
			if binding.Executor != ExecutorNative && binding.Executor != ExecutorToolPipe || !knownOperation(binding.Operation) {
				return projectError("project_registry_invalid", fmt.Sprintf("invalid executor or operation for %s/%s", id, binding.Capability))
			}
			if !validMutation(binding.MutationClass) || !validNetworkPolicy(binding.NetworkPolicy) || !validConfirmation(binding.ConfirmationPolicy) || !validDryRun(binding.DryRun) {
				return projectError("project_registry_invalid", fmt.Sprintf("invalid safety policy for %s/%s", id, binding.Capability))
			}
			for _, requirement := range binding.Tools {
				if requirement.Name == "" || requirement.Executable == "" || !validInstallPolicy(requirement.InstallPolicy) {
					return projectError("project_registry_invalid", fmt.Sprintf("invalid tool requirement for %s/%s", id, binding.Capability))
				}
				seenCandidates := map[string]bool{}
				for _, candidate := range requirement.Alternatives {
					if candidate.Name == "" || candidate.Executable == "" || seenCandidates[candidate.Name] {
						return projectError("project_registry_invalid", fmt.Sprintf("invalid tool alternative for %s/%s", id, binding.Capability))
					}
					seenCandidates[candidate.Name] = true
				}
			}
			seenCaps[binding.Capability] = true
		}
	}
	return nil
}

func knownOperation(operation string) bool {
	switch operation {
	case "application-init", "generate-version", "terraform-init", "terraform-lint", "terraform-format", "terragrunt-init", "terragrunt-lint", "terragrunt-format", "terragrunt-clean", "terragrunt-clean-inputs":
		return true
	default:
		return false
	}
}

func validMutation(value string) bool {
	return value == MutationReadOnly || value == MutationWorkspace || value == MutationGenerated || value == MutationDestructive
}

func validNetworkPolicy(value string) bool {
	return value == NetworkForbidden || value == NetworkDeclared
}

func validConfirmation(value string) bool {
	return value == "none" || value == "yes_for_noninteractive"
}

func validDryRun(value string) bool {
	return value == "required" || value == "metadata_only" || value == "unsupported"
}

func validInstallPolicy(value string) bool {
	return value == "path" || value == "cache" || value == "provision" || value == "unavailable"
}

func (r Registry) Profile(id string) (Profile, bool) {
	for _, profile := range r.Profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return Profile{}, false
}

func (p Profile) Capability(id string, definitions map[string]CapabilityDefinition) (CapabilityBinding, CapabilityDefinition, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, binding := range p.Capabilities {
		if id == binding.Capability || hasString(definitions[binding.Capability].Aliases, id) {
			return binding, definitions[binding.Capability], true
		}
	}
	return CapabilityBinding{}, CapabilityDefinition{}, false
}

func (p Profile) CapabilityIDs() []string {
	ids := make([]string, 0, len(p.Capabilities))
	for _, binding := range p.Capabilities {
		ids = append(ids, binding.Capability)
	}
	sort.Strings(ids)
	return ids
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
