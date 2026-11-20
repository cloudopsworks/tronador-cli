package cli

import (
	"context"
	"fmt"

	"tronador-cli/internal/iac"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	iacWorkDir               string
	iacModuleVersionsPath    string
	iacModuleVersionsUpgrade bool
	iacModuleVersionsMinor   bool
	iacModuleVersionsMajor   bool
	iacModuleVersionsAlpha   bool
	iacModuleVersionsBeta    bool
	iacModuleVersionsFixGit  bool
	iacModuleVersionsMerge   bool
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
When --path is set, module discovery starts there without changing --workdir.

Without --upgrade, the command reports available patch, minor, and major
semantic-version targets without changing files; unavailable tiers are omitted.
With --upgrade, patch is
selected by default; use --minor or --major to select a broader release tier.
When --major has no eligible major target, it falls back to the highest eligible
minor target in the current major version line.
The --minor and --major flags require --upgrade and are mutually exclusive.

With --upgrade --merge-inputs (alias --merge-input, -m), each processed module's
inputs.yaml is merged with the module version's .boilerplate/inputs.yaml using
the same comment-preserving YAML merge as "tronador repos upgrade": the
boilerplate is the baseline and values configured locally are preserved. The
upgraded ref is used when an upgrade applies, otherwise the current ref.
inputs.yaml is left unchanged, with a warning, when it is missing, when the
boilerplate inputs.yaml is missing or contains Go templates (regenerate the
Terragrunt scaffolding instead), or when the merge cannot be performed safely.
terragrunt.hcl processing is unaffected by these conditions.`,
		Args: cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if (iacModuleVersionsMinor || iacModuleVersionsMajor) && !iacModuleVersionsUpgrade {
				return fmt.Errorf("--minor and --major require --upgrade")
			}
			if iacModuleVersionsMerge && !iacModuleVersionsUpgrade {
				return fmt.Errorf("--merge-inputs requires --upgrade")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := iac.NewRunner(iac.ModuleVersionsOptions{
				WorkDir:             iacWorkDir,
				SearchPath:          iacModuleVersionsPath,
				Upgrade:             iacModuleVersionsUpgrade,
				Minor:               iacModuleVersionsMinor,
				Major:               iacModuleVersionsMajor,
				AllowAlpha:          iacModuleVersionsAlpha,
				AllowBeta:           iacModuleVersionsBeta,
				FixPrefix:           iacModuleVersionsFixGit,
				MergeInputs:         iacModuleVersionsMerge,
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
	cmd.Flags().BoolVarP(&iacModuleVersionsUpgrade, "upgrade", "u", false, "Update eligible ?ref= pins to the selected release tier and fix missing git:: prefixes")
	cmd.Flags().BoolVar(&iacModuleVersionsMinor, "minor", false, "With --upgrade, select the latest eligible minor release in the current major series")
	cmd.Flags().BoolVar(&iacModuleVersionsMajor, "major", false, "With --upgrade, select the latest eligible major release")
	cmd.Flags().BoolVar(&iacModuleVersionsAlpha, "alpha", false, "Allow alpha prerelease tags when selecting an update")
	cmd.Flags().BoolVar(&iacModuleVersionsBeta, "beta", false, "Allow beta prerelease tags when selecting an update")
	cmd.Flags().BoolVar(&iacModuleVersionsFixGit, "fix-prefix", false, "Add missing git:: prefixes to eligible GitHub HTTPS module sources without changing refs")
	cmd.Flags().StringVarP(&iacModuleVersionsPath, "path", "p", "", "Module discovery path relative to --workdir, or an in-workdir absolute path")
	cmd.Flags().BoolVarP(&iacModuleVersionsReport, "report-ghaction", "r", false, "Emit GitHub Actions warning annotations for findings")
	cmd.Flags().StringVarP(&iacModuleVersionsComment, "comment-pr-num", "c", "", "PR number to comment on when --report-ghaction is enabled")
	cmd.Flags().BoolVarP(&iacModuleVersionsMerge, "merge-inputs", "m", false, "With --upgrade, merge each module's inputs.yaml with the module version's .boilerplate/inputs.yaml (alias --merge-input)")
	cmd.Flags().SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "merge-input" {
			name = "merge-inputs"
		}
		return pflag.NormalizedName(name)
	})
	cmd.MarkFlagsMutuallyExclusive("minor", "major")
	return cmd
}

func commandDryRun(cmd *cobra.Command) bool {
	if flag := cmd.Flag("dry-run"); flag != nil {
		return flag.Value.String() == "true"
	}
	return false
}
