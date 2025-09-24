// internal/cli/aws.go
package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"tronador-cli/internal/aws"
	"tronador-cli/internal/utils"
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

func init() {
	// Add aws command to root
	rootCmd.AddCommand(awsCmd)

	// Add tag command to aws
	awsCmd.AddCommand(tagCmd)

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

	// AWS configuration flags
	tagCmd.Flags().StringVar(&profile, "profile", "", "AWS profile to use")
	tagCmd.Flags().StringVar(&region, "region", "", "AWS region to use")
	tagCmd.Flags().StringVar(&assumeRoleArn, "assume-role-arn", "", "ARN of role to assume")
	tagCmd.Flags().StringVar(&assumeRoleSessionName, "assume-role-session-name", "", "Session name for assume role")
	tagCmd.Flags().StringVar(&assumeRoleExternalId, "assume-role-external-id", "", "External ID for assume role")
	tagCmd.Flags().Int32Var(&assumeRoleDurationSecs, "assume-role-duration-secs", 3600, "Duration for assume role in seconds")

	// Operation flags
	tagCmd.Flags().StringVar(&resourceTypes, "types", "", "Comma-separated list of resource types (or 'all')")
	tagCmd.Flags().BoolVar(&reapply, "reapply", false, "Reapply tags even if resources already have tags")
	tagCmd.Flags().BoolVar(&includeServiceLinked, "include-service-linked", false, "Include service-linked IAM roles")
	tagCmd.Flags().StringVar(&targetResources, "target", "all", "Target resources: 'resources', 'iam', or 'all'")
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

	// Build AWS configuration
	awsConfig := aws.Config{
		Profile:                profile,
		Region:                 region,
		AssumeRoleArn:          assumeRoleArn,
		AssumeRoleSessionName:  assumeRoleSessionName,
		AssumeRoleExternalId:   assumeRoleExternalId,
		AssumeRoleDurationSecs: assumeRoleDurationSecs,
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
