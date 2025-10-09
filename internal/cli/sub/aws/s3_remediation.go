// internal/cli/sub/aws/s3_remediation.go
package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	awsclient "tronador-cli/internal/aws"
	"tronador-cli/internal/utils"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
)

// RemediationCmd represents the remediation command
var RemediationCmd = &cobra.Command{
	Use:   "remediation",
	Short: "AWS security remediation commands",
	Long: `AWS security remediation commands provide functionality for implementing
security best practices and compliance controls across AWS resources.

Available subcommands:
  s3    Remediate S3 security controls including SSL enforcement
  ec2   Remediate EC2 security controls including default security groups restrictions`,
}

// S3RemediationCmd represents the s3 remediation command
var S3RemediationCmd = &cobra.Command{
	Use:   "s3",
	Short: "Remediate S3 security controls (implements S3-5: SSL enforcement)",
	Long: `Remediate S3 security controls by implementing SSL/TLS enforcement on all S3 buckets.

This command implements AWS Security Hub control S3-5 by:
- Discovering all S3 buckets in the account
- Checking existing bucket policies for SSL enforcement
- Adding or modifying bucket policies to deny requests without SSL/TLS
- Skipping buckets that already have proper SSL enforcement in place

The SSL enforcement policy denies all requests where aws:SecureTransport is false,
ensuring that all data in transit to/from S3 buckets is encrypted.

Use --dry-run to see what would be changed without making modifications.`,
	RunE: runS3RemediationCommand,
}

// InitRemediationCommand initializes the remediation command flags
func InitRemediationCommand() {
	// No specific flags needed for S3 remediation currently
	// Uses dry-run flag from parent command
}

// runS3RemediationCommand executes the S3 remediation command
func runS3RemediationCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Log remediation start
	LogRemediationStart(cmd, "S3")

	// Build common remediation configuration
	config, err := BuildRemediationConfig(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to build remediation configuration: %w", err)
	}

	// Print configuration summary
	PrintRemediationHeader(config, "S3", "S3-5", "SSL/TLS enforcement")

	// Perform S3 security remediation
	totalProcessed, totalSkipped, totalFailed, err := remediateS3SecurityControls(ctx, config.Client, config.DryRun, cmd)
	if err != nil {
		utils.VerboseLog(cmd, "S3 security remediation failed: %v", err)
		return fmt.Errorf("failed to remediate S3 security controls: %w", err)
	}

	// Print summary
	result := &RemediationResult{
		Processed: totalProcessed,
		Skipped:   totalSkipped,
		Failed:    totalFailed,
	}
	PrintRemediationSummary(result, "Buckets", cmd)

	// Log completion
	LogRemediationComplete(cmd, "S3")
	return nil
}

// BucketPolicyStatement represents a single statement in an S3 bucket policy
type BucketPolicyStatement struct {
	Sid       string                 `json:"Sid,omitempty"`
	Effect    string                 `json:"Effect"`
	Principal interface{}            `json:"Principal"`
	Action    interface{}            `json:"Action"`
	Resource  interface{}            `json:"Resource"`
	Condition map[string]interface{} `json:"Condition,omitempty"`
}

// BucketPolicy represents an S3 bucket policy document
type BucketPolicy struct {
	Version   string                  `json:"Version"`
	Statement []BucketPolicyStatement `json:"Statement"`
}

// remediateS3SecurityControls handles the core logic for implementing S3 security controls
func remediateS3SecurityControls(ctx context.Context, awsClient *awsclient.Client, dryRun bool, cmd *cobra.Command) (int, int, int, error) {
	fmt.Println("🔍 Discovering S3 buckets...")
	utils.VerboseLog(cmd, "Starting S3 security controls remediation")

	// Get current region
	currentRegion := awsClient.GetEffectiveRegion()
	if currentRegion == "" {
		currentRegion = "us-east-1" // Default region for S3 operations
	}

	fmt.Printf("Using region: %s for S3 operations\n", currentRegion)
	utils.VerboseLog(cmd, "Current region: %s", currentRegion)

	// Create S3 client
	s3Client := s3.NewFromConfig(awsClient.Config)

	// List all S3 buckets
	bucketsResult, err := s3Client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		utils.VerboseLog(cmd, "Failed to list S3 buckets: %v", err)
		return 0, 0, 0, fmt.Errorf("failed to list S3 buckets: %w", err)
	}

	if len(bucketsResult.Buckets) == 0 {
		fmt.Println("No S3 buckets found in account")
		utils.VerboseLog(cmd, "No S3 buckets found")
		return 0, 0, 0, nil
	}

	fmt.Printf("Found %d S3 buckets total\n", len(bucketsResult.Buckets))
	utils.VerboseLog(cmd, "Found %d S3 buckets", len(bucketsResult.Buckets))

	// Filter buckets to only those in the current region
	var bucketsInRegion []string
	fmt.Println("🌍 Checking bucket locations...")

	for _, bucket := range bucketsResult.Buckets {
		if bucket.Name == nil {
			continue
		}

		bucketName := *bucket.Name
		bucketRegion, err := getBucketLocation(ctx, s3Client, bucketName, cmd)
		if err != nil {
			utils.VerboseLog(cmd, "Failed to get location for bucket %s: %v", bucketName, err)
			fmt.Printf("  ⚠️  Skipping bucket %s (location check failed): %v\n", bucketName, err)
			continue
		}

		if bucketRegion == currentRegion {
			bucketsInRegion = append(bucketsInRegion, bucketName)
			utils.VerboseLog(cmd, "Bucket %s is in current region %s", bucketName, currentRegion)
		} else {
			utils.VerboseLog(cmd, "Skipping bucket %s (in region %s, current region %s)", bucketName, bucketRegion, currentRegion)
		}
	}

	if len(bucketsInRegion) == 0 {
		fmt.Printf("No S3 buckets found in current region (%s)\n", currentRegion)
		utils.VerboseLog(cmd, "No S3 buckets found in current region")
		return 0, 0, 0, nil
	}

	fmt.Printf("Found %d S3 buckets in region %s to process\n", len(bucketsInRegion), currentRegion)
	utils.VerboseLog(cmd, "Processing %d buckets in current region", len(bucketsInRegion))

	var totalProcessed, totalSkipped, totalFailed int

	// Process each bucket in the current region
	for i, bucketName := range bucketsInRegion {
		fmt.Printf("🪣 Processing bucket %d/%d: %s\n", i+1, len(bucketsInRegion), bucketName)
		utils.VerboseLog(cmd, "Processing bucket: %s", bucketName)

		// Check if SSL enforcement is already in place
		hasSSLEnforcement, err := checkSSLEnforcement(ctx, s3Client, bucketName, cmd)
		if err != nil {
			utils.VerboseLog(cmd, "Failed to check SSL enforcement for bucket %s: %v", bucketName, err)
			fmt.Printf("  ❌ Error checking SSL enforcement: %v\n", err)
			totalFailed++
			continue
		}

		if hasSSLEnforcement {
			fmt.Printf("  ✅ SSL enforcement already in place, skipping\n")
			utils.VerboseLog(cmd, "SSL enforcement already exists for bucket %s", bucketName)
			totalSkipped++
			continue
		}

		if dryRun {
			fmt.Printf("  🧪 DRY-RUN: Would add SSL enforcement policy to bucket %s\n", bucketName)
			totalProcessed++
			continue
		}

		// Apply SSL enforcement policy
		err = applySSLEnforcement(ctx, s3Client, bucketName, cmd)
		if err != nil {
			utils.VerboseLog(cmd, "Failed to apply SSL enforcement for bucket %s: %v", bucketName, err)
			fmt.Printf("  ❌ Failed to apply SSL enforcement: %v\n", err)
			totalFailed++
			continue
		}

		fmt.Printf("  ✅ Successfully applied SSL enforcement policy\n")
		utils.VerboseLog(cmd, "Successfully applied SSL enforcement for bucket %s", bucketName)
		totalProcessed++
	}

	utils.VerboseLog(cmd, "S3 security controls remediation completed: %d processed, %d skipped, %d failed", totalProcessed, totalSkipped, totalFailed)
	return totalProcessed, totalSkipped, totalFailed, nil
}

// getBucketLocation gets the region location of an S3 bucket
func getBucketLocation(ctx context.Context, s3Client *s3.Client, bucketName string, cmd *cobra.Command) (string, error) {
	utils.VerboseLog(cmd, "Getting location for bucket %s", bucketName)

	locationResult, err := s3Client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: awssdk.String(bucketName),
	})
	if err != nil {
		return "", fmt.Errorf("failed to get bucket location: %w", err)
	}

	// Handle the special case where LocationConstraint is nil (means us-east-1)
	if locationResult.LocationConstraint == "" {
		utils.VerboseLog(cmd, "Bucket %s is in us-east-1 (no location constraint)", bucketName)
		return "us-east-1", nil
	}

	region := string(locationResult.LocationConstraint)
	utils.VerboseLog(cmd, "Bucket %s is in region %s", bucketName, region)
	return region, nil
}

// checkSSLEnforcement checks if a bucket already has SSL enforcement in place
func checkSSLEnforcement(ctx context.Context, s3Client *s3.Client, bucketName string, cmd *cobra.Command) (bool, error) {
	utils.VerboseLog(cmd, "Checking SSL enforcement for bucket %s", bucketName)

	// Get bucket policy
	policyResult, err := s3Client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{
		Bucket: awssdk.String(bucketName),
	})
	if err != nil {
		// If no policy exists, SSL enforcement is not in place
		if strings.Contains(err.Error(), "NoSuchBucketPolicy") {
			utils.VerboseLog(cmd, "No bucket policy exists for %s", bucketName)
			return false, nil
		}
		return false, fmt.Errorf("failed to get bucket policy: %w", err)
	}

	if policyResult.Policy == nil {
		utils.VerboseLog(cmd, "Bucket policy is nil for %s", bucketName)
		return false, nil
	}

	// Parse the policy JSON
	var policy BucketPolicy
	err = json.Unmarshal([]byte(*policyResult.Policy), &policy)
	if err != nil {
		utils.VerboseLog(cmd, "Failed to parse bucket policy for %s: %v", bucketName, err)
		return false, fmt.Errorf("failed to parse bucket policy: %w", err)
	}

	// Check if SSL enforcement statement exists
	for _, statement := range policy.Statement {
		if statement.Effect == "Deny" && statement.Condition != nil {
			if secureTransport, exists := statement.Condition["Bool"]; exists {
				if secureTransportMap, ok := secureTransport.(map[string]interface{}); ok {
					if value, exists := secureTransportMap["aws:SecureTransport"]; exists {
						if boolValue, ok := value.(bool); ok && !boolValue {
							utils.VerboseLog(cmd, "SSL enforcement found in bucket %s policy", bucketName)
							return true, nil
						}
						if stringValue, ok := value.(string); ok && strings.ToLower(stringValue) == "false" {
							utils.VerboseLog(cmd, "SSL enforcement found in bucket %s policy", bucketName)
							return true, nil
						}
					}
				}
			}
		}
	}

	utils.VerboseLog(cmd, "No SSL enforcement found in bucket %s policy", bucketName)
	return false, nil
}

// applySSLEnforcement applies SSL enforcement policy to a bucket
func applySSLEnforcement(ctx context.Context, s3Client *s3.Client, bucketName string, cmd *cobra.Command) error {
	utils.VerboseLog(cmd, "Applying SSL enforcement to bucket %s", bucketName)

	// Create SSL enforcement statement
	sslStatement := BucketPolicyStatement{
		Sid:       "DenyInsecureConnections",
		Effect:    "Deny",
		Principal: "*",
		Action:    "s3:*",
		Resource: []string{
			fmt.Sprintf("arn:aws:s3:::%s", bucketName),
			fmt.Sprintf("arn:aws:s3:::%s/*", bucketName),
		},
		Condition: map[string]interface{}{
			"Bool": map[string]interface{}{
				"aws:SecureTransport": false,
			},
		},
	}

	// Get existing policy if it exists
	var existingPolicy *BucketPolicy
	policyResult, err := s3Client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{
		Bucket: awssdk.String(bucketName),
	})
	if err != nil {
		// If no policy exists, create a new one
		if strings.Contains(err.Error(), "NoSuchBucketPolicy") {
			utils.VerboseLog(cmd, "No existing policy found for bucket %s, creating new policy", bucketName)
			existingPolicy = &BucketPolicy{
				Version:   "2012-10-17",
				Statement: []BucketPolicyStatement{},
			}
		} else {
			return fmt.Errorf("failed to get existing bucket policy: %w", err)
		}
	} else {
		// Parse existing policy
		existingPolicy = &BucketPolicy{}
		err = json.Unmarshal([]byte(*policyResult.Policy), existingPolicy)
		if err != nil {
			return fmt.Errorf("failed to parse existing bucket policy: %w", err)
		}
		utils.VerboseLog(cmd, "Found existing policy for bucket %s with %d statements", bucketName, len(existingPolicy.Statement))
	}

	// Add SSL enforcement statement to existing policy
	existingPolicy.Statement = append(existingPolicy.Statement, sslStatement)

	// Convert policy back to JSON
	policyJSON, err := json.Marshal(existingPolicy)
	if err != nil {
		return fmt.Errorf("failed to marshal bucket policy: %w", err)
	}

	utils.VerboseLog(cmd, "Applying policy with %d statements to bucket %s", len(existingPolicy.Statement), bucketName)

	// Apply the updated policy
	_, err = s3Client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: awssdk.String(bucketName),
		Policy: awssdk.String(string(policyJSON)),
	})
	if err != nil {
		return fmt.Errorf("failed to put bucket policy: %w", err)
	}

	utils.VerboseLog(cmd, "Successfully applied SSL enforcement policy to bucket %s", bucketName)
	return nil
}

func init() {
	// Initialize remediation command
	InitRemediationCommand()

	// Add S3 subcommand to remediation
	RemediationCmd.AddCommand(S3RemediationCmd)

	// Add EC2 subcommand to remediation
	RemediationCmd.AddCommand(EC2RemediationCmd)
}
