package cli

import (
	"context"
	"fmt"
	"strings"

	"tronador-cli/internal/versions"

	"github.com/spf13/cobra"
)

var (
	versionsWorkDir        string
	versionsGitPath        string
	versionsMainBranch     string
	versionsToolsDir       string
	versionsToolsConfig    string
	versionsNoInstallTools bool
	versionsAllowNetwork   bool
	versionsToolVersions   []string
	versionsToolPaths      []string
	versionsGitFlow        bool
	versionsGitHubFlow     bool
	versionsTrunkBased     bool
	versionsTrunk          bool
)

var versionsCmd = &cobra.Command{
	Use:     "versions",
	Aliases: []string{"gitflow", "gf", "githubflow", "flow"},
	Short:   "Manage repository branching and GitVersion workflow configuration",
	Long: `Manage repository branching and GitVersion workflow configuration.

This is a repository workflow command, not the project-version generator. Use
` + "`tronador project version`" + ` to generate a project version.

The init command selects one of the checked-in GitVersion workflow files. It
creates and publishes develop only for GitFlow, and does so only when the local
primary branch is clean and exactly matches origin.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
}

func init() {
	versionsCmd.PersistentFlags().StringVar(&versionsWorkDir, "workdir", ".", "Target repository directory")
	versionsCmd.PersistentFlags().StringVar(&versionsGitPath, "git", "", "git executable path (defaults to PATH)")
	versionsCmd.PersistentFlags().StringVar(&versionsMainBranch, "main-branch", "", "Primary branch override (defaults to origin HEAD, main, or master)")
	versionsCmd.PersistentFlags().StringVar(&versionsToolsDir, "tools-dir", "", "Directory for provisioned Tronador tools")
	versionsCmd.PersistentFlags().StringVar(&versionsToolsConfig, "tools-config", "", "Tool provisioner JSON override file")
	versionsCmd.PersistentFlags().BoolVar(&versionsNoInstallTools, "no-install-tools", false, "Resolve tools only from explicit paths, PATH, or cache")
	versionsCmd.PersistentFlags().BoolVar(&versionsAllowNetwork, "allow-network", false, "Permit missing GitVersion or gh to be provisioned")
	versionsCmd.PersistentFlags().StringArrayVar(&versionsToolVersions, "tool-version", nil, "Select a download version as name=version")
	versionsCmd.PersistentFlags().StringArrayVar(&versionsToolPaths, "tool-path", nil, "Use an explicit tool executable as name=path")
	versionsCmd.AddCommand(
		newVersionsInitCommand(),
		newVersionsFeatureCommand(),
		newVersionsHotfixCommand(),
		newVersionsReleaseCommand(),
		newVersionsSupportCommand(),
		newVersionsTagCommand(),
	)
	rootCmd.AddCommand(versionsCmd)
}

func newVersionsInitCommand() *cobra.Command {
	command := &cobra.Command{
		Use:          "init",
		Short:        "Select and install a GitVersion branching workflow",
		SilenceUsage: true,
		Long: `Validate all three .cloudopsworks/gitversion_<workflow>.yaml files,
then atomically replace .cloudopsworks/gitversion.yaml with the selected one.

With no workflow flag, GitFlow is selected when config.gitFlow.enabled is true
(or the CI config does not support that setting). When it is false, init offers
a numeric interactive selector. --gitflow enables config.gitFlow.enabled while
preserving the surrounding YAML layout. GitFlow additionally creates and pushes
develop, guarded by a clean worktree, origin, and primary-branch parity.

Workflows: --gitflow, --githubflow, --trunkbased (or --trunk).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			wow, err := versionsWayOfWork()
			if err != nil {
				return err
			}
			runner, err := versions.NewRunner(versions.Options{
				WorkDir: versionsWorkDir, GitPath: versionsGitPath, MainBranch: versionsMainBranch, DryRun: commandDryRun(cmd),
				Stdin: cmd.InOrStdin(), Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}
			result, err := runner.Init(context.Background(), versions.InitOptions{WayOfWork: wow})
			if err != nil {
				return err
			}
			if commandDryRun(cmd) {
				writeVersionsPlan(cmd.OutOrStdout(), "versions init", []string{
					fmt.Sprintf("Validate the checked-in GitVersion workflow configurations and select %s.", result.WayOfWork),
					"Update .cloudopsworks/gitversion.yaml to the selected workflow configuration.",
					"Update cloudopsworks-ci.yaml GitFlow setting when applicable.",
				})
				if result.WayOfWork == versions.WayOfWorkGitFlow {
					fmt.Fprintln(cmd.OutOrStdout(), "  4. Verify the primary branch is clean and matches origin, then create and push develop if needed.")
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "selected WayOfWork=%s\n", result.WayOfWork)
			}
			if result.DevelopCreated {
				fmt.Fprintln(cmd.OutOrStdout(), "created and pushed develop")
			}
			return nil
		},
	}
	command.Flags().BoolVar(&versionsGitFlow, "gitflow", false, "Use GitFlow and create/push develop")
	command.Flags().BoolVar(&versionsGitHubFlow, "githubflow", false, "Use GitHub Flow (no develop branch)")
	command.Flags().BoolVar(&versionsTrunkBased, "trunkbased", false, "Use trunk-based development (no develop branch)")
	command.Flags().BoolVar(&versionsTrunk, "trunk", false, "Alias for --trunkbased")
	return command
}

func versionsWayOfWork() (versions.WayOfWork, error) {
	var choice versions.WayOfWork
	selected := 0
	if versionsGitFlow {
		choice = versions.WayOfWorkGitFlow
		selected++
	}
	if versionsGitHubFlow {
		choice = versions.WayOfWorkGitHubFlow
		selected++
	}
	if versionsTrunkBased {
		choice = versions.WayOfWorkTrunkBased
		selected++
	}
	if versionsTrunk {
		choice = versions.WayOfWorkTrunkBased
		selected++
	}
	if selected > 1 {
		return "", fmt.Errorf("only one of --gitflow, --githubflow, --trunkbased, or --trunk may be used")
	}
	if selected == 1 {
		return choice, nil
	}
	return "", nil
}

func newVersionsWorkflow(cmd *cobra.Command, requireReliableWayOfWork bool) (*versions.Workflows, error) {
	toolVersions, err := parseAssignments(versionsToolVersions, "tool-version")
	if err != nil {
		return nil, err
	}
	toolPaths, err := parseAssignments(versionsToolPaths, "tool-path")
	if err != nil {
		return nil, err
	}
	runner, err := versions.NewRunner(versions.Options{
		WorkDir:        versionsWorkDir,
		GitPath:        versionsGitPath,
		MainBranch:     versionsMainBranch,
		ToolsDir:       versionsToolsDir,
		ToolsConfig:    versionsToolsConfig,
		NoInstallTools: versionsNoInstallTools,
		AllowNetwork:   versionsAllowNetwork,
		ToolVersions:   toolVersions,
		ToolPaths:      toolPaths,
		DryRun:         commandDryRun(cmd),
		Stdin:          cmd.InOrStdin(),
		Stdout:         cmd.OutOrStdout(),
		Stderr:         cmd.ErrOrStderr(),
	})
	if err != nil {
		return nil, err
	}
	var wow versions.WayOfWork
	if requireReliableWayOfWork {
		wow, err = runner.CurrentWayOfWorkForPurge(cmd.Context())
	} else {
		wow, err = runner.CurrentWayOfWork()
	}
	if err != nil {
		return nil, err
	}
	return runner.Workflow(wow)
}

func versionsWorkflowAction(use, short, long string, args cobra.PositionalArgs, requireReliableWayOfWork bool, action func(context.Context, *versions.Workflows, []string) error) *cobra.Command {
	return &cobra.Command{
		Use:          use,
		Short:        short,
		Long:         long,
		Args:         args,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if commandDryRun(cmd) {
				writeVersionsPlan(cmd.OutOrStdout(), versionsCommandLabel(cmd, use), versionsActionPlan(cmd, use, args))
				return nil
			}
			workflow, err := newVersionsWorkflow(cmd, requireReliableWayOfWork)
			if err != nil {
				return err
			}
			if err := action(context.Background(), workflow, args); err != nil {
				return err
			}
			writeVersionsActionResult(cmd.OutOrStdout(), versionsCommandLabel(cmd, use), workflow.ActionResults())
			return nil
		},
	}
}

func versionsAction(use, short, long string, args cobra.PositionalArgs, action func(context.Context, *versions.Workflows, []string) error) *cobra.Command {
	return versionsWorkflowAction(use, short, long, args, false, action)
}

func versionsPurgeAction(use, short, long string, args cobra.PositionalArgs, action func(context.Context, *versions.Workflows, []string) error) *cobra.Command {
	return versionsWorkflowAction(use, short, long, args, true, action)
}

func newVersionsFeatureCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "feature",
		Short: "Manage feature branches (GitFlow, GitHub Flow, and trunk-based)",
		Long: `Manage feature branches for the installed repository WayOfWork.

GitFlow features start from develop. GitHub Flow and trunk-based features start
from the detected main branch. Finish creates a guarded pull request; publish,
finish, and purge infer the feature name from feature/* when omitted.`,
	}
	command.AddCommand(
		versionsAction("start <name>", "Start a feature branch", "Start feature/<name> from develop for GitFlow, otherwise from main.", cobra.ExactArgs(1), func(ctx context.Context, workflow *versions.Workflows, args []string) error {
			return workflow.FeatureStart(ctx, args[0])
		}),
		versionsAction("publish [name]", "Publish a feature branch", "Publish feature/<name>; infer name from the current feature branch when omitted.", cobra.MaximumNArgs(1), func(ctx context.Context, workflow *versions.Workflows, args []string) error {
			return workflow.FeaturePublish(ctx, optionalArg(args))
		}),
		versionsAction("finish [name]", "Create the feature finish pull request", "Create a guarded feature pull request to develop for GitFlow or main otherwise.", cobra.MaximumNArgs(1), func(ctx context.Context, workflow *versions.Workflows, args []string) error {
			return workflow.FeatureFinish(ctx, optionalArg(args))
		}),
		versionsPurgeAction("purge [name]", "Delete a feature branch locally and remotely", "Delete feature/<name>; when active it first checks out the appropriate base branch. Destructive purge requires an explicit or uniquely detected WayOfWork.", cobra.MaximumNArgs(1), func(ctx context.Context, workflow *versions.Workflows, args []string) error {
			return workflow.FeaturePurge(ctx, optionalArg(args))
		}),
	)
	return command
}

func newVersionsHotfixCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "hotfix",
		Short: "Manage hotfix branches",
		Long: `Manage hotfix branches. Start calculates the next patch version from GitVersion.
Finish creates a guarded pull request by default; --local journals local merge
and tag work, then atomically publishes the target, tag, and source deletion.`,
	}
	command.AddCommand(
		versionsAction("start", "Start the next hotfix branch", "Calculate the next patch version and start hotfix/vX.Y.Z.", cobra.NoArgs, func(ctx context.Context, workflow *versions.Workflows, _ []string) error {
			return workflow.HotfixStart(ctx, "")
		}),
		versionsAction("publish", "Publish the current hotfix branch", "Publish the current hotfix/* branch.", cobra.NoArgs, func(ctx context.Context, workflow *versions.Workflows, _ []string) error {
			return workflow.HotfixPublish(ctx, "")
		}),
		newVersionsFinishAction("finish", "Finish the current hotfix", "Finish the current hotfix via pull request, or use --local for journaled atomic remote publication.", func(ctx context.Context, workflow *versions.Workflows, local bool) error {
			return workflow.HotfixFinish(ctx, "", local)
		}),
		versionsPurgeAction("purge [number]", "Delete a hotfix branch locally and remotely", "Delete hotfix/v<number>; infer it from the current hotfix branch when omitted. Destructive purge requires an explicit or uniquely detected WayOfWork.", cobra.MaximumNArgs(1), func(ctx context.Context, workflow *versions.Workflows, args []string) error {
			return workflow.HotfixPurge(ctx, optionalArg(args))
		}),
	)
	return command
}

func newVersionsReleaseCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "release",
		Short: "Manage release branches",
		Long: `Manage release branches. GitFlow starts releases from develop and merges
finished releases back into develop; GitHub Flow and trunk-based use main.
Finish creates a guarded pull request by default; --local uses restartable
breadcrumbs for local merge/tag work, then atomically publishes every required
target, the tag, and source deletion.`,
	}
	var patch, minor, major bool
	start := versionsAction("start", "Start a release branch", "Start release/vX.Y.Z; defaults to --minor when no bump flag is provided.", cobra.NoArgs, func(ctx context.Context, workflow *versions.Workflows, _ []string) error {
		kind, err := releaseBumpKind(patch, minor, major)
		if err != nil {
			return err
		}
		return workflow.ReleaseStart(ctx, kind)
	})
	start.Flags().BoolVar(&patch, "patch", false, "Start the next patch release")
	start.Flags().BoolVar(&minor, "minor", false, "Start the next minor release")
	start.Flags().BoolVar(&major, "major", false, "Start the next major release")
	command.AddCommand(
		start,
		versionsAction("publish [name]", "Publish a release branch", "Publish release/vX.Y.Z; infer the name from the current release branch when omitted.", cobra.MaximumNArgs(1), func(ctx context.Context, workflow *versions.Workflows, args []string) error {
			return workflow.ReleasePublish(ctx, optionalArg(args))
		}),
		newVersionsFinishAction("finish", "Finish the current release", "Finish the current release via pull request, or use --local for journaled atomic remote publication.", func(ctx context.Context, workflow *versions.Workflows, local bool) error {
			return workflow.ReleaseFinish(ctx, "", local)
		}),
		versionsPurgeAction("purge [name]", "Delete a release branch locally and remotely", "Delete release/vX.Y.Z; infer it from the current release branch when omitted. Destructive purge requires an explicit or uniquely detected WayOfWork.", cobra.MaximumNArgs(1), func(ctx context.Context, workflow *versions.Workflows, args []string) error {
			return workflow.ReleasePurge(ctx, optionalArg(args))
		}),
	)
	return command
}

func newVersionsSupportCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "support",
		Short: "Manage GitFlow support branches",
		Long: `Manage permanent support branches. These commands are available only when
the installed GitVersion configuration declares WayOfWork=gitflow.`,
	}
	command.AddCommand(
		versionsAction("start <tag>", "Start a support branch from a tag", "Create support/vX.Y.Z from an existing tag (GitFlow only).", cobra.ExactArgs(1), func(ctx context.Context, workflow *versions.Workflows, args []string) error {
			return workflow.SupportStart(ctx, args[0])
		}),
		versionsAction("publish [tag]", "Publish a support branch", "Publish support/vX.Y.Z; infer the tag from the current support branch when omitted (GitFlow only).", cobra.MaximumNArgs(1), func(ctx context.Context, workflow *versions.Workflows, args []string) error {
			return workflow.SupportPublish(ctx, optionalArg(args))
		}),
		versionsPurgeAction("purge [tag]", "Delete a support branch locally and remotely", "Delete support/vX.Y.Z; infer the tag from the current support branch when omitted (GitFlow only). Destructive purge requires an explicit or uniquely detected WayOfWork.", cobra.MaximumNArgs(1), func(ctx context.Context, workflow *versions.Workflows, args []string) error {
			return workflow.SupportPurge(ctx, optionalArg(args))
		}),
	)
	return command
}

func newVersionsTagCommand() *cobra.Command {
	var publish bool
	command := &cobra.Command{
		Use:          "tag [qualifier]",
		Short:        "Create a GitVersion tag",
		SilenceUsage: true,
		Long: `Create the tag calculated by GitVersion. On main it uses MajorMinorPatch;
on another branch it uses SemVer. The optional qualifier is compatible with the
legacy gitflow version tag target and becomes +deploy-<qualifier>. --publish
pushes a unique unpushed version tag already on HEAD when present (or does
nothing if the selected tag is already published), otherwise publishes the
calculated tag. When several qualifier tags exist, one unpushed tag is preferred;
multiple unpushed candidates are rejected unless a qualifier selects one.
different semantic versions (including different prereleases) on one commit
are rejected; only deployment-qualified aliases of the same version are allowed.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if commandDryRun(cmd) {
				steps := []string{"Calculate the GitVersion tag for the current commit."}
				if qualifier := optionalArg(args); qualifier != "" {
					steps[0] = fmt.Sprintf("Calculate the GitVersion tag and apply deployment qualifier %q.", qualifier)
				}
				steps = append(steps, "Create the annotated tag locally if it does not already exist on this commit.")
				if publish {
					steps = append(steps, "Push the selected tag to origin unless it is already published.")
				}
				writeVersionsPlan(cmd.OutOrStdout(), "versions tag", steps)
				return nil
			}
			workflow, err := newVersionsWorkflow(cmd, false)
			if err != nil {
				return err
			}
			tag, err := workflow.Tag(context.Background(), optionalArg(args), publish)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), tag)
			return nil
		},
	}
	command.Flags().BoolVar(&publish, "publish", false, "Push the tag to origin after creating or finding it")
	return command
}

func writeVersionsPlan(out interface{ Write([]byte) (int, error) }, action string, steps []string) {
	fmt.Fprintf(out, "Dry-run plan for %s (no changes made):\n", action)
	for i, step := range steps {
		fmt.Fprintf(out, "  %d. %s\n", i+1, step)
	}
}

func versionsActionPlan(cmd *cobra.Command, use string, args []string) []string {
	name := optionalArg(args)
	if name == "" {
		name = "the current branch"
	}
	action := strings.TrimPrefix(versionsCommandLabel(cmd, use), "versions ")
	steps := []string{}
	switch action {
	case "feature start":
		steps = []string{"Select the feature base branch (develop for GitFlow; otherwise the primary branch).", "Check out the feature base branch.", fmt.Sprintf("Create and check out feature/%s from that base.", name)}
	case "feature publish":
		steps = []string{fmt.Sprintf("Resolve the feature branch from %s.", name), "Check out that branch.", "Push it to origin and set its upstream tracking branch."}
	case "feature finish":
		steps = []string{fmt.Sprintf("Resolve feature branch %s and its workflow-specific target.", name), "Verify the local branch matches its origin branch.", "Create a pull request with the GitHub CLI."}
	case "feature purge":
		steps = []string{fmt.Sprintf("Resolve feature branch %s and its valid merge target.", name), "Fetch origin and verify branch parity and merge safety.", "Check out the base if the feature is active, then delete the local and remote feature branch."}
	case "hotfix start":
		steps = []string{"Fetch origin and select a synchronized primary or support-line base.", "Calculate the next patch version with GitVersion when no version was supplied.", "Check out the base and create hotfix/vX.Y.Z."}
	case "hotfix publish":
		steps = []string{"Resolve the current hotfix branch.", "Check out that branch.", "Push it to origin and set its upstream tracking branch."}
	case "hotfix finish":
		if local, _ := cmd.Flags().GetBool("local"); local {
			steps = []string{"Resolve or resume the current hotfix finish.", "Validate and record the local finish in the workflow journal.", "Merge/tag locally, then atomically publish required refs and remove the source branch."}
		} else {
			steps = []string{"Resolve the current hotfix branch and primary target.", "Verify the branch matches origin.", "Create a pull request with the GitHub CLI."}
		}
	case "hotfix purge":
		steps = []string{fmt.Sprintf("Resolve hotfix branch %s.", name), "Fetch origin and verify branch parity and merge safety.", "Check out a safe base if necessary, then delete the local and remote hotfix branch."}
	case "release start":
		kind := "minor"
		for _, flag := range []string{"patch", "minor", "major"} {
			if enabled, _ := cmd.Flags().GetBool(flag); enabled {
				kind = flag
			}
		}
		steps = []string{fmt.Sprintf("Select and synchronize the release base branch (GitFlow uses develop; bump: %s).", kind), "Calculate the next release version with GitVersion.", "Create and check out release/vX.Y.Z from the base."}
	case "release publish":
		steps = []string{fmt.Sprintf("Resolve release branch %s.", name), "Check out that branch.", "Push it to origin and set its upstream tracking branch."}
	case "release finish":
		if local, _ := cmd.Flags().GetBool("local"); local {
			steps = []string{"Resolve or resume the current release finish.", "Validate and record the local finish in the workflow journal.", "Merge/tag locally, then atomically publish all required targets and remove the source branch."}
		} else {
			steps = []string{"Resolve the current release branch and workflow targets.", "Fetch origin and verify source parity and target ancestry.", "Create pull requests for any targets that do not already contain the release."}
		}
	case "release purge":
		steps = []string{fmt.Sprintf("Resolve release branch %s.", name), "Fetch origin and verify branch parity and merge safety.", "Check out a safe base if necessary, then delete the local and remote release branch."}
	case "support start":
		steps = []string{fmt.Sprintf("Verify support tag %s exists.", name), "Create and check out support/vX.Y.Z from that tag (GitFlow only)."}
	case "support publish":
		steps = []string{fmt.Sprintf("Resolve support branch for tag %s.", name), "Check out that branch.", "Push it to origin and set its upstream tracking branch."}
	case "support purge":
		steps = []string{fmt.Sprintf("Resolve support branch for tag %s.", name), "Fetch origin and verify branch parity and merge safety.", "Check out a safe base if necessary, then delete the local and remote support branch."}
	default:
		steps = []string{fmt.Sprintf("Run the %s workflow with the supplied arguments and options.", strings.TrimSpace(use))}
	}
	return steps
}

func versionsCommandLabel(cmd *cobra.Command, use string) string {
	fields := strings.Fields(cmd.CommandPath())
	for i, field := range fields {
		if field == "versions" && i+1 < len(fields) {
			return "versions " + strings.Join(fields[i+1:], " ")
		}
	}
	return "versions " + strings.TrimSpace(use)
}

func writeVersionsActionResult(out interface{ Write([]byte) (int, error) }, action string, results []versions.ActionResult) {
	fmt.Fprintf(out, "Completed %s successfully.\n", action)
	if len(results) == 0 {
		return
	}
	fmt.Fprintln(out, "Tool results:")
	for _, result := range results {
		for _, line := range strings.Split(result.Output, "\n") {
			fmt.Fprintf(out, "  %s: %s\n", result.Tool, line)
		}
	}
}

// newVersionsFinishAction creates a no-positional-argument finish command with
// an explicit local mode shared by hotfix and release.
func newVersionsFinishAction(use, short, long string, action func(context.Context, *versions.Workflows, bool) error) *cobra.Command {
	var local bool
	command := versionsAction(use, short, long, cobra.NoArgs, func(ctx context.Context, workflow *versions.Workflows, _ []string) error {
		return action(ctx, workflow, local)
	})
	command.Flags().BoolVar(&local, "local", false, "Finish locally with journaled atomic remote publication")
	return command
}

func optionalArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func releaseBumpKind(patch, minor, major bool) (string, error) {
	count := 0
	if patch {
		count++
	}
	if minor {
		count++
	}
	if major {
		count++
	}
	if count > 1 {
		return "", fmt.Errorf("at most one of --patch, --minor, or --major may be provided")
	}
	if count == 0 {
		return "minor", nil
	}
	if patch {
		return "patch", nil
	}
	if minor {
		return "minor", nil
	}
	return "major", nil
}
