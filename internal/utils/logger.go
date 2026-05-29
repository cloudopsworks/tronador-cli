// internal/utils/logger.go
package utils

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// IsVerbose Return if verbosity is enabled
func IsVerbose(cmd *cobra.Command) bool {
	verbose, _ := cmd.Flags().GetBool("verbose")
	if !verbose {
		// Check viper as fallback
		verbose = viper.GetBool("verbose")
	}
	return verbose
}

// VerboseLog prints a message only if verbose mode is enabled
func VerboseLog(cmd *cobra.Command, format string, args ...interface{}) {
	verbose, _ := cmd.Flags().GetBool("verbose")
	if !verbose {
		// Check viper as fallback
		verbose = viper.GetBool("verbose")
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "[VERBOSE] "+format+"\n", args...)
	}
}

// DebugLog prints detailed debug information only if verbose mode is enabled
func DebugLog(cmd *cobra.Command, title, details string) {
	verbose, _ := cmd.Flags().GetBool("verbose")
	if !verbose {
		// Check viper as fallback
		verbose = viper.GetBool("verbose")
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "[DEBUG] %s:\n%s\n", title, details)
	}
}
