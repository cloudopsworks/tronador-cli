// internal/cli/sub/aws/tag.go
package aws

import (
	"context"
	"fmt"
	"strings"

	awsclient "tronador-cli/internal/aws"
	"tronador-cli/internal/utils"

	"github.com/spf13/cobra"
)

// TagCmd represents the tag command
var TagCmd = &cobra.Command{
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

// Tag command specific variables
var (
	// Organization metadata flags
	organization     string
	organizationUnit string
	applicationName  string
	applicationType  string
	managedBy        string
	fullnameSep      string

	// Operation flags
	resourceTypes        string
	reapply              bool
	includeServiceLinked bool
	targetResources      string // "resources", "iam", or "all"
)

// AWS configuration variables (shared with parent)
var awsConfig *awsclient.AWSConfig

// InitTagCommand initializes the tag command flags
func InitTagCommand() {
	// Required organization metadata flags
	TagCmd.Flags().StringVar(&organization, "organization", "", "Organization name (required)")
	TagCmd.Flags().StringVar(&organizationUnit, "organization-unit", "", "Organization unit name (required)")
	TagCmd.Flags().StringVar(&applicationName, "application-name", "", "Application name (required)")
	TagCmd.Flags().StringVar(&applicationType, "application-type", "", "Application type (required)")
	TagCmd.MarkFlagRequired("organization")
	TagCmd.MarkFlagRequired("organization-unit")
	TagCmd.MarkFlagRequired("application-name")
	TagCmd.MarkFlagRequired("application-type")

	// Optional organization metadata flags
	TagCmd.Flags().StringVar(&managedBy, "managed-by", "manual", "Managed by value")
	TagCmd.Flags().StringVar(&fullnameSep, "fullname-sep", "-", "Separator for organization-full-name")

	// Operation flags
	TagCmd.Flags().StringVar(&resourceTypes, "types", "", "Comma-separated list of resource types (or 'all')")
	TagCmd.Flags().BoolVar(&reapply, "reapply", false, "Reapply tags even if resources already have tags")
	TagCmd.Flags().BoolVar(&includeServiceLinked, "include-service-linked", false, "Include service-linked IAM roles")
	TagCmd.Flags().StringVar(&targetResources, "target", "all", "Target resources: 'resources', 'iam', or 'all'")
}

// SetAWSConfig sets the shared AWS configuration variables
func SetAWSConfig(p, r, ara, arsn, arei string, ards int32) {
	awsConfig = &awsclient.AWSConfig{
		Profile:                p,
		Region:                 r,
		AssumeRoleArn:          ara,
		AssumeRoleSessionName:  arsn,
		AssumeRoleExternalId:   arei,
		AssumeRoleDurationSecs: ards,
	}
}

// buildAWSConfigFromFlags builds an AWS configuration from the shared variables
func buildAWSConfigFromFlags() awsclient.Config {
	return awsConfig.BuildAWSConfig()
}

// runTagCommand executes the tag command
func runTagCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	utils.VerboseLog(cmd, "Starting tag command execution")
	utils.VerboseLog(cmd, "Command arguments: %v", args)

	// Get dry-run flag from global flags
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		dryRun = false
	}
	utils.VerboseLog(cmd, "Dry-run mode: %t", dryRun)

	// Build tag configuration
	tagConfig := utils.TagConfig{
		Organization:     organization,
		OrganizationUnit: organizationUnit,
		ApplicationName:  applicationName,
		ApplicationType:  applicationType,
		ManagedBy:        managedBy,
		FullNameSep:      fullnameSep,
	}
	utils.VerboseLog(cmd, "Tag configuration built: %+v", tagConfig)

	// Validate tag configuration
	if err := utils.ValidateTagConfig(tagConfig); err != nil {
		utils.VerboseLog(cmd, "Tag configuration validation failed: %v", err)
		return fmt.Errorf("invalid tag configuration: %w", err)
	}
	utils.VerboseLog(cmd, "Tag configuration validation passed")

	// Build AWS configuration using helper function
	awsConfig := buildAWSConfigFromFlags()
	utils.VerboseLog(cmd, "AWS configuration: Profile=%s, Region=%s, AssumeRole=%s",
		awsConfig.Profile, awsConfig.Region, awsConfig.AssumeRoleArn)

	// Create AWS client
	utils.VerboseLog(cmd, "Creating AWS client...")
	awsClient, err := awsclient.NewClient(ctx, awsConfig)
	if err != nil {
		utils.VerboseLog(cmd, "Failed to create AWS client: %v", err)
		return fmt.Errorf("failed to create AWS client: %w", err)
	}
	utils.VerboseLog(cmd, "AWS client created successfully")

	// Print configuration summary
	fmt.Printf("🔧 Configuration:\n")
	fmt.Printf("   Organization: %s\n", organization)
	fmt.Printf("   Organization Unit: %s\n", organizationUnit)
	fmt.Printf("   Application: %s (%s)\n", applicationName, applicationType)
	fmt.Printf("   Managed By: %s\n", managedBy)
	if awsConfig.Profile != "" {
		fmt.Printf("   AWS Profile: %s\n", awsConfig.Profile)
	}
	if awsConfig.Region != "" {
		fmt.Printf("   AWS Region: %s\n", awsConfig.Region)
	}
	if awsConfig.AssumeRoleArn != "" {
		fmt.Printf("   Assume Role: %s\n", awsConfig.AssumeRoleArn)
	}
	if dryRun {
		fmt.Printf("   🧪 DRY-RUN MODE: No changes will be made\n")
	}
	fmt.Println()

	// Build standard tags
	standardTags := utils.BuildStandardTags(tagConfig)
	utils.VerboseLog(cmd, "Standard tags built: %v", standardTags)
	utils.DebugLog(cmd, "Tags to be applied", fmt.Sprintf("%+v", standardTags))

	var totalTagged, totalSkipped, totalFailed int

	utils.VerboseLog(cmd, "Target resources: %s", targetResources)

	// Tag AWS resources (non-IAM)
	if targetResources == "all" || targetResources == "resources" {
		utils.VerboseLog(cmd, "Starting AWS resources tagging...")
		tagged, skipped, failed, err := tagAWSResources(ctx, awsClient, standardTags, dryRun, cmd)
		if err != nil {
			utils.VerboseLog(cmd, "AWS resources tagging failed: %v", err)
			return fmt.Errorf("failed to tag AWS resources: %w", err)
		}
		utils.VerboseLog(cmd, "AWS resources tagging completed: %d tagged, %d skipped, %d failed", tagged, skipped, failed)
		totalTagged += tagged
		totalSkipped += skipped
		totalFailed += failed
	}

	// Tag IAM resources
	if targetResources == "all" || targetResources == "iam" {
		utils.VerboseLog(cmd, "Starting IAM resources tagging...")
		tagged, skipped, failed, err := tagIAMResources(ctx, awsClient, standardTags, dryRun, cmd)
		if err != nil {
			utils.VerboseLog(cmd, "IAM resources tagging failed: %v", err)
			return fmt.Errorf("failed to tag IAM resources: %w", err)
		}
		utils.VerboseLog(cmd, "IAM resources tagging completed: %d tagged, %d skipped, %d failed", tagged, skipped, failed)
		totalTagged += tagged
		totalSkipped += skipped
		totalFailed += failed
	}

	// Print summary
	utils.VerboseLog(cmd, "Final totals: %d tagged, %d skipped, %d failed", totalTagged, totalSkipped, totalFailed)
	fmt.Printf("\n✅ Tagging Summary:\n")
	fmt.Printf("   Resources tagged: %d\n", totalTagged)
	fmt.Printf("   Resources skipped: %d\n", totalSkipped)
	if totalFailed > 0 {
		fmt.Printf("   Resources failed: %d\n", totalFailed)
	}

	utils.VerboseLog(cmd, "Tag command execution completed successfully")
	return nil
}

// tagAWSResources handles tagging of non-IAM AWS resources
func tagAWSResources(ctx context.Context, awsClient *awsclient.Client, tags map[string]string, dryRun bool, cmd *cobra.Command) (int, int, int, error) {
	fmt.Println("🔎 Discovering AWS resources...")
	utils.VerboseLog(cmd, "Starting AWS resources discovery and tagging process")

	// Normalize resource types
	utils.VerboseLog(cmd, "Input resource types: %s", resourceTypes)
	resourceTypesList, err := awsclient.NormalizeResourceTypes(resourceTypes)
	if err != nil {
		utils.VerboseLog(cmd, "Failed to normalize resource types: %v", err)
		return 0, 0, 0, err
	}
	utils.VerboseLog(cmd, "Normalized resource types: %v", resourceTypesList)

	// Create resource tagger
	utils.VerboseLog(cmd, "Creating resource tagger...")
	resourceTagger := awsclient.NewResourceTagger(awsClient)

	// Discover resources via RGTA
	utils.VerboseLog(cmd, "Discovering resources using Resource Groups Tagging API and direct enumeration...")
	resources, err := resourceTagger.DiscoverResources(ctx, resourceTypesList)
	if err != nil {
		utils.VerboseLog(cmd, "Resource discovery failed: %v", err)
		return 0, 0, 0, fmt.Errorf("failed to discover resources: %w", err)
	}

	fmt.Printf("Found %d resources via Resource Groups Tagging API\n", len(resources))
	utils.VerboseLog(cmd, "Resource discovery completed: %d total resources found", len(resources))

	if len(resources) > 0 && len(resources) <= 10 {
		utils.DebugLog(cmd, "Discovered Resources",
			fmt.Sprintf("Total: %d resources\n%s", len(resources),
				func() string {
					var details []string
					for i, r := range resources {
						details = append(details, fmt.Sprintf("%d. %s (tags: %d)", i+1, r.ARN, len(r.Tags)))
					}
					return fmt.Sprintf("  %s", strings.Join(details, "\n  "))
				}()))
	} else if len(resources) > 10 {
		utils.DebugLog(cmd, "Discovered Resources Sample",
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
	utils.VerboseLog(cmd, "Filtering resources based on tagging logic (reapply: %t)...", reapply)
	var resourcesToTag []string
	skipped := 0

	for i, resource := range resources {
		utils.VerboseLog(cmd, "Evaluating resource %d/%d: %s", i+1, len(resources), resource.ARN)
		shouldSkip, reason := utils.ShouldSkipResource(resource.Tags, reapply)
		if shouldSkip {
			skipped++
			utils.VerboseLog(cmd, "Skipping resource: %s (reason: %s)", resource.ARN, reason)
			if reason != "" {
				fmt.Printf("⏭️  Skip %s: %s\n", resource.ARN, reason)
			}
			continue
		}
		utils.VerboseLog(cmd, "Resource will be tagged: %s", resource.ARN)
		resourcesToTag = append(resourcesToTag, resource.ARN)
	}

	utils.VerboseLog(cmd, "Resource filtering completed: %d to tag, %d skipped", len(resourcesToTag), skipped)

	if len(resourcesToTag) == 0 {
		fmt.Println("No resources need tagging.")
		utils.VerboseLog(cmd, "No resources require tagging - operation complete")
		return 0, skipped, 0, nil
	}

	fmt.Printf("Will tag %d resources\n", len(resourcesToTag))
	utils.VerboseLog(cmd, "Proceeding to tag %d resources", len(resourcesToTag))

	if dryRun {
		fmt.Println("🧪 DRY-RUN: Would tag the following resources:")
		utils.VerboseLog(cmd, "DRY-RUN mode: listing resources that would be tagged")
		for i, arn := range resourcesToTag {
			fmt.Printf("  %d. %s\n", i+1, arn)
			if i >= 9 { // Show only first 10 in dry-run
				fmt.Printf("  ... and %d more\n", len(resourcesToTag)-10)
				break
			}
		}
		fmt.Printf("Tags to apply: %v\n", tags)
		utils.VerboseLog(cmd, "DRY-RUN completed: %d resources would be tagged", len(resourcesToTag))
		return len(resourcesToTag), skipped, 0, nil
	}

	// Perform tagging
	utils.VerboseLog(cmd, "Starting actual tagging operation...")
	utils.DebugLog(cmd, "Tagging Details",
		fmt.Sprintf("Resources to tag: %d\nTags to apply: %v\nBatch size: 20 (Resource Groups API limit)",
			len(resourcesToTag), tags))

	err = resourceTagger.TagResources(ctx, resourcesToTag, tags)
	if err != nil {
		utils.VerboseLog(cmd, "Tagging operation failed: %v", err)
		return 0, skipped, len(resourcesToTag), fmt.Errorf("failed to tag resources: %w", err)
	}

	fmt.Printf("✅ Successfully tagged %d resources\n", len(resourcesToTag))
	utils.VerboseLog(cmd, "AWS resources tagging completed successfully: %d tagged, %d skipped", len(resourcesToTag), skipped)
	return len(resourcesToTag), skipped, 0, nil
}

// tagIAMResources handles tagging of IAM roles and policies
func tagIAMResources(ctx context.Context, awsClient *awsclient.Client, tags map[string]string, dryRun bool, cmd *cobra.Command) (int, int, int, error) {
	fmt.Println("🔎 Discovering IAM resources...")
	utils.VerboseLog(cmd, "Starting IAM resources discovery and tagging process")

	// Create IAM tagger
	utils.VerboseLog(cmd, "Creating IAM tagger...")
	iamTagger := awsclient.NewIAMTagger(awsClient)

	var totalTagged, totalSkipped, totalFailed int

	// Tag IAM roles
	fmt.Println("Discovering IAM roles...")
	utils.VerboseLog(cmd, "Starting IAM roles discovery (includeServiceLinked: %t)...", includeServiceLinked)
	roles, err := iamTagger.DiscoverRoles(ctx, includeServiceLinked)
	if err != nil {
		utils.VerboseLog(cmd, "Failed to discover IAM roles: %v", err)
		return 0, 0, 0, fmt.Errorf("failed to discover IAM roles: %w", err)
	}
	utils.VerboseLog(cmd, "IAM roles discovery completed: %d roles found", len(roles))

	// Filter roles based on tagging logic
	utils.VerboseLog(cmd, "Filtering IAM roles based on tagging logic (reapply: %t)...", reapply)
	var rolesToTag []awsclient.IAMRole
	for i, role := range roles {
		utils.VerboseLog(cmd, "Evaluating IAM role %d/%d: %s", i+1, len(roles), role.Name)
		shouldSkip, reason := utils.ShouldSkipResource(role.Tags, reapply)
		if shouldSkip {
			totalSkipped++
			utils.VerboseLog(cmd, "Skipping IAM role: %s (reason: %s)", role.Name, reason)
			if reason != "" {
				fmt.Printf("⏭️  Skip role %s: %s\n", role.Name, reason)
			}
			continue
		}
		utils.VerboseLog(cmd, "IAM role will be tagged: %s", role.Name)
		rolesToTag = append(rolesToTag, role)
	}

	fmt.Printf("Found %d IAM roles, %d need tagging\n", len(roles), len(rolesToTag))
	utils.VerboseLog(cmd, "IAM roles filtering completed: %d total, %d to tag, %d skipped", len(roles), len(rolesToTag), totalSkipped)

	// Tag roles
	utils.VerboseLog(cmd, "Starting IAM roles tagging operation...")
	if len(rolesToTag) > 0 {
		utils.DebugLog(cmd, "IAM Roles to Tag",
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
		utils.VerboseLog(cmd, "Failed to tag IAM roles: %v", err)
		return 0, 0, 0, fmt.Errorf("failed to tag IAM roles: %w", err)
	}
	utils.VerboseLog(cmd, "IAM roles tagging completed: %d tagged, %d skipped", tagged, skipped)
	totalTagged += tagged
	totalSkipped += skipped

	// Tag IAM policies
	fmt.Println("Discovering customer-managed IAM policies...")
	utils.VerboseLog(cmd, "Starting IAM policies discovery...")
	policies, err := iamTagger.DiscoverPolicies(ctx)
	if err != nil {
		utils.VerboseLog(cmd, "Failed to discover IAM policies: %v", err)
		return totalTagged, totalSkipped, 0, fmt.Errorf("failed to discover IAM policies: %w", err)
	}
	utils.VerboseLog(cmd, "IAM policies discovery completed: %d policies found", len(policies))

	// Filter policies based on tagging logic
	utils.VerboseLog(cmd, "Filtering IAM policies based on tagging logic (reapply: %t)...", reapply)
	var policiesToTag []awsclient.IAMPolicy
	for i, policy := range policies {
		utils.VerboseLog(cmd, "Evaluating IAM policy %d/%d: %s", i+1, len(policies), policy.Name)
		shouldSkip, reason := utils.ShouldSkipResource(policy.Tags, reapply)
		if shouldSkip {
			totalSkipped++
			utils.VerboseLog(cmd, "Skipping IAM policy: %s (reason: %s)", policy.Name, reason)
			if reason != "" {
				fmt.Printf("⏭️  Skip policy %s: %s\n", policy.Name, reason)
			}
			continue
		}
		utils.VerboseLog(cmd, "IAM policy will be tagged: %s", policy.Name)
		policiesToTag = append(policiesToTag, policy)
	}

	fmt.Printf("Found %d customer-managed policies, %d need tagging\n", len(policies), len(policiesToTag))
	utils.VerboseLog(cmd, "IAM policies filtering completed: %d total, %d to tag", len(policies), len(policiesToTag))

	// Tag policies
	utils.VerboseLog(cmd, "Starting IAM policies tagging operation...")
	if len(policiesToTag) > 0 {
		utils.DebugLog(cmd, "IAM Policies to Tag",
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
		utils.VerboseLog(cmd, "Failed to tag IAM policies: %v", err)
		return totalTagged, totalSkipped, 0, fmt.Errorf("failed to tag IAM policies: %w", err)
	}
	utils.VerboseLog(cmd, "IAM policies tagging completed: %d tagged, %d skipped", tagged, skipped)
	totalTagged += tagged
	totalSkipped += skipped

	utils.VerboseLog(cmd, "IAM resources tagging completed successfully: %d total tagged, %d total skipped, %d failed", totalTagged, totalSkipped, totalFailed)
	return totalTagged, totalSkipped, totalFailed, nil
}
