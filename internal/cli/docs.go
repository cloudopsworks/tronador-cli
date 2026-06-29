package cli

import (
	"context"

	docspkg "tronador-cli/internal/docs"

	"github.com/spf13/cobra"
)

var (
	docsWorkDir string
	docsDir     string

	docsTargetsOutput     string
	docsTargetsMake       string
	docsTargetsHelpTarget string
	docsTargetsAll        bool
	docsTargetsTitle      string

	docsTerraformOutput string
	docsTerraformPath   string
	docsTerraformFormat string

	docsCopyrightCommand             string
	docsCopyrightSoftware            string
	docsCopyrightSoftwareDescription string
	docsCopyrightLicense             string
	docsCopyrightHolder              string
	docsCopyrightYear                string
	docsCopyrightOutputDir           string
	docsCopyrightWordWrap            string
)

var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Documentation generation commands",
	Long: `Documentation generation commands port Tronador docs/* Makefile targets
into tronador-cli, including Make target docs, terraform-docs output, and
copyright-header execution.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	docsCmd.PersistentFlags().StringVar(&docsWorkDir, "workdir", ".", "Target repository directory")
	docsCmd.PersistentFlags().StringVar(&docsDir, "docs-dir", "", "Documentation output directory (default docs or DOCS_DIR)")
	docsCmd.AddCommand(newDocsInitCommand())
	docsCmd.AddCommand(newDocsTargetsCommand())
	docsCmd.AddCommand(newDocsTerraformCommand())
	docsCmd.AddCommand(newDocsCopyrightCommand())
	rootCmd.AddCommand(docsCmd)
}

func newDocsRunner(cmd *cobra.Command) (*docspkg.Runner, error) {
	return docspkg.NewRunner(docspkg.Options{
		WorkDir: docsWorkDir,
		DocsDir: docsDir,
		DryRun:  commandDryRun(cmd),
		Stdout:  cmd.OutOrStdout(),
		Stderr:  cmd.ErrOrStderr(),
	})
}

func newDocsInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Ensure the docs directory exists",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newDocsRunner(cmd)
			if err != nil {
				return err
			}
			return runner.Init()
		},
	}
}

func newDocsTargetsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "targets",
		Short: "Update docs/targets.md from make help output",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newDocsRunner(cmd)
			if err != nil {
				return err
			}
			helpTarget := docsTargetsHelpTarget
			if docsTargetsAll {
				helpTarget = "help/all"
			}
			return runner.Targets(context.Background(), docspkg.TargetsOptions{
				Output:     docsTargetsOutput,
				MakePath:   docsTargetsMake,
				HelpTarget: helpTarget,
				Title:      docsTargetsTitle,
			})
		},
	}
	cmd.Flags().StringVar(&docsTargetsOutput, "output", "", "Output Markdown path (default docs/targets.md)")
	cmd.Flags().StringVar(&docsTargetsMake, "make", "", "make/gmake executable path")
	cmd.Flags().StringVar(&docsTargetsHelpTarget, "help-target", "", "Make help target (default DEFAULT_HELP_TARGET or help/short)")
	cmd.Flags().BoolVar(&docsTargetsAll, "all", false, "Use help/all")
	cmd.Flags().StringVar(&docsTargetsTitle, "title", "", "Markdown heading (default ## Makefile Targets)")
	return cmd
}

func newDocsTerraformCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "terraform",
		Short: "Update docs/terraform.md from terraform-docs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newDocsRunner(cmd)
			if err != nil {
				return err
			}
			return runner.Terraform(context.Background(), docspkg.TerraformOptions{
				Output:            docsTerraformOutput,
				TerraformDocsPath: docsTerraformPath,
				Format:            docsTerraformFormat,
			})
		},
	}
	cmd.Flags().StringVar(&docsTerraformOutput, "output", "", "Output Markdown path (default docs/terraform.md)")
	cmd.Flags().StringVar(&docsTerraformPath, "terraform-docs", "", "terraform-docs executable path")
	cmd.Flags().StringVar(&docsTerraformFormat, "format", "md", "terraform-docs output format")
	return cmd
}

func newDocsCopyrightCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "copyright-add",
		Short: "Add copyright headers to source code",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newDocsRunner(cmd)
			if err != nil {
				return err
			}
			return runner.CopyrightAdd(context.Background(), docspkg.CopyrightOptions{
				Command:             docsCopyrightCommand,
				Software:            docsCopyrightSoftware,
				SoftwareDescription: docsCopyrightSoftwareDescription,
				License:             docsCopyrightLicense,
				Holder:              docsCopyrightHolder,
				Year:                docsCopyrightYear,
				OutputDir:           docsCopyrightOutputDir,
				WordWrap:            docsCopyrightWordWrap,
			})
		},
	}
	cmd.Flags().StringVar(&docsCopyrightCommand, "cmd", "", "copyright-header command prefix")
	cmd.Flags().StringVar(&docsCopyrightSoftware, "software", "", "Software name")
	cmd.Flags().StringVar(&docsCopyrightSoftwareDescription, "software-description", "", "Software description (required)")
	cmd.Flags().StringVar(&docsCopyrightLicense, "license", "", "License identifier (default ASL2)")
	cmd.Flags().StringVar(&docsCopyrightHolder, "holder", "", "Copyright holder")
	cmd.Flags().StringVar(&docsCopyrightYear, "year", "", "Copyright year")
	cmd.Flags().StringVar(&docsCopyrightOutputDir, "output-dir", "", "Container/output directory")
	cmd.Flags().StringVar(&docsCopyrightWordWrap, "word-wrap", "", "Copyright header word wrap")
	return cmd
}
