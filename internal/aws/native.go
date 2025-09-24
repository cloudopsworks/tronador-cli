// internal/aws/native.go
package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/backup"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// NativeResourceDiscovery provides native service discovery as fallback for RGTA
type NativeResourceDiscovery struct {
	client    *Client
	accountID string
	region    string
	partition string
}

// NewNativeResourceDiscovery creates a new native resource discovery instance
func NewNativeResourceDiscovery(client *Client) *NativeResourceDiscovery {
	return &NativeResourceDiscovery{
		client:    client,
		partition: "aws", // Default, will be resolved later
	}
}

// Initialize resolves account ID, region, and partition information
func (nrd *NativeResourceDiscovery) Initialize(ctx context.Context) error {
	// Get account ID
	accountID, err := nrd.client.GetAccountId(ctx)
	if err != nil {
		return fmt.Errorf("failed to get account ID: %w", err)
	}
	nrd.accountID = accountID

	// Get effective region
	nrd.region = nrd.client.GetEffectiveRegion()

	// Get partition from caller ARN
	callerArn, err := nrd.client.GetCallerArn(ctx)
	if err == nil && callerArn != "" {
		// Parse partition from ARN: arn:partition:service:region:account:resourcetype/resource
		parts := strings.Split(callerArn, ":")
		if len(parts) >= 2 {
			nrd.partition = parts[1]
		}
	}

	return nil
}

// buildEC2Arn constructs an EC2 resource ARN
func (nrd *NativeResourceDiscovery) buildEC2Arn(resourceType, resourceID string) string {
	return fmt.Sprintf("arn:%s:ec2:%s:%s:%s/%s",
		nrd.partition, nrd.region, nrd.accountID, resourceType, resourceID)
}

// DiscoverResourcesByType discovers resources of a specific type using native service APIs
func (nrd *NativeResourceDiscovery) DiscoverResourcesByType(ctx context.Context, resourceType string) ([]string, error) {
	switch resourceType {
	case "ec2:instance":
		return nrd.discoverEC2Instances(ctx)
	case "ec2:security-group":
		return nrd.discoverSecurityGroups(ctx)
	case "ec2:vpc":
		return nrd.discoverVPCs(ctx)
	case "ec2:subnet":
		return nrd.discoverSubnets(ctx)
	case "ec2:dhcp-options":
		return nrd.discoverDHCPOptions(ctx)
	case "ec2:route-table":
		return nrd.discoverRouteTables(ctx)
	case "ec2:internet-gateway":
		return nrd.discoverInternetGateways(ctx)
	case "ec2:network-acl":
		return nrd.discoverNetworkACLs(ctx)
	case "ec2:network-interface":
		return nrd.discoverNetworkInterfaces(ctx)
	case "ec2:elastic-ip":
		return nrd.discoverElasticIPs(ctx)
	case "autoscaling:autoScalingGroup":
		return nrd.discoverAutoScalingGroups(ctx)
	case "s3:bucket":
		return nrd.discoverS3Buckets(ctx)
	case "secretsmanager:secret":
		return nrd.discoverSecretsManagerSecrets(ctx)
	case "sns:topic":
		return nrd.discoverSNSTopics(ctx)
	case "sqs:queue":
		return nrd.discoverSQSQueues(ctx)
	case "acm:certificate":
		return nrd.discoverACMCertificates(ctx)
	case "kms:key":
		return nrd.discoverKMSKeys(ctx)
	case "backup:backup-vault":
		return nrd.discoverBackupVaults(ctx)
	case "backup:backup-plan":
		return nrd.discoverBackupPlans(ctx)
	case "backup:framework":
		return nrd.discoverBackupFrameworks(ctx)
	case "backup:report-plan":
		return nrd.discoverBackupReportPlans(ctx)
	case "backup:recovery-point":
		return nrd.discoverRecoveryPoints(ctx)
	default:
		// Unsupported resource type for native discovery
		return nil, nil
	}
}

// EC2 resource discovery methods
func (nrd *NativeResourceDiscovery) discoverEC2Instances(ctx context.Context) ([]string, error) {
	client := ec2.NewFromConfig(nrd.client.Config)
	result, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to describe instances: %w", err)
	}

	var arns []string
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			if instance.InstanceId != nil {
				arn := nrd.buildEC2Arn("instance", *instance.InstanceId)
				arns = append(arns, arn)
			}
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverSecurityGroups(ctx context.Context) ([]string, error) {
	client := ec2.NewFromConfig(nrd.client.Config)
	result, err := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to describe security groups: %w", err)
	}

	var arns []string
	for _, sg := range result.SecurityGroups {
		if sg.GroupId != nil {
			arn := nrd.buildEC2Arn("security-group", *sg.GroupId)
			arns = append(arns, arn)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverVPCs(ctx context.Context) ([]string, error) {
	client := ec2.NewFromConfig(nrd.client.Config)
	result, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to describe VPCs: %w", err)
	}

	var arns []string
	for _, vpc := range result.Vpcs {
		if vpc.VpcId != nil {
			arn := nrd.buildEC2Arn("vpc", *vpc.VpcId)
			arns = append(arns, arn)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverSubnets(ctx context.Context) ([]string, error) {
	client := ec2.NewFromConfig(nrd.client.Config)
	result, err := client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to describe subnets: %w", err)
	}

	var arns []string
	for _, subnet := range result.Subnets {
		if subnet.SubnetId != nil {
			arn := nrd.buildEC2Arn("subnet", *subnet.SubnetId)
			arns = append(arns, arn)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverDHCPOptions(ctx context.Context) ([]string, error) {
	client := ec2.NewFromConfig(nrd.client.Config)
	result, err := client.DescribeDhcpOptions(ctx, &ec2.DescribeDhcpOptionsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to describe DHCP options: %w", err)
	}

	var arns []string
	for _, dhcp := range result.DhcpOptions {
		if dhcp.DhcpOptionsId != nil {
			arn := nrd.buildEC2Arn("dhcp-options", *dhcp.DhcpOptionsId)
			arns = append(arns, arn)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverRouteTables(ctx context.Context) ([]string, error) {
	client := ec2.NewFromConfig(nrd.client.Config)
	result, err := client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to describe route tables: %w", err)
	}

	var arns []string
	for _, rt := range result.RouteTables {
		if rt.RouteTableId != nil {
			arn := nrd.buildEC2Arn("route-table", *rt.RouteTableId)
			arns = append(arns, arn)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverInternetGateways(ctx context.Context) ([]string, error) {
	client := ec2.NewFromConfig(nrd.client.Config)
	result, err := client.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to describe internet gateways: %w", err)
	}

	var arns []string
	for _, igw := range result.InternetGateways {
		if igw.InternetGatewayId != nil {
			arn := nrd.buildEC2Arn("internet-gateway", *igw.InternetGatewayId)
			arns = append(arns, arn)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverNetworkACLs(ctx context.Context) ([]string, error) {
	client := ec2.NewFromConfig(nrd.client.Config)
	result, err := client.DescribeNetworkAcls(ctx, &ec2.DescribeNetworkAclsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to describe network ACLs: %w", err)
	}

	var arns []string
	for _, nacl := range result.NetworkAcls {
		if nacl.NetworkAclId != nil {
			arn := nrd.buildEC2Arn("network-acl", *nacl.NetworkAclId)
			arns = append(arns, arn)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverNetworkInterfaces(ctx context.Context) ([]string, error) {
	client := ec2.NewFromConfig(nrd.client.Config)
	result, err := client.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to describe network interfaces: %w", err)
	}

	var arns []string
	for _, eni := range result.NetworkInterfaces {
		if eni.NetworkInterfaceId != nil {
			arn := nrd.buildEC2Arn("network-interface", *eni.NetworkInterfaceId)
			arns = append(arns, arn)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverElasticIPs(ctx context.Context) ([]string, error) {
	client := ec2.NewFromConfig(nrd.client.Config)
	result, err := client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to describe elastic IPs: %w", err)
	}

	var arns []string
	for _, addr := range result.Addresses {
		// Only VPC EIPs with AllocationId produce stable ARNs
		if addr.AllocationId != nil {
			arn := nrd.buildEC2Arn("elastic-ip", *addr.AllocationId)
			arns = append(arns, arn)
		}
	}
	return arns, nil
}

// Other AWS services discovery methods
func (nrd *NativeResourceDiscovery) discoverAutoScalingGroups(ctx context.Context) ([]string, error) {
	client := autoscaling.NewFromConfig(nrd.client.Config)
	result, err := client.DescribeAutoScalingGroups(ctx, &autoscaling.DescribeAutoScalingGroupsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to describe auto scaling groups: %w", err)
	}

	var arns []string
	for _, asg := range result.AutoScalingGroups {
		if asg.AutoScalingGroupARN != nil {
			arns = append(arns, *asg.AutoScalingGroupARN)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverS3Buckets(ctx context.Context) ([]string, error) {
	client := s3.NewFromConfig(nrd.client.Config)
	result, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list S3 buckets: %w", err)
	}

	var arns []string
	for _, bucket := range result.Buckets {
		if bucket.Name != nil {
			arn := fmt.Sprintf("arn:%s:s3:::%s", nrd.partition, *bucket.Name)
			arns = append(arns, arn)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverSecretsManagerSecrets(ctx context.Context) ([]string, error) {
	client := secretsmanager.NewFromConfig(nrd.client.Config)
	result, err := client.ListSecrets(ctx, &secretsmanager.ListSecretsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	var arns []string
	for _, secret := range result.SecretList {
		if secret.ARN != nil {
			arns = append(arns, *secret.ARN)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverSNSTopics(ctx context.Context) ([]string, error) {
	client := sns.NewFromConfig(nrd.client.Config)
	result, err := client.ListTopics(ctx, &sns.ListTopicsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list SNS topics: %w", err)
	}

	var arns []string
	for _, topic := range result.Topics {
		if topic.TopicArn != nil {
			arns = append(arns, *topic.TopicArn)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverSQSQueues(ctx context.Context) ([]string, error) {
	client := sqs.NewFromConfig(nrd.client.Config)
	result, err := client.ListQueues(ctx, &sqs.ListQueuesInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list SQS queues: %w", err)
	}

	var arns []string
	for _, queueURL := range result.QueueUrls {
		// Get queue ARN from attributes
		attrs, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
			QueueUrl:       aws.String(queueURL),
			AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
		})
		if err != nil {
			continue // Skip this queue if we can't get its ARN
		}
		if queueArn, exists := attrs.Attributes["QueueArn"]; exists {
			arns = append(arns, queueArn)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverACMCertificates(ctx context.Context) ([]string, error) {
	client := acm.NewFromConfig(nrd.client.Config)
	result, err := client.ListCertificates(ctx, &acm.ListCertificatesInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list ACM certificates: %w", err)
	}

	var arns []string
	for _, cert := range result.CertificateSummaryList {
		if cert.CertificateArn != nil {
			arns = append(arns, *cert.CertificateArn)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverKMSKeys(ctx context.Context) ([]string, error) {
	client := kms.NewFromConfig(nrd.client.Config)
	result, err := client.ListKeys(ctx, &kms.ListKeysInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list KMS keys: %w", err)
	}

	var arns []string
	for _, key := range result.Keys {
		if key.KeyId != nil {
			// Get key ARN using DescribeKey
			desc, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{
				KeyId: key.KeyId,
			})
			if err != nil {
				continue // Skip this key if we can't describe it
			}
			if desc.KeyMetadata != nil && desc.KeyMetadata.Arn != nil {
				arns = append(arns, *desc.KeyMetadata.Arn)
			}
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverBackupVaults(ctx context.Context) ([]string, error) {
	client := backup.NewFromConfig(nrd.client.Config)
	result, err := client.ListBackupVaults(ctx, &backup.ListBackupVaultsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list backup vaults: %w", err)
	}

	var arns []string
	for _, vault := range result.BackupVaultList {
		if vault.BackupVaultArn != nil {
			arns = append(arns, *vault.BackupVaultArn)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverBackupPlans(ctx context.Context) ([]string, error) {
	client := backup.NewFromConfig(nrd.client.Config)
	result, err := client.ListBackupPlans(ctx, &backup.ListBackupPlansInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list backup plans: %w", err)
	}

	var arns []string
	for _, plan := range result.BackupPlansList {
		if plan.BackupPlanArn != nil {
			arns = append(arns, *plan.BackupPlanArn)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverBackupFrameworks(ctx context.Context) ([]string, error) {
	client := backup.NewFromConfig(nrd.client.Config)
	result, err := client.ListFrameworks(ctx, &backup.ListFrameworksInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list backup frameworks: %w", err)
	}

	var arns []string
	for _, framework := range result.Frameworks {
		if framework.FrameworkArn != nil {
			arns = append(arns, *framework.FrameworkArn)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverBackupReportPlans(ctx context.Context) ([]string, error) {
	client := backup.NewFromConfig(nrd.client.Config)
	result, err := client.ListReportPlans(ctx, &backup.ListReportPlansInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list backup report plans: %w", err)
	}

	var arns []string
	for _, plan := range result.ReportPlans {
		if plan.ReportPlanArn != nil {
			arns = append(arns, *plan.ReportPlanArn)
		}
	}
	return arns, nil
}

func (nrd *NativeResourceDiscovery) discoverRecoveryPoints(ctx context.Context) ([]string, error) {
	client := backup.NewFromConfig(nrd.client.Config)

	// First get all backup vaults
	vaults, err := client.ListBackupVaults(ctx, &backup.ListBackupVaultsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list backup vaults for recovery points: %w", err)
	}

	var arns []string
	for _, vault := range vaults.BackupVaultList {
		if vault.BackupVaultName != nil {
			// List recovery points for each vault
			result, err := client.ListRecoveryPointsByBackupVault(ctx, &backup.ListRecoveryPointsByBackupVaultInput{
				BackupVaultName: vault.BackupVaultName,
			})
			if err != nil {
				continue // Skip this vault if we can't list its recovery points
			}

			for _, rp := range result.RecoveryPoints {
				if rp.RecoveryPointArn != nil {
					arns = append(arns, *rp.RecoveryPointArn)
				}
			}
		}
	}
	return arns, nil
}
