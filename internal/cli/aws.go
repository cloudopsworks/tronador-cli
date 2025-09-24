// internal/cli/aws.go
package cli

import (
	"fmt"
	"os"

	"tronador-cli/internal/aws"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// awsCmd represents the aws command
var awsCmd = &cobra.Command{
	Use:   "aws",
	Short: "AWS resource management commands",
	Long: `AWS resource management commands provide functionality for managing
AWS resources including tagging, discovery, and configuration.

Available subcommands:
  tag                   Tag AWS resources with organization metadata
  remove-default-vpc    Remove default VPCs from all regions in the account`,
}

// AWS configuration flags (shared across all subcommands)
var (
	profile                string
	region                 string
	assumeRoleArn          string
	assumeRoleSessionName  string
	assumeRoleExternalId   string
	assumeRoleDurationSecs int32
)

// verboseLog prints a message only if verbose mode is enabled
func verboseLog(cmd *cobra.Command, format string, args ...interface{}) {
	verbose, _ := cmd.Flags().GetBool("verbose")
	if !verbose {
		// Check viper as fallback
		verbose = viper.GetBool("verbose")
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "[VERBOSE] "+format+"\n", args...)
	}
}

// debugLog prints detailed debug information only if verbose mode is enabled
func debugLog(cmd *cobra.Command, title, details string) {
	verbose, _ := cmd.Flags().GetBool("verbose")
	if !verbose {
		// Check viper as fallback
		verbose = viper.GetBool("verbose")
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "[DEBUG] %s:\n%s\n", title, details)
	}
}

// buildAWSConfigFromFlags builds an AWS configuration from the persistent flags
func buildAWSConfigFromFlags(cmd *cobra.Command) (aws.Config, error) {
	// AWS configuration flags are persistent flags on the aws parent command,
	// so they should be accessible from any subcommand
	config := aws.Config{
		Profile:                profile,
		Region:                 region,
		AssumeRoleArn:          assumeRoleArn,
		AssumeRoleSessionName:  assumeRoleSessionName,
		AssumeRoleExternalId:   assumeRoleExternalId,
		AssumeRoleDurationSecs: assumeRoleDurationSecs,
	}

	return config, nil
}

func init() {
	// Add aws command to root
	rootCmd.AddCommand(awsCmd)

	// Add subcommands to aws
	awsCmd.AddCommand(tagCmd)
	awsCmd.AddCommand(removeDefaultVpcCmd)

	// AWS configuration flags (promoted to aws parent command)
	awsCmd.PersistentFlags().StringVar(&profile, "profile", "", "AWS profile to use")
	awsCmd.PersistentFlags().StringVar(&region, "region", "", "AWS region to use")
	awsCmd.PersistentFlags().StringVar(&assumeRoleArn, "assume-role-arn", "", "ARN of role to assume")
	awsCmd.PersistentFlags().StringVar(&assumeRoleSessionName, "assume-role-session-name", "", "Session name for assume role")
	awsCmd.PersistentFlags().StringVar(&assumeRoleExternalId, "assume-role-external-id", "", "External ID for assume role")
	awsCmd.PersistentFlags().Int32Var(&assumeRoleDurationSecs, "assume-role-duration-secs", 3600, "Duration for assume role in seconds")

	// Initialize subcommand flags
	initTagCommand()
	initRemoveDefaultVpcCommand()
}
