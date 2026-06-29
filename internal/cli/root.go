// internal/cli/root.go
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "tronador-cli",
	Short: "A CLI tool for CloudOps Works Tronador workflows",
	Long: `Tronador CLI is a command-line tool for CloudOps Works Tronador workflows.
It provides AWS resource automation, infrastructure-as-code helpers, repository
template lifecycle workflows, and README/documentation generation commands.

Installed releases expose the executable as tronador while this repository and
Go module remain named tronador-cli.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().Bool("dry-run", false, "Show what would be done without making changes")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	// Enable reading from environment variables
	viper.AutomaticEnv()

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		if viper.GetBool("verbose") {
			fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
		}
	}
}
