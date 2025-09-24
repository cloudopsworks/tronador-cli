// internal/cli/aws.go
package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"tronador-cli/internal/aws"
	"tronador-cli/internal/utils"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
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
  tag    Tag AWS resources with organization metadata`,
}

// tagCmd represents the tag command
var tagCmd = &cobra.Command{
	Use:   "tag",
	Short: "Tag AWS resources with organization metadata",
	Long: `Tag AWS resources with organization metadata.

This command supports tagging various AWS resource types including:
- EC2 resources (instances, VPCs, subnets, security groups, etc.)
- S3 buckets
- IAM roles and policies
- SNS topics, SQS queues
- Secrets Manager secrets
- ACM certificates, KMS keys
- AWS Backup resources

The command uses Resource Groups Tagging API with native service discovery
fallback to ensure comprehensive resource coverage.`,
	RunE: runTagCommand,
}

// removeDefaultVpcCmd represents the remove-default-vpc command
var removeDefaultVpcCmd = &cobra.Command{
	Use:   "remove-default-vpc",
	Short: "Remove default VPCs from all regions in the account",
	Long: `Remove default VPCs from all regions in the account.

This command will:
- Iterate through all AWS regions
- Find default VPCs in each region
- Delete associated resources in the correct order:
  * Internet gateways (detach and delete)
  * Subnets
  * Non-default security groups
- Delete the default VPC itself

Note: The region parameter has no effect as all regions will be scrubbed for default VPCs.
AWS managed resources like the main route table, default network ACL, and default 
security group cannot be deleted and will be cleaned up automatically when the VPC is deleted.

Use --exclude-regions to skip specific regions (comma-separated list).
Use --dry-run to see what would be deleted without making changes.`,
	RunE: runRemoveDefaultVpcCommand,
}

// Global variables for tag command flags
var (
	// Organization metadata flags
	organization     string
	organizationUnit string
	applicationName  string
	applicationType  string
	managedBy        string
	fullnameSep      string

	// AWS configuration flags
	profile                string
	region                 string
	assumeRoleArn          string
	assumeRoleSessionName  string
	assumeRoleExternalId   string
	assumeRoleDurationSecs int32

	// Operation flags
	resourceTypes        string
	reapply              bool
	includeServiceLinked bool
	targetResources      string // "resources", "iam", or "all"
	excludeRegions       string // comma-separated list of regions to exclude from VPC removal
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

	// Add tag command to aws
	awsCmd.AddCommand(tagCmd)

	// Add remove-default-vpc command to aws
	awsCmd.AddCommand(removeDefaultVpcCmd)

	// AWS configuration flags (promoted to aws parent command)
	awsCmd.PersistentFlags().StringVar(&profile, "profile", "", "AWS profile to use")
	awsCmd.PersistentFlags().StringVar(&region, "region", "", "AWS region to use")
	awsCmd.PersistentFlags().StringVar(&assumeRoleArn, "assume-role-arn", "", "ARN of role to assume")
	awsCmd.PersistentFlags().StringVar(&assumeRoleSessionName, "assume-role-session-name", "", "Session name for assume role")
	awsCmd.PersistentFlags().StringVar(&assumeRoleExternalId, "assume-role-external-id", "", "External ID for assume role")
	awsCmd.PersistentFlags().Int32Var(&assumeRoleDurationSecs, "assume-role-duration-secs", 3600, "Duration for assume role in seconds")

	// Required organization metadata flags
	tagCmd.Flags().StringVar(&organization, "organization", "", "Organization name (required)")
	tagCmd.Flags().StringVar(&organizationUnit, "organization-unit", "", "Organization unit name (required)")
	tagCmd.Flags().StringVar(&applicationName, "application-name", "", "Application name (required)")
	tagCmd.Flags().StringVar(&applicationType, "application-type", "", "Application type (required)")
	tagCmd.MarkFlagRequired("organization")
	tagCmd.MarkFlagRequired("organization-unit")
	tagCmd.MarkFlagRequired("application-name")
	tagCmd.MarkFlagRequired("application-type")

	// Optional organization metadata flags
	tagCmd.Flags().StringVar(&managedBy, "managed-by", "manual", "Managed by value")
	tagCmd.Flags().StringVar(&fullnameSep, "fullname-sep", "-", "Separator for organization-full-name")

	// Operation flags
	tagCmd.Flags().StringVar(&resourceTypes, "types", "", "Comma-separated list of resource types (or 'all')")
	tagCmd.Flags().BoolVar(&reapply, "reapply", false, "Reapply tags even if resources already have tags")
	tagCmd.Flags().BoolVar(&includeServiceLinked, "include-service-linked", false, "Include service-linked IAM roles")
	tagCmd.Flags().StringVar(&targetResources, "target", "all", "Target resources: 'resources', 'iam', or 'all'")

	// Remove-default-vpc command flags
	removeDefaultVpcCmd.Flags().StringVar(&excludeRegions, "exclude-regions", "", "Comma-separated list of regions to exclude from VPC removal")
}

// runTagCommand executes the tag command
func runTagCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	verboseLog(cmd, "Starting tag command execution")
	verboseLog(cmd, "Command arguments: %v", args)

	// Get dry-run flag from global flags
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		dryRun = false
	}
	verboseLog(cmd, "Dry-run mode: %t", dryRun)

	// Build tag configuration
	tagConfig := utils.TagConfig{
		Organization:     organization,
		OrganizationUnit: organizationUnit,
		ApplicationName:  applicationName,
		ApplicationType:  applicationType,
		ManagedBy:        managedBy,
		FullNameSep:      fullnameSep,
	}
	verboseLog(cmd, "Tag configuration built: %+v", tagConfig)

	// Validate tag configuration
	if err := utils.ValidateTagConfig(tagConfig); err != nil {
		verboseLog(cmd, "Tag configuration validation failed: %v", err)
		return fmt.Errorf("invalid tag configuration: %w", err)
	}
	verboseLog(cmd, "Tag configuration validation passed")

	// Build AWS configuration using helper function
	awsConfig, err := buildAWSConfigFromFlags(cmd)
	if err != nil {
		verboseLog(cmd, "Failed to build AWS configuration: %v", err)
		return fmt.Errorf("failed to build AWS configuration: %w", err)
	}
	verboseLog(cmd, "AWS configuration: Profile=%s, Region=%s, AssumeRole=%s",
		awsConfig.Profile, awsConfig.Region, awsConfig.AssumeRoleArn)

	// Create AWS client
	verboseLog(cmd, "Creating AWS client...")
	awsClient, err := aws.NewClient(ctx, awsConfig)
	if err != nil {
		verboseLog(cmd, "Failed to create AWS client: %v", err)
		return fmt.Errorf("failed to create AWS client: %w", err)
	}
	verboseLog(cmd, "AWS client created successfully")

	// Print configuration summary
	fmt.Printf("🔧 Configuration:\n")
	fmt.Printf("   Organization: %s\n", organization)
	fmt.Printf("   Organization Unit: %s\n", organizationUnit)
	fmt.Printf("   Application: %s (%s)\n", applicationName, applicationType)
	fmt.Printf("   Managed By: %s\n", managedBy)
	if profile != "" {
		fmt.Printf("   AWS Profile: %s\n", profile)
	}
	if region != "" {
		fmt.Printf("   AWS Region: %s\n", region)
	}
	if assumeRoleArn != "" {
		fmt.Printf("   Assume Role: %s\n", assumeRoleArn)
	}
	if dryRun {
		fmt.Printf("   🧪 DRY-RUN MODE: No changes will be made\n")
	}
	fmt.Println()

	// Build standard tags
	standardTags := utils.BuildStandardTags(tagConfig)
	verboseLog(cmd, "Standard tags built: %v", standardTags)
	debugLog(cmd, "Tags to be applied", fmt.Sprintf("%+v", standardTags))

	var totalTagged, totalSkipped, totalFailed int

	verboseLog(cmd, "Target resources: %s", targetResources)

	// Tag AWS resources (non-IAM)
	if targetResources == "all" || targetResources == "resources" {
		verboseLog(cmd, "Starting AWS resources tagging...")
		tagged, skipped, failed, err := tagAWSResources(ctx, awsClient, standardTags, dryRun, cmd)
		if err != nil {
			verboseLog(cmd, "AWS resources tagging failed: %v", err)
			return fmt.Errorf("failed to tag AWS resources: %w", err)
		}
		verboseLog(cmd, "AWS resources tagging completed: %d tagged, %d skipped, %d failed", tagged, skipped, failed)
		totalTagged += tagged
		totalSkipped += skipped
		totalFailed += failed
	}

	// Tag IAM resources
	if targetResources == "all" || targetResources == "iam" {
		verboseLog(cmd, "Starting IAM resources tagging...")
		tagged, skipped, failed, err := tagIAMResources(ctx, awsClient, standardTags, dryRun, cmd)
		if err != nil {
			verboseLog(cmd, "IAM resources tagging failed: %v", err)
			return fmt.Errorf("failed to tag IAM resources: %w", err)
		}
		verboseLog(cmd, "IAM resources tagging completed: %d tagged, %d skipped, %d failed", tagged, skipped, failed)
		totalTagged += tagged
		totalSkipped += skipped
		totalFailed += failed
	}

	// Print summary
	verboseLog(cmd, "Final totals: %d tagged, %d skipped, %d failed", totalTagged, totalSkipped, totalFailed)
	fmt.Printf("\n✅ Tagging Summary:\n")
	fmt.Printf("   Resources tagged: %d\n", totalTagged)
	fmt.Printf("   Resources skipped: %d\n", totalSkipped)
	if totalFailed > 0 {
		fmt.Printf("   Resources failed: %d\n", totalFailed)
	}

	verboseLog(cmd, "Tag command execution completed successfully")
	return nil
}

// tagAWSResources handles tagging of non-IAM AWS resources
func tagAWSResources(ctx context.Context, awsClient *aws.Client, tags map[string]string, dryRun bool, cmd *cobra.Command) (int, int, int, error) {
	fmt.Println("🔎 Discovering AWS resources...")
	verboseLog(cmd, "Starting AWS resources discovery and tagging process")

	// Normalize resource types
	verboseLog(cmd, "Input resource types: %s", resourceTypes)
	resourceTypesList, err := aws.NormalizeResourceTypes(resourceTypes)
	if err != nil {
		verboseLog(cmd, "Failed to normalize resource types: %v", err)
		return 0, 0, 0, err
	}
	verboseLog(cmd, "Normalized resource types: %v", resourceTypesList)

	// Create resource tagger
	verboseLog(cmd, "Creating resource tagger...")
	resourceTagger := aws.NewResourceTagger(awsClient)

	// Discover resources via RGTA
	verboseLog(cmd, "Discovering resources using Resource Groups Tagging API and direct enumeration...")
	resources, err := resourceTagger.DiscoverResources(ctx, resourceTypesList)
	if err != nil {
		verboseLog(cmd, "Resource discovery failed: %v", err)
		return 0, 0, 0, fmt.Errorf("failed to discover resources: %w", err)
	}

	fmt.Printf("Found %d resources via Resource Groups Tagging API\n", len(resources))
	verboseLog(cmd, "Resource discovery completed: %d total resources found", len(resources))

	if len(resources) > 0 && len(resources) <= 10 {
		debugLog(cmd, "Discovered Resources",
			fmt.Sprintf("Total: %d resources\n%s", len(resources),
				func() string {
					var details []string
					for i, r := range resources {
						details = append(details, fmt.Sprintf("%d. %s (tags: %d)", i+1, r.ARN, len(r.Tags)))
					}
					return fmt.Sprintf("  %s", strings.Join(details, "\n  "))
				}()))
	} else if len(resources) > 10 {
		debugLog(cmd, "Discovered Resources Sample",
			fmt.Sprintf("Total: %d resources (showing first 10)\n%s", len(resources),
				func() string {
					var details []string
					for i := 0; i < 10; i++ {
						r := resources[i]
						details = append(details, fmt.Sprintf("%d. %s (tags: %d)", i+1, r.ARN, len(r.Tags)))
					}
					return fmt.Sprintf("  %s\n  ... and %d more", strings.Join(details, "\n  "), len(resources)-10)
				}()))
	}

	// Filter resources based on tagging logic
	verboseLog(cmd, "Filtering resources based on tagging logic (reapply: %t)...", reapply)
	var resourcesToTag []string
	skipped := 0

	for i, resource := range resources {
		verboseLog(cmd, "Evaluating resource %d/%d: %s", i+1, len(resources), resource.ARN)
		shouldSkip, reason := utils.ShouldSkipResource(resource.Tags, reapply)
		if shouldSkip {
			skipped++
			verboseLog(cmd, "Skipping resource: %s (reason: %s)", resource.ARN, reason)
			if reason != "" {
				fmt.Printf("⏭️  Skip %s: %s\n", resource.ARN, reason)
			}
			continue
		}
		verboseLog(cmd, "Resource will be tagged: %s", resource.ARN)
		resourcesToTag = append(resourcesToTag, resource.ARN)
	}

	verboseLog(cmd, "Resource filtering completed: %d to tag, %d skipped", len(resourcesToTag), skipped)

	if len(resourcesToTag) == 0 {
		fmt.Println("No resources need tagging.")
		verboseLog(cmd, "No resources require tagging - operation complete")
		return 0, skipped, 0, nil
	}

	fmt.Printf("Will tag %d resources\n", len(resourcesToTag))
	verboseLog(cmd, "Proceeding to tag %d resources", len(resourcesToTag))

	if dryRun {
		fmt.Println("🧪 DRY-RUN: Would tag the following resources:")
		verboseLog(cmd, "DRY-RUN mode: listing resources that would be tagged")
		for i, arn := range resourcesToTag {
			fmt.Printf("  %d. %s\n", i+1, arn)
			if i >= 9 { // Show only first 10 in dry-run
				fmt.Printf("  ... and %d more\n", len(resourcesToTag)-10)
				break
			}
		}
		fmt.Printf("Tags to apply: %v\n", tags)
		verboseLog(cmd, "DRY-RUN completed: %d resources would be tagged", len(resourcesToTag))
		return len(resourcesToTag), skipped, 0, nil
	}

	// Perform tagging
	verboseLog(cmd, "Starting actual tagging operation...")
	debugLog(cmd, "Tagging Details",
		fmt.Sprintf("Resources to tag: %d\nTags to apply: %v\nBatch size: 20 (Resource Groups API limit)",
			len(resourcesToTag), tags))

	err = resourceTagger.TagResources(ctx, resourcesToTag, tags)
	if err != nil {
		verboseLog(cmd, "Tagging operation failed: %v", err)
		return 0, skipped, len(resourcesToTag), fmt.Errorf("failed to tag resources: %w", err)
	}

	fmt.Printf("✅ Successfully tagged %d resources\n", len(resourcesToTag))
	verboseLog(cmd, "AWS resources tagging completed successfully: %d tagged, %d skipped", len(resourcesToTag), skipped)
	return len(resourcesToTag), skipped, 0, nil
}

// tagIAMResources handles tagging of IAM roles and policies
func tagIAMResources(ctx context.Context, awsClient *aws.Client, tags map[string]string, dryRun bool, cmd *cobra.Command) (int, int, int, error) {
	fmt.Println("🔎 Discovering IAM resources...")
	verboseLog(cmd, "Starting IAM resources discovery and tagging process")

	// Create IAM tagger
	verboseLog(cmd, "Creating IAM tagger...")
	iamTagger := aws.NewIAMTagger(awsClient)

	var totalTagged, totalSkipped, totalFailed int

	// Tag IAM roles
	fmt.Println("Discovering IAM roles...")
	verboseLog(cmd, "Starting IAM roles discovery (includeServiceLinked: %t)...", includeServiceLinked)
	roles, err := iamTagger.DiscoverRoles(ctx, includeServiceLinked)
	if err != nil {
		verboseLog(cmd, "Failed to discover IAM roles: %v", err)
		return 0, 0, 0, fmt.Errorf("failed to discover IAM roles: %w", err)
	}
	verboseLog(cmd, "IAM roles discovery completed: %d roles found", len(roles))

	// Filter roles based on tagging logic
	verboseLog(cmd, "Filtering IAM roles based on tagging logic (reapply: %t)...", reapply)
	var rolesToTag []aws.IAMRole
	for i, role := range roles {
		verboseLog(cmd, "Evaluating IAM role %d/%d: %s", i+1, len(roles), role.Name)
		shouldSkip, reason := utils.ShouldSkipResource(role.Tags, reapply)
		if shouldSkip {
			totalSkipped++
			verboseLog(cmd, "Skipping IAM role: %s (reason: %s)", role.Name, reason)
			if reason != "" {
				fmt.Printf("⏭️  Skip role %s: %s\n", role.Name, reason)
			}
			continue
		}
		verboseLog(cmd, "IAM role will be tagged: %s", role.Name)
		rolesToTag = append(rolesToTag, role)
	}

	fmt.Printf("Found %d IAM roles, %d need tagging\n", len(roles), len(rolesToTag))
	verboseLog(cmd, "IAM roles filtering completed: %d total, %d to tag, %d skipped", len(roles), len(rolesToTag), totalSkipped)

	// Tag roles
	verboseLog(cmd, "Starting IAM roles tagging operation...")
	if len(rolesToTag) > 0 {
		debugLog(cmd, "IAM Roles to Tag",
			fmt.Sprintf("Count: %d\nSample roles: %s", len(rolesToTag),
				func() string {
					var samples []string
					limit := len(rolesToTag)
					if limit > 5 {
						limit = 5
					}
					for i := 0; i < limit; i++ {
						samples = append(samples, fmt.Sprintf("- %s", rolesToTag[i].Name))
					}
					if len(rolesToTag) > 5 {
						samples = append(samples, fmt.Sprintf("... and %d more", len(rolesToTag)-5))
					}
					return strings.Join(samples, "\n")
				}()))
	}

	tagged, skipped, err := iamTagger.TagRoles(ctx, rolesToTag, tags, dryRun)
	if err != nil {
		verboseLog(cmd, "Failed to tag IAM roles: %v", err)
		return 0, 0, 0, fmt.Errorf("failed to tag IAM roles: %w", err)
	}
	verboseLog(cmd, "IAM roles tagging completed: %d tagged, %d skipped", tagged, skipped)
	totalTagged += tagged
	totalSkipped += skipped

	// Tag IAM policies
	fmt.Println("Discovering customer-managed IAM policies...")
	verboseLog(cmd, "Starting IAM policies discovery...")
	policies, err := iamTagger.DiscoverPolicies(ctx)
	if err != nil {
		verboseLog(cmd, "Failed to discover IAM policies: %v", err)
		return totalTagged, totalSkipped, 0, fmt.Errorf("failed to discover IAM policies: %w", err)
	}
	verboseLog(cmd, "IAM policies discovery completed: %d policies found", len(policies))

	// Filter policies based on tagging logic
	verboseLog(cmd, "Filtering IAM policies based on tagging logic (reapply: %t)...", reapply)
	var policiesToTag []aws.IAMPolicy
	for i, policy := range policies {
		verboseLog(cmd, "Evaluating IAM policy %d/%d: %s", i+1, len(policies), policy.Name)
		shouldSkip, reason := utils.ShouldSkipResource(policy.Tags, reapply)
		if shouldSkip {
			totalSkipped++
			verboseLog(cmd, "Skipping IAM policy: %s (reason: %s)", policy.Name, reason)
			if reason != "" {
				fmt.Printf("⏭️  Skip policy %s: %s\n", policy.Name, reason)
			}
			continue
		}
		verboseLog(cmd, "IAM policy will be tagged: %s", policy.Name)
		policiesToTag = append(policiesToTag, policy)
	}

	fmt.Printf("Found %d customer-managed policies, %d need tagging\n", len(policies), len(policiesToTag))
	verboseLog(cmd, "IAM policies filtering completed: %d total, %d to tag", len(policies), len(policiesToTag))

	// Tag policies
	verboseLog(cmd, "Starting IAM policies tagging operation...")
	if len(policiesToTag) > 0 {
		debugLog(cmd, "IAM Policies to Tag",
			fmt.Sprintf("Count: %d\nSample policies: %s", len(policiesToTag),
				func() string {
					var samples []string
					limit := len(policiesToTag)
					if limit > 5 {
						limit = 5
					}
					for i := 0; i < limit; i++ {
						samples = append(samples, fmt.Sprintf("- %s", policiesToTag[i].Name))
					}
					if len(policiesToTag) > 5 {
						samples = append(samples, fmt.Sprintf("... and %d more", len(policiesToTag)-5))
					}
					return strings.Join(samples, "\n")
				}()))
	}

	tagged, skipped, err = iamTagger.TagPolicies(ctx, policiesToTag, tags, dryRun)
	if err != nil {
		verboseLog(cmd, "Failed to tag IAM policies: %v", err)
		return totalTagged, totalSkipped, 0, fmt.Errorf("failed to tag IAM policies: %w", err)
	}
	verboseLog(cmd, "IAM policies tagging completed: %d tagged, %d skipped", tagged, skipped)
	totalTagged += tagged
	totalSkipped += skipped

	verboseLog(cmd, "IAM resources tagging completed successfully: %d total tagged, %d total skipped, %d failed", totalTagged, totalSkipped, totalFailed)
	return totalTagged, totalSkipped, totalFailed, nil
}

// runRemoveDefaultVpcCommand executes the remove-default-vpc command
func runRemoveDefaultVpcCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	verboseLog(cmd, "Starting remove-default-vpc command execution")
	verboseLog(cmd, "Command arguments: %v", args)

	// Get dry-run flag from global flags
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		dryRun = false
	}
	verboseLog(cmd, "Dry-run mode: %t", dryRun)

	// Build AWS configuration using helper function
	awsConfig, err := buildAWSConfigFromFlags(cmd)
	if err != nil {
		verboseLog(cmd, "Failed to build AWS configuration: %v", err)
		return fmt.Errorf("failed to build AWS configuration: %w", err)
	}
	verboseLog(cmd, "AWS configuration: Profile=%s, Region=%s, AssumeRole=%s",
		awsConfig.Profile, awsConfig.Region, awsConfig.AssumeRoleArn)

	// Create AWS client
	verboseLog(cmd, "Creating AWS client...")
	awsClient, err := aws.NewClient(ctx, awsConfig)
	if err != nil {
		verboseLog(cmd, "Failed to create AWS client: %v", err)
		return fmt.Errorf("failed to create AWS client: %w", err)
	}
	verboseLog(cmd, "AWS client created successfully")

	// Print configuration summary
	fmt.Printf("🗑️ Default VPC Removal Configuration:\n")
	if profile != "" {
		fmt.Printf("   AWS Profile: %s\n", profile)
	}
	if assumeRoleArn != "" {
		fmt.Printf("   Assume Role: %s\n", assumeRoleArn)
	}
	if excludeRegions != "" {
		fmt.Printf("   Excluded Regions: %s\n", excludeRegions)
	}
	if dryRun {
		fmt.Printf("   🧪 DRY-RUN MODE: No changes will be made\n")
	}
	fmt.Printf("   Note: All regions will be processed (region parameter ignored)\n")
	fmt.Println()

	// Perform default VPC removal
	totalRemoved, totalSkipped, totalFailed, err := removeDefaultVPCs(ctx, awsClient, dryRun, cmd)
	if err != nil {
		verboseLog(cmd, "Default VPC removal failed: %v", err)
		return fmt.Errorf("failed to remove default VPCs: %w", err)
	}

	// Print summary
	verboseLog(cmd, "Final totals: %d removed, %d skipped, %d failed", totalRemoved, totalSkipped, totalFailed)
	fmt.Printf("\n✅ Default VPC Removal Summary:\n")
	fmt.Printf("   VPCs removed: %d\n", totalRemoved)
	fmt.Printf("   VPCs skipped: %d\n", totalSkipped)
	if totalFailed > 0 {
		fmt.Printf("   VPCs failed: %d\n", totalFailed)
	}

	verboseLog(cmd, "Remove-default-vpc command execution completed successfully")
	return nil
}

// removeDefaultVPCs handles the core logic for removing default VPCs from all regions
func removeDefaultVPCs(ctx context.Context, awsClient *aws.Client, dryRun bool, cmd *cobra.Command) (int, int, int, error) {
	fmt.Println("🔎 Discovering AWS regions...")
	verboseLog(cmd, "Starting default VPC removal process")

	// Create EC2 client for region discovery (using us-east-1 as per shell script)
	regionDiscoveryConfig := awsClient.Config
	regionDiscoveryConfig.Region = "us-east-1"
	ec2ClientForRegions := ec2.NewFromConfig(regionDiscoveryConfig)

	// Get all AWS regions
	regionsResult, err := ec2ClientForRegions.DescribeRegions(ctx, &ec2.DescribeRegionsInput{})
	if err != nil {
		verboseLog(cmd, "Failed to describe regions: %v", err)
		return 0, 0, 0, fmt.Errorf("failed to describe regions: %w", err)
	}

	regions := make([]string, 0, len(regionsResult.Regions))
	for _, region := range regionsResult.Regions {
		if region.RegionName != nil {
			regions = append(regions, *region.RegionName)
		}
	}

	fmt.Printf("Found %d AWS regions to process\n", len(regions))
	verboseLog(cmd, "Regions to process: %v", regions)

	// Parse excluded regions
	var excludedRegionsList []string
	if excludeRegions != "" {
		excludedRegionsList = strings.Split(excludeRegions, ",")
		for i, region := range excludedRegionsList {
			excludedRegionsList[i] = strings.TrimSpace(region)
		}
		verboseLog(cmd, "Excluded regions: %v", excludedRegionsList)
	}

	var totalRemoved, totalSkipped, totalFailed int

	// Process each region
	for i, regionName := range regions {
		fmt.Printf("🌍 Processing region %d/%d: %s\n", i+1, len(regions), regionName)
		verboseLog(cmd, "Processing region: %s", regionName)

		// Check if region is excluded
		isExcluded := false
		for _, excludedRegion := range excludedRegionsList {
			if regionName == excludedRegion {
				isExcluded = true
				break
			}
		}

		if isExcluded {
			fmt.Printf("  ⏭️  Skipping region %s (excluded)\n", regionName)
			verboseLog(cmd, "Skipping excluded region: %s", regionName)
			totalSkipped++
			continue
		}

		// Create region-specific EC2 client
		regionConfig := awsClient.Config
		regionConfig.Region = regionName
		ec2Client := ec2.NewFromConfig(regionConfig)

		// Find default VPCs in this region
		vpcsResult, err := ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
			Filters: []types.Filter{
				{
					Name:   awssdk.String("isDefault"),
					Values: []string{"true"},
				},
			},
		})
		if err != nil {
			verboseLog(cmd, "Failed to describe VPCs in region %s: %v", regionName, err)
			fmt.Printf("  ❌ Error describing VPCs in region %s: %v\n", regionName, err)
			totalFailed++
			continue
		}

		if len(vpcsResult.Vpcs) == 0 {
			fmt.Printf("  ✅ No default VPC found in region %s\n", regionName)
			verboseLog(cmd, "No default VPC found in region: %s", regionName)
			totalSkipped++
			continue
		}

		// Process each default VPC (typically only one per region)
		for _, vpc := range vpcsResult.Vpcs {
			if vpc.VpcId == nil {
				continue
			}

			vpcId := *vpc.VpcId
			fmt.Printf("  🎯 Found default VPC: %s\n", vpcId)
			verboseLog(cmd, "Processing default VPC %s in region %s", vpcId, regionName)

			if dryRun {
				fmt.Printf("  🧪 DRY-RUN: Would remove VPC %s and its resources\n", vpcId)
				totalRemoved++
				continue
			}

			// Remove VPC and its resources
			err := removeVPCResources(ctx, ec2Client, vpcId, regionName, cmd)
			if err != nil {
				verboseLog(cmd, "Failed to remove VPC %s in region %s: %v", vpcId, regionName, err)
				fmt.Printf("  ❌ Failed to remove VPC %s: %v\n", vpcId, err)
				totalFailed++
				continue
			}

			fmt.Printf("  ✅ Successfully removed VPC %s\n", vpcId)
			verboseLog(cmd, "Successfully removed VPC %s in region %s", vpcId, regionName)
			totalRemoved++
		}
	}

	verboseLog(cmd, "Default VPC removal completed: %d removed, %d skipped, %d failed", totalRemoved, totalSkipped, totalFailed)
	return totalRemoved, totalSkipped, totalFailed, nil
}

// removeVPCResources systematically removes all resources associated with a VPC
func removeVPCResources(ctx context.Context, ec2Client *ec2.Client, vpcId, regionName string, cmd *cobra.Command) error {
	verboseLog(cmd, "Starting removal of resources for VPC %s in region %s", vpcId, regionName)

	// 1. Remove Internet Gateways (detach and delete)
	verboseLog(cmd, "Looking for internet gateways attached to VPC %s", vpcId)
	igwResult, err := ec2Client.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{
		Filters: []types.Filter{
			{
				Name:   awssdk.String("attachment.vpc-id"),
				Values: []string{vpcId},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to describe internet gateways: %w", err)
	}

	for _, igw := range igwResult.InternetGateways {
		if igw.InternetGatewayId == nil {
			continue
		}
		igwId := *igw.InternetGatewayId
		fmt.Printf("    🌐 Detaching and deleting internet gateway %s\n", igwId)
		verboseLog(cmd, "Detaching internet gateway %s from VPC %s", igwId, vpcId)

		// Detach IGW from VPC
		_, err := ec2Client.DetachInternetGateway(ctx, &ec2.DetachInternetGatewayInput{
			InternetGatewayId: igw.InternetGatewayId,
			VpcId:             awssdk.String(vpcId),
		})
		if err != nil {
			return fmt.Errorf("failed to detach internet gateway %s: %w", igwId, err)
		}

		// Delete IGW
		verboseLog(cmd, "Deleting internet gateway %s", igwId)
		_, err = ec2Client.DeleteInternetGateway(ctx, &ec2.DeleteInternetGatewayInput{
			InternetGatewayId: igw.InternetGatewayId,
		})
		if err != nil {
			return fmt.Errorf("failed to delete internet gateway %s: %w", igwId, err)
		}
	}

	// 2. Remove Subnets
	verboseLog(cmd, "Looking for subnets in VPC %s", vpcId)
	subnetsResult, err := ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: []types.Filter{
			{
				Name:   awssdk.String("vpc-id"),
				Values: []string{vpcId},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to describe subnets: %w", err)
	}

	for _, subnet := range subnetsResult.Subnets {
		if subnet.SubnetId == nil {
			continue
		}
		subnetId := *subnet.SubnetId
		fmt.Printf("    🏠 Deleting subnet %s\n", subnetId)
		verboseLog(cmd, "Deleting subnet %s", subnetId)

		_, err := ec2Client.DeleteSubnet(ctx, &ec2.DeleteSubnetInput{
			SubnetId: subnet.SubnetId,
		})
		if err != nil {
			return fmt.Errorf("failed to delete subnet %s: %w", subnetId, err)
		}
	}

	// 3. Remove non-default Security Groups
	verboseLog(cmd, "Looking for security groups in VPC %s", vpcId)
	sgResult, err := ec2Client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []types.Filter{
			{
				Name:   awssdk.String("vpc-id"),
				Values: []string{vpcId},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to describe security groups: %w", err)
	}

	for _, sg := range sgResult.SecurityGroups {
		if sg.GroupId == nil || sg.GroupName == nil {
			continue
		}

		// Skip the default security group (it will be deleted automatically with the VPC)
		if *sg.GroupName == "default" {
			verboseLog(cmd, "Skipping default security group %s", *sg.GroupId)
			continue
		}

		sgId := *sg.GroupId
		fmt.Printf("    🛡️  Deleting security group %s\n", sgId)
		verboseLog(cmd, "Deleting security group %s", sgId)

		_, err := ec2Client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
			GroupId: sg.GroupId,
		})
		if err != nil {
			return fmt.Errorf("failed to delete security group %s: %w", sgId, err)
		}
	}

	// 4. Finally, delete the VPC itself
	fmt.Printf("    🗑️  Deleting VPC %s\n", vpcId)
	verboseLog(cmd, "Deleting VPC %s", vpcId)

	_, err = ec2Client.DeleteVpc(ctx, &ec2.DeleteVpcInput{
		VpcId: awssdk.String(vpcId),
	})
	if err != nil {
		return fmt.Errorf("failed to delete VPC %s: %w", vpcId, err)
	}

	verboseLog(cmd, "Successfully removed all resources for VPC %s", vpcId)
	return nil
}
