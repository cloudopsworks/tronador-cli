// internal/cli/version.go
package cli

import (
	"fmt"
	"tronador-cli/versions"

	"github.com/spf13/cobra"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of tronador-cli",
	Long:  `All software has versions. This is tronador-cli's version.`,
	Run: func(cmd *cobra.Command, args []string) {
		// The actual implementation will access the Version variable from main package
		// retrieve Version from ../../version.go module
		fmt.Println(versions.GetVersion())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
