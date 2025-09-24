// internal/cli/aws_remove_vpc.go
package cli

import (
	"context"
	"fmt"
	"strings"

	"tronador-cli/internal/aws"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/spf13/cobra"
)

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

// Remove-default-vpc command specific variables
var (
	excludeRegions string // comma-separated list of regions to exclude from VPC removal
)

// initRemoveDefaultVpcCommand initializes the remove-default-vpc command flags
func initRemoveDefaultVpcCommand() {
	// Remove-default-vpc command flags
	removeDefaultVpcCmd.Flags().StringVar(&excludeRegions, "exclude-regions", "", "Comma-separated list of regions to exclude from VPC removal")
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
