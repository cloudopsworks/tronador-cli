package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	projectpkg "tronador-cli/internal/project"

	"github.com/spf13/cobra"
)

var (
	projectWorkDir        string
	projectToolsDir       string
	projectToolsConfig    string
	projectNoInstallTools bool
	projectAllowNetwork   bool
	projectToolVersions   []string
	projectToolPaths      []string
	projectEngine         string
	projectYes            bool
	projectJSON           bool
	projectSnapshot       bool
	projectPlain          bool
)

var projectCmd = &cobra.Command{
	Use:           "project <capability> [validated-arguments...]",
	Short:         "Run a capability against the implicitly detected project",
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.MinimumNArgs(1),
	Long: `Run project capabilities using the implementation detected from
<workdir>/.cloudopsworks. The project command never dispatches through a
Makefile and does not expose implementation namespaces.

Examples:
  tronador project detect
  tronador project capabilities
  tronador project init
  tronador project init aws
  tronador project clean --yes`,
	RunE: runProjectCommand,
}

var projectVersionCmd = &cobra.Command{
	Use:           "version",
	Short:         "Generate and write the detected project's version",
	SilenceUsage:  true,
	SilenceErrors: true,
	Long: `Generate and write the detected project's version.

The default uses GitVersion's FullSemVer. When HEAD is tagged, the tag is
written instead of GitVersion's calculated value. Version generation writes
VERSION and profile metadata when it exists, such as package.json for Node or
pyproject.toml for Python.

Use --plain to write an exact x.y.z version from GitVersion's MajorMinorPatch
for Node and Python projects. It applies even when HEAD is tagged. Use
--snapshot to write x.y.z-SNAPSHOT from MajorMinorPatch for untagged Java
projects.

The version dry-run calculates GitVersion and previews only actual file
changes; it never writes project files.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runProjectCommand(cmd, append([]string{"version"}, args...))
	},
}

func init() {
	projectCmd.PersistentFlags().StringVar(&projectWorkDir, "workdir", ".", "Target project directory")
	projectCmd.PersistentFlags().StringVar(&projectToolsDir, "tools-dir", "", "Directory for provisioned Tronador tools")
	projectCmd.PersistentFlags().StringVar(&projectToolsConfig, "tools-config", "", "Tool provisioner JSON override file")
	projectCmd.PersistentFlags().BoolVar(&projectNoInstallTools, "no-install-tools", false, "Resolve tools only from explicit paths, PATH, or cache")
	projectCmd.PersistentFlags().BoolVar(&projectAllowNetwork, "allow-network", false, "Permit missing declared tools to be provisioned")
	projectCmd.PersistentFlags().StringArrayVar(&projectToolVersions, "tool-version", nil, "Pin one tool version as name=version")
	projectCmd.PersistentFlags().StringArrayVar(&projectToolPaths, "tool-path", nil, "Use an explicit tool executable as name=path")
	projectCmd.PersistentFlags().BoolVar(&projectJSON, "json", false, "Emit stable JSON output")
	projectCmd.PersistentFlags().BoolVar(&projectPlain, "plain", false, "Generate an exact x.y.z version using GitVersion's MajorMinorPatch (Node and Python only)")
	projectCmd.PersistentFlags().BoolVar(&projectSnapshot, "snapshot", false, "Generate x.y.z-SNAPSHOT using GitVersion's MajorMinorPatch (untagged Java only)")
	projectCmd.Flags().StringVar(&projectEngine, "engine", "tofu", "IaC engine: tofu (default), terraform, or auto")
	projectCmd.Flags().BoolVar(&projectYes, "yes", false, "Confirm destructive operations")
	projectCmd.AddCommand(projectVersionCmd)
	rootCmd.AddCommand(projectCmd)
}

func runProjectCommand(cmd *cobra.Command, args []string) error {
	versions, err := parseAssignments(projectToolVersions, "tool-version")
	if err != nil {
		return emitProjectError(cmd, err)
	}
	paths, err := parseAssignments(projectToolPaths, "tool-path")
	if err != nil {
		return emitProjectError(cmd, err)
	}
	if err := validateSnapshotCapability(args[0], projectSnapshot); err != nil {
		return emitProjectError(cmd, err)
	}
	if err := validatePlainCapability(args[0], projectPlain); err != nil {
		return emitProjectError(cmd, err)
	}
	runner, err := projectpkg.NewRunner(projectpkg.Options{
		WorkDir: projectWorkDir, ToolsDir: projectToolsDir, ToolsConfig: projectToolsConfig,
		NoInstallTools: projectNoInstallTools, AllowNetwork: projectAllowNetwork,
		ToolVersions: versions, ToolPaths: paths, Engine: projectEngine, Yes: projectYes,
		JSON: projectJSON, Snapshot: projectSnapshot, Plain: projectPlain,
		DryRun: commandDryRun(cmd), Stdin: cmd.InOrStdin(), Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr(),
	})
	if err != nil {
		return emitProjectError(cmd, err)
	}

	switch strings.ToLower(args[0]) {
	case "detect":
		if len(args) != 1 {
			return emitProjectError(cmd, projectErrorForCLI("project_argument_invalid", "detect does not accept positional arguments"))
		}
		detection, err := runner.Detect()
		if err != nil {
			return emitProjectError(cmd, err)
		}
		if projectJSON {
			return writeProjectJSON(cmd, detection)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "workdir: %s\nimplementation: %s\nmarker: %s\nregistry: %s\n", detection.WorkDir, detection.ProfileID, detection.Marker, detection.RegistryVersion)
		return nil
	case "capabilities":
		if len(args) != 1 {
			return emitProjectError(cmd, projectErrorForCLI("project_argument_invalid", "capabilities does not accept positional arguments"))
		}
		detection, err := runner.Detect()
		if err != nil {
			return emitProjectError(cmd, err)
		}
		descriptions, err := runner.Describe(detection)
		if err != nil {
			return emitProjectError(cmd, err)
		}
		if projectJSON {
			return writeProjectJSON(cmd, map[string]any{"command": "project", "workdir": detection.WorkDir, "implementation": detection.ProfileID, "detected_marker": detection.Marker, "capabilities": descriptions})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "implementation: %s (%s)\n", detection.ProfileID, detection.Marker)
		for _, description := range descriptions {
			fmt.Fprintf(cmd.OutOrStdout(), "- %s", description.ID)
			if len(description.Aliases) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), " (aliases: %s)", strings.Join(description.Aliases, ", "))
			}
			if len(description.Arguments) > 0 {
				args := make([]string, 0, len(description.Arguments))
				for _, argument := range description.Arguments {
					name := argument.Name
					if argument.Required {
						name += " (required)"
					}
					args = append(args, name)
				}
				fmt.Fprintf(cmd.OutOrStdout(), " [%s]", strings.Join(args, ", "))
			}
			fmt.Fprintf(cmd.OutOrStdout(), " — %s; executor=%s; mutation=%s; tools=%s; flags=%s%s\n", description.Semantics, description.Executor, description.MutationClass, capabilityToolNames(description.Tools), capabilityFlagNames(description.Flags), dryRunBehavior(description.Flags))
		}
		return nil
	default:
		result, err := runner.Run(context.Background(), args[0], args[1:])
		if err != nil {
			return emitProjectError(cmd, err)
		}
		if projectJSON {
			return writeProjectJSON(cmd, result)
		}
		if result.Version == "" && result.ExitStatus == 0 && len(result.Removed) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "implementation: %s\ncapability: %s\nexecutor: %s\n", result.Implementation, result.Capability, result.Executor)
		}
		return nil
	}
}

func dryRunBehavior(flags []projectpkg.FlagDefinition) string {
	for _, flag := range flags {
		if flag.Name == "dry-run" {
			return "; dry-run=" + flag.Description
		}
	}
	return ""
}

func capabilityToolNames(requirements []projectpkg.ToolRequirement) string {
	if len(requirements) == 0 {
		return "none"
	}
	names := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		if len(requirement.Alternatives) == 0 {
			names = append(names, requirement.Name)
			continue
		}
		candidates := make([]string, 0, len(requirement.Alternatives))
		for _, candidate := range requirement.Alternatives {
			candidates = append(candidates, candidate.Name)
		}
		names = append(names, "{"+strings.Join(candidates, "|")+"}")
	}
	return strings.Join(names, ", ")
}

func capabilityFlagNames(flags []projectpkg.FlagDefinition) string {
	if len(flags) == 0 {
		return "none"
	}
	names := make([]string, 0, len(flags))
	for _, flag := range flags {
		names = append(names, "--"+flag.Name)
	}
	return strings.Join(names, ", ")
}

func validateSnapshotCapability(capability string, snapshot bool) error {
	if snapshot && !strings.EqualFold(strings.TrimSpace(capability), "version") {
		return projectErrorForCLI("project_snapshot_unsupported", "--snapshot is supported only for `tronador project version`")
	}
	return nil
}

func validatePlainCapability(capability string, plain bool) error {
	if plain && !strings.EqualFold(strings.TrimSpace(capability), "version") {
		return projectErrorForCLI("project_plain_unsupported", "--plain is supported only for `tronador project version`")
	}
	return nil
}

func parseAssignments(values []string, flagName string) (map[string]string, error) {
	result := map[string]string{}
	for _, value := range values {
		name, assigned, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(assigned) == "" {
			return nil, projectErrorForCLI("project_argument_invalid", fmt.Sprintf("--%s expects name=value", flagName))
		}
		result[strings.TrimSpace(name)] = strings.TrimSpace(assigned)
	}
	return result, nil
}

func projectErrorForCLI(code, message string) error {
	return &projectpkg.Error{Code: code, Command: "project", ExitStatus: 1, Cause: fmt.Errorf("%s", message)}
}

func emitProjectError(cmd *cobra.Command, err error) error {
	if projectJSON {
		_ = writeProjectJSON(cmd, err)
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
	}
	return err
}

func writeProjectJSON(cmd *cobra.Command, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return err
}
