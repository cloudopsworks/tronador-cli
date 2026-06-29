package cli

import (
	"context"
	"fmt"

	readmepkg "tronador-cli/internal/readme"

	"github.com/spf13/cobra"
)

var (
	readmeWorkDir           string
	readmeFile              string
	readmeYAML              string
	readmeTemplateFile      string
	readmeTemplateYAML      string
	readmeIncludesURI       string
	readmeGomplate          string
	readmeGomplateVersion   string
	readmeToolsDir          string
	readmeToolsConfig       string
	readmeNoInstallGomplate bool
	readmeAssetRepo         string
	readmeAssetRef          string

	readmeAssetsGlobal       bool
	readmeAssetsForce        bool
	readmeAssetsSyncRepo     string
	readmeAssetsSyncRef      string
	readmeAssetsSyncVersion  string
	readmeAssetsSyncBasePath string
	readmeAssetsManifestPath string
	readmeAssetsProject      bool
)

var readmeCmd = &cobra.Command{
	Use:   "readme",
	Short: "README generation and validation commands",
	Long: `README generation and validation commands port the Tronador readme/*
Makefile targets into tronador-cli, with runtime-overridable templates and
explicit GitHub-backed asset sync/cache support.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		runner, err := newReadmeRunner(cmd)
		if err != nil {
			return err
		}
		return runner.Build(context.Background())
	},
}

func init() {
	readmeCmd.PersistentFlags().StringVar(&readmeWorkDir, "workdir", ".", "Target repository directory")
	readmeCmd.PersistentFlags().StringVar(&readmeFile, "readme-file", "", "README output file (default README.md or README_FILE)")
	readmeCmd.PersistentFlags().StringVar(&readmeFile, "output", "", "Alias for --readme-file")
	readmeCmd.PersistentFlags().StringVar(&readmeYAML, "readme-yaml", "", "README data YAML file (default README.yaml or README_YAML)")
	readmeCmd.PersistentFlags().StringVar(&readmeTemplateFile, "template-file", "", "README gomplate template override")
	readmeCmd.PersistentFlags().StringVar(&readmeTemplateYAML, "template-yaml", "", "README.yaml initialization template override")
	readmeCmd.PersistentFlags().StringVar(&readmeIncludesURI, "includes-uri", "", "gomplate include datasource URI (default file://<workdir>/?type=text/plain)")
	readmeCmd.PersistentFlags().StringVar(&readmeGomplate, "gomplate", "", "gomplate executable path")
	readmeCmd.PersistentFlags().StringVar(&readmeGomplateVersion, "gomplate-version", "", "gomplate version to download on demand (default from tool config or configured env vars)")
	readmeCmd.PersistentFlags().StringVar(&readmeToolsDir, "tools-dir", "", "Directory for provisioned Tronador tools (default ~/.cloudopsworks/tronador or TRONADOR_TOOLS_DIR)")
	readmeCmd.PersistentFlags().StringVar(&readmeToolsConfig, "tools-config", "", "Tool provisioner JSON override file (default TRONADOR_TOOLS_CONFIG plus project/user overrides)")
	readmeCmd.PersistentFlags().BoolVar(&readmeNoInstallGomplate, "no-install-gomplate", false, "Do not auto-provision gomplate when missing")
	readmeCmd.PersistentFlags().StringVar(&readmeAssetRepo, "asset-repo", readmepkg.DefaultAssetRepo, "GitHub repo used for cached README assets")
	readmeCmd.PersistentFlags().StringVar(&readmeAssetRef, "asset-ref", readmepkg.DefaultAssetRef, "Git ref used for cached README assets")

	readmeCmd.AddCommand(newReadmeBuildCommand())
	readmeCmd.AddCommand(newReadmeInitCommand())
	readmeCmd.AddCommand(newReadmeLintCommand())
	readmeCmd.AddCommand(newReadmeDepsCommand())
	readmeCmd.AddCommand(newReadmeAssetsCommand())
	rootCmd.AddCommand(readmeCmd)
}

func newReadmeRunner(cmd *cobra.Command) (*readmepkg.Runner, error) {
	return readmepkg.NewRunner(readmepkg.Options{
		WorkDir:             readmeWorkDir,
		ReadmeFile:          readmeFile,
		ReadmeYAML:          readmeYAML,
		TemplateFile:        readmeTemplateFile,
		TemplateYAML:        readmeTemplateYAML,
		IncludesURI:         readmeIncludesURI,
		GomplatePath:        readmeGomplate,
		GomplateVersion:     readmeGomplateVersion,
		ToolsDir:            readmeToolsDir,
		ToolsConfigPath:     readmeToolsConfig,
		SkipGomplateInstall: readmeNoInstallGomplate,
		AssetRepo:           readmeAssetRepo,
		AssetRef:            readmeAssetRef,
		DryRun:              commandDryRun(cmd),
		Stdout:              cmd.OutOrStdout(),
		Stderr:              cmd.ErrOrStderr(),
	})
}

func newReadmeBuildCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "build",
		Short: "Create README.md by building it from README.yaml",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newReadmeRunner(cmd)
			if err != nil {
				return err
			}
			return runner.Build(context.Background())
		},
	}
}

func newReadmeInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create a basic README.yaml when missing",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newReadmeRunner(cmd)
			if err != nil {
				return err
			}
			return runner.Init()
		},
	}
}

func newReadmeLintCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "lint",
		Short: "Verify README.md is up to date",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newReadmeRunner(cmd)
			if err != nil {
				return err
			}
			return runner.Lint(context.Background())
		},
	}
}

func newReadmeDepsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "deps",
		Short: "Check README generation dependencies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newReadmeRunner(cmd)
			if err != nil {
				return err
			}
			return runner.Deps(context.Background())
		},
	}
}

func newReadmeAssetsCommand() *cobra.Command {
	assetsCmd := &cobra.Command{
		Use:   "assets",
		Short: "Manage runtime README template assets",
	}
	assetsCmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Show active README asset paths and sources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newReadmeRunner(cmd)
			if err != nil {
				return err
			}
			paths, err := runner.AssetPaths()
			if err != nil {
				return err
			}
			for _, asset := range paths {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", asset.Name, asset.Source, asset.DisplayPath())
			}
			return nil
		},
	})
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Copy embedded fallback assets into an editable override directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return readmepkg.InitAssets(readmeWorkDir, readmeAssetsGlobal, readmeAssetsForce, commandDryRun(cmd), cmd.OutOrStdout())
		},
	}
	initCmd.Flags().BoolVar(&readmeAssetsGlobal, "global", false, "Write assets to the user config directory instead of the project")
	initCmd.Flags().BoolVar(&readmeAssetsForce, "force", false, "Overwrite existing assets")
	assetsCmd.AddCommand(initCmd)
	assetsCmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate that active README assets can be loaded",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newReadmeRunner(cmd)
			if err != nil {
				return err
			}
			paths, err := runner.AssetPaths()
			if err != nil {
				return err
			}
			for _, asset := range paths {
				data, err := asset.Read()
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s: ok (%d bytes from %s)\n", asset.Name, len(data), asset.Source)
			}
			return nil
		},
	})
	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync README assets from GitHub into the local cache or project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := readmeAssetsSyncRef
			if readmeAssetsSyncVersion != "" {
				ref = readmeAssetsSyncVersion
			}
			return readmepkg.SyncAssets(context.Background(), readmepkg.SyncOptions{
				WorkDir:      readmeWorkDir,
				Repo:         readmeAssetsSyncRepo,
				Ref:          ref,
				BasePath:     readmeAssetsSyncBasePath,
				ManifestPath: readmeAssetsManifestPath,
				Project:      readmeAssetsProject,
				Force:        readmeAssetsForce,
				DryRun:       commandDryRun(cmd),
				Stdout:       cmd.OutOrStdout(),
			})
		},
	}
	syncCmd.Flags().StringVar(&readmeAssetsSyncRepo, "repo", readmepkg.DefaultAssetRepo, "GitHub repo to sync assets from")
	syncCmd.Flags().StringVar(&readmeAssetsSyncRef, "ref", readmepkg.DefaultAssetRef, "Git ref to sync assets from")
	syncCmd.Flags().StringVar(&readmeAssetsSyncVersion, "version", "", "Release tag/ref to sync assets from (overrides --ref)")
	syncCmd.Flags().StringVar(&readmeAssetsSyncBasePath, "base-path", readmepkg.DefaultAssetBasePath, "Path in repo containing README assets")
	syncCmd.Flags().StringVar(&readmeAssetsManifestPath, "manifest-path", "", "Optional manifest path in repo with checksums")
	syncCmd.Flags().BoolVar(&readmeAssetsProject, "project", false, "Also copy synced assets into project .tronador/readme")
	syncCmd.Flags().BoolVar(&readmeAssetsForce, "force", false, "Overwrite existing cached/project assets")
	assetsCmd.AddCommand(syncCmd)
	assetsCmd.AddCommand(newReadmeAssetsCacheCommand())
	return assetsCmd
}

func newReadmeAssetsCacheCommand() *cobra.Command {
	cacheCmd := &cobra.Command{
		Use:   "cache",
		Short: "Inspect or clean the README asset cache",
	}
	cacheCmd.AddCommand(&cobra.Command{
		Use:   "ls",
		Short: "List cached README asset refs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := readmepkg.ListCache()
			if err != nil {
				return err
			}
			for _, entry := range entries {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", entry.Repo, entry.Ref, entry.Path)
			}
			return nil
		},
	})
	cacheCmd.AddCommand(&cobra.Command{
		Use:   "clean",
		Short: "Remove cached README assets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if commandDryRun(cmd) {
				fmt.Fprintln(cmd.OutOrStdout(), "DRY-RUN: clean README asset cache")
				return nil
			}
			return readmepkg.CleanCache()
		},
	})
	return cacheCmd
}
