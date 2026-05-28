package cli

import (
	"context"
	"fmt"

	"tronador-cli/internal/repos"

	"github.com/spf13/cobra"
)

var (
	reposConfigPath string
	reposWorkDir    string
	reposGitPath    string
	reposGHPath     string
	reposPullBranch string
)

var reposCmd = &cobra.Command{
	Use:   "repos",
	Short: "Repository template lifecycle commands",
	Long: `Repository template lifecycle commands port the Tronador make repos/* targets
into tronador-cli. Template metadata and migration paths are loaded from JSON so
new repository types or future upgrade paths such as 5.11 and 5.12 can be added
without changing command dispatch code.`,
}

func init() {
	reposCmd.PersistentFlags().StringVar(&reposConfigPath, "config", "", "Repos JSON config path (defaults to embedded config)")
	reposCmd.PersistentFlags().StringVar(&reposWorkDir, "workdir", ".", "Target repository directory")
	reposCmd.PersistentFlags().StringVar(&reposGitPath, "git", "", "git executable path (defaults to GIT env or PATH)")
	reposCmd.PersistentFlags().StringVar(&reposGHPath, "gh", "", "gh executable path (defaults to GH env or PATH)")
	reposCmd.PersistentFlags().StringVar(&reposPullBranch, "pull-branch", "", "Template branch/tag for recover; upgrade uses optional [version]")

	reposCmd.AddCommand(newReposAvailableCommand())
	reposCmd.AddCommand(newReposCleanCommand())
	reposCmd.AddCommand(newReposCICDCommand())
	reposCmd.AddCommand(newReposTemplateCommand())
	reposCmd.AddCommand(newReposUpgradeCommand())
	reposCmd.AddCommand(newReposRecoverCommand())
	reposCmd.AddCommand(newReposPushCommand())
	reposCmd.AddCommand(newReposMigrateCommand())

	rootCmd.AddCommand(reposCmd)
}

func newReposRunner(cmd *cobra.Command) (*repos.Runner, error) {
	dryRun := false
	if flag := cmd.Flag("dry-run"); flag != nil {
		dryRun = flag.Value.String() == "true"
	}
	return repos.NewRunner(repos.Options{
		WorkDir:    reposWorkDir,
		ConfigPath: reposConfigPath,
		GitPath:    reposGitPath,
		GHPath:     reposGHPath,
		PullBranch: reposPullBranch,
		DryRun:     dryRun,
	})
}

func newReposAvailableCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "available",
		Aliases: []string{"avail"},
		Short:   "List available template repository versions",
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newReposRunner(cmd)
			if err != nil {
				return err
			}
			return runner.Available(context.Background())
		},
	}
}

func newReposCleanCommand() *cobra.Command {
	cleanCmd := &cobra.Command{
		Use:   "clean",
		Short: "Clean repository workflows",
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newReposRunner(cmd)
			if err != nil {
				return err
			}
			return runner.Clean()
		},
	}
	cleanCmd.AddCommand(&cobra.Command{
		Use:   "template",
		Short: "Remove the temporary template checkout",
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newReposRunner(cmd)
			if err != nil {
				return err
			}
			return runner.CleanTemplate(context.Background())
		},
	})
	return cleanCmd
}

func newReposCICDCommand() *cobra.Command {
	cicdCmd := &cobra.Command{
		Use:   "cicd",
		Short: "CICD pipeline helpers",
	}
	cicdCmd.AddCommand(&cobra.Command{
		Use:   "update",
		Short: "Update CICD Pipeline footer versioning",
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newReposRunner(cmd)
			if err != nil {
				return err
			}
			state, err := runner.Detect()
			if err != nil {
				return err
			}
			return runner.CICDUpdate(state, "")
		},
	})
	return cicdCmd
}

func newReposTemplateCommand() *cobra.Command {
	templateCmd := &cobra.Command{
		Use:   "template [name]",
		Short: "Template checkout commands",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			runner, err := newReposRunner(cmd)
			if err != nil {
				return err
			}
			return runner.Template(context.Background(), args[0])
		},
	}
	templateCmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Clean and initialize the detected template checkout",
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newReposRunner(cmd)
			if err != nil {
				return err
			}
			_, _, err = runner.TemplateInit(context.Background())
			return err
		},
	})
	templateCmd.AddCommand(&cobra.Command{
		Use:     "clean",
		Aliases: []string{"clean-template"},
		Short:   "Remove the temporary template checkout",
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newReposRunner(cmd)
			if err != nil {
				return err
			}
			return runner.CleanTemplate(context.Background())
		},
	})
	addConfiguredTemplateCommands(templateCmd)
	return templateCmd
}

func addConfiguredTemplateCommands(parent *cobra.Command) {
	cfg, err := repos.LoadConfig("")
	if err != nil {
		return
	}
	for _, tmpl := range cfg.Templates {
		tmpl := tmpl
		parent.AddCommand(&cobra.Command{
			Use:   tmpl.Name,
			Short: fmt.Sprintf("Pull %s", tmpl.Description),
			RunE: func(cmd *cobra.Command, args []string) error {
				runner, err := newReposRunner(cmd)
				if err != nil {
					return err
				}
				return runner.Template(context.Background(), tmpl.Name)
			},
		})
	}
}

func newReposUpgradeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade [version]",
		Short: "Run the full repository template upgrade workflow",
		Long: `Run the full repository template upgrade workflow.

Without [version], the command mirrors the Tronador make repos/upgrade target:
it detects the current template type, resolves the latest tag in the current
major/minor line, fetches that template internally, applies the stack, updates
CICD metadata when applicable, and commits the result.

Passing [version] is equivalent to the Makefile repos/upgrade/<version> target
and runs the same full workflow against that explicit tag or branch.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newReposRunner(cmd)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				return runner.UpgradeVersion(context.Background(), args[0])
			}
			return runner.Upgrade(context.Background())
		},
	}
}

func newReposRecoverCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "recover",
		Short: "Recover repository files from templates without committing",
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newReposRunner(cmd)
			if err != nil {
				return err
			}
			return runner.Recover(context.Background())
		},
	}
}

func newReposPushCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "push",
		Short: "Stage and commit template upgrade changes",
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newReposRunner(cmd)
			if err != nil {
				return err
			}
			tmpl, state, err := runner.ActiveTemplate()
			if err != nil {
				return err
			}
			return runner.Push(context.Background(), tmpl, state)
		},
	}
}

func newReposMigrateCommand() *cobra.Command {
	migrateCmd := &cobra.Command{
		Use:   "migrate [template] [version]",
		Short: "Run configured repository layout migrations",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newReposRunner(cmd)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				return runner.Migrate("", args[0])
			}
			return runner.Migrate(args[0], args[1])
		},
	}
	addConfiguredMigrationCommands(migrateCmd)
	return migrateCmd
}

func addConfiguredMigrationCommands(parent *cobra.Command) {
	cfg, err := repos.LoadConfig("")
	if err != nil {
		return
	}
	for _, plan := range cfg.MigrationPlans {
		plan := plan
		parent.AddCommand(&cobra.Command{
			Use:   plan.Version,
			Short: plan.Description,
			RunE: func(cmd *cobra.Command, args []string) error {
				runner, err := newReposRunner(cmd)
				if err != nil {
					return err
				}
				return runner.Migrate("", plan.Version)
			},
		})
	}
	for _, tmpl := range cfg.Templates {
		tmpl := tmpl
		cmd := &cobra.Command{
			Use:   tmpl.Name + " [version]",
			Short: fmt.Sprintf("Run %s template migration", tmpl.Name),
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				runner, err := newReposRunner(cmd)
				if err != nil {
					return err
				}
				return runner.Migrate(tmpl.Name, args[0])
			},
		}
		parent.AddCommand(cmd)
	}
}
