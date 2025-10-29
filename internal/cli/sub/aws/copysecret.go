// internal/cli/sub/aws/copysecret.go
package aws

import (
	"context"
	"fmt"

	awsclient "tronador-cli/internal/aws"
	"tronador-cli/internal/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/spf13/cobra"
)

// CopySecretCmd represents the copysecret command
var CopySecretCmd = &cobra.Command{
	Use:   "copysecret",
	Short: "Copy a secret from one location to another",
	Long: `Copy a secret from one location to another.

This command supports copying AWS Secrets Manager secrets within the same account,
across accounts using assume role authentication, or across regions.

Examples:
  # Copy secret within the same account and region
  tronador-cli aws copysecret --source my-secret --dest my-secret-copy

  # Copy secret across regions in the same account
  tronador-cli aws copysecret --source my-secret --dest my-secret-copy --dest-region us-west-2

  # Copy secret across accounts using assume role
  tronador-cli aws copysecret --source my-secret --dest my-secret-copy --dest-assume-role-arn arn:aws:iam::123456789012:role/MyRole

  # Copy secret across accounts and regions
  tronador-cli aws copysecret --source my-secret --dest my-secret-copy --dest-assume-role-arn arn:aws:iam::123456789012:role/MyRole --dest-region us-west-2

Note: If --dest parameter is omitted, the destination secret will have the same name as the source.
If the destination secret already exists, a new version will be created instead of overwriting the current version.`,
	RunE: runCopySecretCommand,
}

// Copysecret command specific variables
var (
	sourceSecret      string
	destSecret        string
	destAssumeRoleArn string
	destRegion        string
)

// InitCopySecretCommand initializes the copysecret command flags
func InitCopySecretCommand() {
	// Required source secret flag
	CopySecretCmd.Flags().StringVar(&sourceSecret, "source", "", "Source secret name or ARN (required)")
	CopySecretCmd.MarkFlagRequired("source")

	// Optional destination secret flag
	CopySecretCmd.Flags().StringVar(&destSecret, "dest", "", "Destination secret name (optional, defaults to source name)")

	// Optional destination assume role flag
	CopySecretCmd.Flags().StringVar(&destAssumeRoleArn, "dest-assume-role-arn", "", "ARN of role to assume for destination account (optional)")

	// Optional destination region flag
	CopySecretCmd.Flags().StringVar(&destRegion, "dest-region", "", "Destination region for cross-region copy (optional)")
}

// runCopySecretCommand executes the copysecret command
func runCopySecretCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	utils.VerboseLog(cmd, "Starting copysecret command execution")
	utils.VerboseLog(cmd, "Command arguments: %v", args)

	// Get dry-run flag from global flags
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		dryRun = false
	}
	utils.VerboseLog(cmd, "Dry-run mode: %t", dryRun)

	// Build source AWS configuration using helper function
	awsConfig := buildAWSConfigFromFlags()
	utils.VerboseLog(cmd, "AWS configuration: Profile=%s, Region=%s, AssumeRole=%s",
		awsConfig.Profile, awsConfig.Region, awsConfig.AssumeRoleArn)

	// Validate and set destination secret name
	if destSecret == "" {
		destSecret = sourceSecret
		utils.VerboseLog(cmd, "Destination secret not specified, using source name: %s", destSecret)
	}

	// Print configuration summary
	fmt.Printf("🔐 Secret Copy Configuration:\n")
	fmt.Printf("   Source Secret: %s\n", sourceSecret)
	fmt.Printf("   Destination Secret: %s\n", destSecret)
	if destRegion != "" {
		fmt.Printf("   Destination Region: %s\n", destRegion)
	}
	if destAssumeRoleArn != "" {
		fmt.Printf("   Destination Assume Role: %s\n", destAssumeRoleArn)
		fmt.Printf("   Note: Cross-account copy enabled\n")
	}
	fmt.Printf("   Source Region: %s\n", awsConfig.Region)
	if awsConfig.Profile != "" {
		fmt.Printf("   AWS Profile: %s\n", awsConfig.Profile)
	}
	if awsConfig.AssumeRoleArn != "" {
		fmt.Printf("   Assume Role: %s\n", awsConfig.AssumeRoleArn)
	}
	if dryRun {
		fmt.Printf("   🧪 DRY-RUN MODE: No changes will be made\n")
	}
	fmt.Println()

	// Perform secret copying
	err = copySecret(ctx, awsConfig, sourceSecret, destSecret, destAssumeRoleArn, destRegion, dryRun, cmd)
	if err != nil {
		utils.VerboseLog(cmd, "Secret copy failed: %v", err)
		return fmt.Errorf("failed to copy secret: %w", err)
	}

	// Print summary
	fmt.Printf("\n✅ Secret Copy Summary:\n")
	if dryRun {
		fmt.Printf("   🧪 DRY-RUN: Validation completed successfully\n")
	} else {
		fmt.Printf("   Secret copied successfully from %s to %s\n", sourceSecret, destSecret)
		if destAssumeRoleArn != "" {
			fmt.Printf("   Cross-account copy completed using role %s\n", destAssumeRoleArn)
		}
	}

	utils.VerboseLog(cmd, "Copysecret command execution completed successfully")
	return nil
}

// copySecret handles the core logic for copying a secret
func copySecret(ctx context.Context, awsConfig awsclient.Config, sourceSecret, destSecret, destAssumeRoleArn, destRegion string, dryRun bool, cmd *cobra.Command) error {
	utils.VerboseLog(cmd, "Starting secret copy process: %s -> %s", sourceSecret, destSecret)

	// Create source AWS client
	utils.VerboseLog(cmd, "Creating source AWS client...")
	sourceClient, err := awsclient.NewClient(ctx, awsConfig)
	if err != nil {
		utils.VerboseLog(cmd, "Failed to create source AWS client: %v", err)
		return fmt.Errorf("failed to create source AWS client: %w", err)
	}
	utils.VerboseLog(cmd, "Source AWS client created successfully")

	// Get account ID for detailed logging
	sourceAccountId, err := sourceClient.GetAccountId(ctx)
	if err != nil {
		utils.VerboseLog(cmd, "Failed to get source account ID: %v", err)
		// Don't fail on this, just log
		sourceAccountId = "unknown"
	}
	utils.VerboseLog(cmd, "Source account ID: %s", sourceAccountId)

	// Create Secrets Manager client for source
	sourceSMClient := secretsmanager.NewFromConfig(sourceClient.Config)
	utils.VerboseLog(cmd, "Created source Secrets Manager client")

	// Describe source secret to get metadata
	utils.VerboseLog(cmd, "Describing source secret: %s", sourceSecret)
	describeInput := &secretsmanager.DescribeSecretInput{
		SecretId: aws.String(sourceSecret),
	}

	describeResult, err := sourceSMClient.DescribeSecret(ctx, describeInput)
	if err != nil {
		utils.VerboseLog(cmd, "Failed to describe source secret: %v", err)
		return fmt.Errorf("failed to describe source secret '%s': %w", sourceSecret, err)
	}

	utils.VerboseLog(cmd, "Source secret description retrieved successfully")
	utils.DebugLog(cmd, "Source Secret Details",
		fmt.Sprintf("Name: %s\nARN: %s\nDescription: %s\nKMS Key: %s\nCreated: %v\nLast Changed: %v\nTags: %v",
			describeResult.Name,
			describeResult.ARN,
			describeResult.Description,
			describeResult.KmsKeyId,
			describeResult.CreatedDate,
			describeResult.LastChangedDate,
			describeResult.Tags))

	// Get source secret value
	utils.VerboseLog(cmd, "Retrieving source secret value")
	getValueInput := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(sourceSecret),
	}

	getValueResult, err := sourceSMClient.GetSecretValue(ctx, getValueInput)
	if err != nil {
		utils.VerboseLog(cmd, "Failed to get source secret value: %v", err)
		return fmt.Errorf("failed to get source secret value '%s': %w", sourceSecret, err)
	}

	utils.VerboseLog(cmd, "Source secret value retrieved successfully")
	secretValue := *getValueResult.SecretString
	utils.VerboseLog(cmd, "Secret value length: %d characters", len(secretValue))
	utils.DebugLog(cmd, "Secret Preview", fmt.Sprintf("Value starts with: %s...", secretValue[:min(10, len(secretValue))]))

	// Handle destination
	var destClient *awsclient.Client
	var destAccountId string

	if destAssumeRoleArn != "" || destRegion != "" {
		// Cross-account or cross-region copy - create destination client
		var clientType string
		var destConfig awsclient.Config

		if destAssumeRoleArn != "" {
			utils.VerboseLog(cmd, "Cross-account copy: creating destination client with assume role: %s", destAssumeRoleArn)
			// Build destination config with assume role
			destConfig = awsConfig
			destConfig.AssumeRoleArn = destAssumeRoleArn
			// Use specified destination region if provided
			if destRegion != "" {
				destConfig.Region = destRegion
			}
			// Reuse session name and external ID from parent config
			if awsConfig.AssumeRoleSessionName != "" {
				destConfig.AssumeRoleSessionName = awsConfig.AssumeRoleSessionName
			}
			if awsConfig.AssumeRoleExternalId != "" {
				destConfig.AssumeRoleExternalId = awsConfig.AssumeRoleExternalId
			}
			clientType = "cross-account"
		} else if destRegion != "" {
			utils.VerboseLog(cmd, "Cross-region copy: creating destination client for region: %s", destRegion)
			// Build destination config with different region
			destConfig = awsConfig
			destConfig.Region = destRegion
			clientType = "cross-region"
		}

		utils.VerboseLog(cmd, "Creating destination AWS client...")
		destClient, err = awsclient.NewClient(ctx, destConfig)
		if err != nil {
			utils.VerboseLog(cmd, "Failed to create destination AWS client: %v", err)
			return fmt.Errorf("failed to create destination AWS client: %w", err)
		}

		destAccountId, err = destClient.GetAccountId(ctx)
		if err != nil {
			utils.VerboseLog(cmd, "Failed to get destination account ID: %v", err)
			destAccountId = "unknown"
		}

		if clientType == "cross-account" {
			fmt.Printf("🎯 Cross-account copy initiated\n")
			fmt.Printf("   Source Account: %s\n", sourceAccountId)
			fmt.Printf("   Destination Account: %s\n", destAccountId)
			if destRegion != "" {
				fmt.Printf("   Destination Region: %s\n", destRegion)
			}
		} else if clientType == "cross-region" {
			fmt.Printf("🎯 Cross-region copy initiated\n")
			fmt.Printf("   Account: %s\n", sourceAccountId)
			fmt.Printf("   Source Region: %s\n", awsConfig.Region)
			fmt.Printf("   Destination Region: %s\n", destRegion)
		}
		utils.VerboseLog(cmd, "Destination account ID: %s", destAccountId)

	} else {
		// Same account, same region copy
		utils.VerboseLog(cmd, "Same-account, same-region copy: using source client for destination")
		destClient = sourceClient
		destAccountId = sourceAccountId

		fmt.Printf("🎯 Same account/region copy initiated\n")
		fmt.Printf("   Account: %s\n", sourceAccountId)
		fmt.Printf("   Region: %s\n", awsConfig.Region)
	}

	// Create destination Secrets Manager client
	destSMClient := secretsmanager.NewFromConfig(destClient.Config)
	utils.VerboseLog(cmd, "Created destination Secrets Manager client")

	// Check if destination secret already exists
	utils.VerboseLog(cmd, "Checking if destination secret already exists: %s", destSecret)
	_, err = destSMClient.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
		SecretId: aws.String(destSecret),
	})

	secretExists := err == nil
	utils.VerboseLog(cmd, "Destination secret exists: %t", secretExists)

	if secretExists {
		fmt.Printf("   ⚠️  Destination secret '%s' already exists, will create new version\n", destSecret)
		utils.VerboseLog(cmd, "Destination secret already exists, will create new version")
	} else {
		fmt.Printf("   ✅ Destination secret '%s' does not exist, will create it\n", destSecret)
		utils.VerboseLog(cmd, "Destination secret does not exist, will create new secret")
	}

	// Prepare create/update operation
	var operation string
	if secretExists {
		operation = "add new version to"
	} else {
		operation = "create"
	}

	if dryRun {
		fmt.Printf("🧪 DRY-RUN: Would %s destination secret '%s'\n", operation, destSecret)
		utils.VerboseLog(cmd, "DRY-RUN: validated secret copy operation would succeed")
		fmt.Printf("   Secret value length: %d characters\n", len(secretValue))
		if describeResult.Description != nil {
			fmt.Printf("   Description: %s\n", *describeResult.Description)
		}
		if describeResult.KmsKeyId != nil {
			fmt.Printf("   KMS Key: %s\n", *describeResult.KmsKeyId)
		}
		if len(describeResult.Tags) > 0 {
			fmt.Printf("   Tags: %d tag(s)\n", len(describeResult.Tags))
		}
		return nil
	}

	// Perform the actual copy operation
	if secretExists {
		// Create new version of existing secret
		utils.VerboseLog(cmd, "Creating new version of existing destination secret: %s", destSecret)
		putValueInput := &secretsmanager.PutSecretValueInput{
			SecretId:     aws.String(destSecret),
			SecretString: aws.String(secretValue),
		}

		_, err = destSMClient.PutSecretValue(ctx, putValueInput)
		if err != nil {
			utils.VerboseLog(cmd, "Failed to create new version of destination secret: %v", err)
			return fmt.Errorf("failed to create new version of destination secret '%s': %w", destSecret, err)
		}

		utils.VerboseLog(cmd, "New version of destination secret created successfully")

	} else {
		// Create new secret
		utils.VerboseLog(cmd, "Creating new destination secret: %s", destSecret)
		createInput := &secretsmanager.CreateSecretInput{
			Name:         aws.String(destSecret),
			SecretString: aws.String(secretValue),
		}

		if describeResult.Description != nil {
			createInput.Description = describeResult.Description
		}

		if describeResult.KmsKeyId != nil {
			createInput.KmsKeyId = describeResult.KmsKeyId
		}

		// Copy tags if they exist
		if len(describeResult.Tags) > 0 {
			tags := make([]types.Tag, len(describeResult.Tags))
			for i, tag := range describeResult.Tags {
				tags[i] = types.Tag{
					Key:   tag.Key,
					Value: tag.Value,
				}
			}
			createInput.Tags = tags
		}

		_, err = destSMClient.CreateSecret(ctx, createInput)
		if err != nil {
			utils.VerboseLog(cmd, "Failed to create destination secret: %v", err)
			return fmt.Errorf("failed to create destination secret '%s': %w", destSecret, err)
		}

		utils.VerboseLog(cmd, "Destination secret created successfully")
	}

	fmt.Printf("   ✅ Secret successfully %sd: %s\n", operation, destSecret)
	utils.VerboseLog(cmd, "Secret copy operation completed successfully")
	return nil
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
