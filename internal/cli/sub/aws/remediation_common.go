// internal/cli/sub/aws/remediation_common.go
package aws

import (
	"context"
	"fmt"

	awsclient "tronador-cli/internal/aws"
	"tronador-cli/internal/utils"

	"github.com/spf13/cobra"
)

// RemediationConfig holds common configuration for all remediation commands
type RemediationConfig struct {
	DryRun    bool
	AWSConfig awsclient.Config
	Client    *awsclient.Client
}

// RemediationResult holds the results of a remediation operation
type RemediationResult struct {
	Processed int
	Skipped   int
	Failed    int
}

// BuildRemediationConfig builds a common remediation configuration from command flags
func BuildRemediationConfig(ctx context.Context, cmd *cobra.Command) (*RemediationConfig, error) {
	utils.VerboseLog(cmd, "Building remediation configuration")

	// Get dry-run flag from global flags
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		dryRun = false
	}
	utils.VerboseLog(cmd, "Dry-run mode: %t", dryRun)

	// Build AWS configuration using helper function
	awsConfig := buildAWSConfigFromFlags()
	utils.VerboseLog(cmd, "AWS configuration: Profile=%s, Region=%s, AssumeRole=%s",
		awsConfig.Profile, awsConfig.Region, awsConfig.AssumeRoleArn)

	// Create AWS client
	utils.VerboseLog(cmd, "Creating AWS client...")
	awsClient, err := awsclient.NewClient(ctx, awsConfig)
	if err != nil {
		utils.VerboseLog(cmd, "Failed to create AWS client: %v", err)
		return nil, fmt.Errorf("failed to create AWS client: %w", err)
	}
	utils.VerboseLog(cmd, "AWS client created successfully")

	return &RemediationConfig{
		DryRun:    dryRun,
		AWSConfig: awsConfig,
		Client:    awsClient,
	}, nil
}

// PrintRemediationHeader prints a common header for remediation commands
func PrintRemediationHeader(config *RemediationConfig, serviceName, controlName, description string) {
	fmt.Printf("🔒 %s Security Remediation Configuration:\n", serviceName)
	if config.AWSConfig.Profile != "" {
		fmt.Printf("   AWS Profile: %s\n", config.AWSConfig.Profile)
	}
	if config.AWSConfig.AssumeRoleArn != "" {
		fmt.Printf("   Assume Role: %s\n", config.AWSConfig.AssumeRoleArn)
	}
	fmt.Printf("   Control: %s (%s)\n", controlName, description)
	if config.DryRun {
		fmt.Printf("   🧪 DRY-RUN MODE: No changes will be made\n")
	}
	fmt.Println()
}

// PrintRemediationSummary prints a common summary for remediation results
func PrintRemediationSummary(result *RemediationResult, resourceType string, cmd *cobra.Command) {
	utils.VerboseLog(cmd, "Final totals: %d processed, %d skipped, %d failed", result.Processed, result.Skipped, result.Failed)
	fmt.Printf("\n✅ Security Remediation Summary:\n")
	fmt.Printf("   %s processed: %d\n", resourceType, result.Processed)
	fmt.Printf("   %s skipped: %d\n", resourceType, result.Skipped)
	if result.Failed > 0 {
		fmt.Printf("   %s failed: %d\n", resourceType, result.Failed)
	}
}

// LogRemediationStart logs the start of a remediation operation
func LogRemediationStart(cmd *cobra.Command, serviceName string) {
	utils.VerboseLog(cmd, "Starting %s remediation command execution", serviceName)
	utils.VerboseLog(cmd, "Command arguments: %v", cmd.Flags().Args())
}

// LogRemediationComplete logs the completion of a remediation operation
func LogRemediationComplete(cmd *cobra.Command, serviceName string) {
	utils.VerboseLog(cmd, "%s remediation command execution completed successfully", serviceName)
}

// GetCurrentRegion gets the current AWS region from the client, with fallback
func GetCurrentRegion(client *awsclient.Client, fallback string) string {
	currentRegion := client.GetEffectiveRegion()
	if currentRegion == "" {
		currentRegion = fallback
	}
	return currentRegion
}
