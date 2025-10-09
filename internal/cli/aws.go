// internal/cli/aws.go
package cli

import (
	"tronador-cli/internal/aws"
	awssub "tronador-cli/internal/cli/sub/aws"

	"github.com/spf13/cobra"
)

// awsCmd represents the aws command
var awsCmd = &cobra.Command{
	Use:   "aws",
	Short: "AWS resource management commands",
	Long: `AWS resource management commands provide functionality for managing
AWS resources including tagging, discovery, and configuration.

Available subcommands:
  tag                   Tag AWS resources with organization metadata
  remove-default-vpc    Remove default VPCs from all regions in the account
  remediation           AWS security remediation commands`,
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

	// AWS configuration flags (promoted to aws parent command)
	awsCmd.PersistentFlags().StringVar(&profile, "profile", "", "AWS profile to use")
	awsCmd.PersistentFlags().StringVar(&region, "region", "", "AWS region to use")
	awsCmd.PersistentFlags().StringVar(&assumeRoleArn, "assume-role-arn", "", "ARN of role to assume")
	awsCmd.PersistentFlags().StringVar(&assumeRoleSessionName, "assume-role-session-name", "", "Session name for assume role")
	awsCmd.PersistentFlags().StringVar(&assumeRoleExternalId, "assume-role-external-id", "", "External ID for assume role")
	awsCmd.PersistentFlags().Int32Var(&assumeRoleDurationSecs, "assume-role-duration-secs", 3600, "Duration for assume role in seconds")

	// Initialize subcommand flags
	awssub.InitTagCommand()
	awssub.InitRemoveDefaultVpcCommand()
	awssub.InitRemediationCommand()

	// Add subcommands to aws
	awsCmd.AddCommand(awssub.TagCmd)
	awsCmd.AddCommand(awssub.RemoveDefaultVpcCmd)
	awsCmd.AddCommand(awssub.RemediationCmd)

	// Set up a pre-run hook to share AWS configuration with subcommands
	awsCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		awssub.SetAWSConfig(profile, region, assumeRoleArn, assumeRoleSessionName, assumeRoleExternalId, assumeRoleDurationSecs)
	}
}
