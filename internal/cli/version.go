package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var cliVersion = "dev"

// SetVersion injects the build version from the root main package. Keeping the
// ldflag-backed version variables at the module root matches the GoReleaser
// pipeline while keeping the CLI package importable for tests.
func SetVersion(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "dev"
	}
	cliVersion = value
}

func currentVersion() string {
	return cliVersion
}

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of tronador-cli",
	Long:  `All software has versions. This is tronador-cli's version.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(currentVersion())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
