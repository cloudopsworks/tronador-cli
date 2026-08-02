package cli

import (
	"context"

	"tronador-cli/internal/iac"

	"github.com/spf13/cobra"
)

var (
	iacWorkDir               string
	iacModuleVersionsPath    string
	iacModuleVersionsUpgrade bool
	iacModuleVersionsAlpha   bool
	iacModuleVersionsBeta    bool
	iacModuleVersionsFixGit  bool
	iacModuleVersionsReport  bool
	iacModuleVersionsComment string
)

var iacCmd = &cobra.Command{
	Use:   "iac",
	Short: "Infrastructure-as-code workspace commands",
	Long: `Infrastructure-as-code workspace commands operate only on repositories that
carry the .cloudopsworks/.iac workspace marker.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	iacCmd.PersistentFlags().StringVar(&iacWorkDir, "workdir", ".", "Target IaC workspace directory")
	iacCmd.AddCommand(newIACModuleVersionsCommand())
	rootCmd.AddCommand(iacCmd)
}

func newIACModuleVersionsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "module",
		Aliases: []string{"module-versions", "module_versions"},
		Short:   "Report and optionally update Terragrunt module source versions",
		Long: `Report Terraform/Terragrunt GitHub module source versions in terragrunt.hcl
files and optionally update ?ref= pins or normalize missing git:: prefixes.

The command refuses to run unless --workdir contains .cloudopsworks/.iac.
When --path is set, module discovery starts there without changing --workdir.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := iac.NewRunner(iac.ModuleVersionsOptions{
				WorkDir:             iacWorkDir,
				SearchPath:          iacModuleVersionsPath,
				Upgrade:             iacModuleVersionsUpgrade,
				AllowAlpha:          iacModuleVersionsAlpha,
				AllowBeta:           iacModuleVersionsBeta,
				FixPrefix:           iacModuleVersionsFixGit,
				DryRun:              commandDryRun(cmd),
				ReportGitHubActions: iacModuleVersionsReport,
				CommentPRNumber:     iacModuleVersionsComment,
				Stdout:              cmd.OutOrStdout(),
				Stderr:              cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}
			return runner.ModuleVersions(context.Background())
		},
	}
	cmd.Flags().BoolVarP(&iacModuleVersionsUpgrade, "upgrade", "u", false, "Update eligible ?ref= pins to latest semantic version tags and fix missing git:: prefixes")
	cmd.Flags().BoolVar(&iacModuleVersionsAlpha, "alpha", false, "Allow alpha prerelease tags when selecting an update")
	cmd.Flags().BoolVar(&iacModuleVersionsBeta, "beta", false, "Allow beta prerelease tags when selecting an update")
	cmd.Flags().BoolVar(&iacModuleVersionsFixGit, "fix-prefix", false, "Add missing git:: prefixes to eligible GitHub HTTPS module sources without changing refs")
	cmd.Flags().StringVarP(&iacModuleVersionsPath, "path", "p", "", "Module discovery path relative to --workdir, or an in-workdir absolute path")
	cmd.Flags().BoolVarP(&iacModuleVersionsReport, "report-ghaction", "r", false, "Emit GitHub Actions warning annotations for findings")
	cmd.Flags().StringVarP(&iacModuleVersionsComment, "comment-pr-num", "c", "", "PR number to comment on when --report-ghaction is enabled")
	return cmd
}

func commandDryRun(cmd *cobra.Command) bool {
	if flag := cmd.Flag("dry-run"); flag != nil {
		return flag.Value.String() == "true"
	}
	return false
}
