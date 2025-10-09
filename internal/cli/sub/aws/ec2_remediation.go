// internal/cli/sub/aws/ec2_remediation.go
package aws

import (
	"context"
	"fmt"

	awsclient "tronador-cli/internal/aws"
	"tronador-cli/internal/utils"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/spf13/cobra"
)

// EC2RemediationCmd represents the ec2 remediation command
var EC2RemediationCmd = &cobra.Command{
	Use:   "ec2",
	Short: "Remediate EC2 security controls (implements EC2-2: Default security groups restrictions)",
	Long: `Remediate EC2 security controls by implementing restrictions on default security groups.

This command implements AWS Security Hub control EC2-2 by:
- Discovering all default security groups across all VPCs in the current region
- Checking for unrestricted inbound and outbound rules (0.0.0.0/0, ::/0)
- Removing unrestricted rules from default security groups
- Ensuring default security groups follow the principle of least privilege

Default security groups should not have inbound or outbound rules that allow
unrestricted access (0.0.0.0/0 for IPv4 or ::/0 for IPv6) on any port.

Use --dry-run to see what would be changed without making modifications.`,
	RunE: runEC2RemediationCommand,
}

// runEC2RemediationCommand executes the EC2 remediation command
func runEC2RemediationCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Log remediation start
	LogRemediationStart(cmd, "EC2")

	// Build common remediation configuration
	config, err := BuildRemediationConfig(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to build remediation configuration: %w", err)
	}

	// Get current region
	currentRegion := GetCurrentRegion(config.Client, "us-east-1")

	// Print configuration summary (custom format for EC2 to show region)
	fmt.Printf("🛡️  EC2 Security Remediation Configuration:\n")
	if config.AWSConfig.Profile != "" {
		fmt.Printf("   AWS Profile: %s\n", config.AWSConfig.Profile)
	}
	if config.AWSConfig.AssumeRoleArn != "" {
		fmt.Printf("   Assume Role: %s\n", config.AWSConfig.AssumeRoleArn)
	}
	fmt.Printf("   Region: %s\n", currentRegion)
	fmt.Printf("   Control: EC2-2 (Default security groups restrictions)\n")
	if config.DryRun {
		fmt.Printf("   🧪 DRY-RUN MODE: No changes will be made\n")
	}
	fmt.Println()

	// Perform EC2 security remediation
	totalProcessed, totalSkipped, totalFailed, err := remediateEC2SecurityControls(ctx, config.Client, config.DryRun, cmd)
	if err != nil {
		utils.VerboseLog(cmd, "EC2 security remediation failed: %v", err)
		return fmt.Errorf("failed to remediate EC2 security controls: %w", err)
	}

	// Print summary
	result := &RemediationResult{
		Processed: totalProcessed,
		Skipped:   totalSkipped,
		Failed:    totalFailed,
	}
	PrintRemediationSummary(result, "Security groups", cmd)

	// Log completion
	LogRemediationComplete(cmd, "EC2")
	return nil
}

// remediateEC2SecurityControls handles the core logic for implementing EC2 security controls
func remediateEC2SecurityControls(ctx context.Context, awsClient *awsclient.Client, dryRun bool, cmd *cobra.Command) (int, int, int, error) {
	fmt.Println("🔍 Discovering default security groups...")
	utils.VerboseLog(cmd, "Starting EC2 security controls remediation")

	// Get current region
	currentRegion := awsClient.GetEffectiveRegion()
	if currentRegion == "" {
		currentRegion = "us-east-1"
	}

	fmt.Printf("Processing region: %s\n", currentRegion)
	utils.VerboseLog(cmd, "Current region: %s", currentRegion)

	// Create EC2 client
	ec2Client := ec2.NewFromConfig(awsClient.Config)

	// Find all default security groups in the region
	sgResult, err := ec2Client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []types.Filter{
			{
				Name:   awssdk.String("group-name"),
				Values: []string{"default"},
			},
		},
	})
	if err != nil {
		utils.VerboseLog(cmd, "Failed to describe security groups: %v", err)
		return 0, 0, 0, fmt.Errorf("failed to describe security groups: %w", err)
	}

	if len(sgResult.SecurityGroups) == 0 {
		fmt.Println("No default security groups found in region")
		utils.VerboseLog(cmd, "No default security groups found")
		return 0, 0, 0, nil
	}

	fmt.Printf("Found %d default security groups to process\n", len(sgResult.SecurityGroups))
	utils.VerboseLog(cmd, "Found %d default security groups", len(sgResult.SecurityGroups))

	var totalProcessed, totalSkipped, totalFailed int

	// Process each default security group
	for i, sg := range sgResult.SecurityGroups {
		if sg.GroupId == nil || sg.VpcId == nil {
			continue
		}

		sgId := *sg.GroupId
		vpcId := *sg.VpcId
		fmt.Printf("🛡️  Processing security group %d/%d: %s (VPC: %s)\n", i+1, len(sgResult.SecurityGroups), sgId, vpcId)
		utils.VerboseLog(cmd, "Processing security group: %s in VPC: %s", sgId, vpcId)

		// Check if security group has unrestricted rules
		hasUnrestrictedRules, unrestrictedInbound, unrestrictedOutbound, err := checkUnrestrictedRules(ctx, ec2Client, &sg, cmd)
		if err != nil {
			utils.VerboseLog(cmd, "Failed to check unrestricted rules for security group %s: %v", sgId, err)
			fmt.Printf("  ❌ Error checking security group rules: %v\n", err)
			totalFailed++
			continue
		}

		if !hasUnrestrictedRules {
			fmt.Printf("  ✅ Security group already compliant, skipping\n")
			utils.VerboseLog(cmd, "Security group %s is already compliant", sgId)
			totalSkipped++
			continue
		}

		if dryRun {
			fmt.Printf("  🧪 DRY-RUN: Would remove %d unrestricted inbound and %d unrestricted outbound rules\n",
				len(unrestrictedInbound), len(unrestrictedOutbound))
			totalProcessed++
			continue
		}

		// Remove unrestricted rules
		err = removeUnrestrictedRules(ctx, ec2Client, sgId, unrestrictedInbound, unrestrictedOutbound, cmd)
		if err != nil {
			utils.VerboseLog(cmd, "Failed to remove unrestricted rules for security group %s: %v", sgId, err)
			fmt.Printf("  ❌ Failed to remove unrestricted rules: %v\n", err)
			totalFailed++
			continue
		}

		fmt.Printf("  ✅ Successfully removed unrestricted rules\n")
		utils.VerboseLog(cmd, "Successfully removed unrestricted rules for security group %s", sgId)
		totalProcessed++
	}

	utils.VerboseLog(cmd, "EC2 security controls remediation completed: %d processed, %d skipped, %d failed", totalProcessed, totalSkipped, totalFailed)
	return totalProcessed, totalSkipped, totalFailed, nil
}

// checkUnrestrictedRules checks if a security group has unrestricted inbound or outbound rules
func checkUnrestrictedRules(ctx context.Context, ec2Client *ec2.Client, sg *types.SecurityGroup, cmd *cobra.Command) (bool, []types.IpPermission, []types.IpPermission, error) {
	if sg.GroupId == nil {
		return false, nil, nil, fmt.Errorf("security group ID is nil")
	}

	sgId := *sg.GroupId
	utils.VerboseLog(cmd, "Checking unrestricted rules for security group %s", sgId)

	var unrestrictedInbound []types.IpPermission
	var unrestrictedOutbound []types.IpPermission

	// Check inbound rules
	for _, rule := range sg.IpPermissions {
		if isUnrestrictedRule(rule) {
			utils.VerboseLog(cmd, "Found unrestricted inbound rule in security group %s", sgId)
			unrestrictedInbound = append(unrestrictedInbound, rule)
		}
	}

	// Check outbound rules
	for _, rule := range sg.IpPermissionsEgress {
		if isUnrestrictedRule(rule) {
			utils.VerboseLog(cmd, "Found unrestricted outbound rule in security group %s", sgId)
			unrestrictedOutbound = append(unrestrictedOutbound, rule)
		}
	}

	hasUnrestricted := len(unrestrictedInbound) > 0 || len(unrestrictedOutbound) > 0
	utils.VerboseLog(cmd, "Security group %s has %d unrestricted inbound and %d unrestricted outbound rules",
		sgId, len(unrestrictedInbound), len(unrestrictedOutbound))

	return hasUnrestricted, unrestrictedInbound, unrestrictedOutbound, nil
}

// isUnrestrictedRule checks if a rule allows unrestricted access (0.0.0.0/0 or ::/0)
func isUnrestrictedRule(rule types.IpPermission) bool {
	// Check IPv4 ranges
	for _, ipRange := range rule.IpRanges {
		if ipRange.CidrIp != nil && (*ipRange.CidrIp == "0.0.0.0/0") {
			return true
		}
	}

	// Check IPv6 ranges
	for _, ipv6Range := range rule.Ipv6Ranges {
		if ipv6Range.CidrIpv6 != nil && (*ipv6Range.CidrIpv6 == "::/0") {
			return true
		}
	}

	return false
}

// removeUnrestrictedRules removes unrestricted rules from a security group
func removeUnrestrictedRules(ctx context.Context, ec2Client *ec2.Client, sgId string, unrestrictedInbound, unrestrictedOutbound []types.IpPermission, cmd *cobra.Command) error {
	utils.VerboseLog(cmd, "Removing unrestricted rules from security group %s", sgId)

	// Remove unrestricted inbound rules
	if len(unrestrictedInbound) > 0 {
		utils.VerboseLog(cmd, "Revoking %d unrestricted inbound rules from security group %s", len(unrestrictedInbound), sgId)
		_, err := ec2Client.RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
			GroupId:       awssdk.String(sgId),
			IpPermissions: unrestrictedInbound,
		})
		if err != nil {
			return fmt.Errorf("failed to revoke unrestricted inbound rules: %w", err)
		}
		fmt.Printf("    🔓 Removed %d unrestricted inbound rules\n", len(unrestrictedInbound))
	}

	// Remove unrestricted outbound rules
	if len(unrestrictedOutbound) > 0 {
		utils.VerboseLog(cmd, "Revoking %d unrestricted outbound rules from security group %s", len(unrestrictedOutbound), sgId)
		_, err := ec2Client.RevokeSecurityGroupEgress(ctx, &ec2.RevokeSecurityGroupEgressInput{
			GroupId:       awssdk.String(sgId),
			IpPermissions: unrestrictedOutbound,
		})
		if err != nil {
			return fmt.Errorf("failed to revoke unrestricted outbound rules: %w", err)
		}
		fmt.Printf("    🔒 Removed %d unrestricted outbound rules\n", len(unrestrictedOutbound))
	}

	utils.VerboseLog(cmd, "Successfully removed unrestricted rules from security group %s", sgId)
	return nil
}
