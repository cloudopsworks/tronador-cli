package cli

import (
	"context"
	"fmt"

	"tronador-cli/internal/versions"

	"github.com/spf13/cobra"
)

var (
	versionsWorkDir    string
	versionsGitPath    string
	versionsMainBranch string
	versionsGitFlow    bool
	versionsGitHubFlow bool
	versionsTrunkBased bool
	versionsTrunk      bool
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
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would select %s\n", result.WayOfWork)
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

func newVersionsWorkflow(cmd *cobra.Command) (*versions.Workflows, error) {
	runner, err := versions.NewRunner(versions.Options{
		WorkDir:    versionsWorkDir,
		GitPath:    versionsGitPath,
		MainBranch: versionsMainBranch,
		DryRun:     commandDryRun(cmd),
		Stdin:      cmd.InOrStdin(),
		Stdout:     cmd.OutOrStdout(),
		Stderr:     cmd.ErrOrStderr(),
	})
	if err != nil {
		return nil, err
	}
	wow, err := runner.CurrentWayOfWork()
	if err != nil {
		return nil, err
	}
	return runner.Workflow(wow)
}

func versionsAction(use, short, long string, args cobra.PositionalArgs, action func(context.Context, *versions.Workflows, []string) error) *cobra.Command {
	return &cobra.Command{
		Use:          use,
		Short:        short,
		Long:         long,
		Args:         args,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if commandDryRun(cmd) {
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would run versions %s\n", use)
				return nil
			}
			workflow, err := newVersionsWorkflow(cmd)
			if err != nil {
				return err
			}
			return action(context.Background(), workflow, args)
		},
	}
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
		versionsAction("purge [name]", "Delete a feature branch locally and remotely", "Delete feature/<name>; when active it first checks out the appropriate base branch.", cobra.MaximumNArgs(1), func(ctx context.Context, workflow *versions.Workflows, args []string) error {
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
		versionsAction("purge [number]", "Delete a hotfix branch locally and remotely", "Delete hotfix/v<number>; infer it from the current hotfix branch when omitted.", cobra.MaximumNArgs(1), func(ctx context.Context, workflow *versions.Workflows, args []string) error {
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
		versionsAction("purge [name]", "Delete a release branch locally and remotely", "Delete release/vX.Y.Z; infer it from the current release branch when omitted.", cobra.MaximumNArgs(1), func(ctx context.Context, workflow *versions.Workflows, args []string) error {
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
		versionsAction("purge [tag]", "Delete a support branch locally and remotely", "Delete support/vX.Y.Z; infer the tag from the current support branch when omitted (GitFlow only).", cobra.MaximumNArgs(1), func(ctx context.Context, workflow *versions.Workflows, args []string) error {
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
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would run versions tag\n")
				return nil
			}
			workflow, err := newVersionsWorkflow(cmd)
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
