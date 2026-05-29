// internal/aws/resources.go
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
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// DefaultResourceTypes defines the resource types supported by default
var DefaultResourceTypes = []string{
	"secretsmanager:secret",
	"ec2:security-group",
	"ec2:instance",
	"ec2:vpc",
	"ec2:subnet",
	"ec2:dhcp-options",
	"ec2:route-table",
	"ec2:internet-gateway",
	"ec2:network-acl",
	"ec2:network-interface",
	"ec2:elastic-ip",
	"autoscaling:autoScalingGroup",
	"s3:bucket",
	"sns:topic",
	"sqs:queue",
	"acm:certificate",
	"kms:key",
	"backup:backup-vault",
	"backup:recovery-point",
	"backup:backup-plan",
	"backup:framework",
	"backup:report-plan",
	"events:event-bus",
	"events:schedule-group",
}

// ResourceTypeAliases maps friendly names to AWS resource types
var ResourceTypeAliases = map[string]string{
	"all":                   "__ALL__",
	"secretsmanager-secret": "secretsmanager:secret",
	"secretsmanager_secret": "secretsmanager:secret",
	"secret":                "secretsmanager:secret",
	"secretsmanager":        "secretsmanager:secret",
	"security-group":        "ec2:security-group",
	"security_group":        "ec2:security-group",
	"sg":                    "ec2:security-group",
	"ec2-instance":          "ec2:instance",
	"ec2_instance":          "ec2:instance",
	"instance":              "ec2:instance",
	"instances":             "ec2:instance",
	"vpc":                   "ec2:vpc",
	"subnet":                "ec2:subnet",
	"subnets":               "ec2:subnet",
	"dhcp-options":          "ec2:dhcp-options",
	"dhcp_options":          "ec2:dhcp-options",
	"route-table":           "ec2:route-table",
	"route_table":           "ec2:route-table",
	"routetable":            "ec2:route-table",
	"route-tables":          "ec2:route-table",
	"internet-gateway":      "ec2:internet-gateway",
	"internet_gateway":      "ec2:internet-gateway",
	"igw":                   "ec2:internet-gateway",
	"network-acl":           "ec2:network-acl",
	"network_acl":           "ec2:network-acl",
	"nacl":                  "ec2:network-acl",
	"nacls":                 "ec2:network-acl",
	"network-interface":     "ec2:network-interface",
	"network_interface":     "ec2:network-interface",
	"eni":                   "ec2:network-interface",
	"enis":                  "ec2:network-interface",
	"elastic-ip":            "ec2:elastic-ip",
	"elastic_ip":            "ec2:elastic-ip",
	"eip":                   "ec2:elastic-ip",
	"eips":                  "ec2:elastic-ip",
	"autoscaling-group":     "autoscaling:autoScalingGroup",
	"autoscaling_group":     "autoscaling:autoScalingGroup",
	"asg":                   "autoscaling:autoScalingGroup",
	"asgs":                  "autoscaling:autoScalingGroup",
	"eventbus":              "events:event-bus",
	"eventbuses":            "events:event-bus",
	"schedulegroup":         "events:schedule-group",
	"schedulegroups":        "events:schedule-group",
	"schedule-group":        "events:schedule-group",
	"schedule-groups":       "events:schedule-group",
	"scheduled-events":      "events:schedule-group",
	"s3-bucket":             "s3:bucket",
	"s3_bucket":             "s3:bucket",
	"bucket":                "s3:bucket",
	"buckets":               "s3:bucket",
	"sns-topic":             "sns:topic",
	"sns_topic":             "sns:topic",
	"sns":                   "sns:topic",
	"sqs-queue":             "sqs:queue",
	"sqs_queue":             "sqs:queue",
	"sqs":                   "sqs:queue",
	"acm-certificate":       "acm:certificate",
	"acm_certificate":       "acm:certificate",
	"kms-key":               "kms:key",
	"kms_key":               "kms:key",
	"kms":                   "kms:key",
	"backup-vault":          "backup:backup-vault",
	"backup_vault":          "backup:backup-vault",
	"backup-backup-vault":   "backup:backup-vault",
	"recovery-point":        "backup:recovery-point",
	"recovery_point":        "backup:recovery-point",
	"backup-plan":           "backup:backup-plan",
	"backup_plan":           "backup:backup-plan",
	"backup-framework":      "backup:framework",
	"backup_framework":      "backup:framework",
	"framework":             "backup:framework",
	"backup-report-plan":    "backup:report-plan",
	"backup_report_plan":    "backup:report-plan",
	"report-plan":           "backup:report-plan",
	"report_plan":           "backup:report-plan",
}

// Resource represents an AWS resource with its ARN and tags
type Resource struct {
	ARN  string
	Tags map[string]string
}

// ResourceTagger handles resource discovery and tagging operations
type ResourceTagger struct {
	client        *Client
	rgta          *resourcegroupstaggingapi.Client
	ec2Client     *ec2.Client
	s3Client      *s3.Client
	snsClient     *sns.Client
	sqsClient     *sqs.Client
	acmClient     *acm.Client
	kmsClient     *kms.Client
	asgClient     *autoscaling.Client
	secretsClient *secretsmanager.Client
	backupClient  *backup.Client
}

// NewResourceTagger creates a new ResourceTagger instance
func NewResourceTagger(client *Client) *ResourceTagger {
	return &ResourceTagger{
		client:        client,
		rgta:          resourcegroupstaggingapi.NewFromConfig(client.Config),
		ec2Client:     ec2.NewFromConfig(client.Config),
		s3Client:      s3.NewFromConfig(client.Config),
		snsClient:     sns.NewFromConfig(client.Config),
		sqsClient:     sqs.NewFromConfig(client.Config),
		acmClient:     acm.NewFromConfig(client.Config),
		kmsClient:     kms.NewFromConfig(client.Config),
		asgClient:     autoscaling.NewFromConfig(client.Config),
		secretsClient: secretsmanager.NewFromConfig(client.Config),
		backupClient:  backup.NewFromConfig(client.Config),
	}
}

// NormalizeResourceTypes converts resource type aliases to canonical AWS resource types
func NormalizeResourceTypes(typesList string) ([]string, error) {
	if typesList == "" {
		return DefaultResourceTypes, nil
	}

	// Handle special "all" case
	if strings.TrimSpace(typesList) == "all" || strings.TrimSpace(typesList) == "ALL" {
		return DefaultResourceTypes, nil
	}

	inputTypes := strings.Split(typesList, ",")
	var resourceTypes []string
	var invalids []string

	for _, rawType := range inputTypes {
		t := strings.TrimSpace(rawType)
		if t == "" {
			continue
		}

		// Check if it's already a canonical type (contains colon)
		if strings.Contains(t, ":") {
			resourceTypes = append(resourceTypes, t)
			continue
		}

		// Try to resolve alias
		if canonical, exists := ResourceTypeAliases[strings.ToLower(t)]; exists {
			if canonical == "__ALL__" {
				return DefaultResourceTypes, nil
			}
			resourceTypes = append(resourceTypes, canonical)
		} else {
			invalids = append(invalids, t)
		}
	}

	if len(invalids) > 0 {
		return nil, fmt.Errorf("unknown resource type(s): %s", strings.Join(invalids, ", "))
	}

	if len(resourceTypes) == 0 {
		return nil, fmt.Errorf("no valid resource types were provided")
	}

	return resourceTypes, nil
}

// discoverEC2Instances enumerates all EC2 instances directly
func (rt *ResourceTagger) discoverEC2Instances(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	// Get account ID
	accountID, err := rt.client.GetAccountId(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get account ID: %w", err)
	}

	// Get all instances
	paginator := ec2.NewDescribeInstancesPaginator(rt.ec2Client, &ec2.DescribeInstancesInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to describe EC2 instances: %w", err)
		}

		for _, reservation := range page.Reservations {
			for _, instance := range reservation.Instances {
				if instance.InstanceId == nil {
					continue
				}

				// Build ARN
				arn := fmt.Sprintf("arn:aws:ec2:%s:%s:instance/%s",
					rt.client.GetEffectiveRegion(),
					accountID,
					*instance.InstanceId)

				resource := Resource{
					ARN:  arn,
					Tags: make(map[string]string),
				}

				// Convert EC2 tags
				for _, tag := range instance.Tags {
					if tag.Key != nil && tag.Value != nil {
						resource.Tags[*tag.Key] = *tag.Value
					}
				}

				resources = append(resources, resource)
			}
		}
	}

	return resources, nil
}

// discoverS3Buckets enumerates all S3 buckets directly
func (rt *ResourceTagger) discoverS3Buckets(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	// List all buckets
	result, err := rt.s3Client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list S3 buckets: %w", err)
	}

	for _, bucket := range result.Buckets {
		if bucket.Name == nil {
			continue
		}

		// Build ARN
		arn := fmt.Sprintf("arn:aws:s3:::%s", *bucket.Name)

		resource := Resource{
			ARN:  arn,
			Tags: make(map[string]string),
		}

		// Get bucket tags
		tagsResult, err := rt.s3Client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
			Bucket: bucket.Name,
		})
		if err == nil && tagsResult.TagSet != nil {
			for _, tag := range tagsResult.TagSet {
				if tag.Key != nil && tag.Value != nil {
					resource.Tags[*tag.Key] = *tag.Value
				}
			}
		}
		// Ignore error if bucket has no tags

		resources = append(resources, resource)
	}

	return resources, nil
}

// discoverEC2SecurityGroups enumerates all EC2 security groups directly
func (rt *ResourceTagger) discoverEC2SecurityGroups(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	// Get account ID
	accountID, err := rt.client.GetAccountId(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get account ID: %w", err)
	}

	// Get all security groups
	paginator := ec2.NewDescribeSecurityGroupsPaginator(rt.ec2Client, &ec2.DescribeSecurityGroupsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to describe security groups: %w", err)
		}

		for _, sg := range page.SecurityGroups {
			if sg.GroupId == nil {
				continue
			}

			// Build ARN
			arn := fmt.Sprintf("arn:aws:ec2:%s:%s:security-group/%s",
				rt.client.GetEffectiveRegion(),
				accountID,
				*sg.GroupId)

			resource := Resource{
				ARN:  arn,
				Tags: make(map[string]string),
			}

			// Convert EC2 tags
			for _, tag := range sg.Tags {
				if tag.Key != nil && tag.Value != nil {
					resource.Tags[*tag.Key] = *tag.Value
				}
			}

			resources = append(resources, resource)
		}
	}

	return resources, nil
}

// discoverSNSTopics enumerates all SNS topics directly
func (rt *ResourceTagger) discoverSNSTopics(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	// List all topics
	paginator := sns.NewListTopicsPaginator(rt.snsClient, &sns.ListTopicsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list SNS topics: %w", err)
		}

		for _, topic := range page.Topics {
			if topic.TopicArn == nil {
				continue
			}

			resource := Resource{
				ARN:  *topic.TopicArn,
				Tags: make(map[string]string),
			}

			// Get topic tags
			tagsResult, err := rt.snsClient.ListTagsForResource(ctx, &sns.ListTagsForResourceInput{
				ResourceArn: topic.TopicArn,
			})
			if err == nil && tagsResult.Tags != nil {
				for _, tag := range tagsResult.Tags {
					if tag.Key != nil && tag.Value != nil {
						resource.Tags[*tag.Key] = *tag.Value
					}
				}
			}
			// Ignore error if topic has no tags

			resources = append(resources, resource)
		}
	}

	return resources, nil
}

// discoverSQSQueues enumerates all SQS queues directly
func (rt *ResourceTagger) discoverSQSQueues(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	// List all queues
	result, err := rt.sqsClient.ListQueues(ctx, &sqs.ListQueuesInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list SQS queues: %w", err)
	}

	for _, queueUrl := range result.QueueUrls {
		// Get queue attributes to get the ARN
		attrsResult, err := rt.sqsClient.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
			QueueUrl:       &queueUrl,
			AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
		})
		if err != nil {
			continue // Skip this queue if we can't get its ARN
		}

		queueArn, exists := attrsResult.Attributes["QueueArn"]
		if !exists {
			continue
		}

		resource := Resource{
			ARN:  queueArn,
			Tags: make(map[string]string),
		}

		// Get queue tags
		tagsResult, err := rt.sqsClient.ListQueueTags(ctx, &sqs.ListQueueTagsInput{
			QueueUrl: &queueUrl,
		})
		if err == nil && tagsResult.Tags != nil {
			for k, v := range tagsResult.Tags {
				resource.Tags[k] = v
			}
		}
		// Ignore error if queue has no tags

		resources = append(resources, resource)
	}

	return resources, nil
}

// discoverACMCertificates enumerates all ACM certificates directly
func (rt *ResourceTagger) discoverACMCertificates(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	// List all certificates
	paginator := acm.NewListCertificatesPaginator(rt.acmClient, &acm.ListCertificatesInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list ACM certificates: %w", err)
		}

		for _, cert := range page.CertificateSummaryList {
			if cert.CertificateArn == nil {
				continue
			}

			resource := Resource{
				ARN:  *cert.CertificateArn,
				Tags: make(map[string]string),
			}

			// Get certificate tags
			tagsResult, err := rt.acmClient.ListTagsForCertificate(ctx, &acm.ListTagsForCertificateInput{
				CertificateArn: cert.CertificateArn,
			})
			if err == nil && tagsResult.Tags != nil {
				for _, tag := range tagsResult.Tags {
					if tag.Key != nil && tag.Value != nil {
						resource.Tags[*tag.Key] = *tag.Value
					}
				}
			}
			// Ignore error if certificate has no tags

			resources = append(resources, resource)
		}
	}

	return resources, nil
}

// discoverKMSKeys enumerates all customer-managed KMS keys directly
func (rt *ResourceTagger) discoverKMSKeys(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	// List all customer-managed keys
	paginator := kms.NewListKeysPaginator(rt.kmsClient, &kms.ListKeysInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list KMS keys: %w", err)
		}

		for _, key := range page.Keys {
			if key.KeyId == nil {
				continue
			}

			// Get key details to filter out AWS managed keys
			keyResult, err := rt.kmsClient.DescribeKey(ctx, &kms.DescribeKeyInput{
				KeyId: key.KeyId,
			})
			if err != nil {
				continue // Skip this key if we can't describe it
			}

			// Skip AWS managed keys
			if keyResult.KeyMetadata != nil && keyResult.KeyMetadata.KeyManager == kmstypes.KeyManagerTypeAws {
				continue
			}

			// Build ARN
			arn := *keyResult.KeyMetadata.Arn

			resource := Resource{
				ARN:  arn,
				Tags: make(map[string]string),
			}

			// Get key tags
			tagsResult, err := rt.kmsClient.ListResourceTags(ctx, &kms.ListResourceTagsInput{
				KeyId: key.KeyId,
			})
			if err == nil && tagsResult.Tags != nil {
				for _, tag := range tagsResult.Tags {
					if tag.TagKey != nil && tag.TagValue != nil {
						resource.Tags[*tag.TagKey] = *tag.TagValue
					}
				}
			}
			// Ignore error if key has no tags

			resources = append(resources, resource)
		}
	}

	return resources, nil
}

// discoverAutoScalingGroups enumerates all Auto Scaling groups directly
func (rt *ResourceTagger) discoverAutoScalingGroups(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	// Get account ID
	accountID, err := rt.client.GetAccountId(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get account ID: %w", err)
	}

	// List all Auto Scaling groups
	paginator := autoscaling.NewDescribeAutoScalingGroupsPaginator(rt.asgClient, &autoscaling.DescribeAutoScalingGroupsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to describe Auto Scaling groups: %w", err)
		}

		for _, asg := range page.AutoScalingGroups {
			if asg.AutoScalingGroupName == nil {
				continue
			}

			// Build ARN
			arn := fmt.Sprintf("arn:aws:autoscaling:%s:%s:autoScalingGroup:*:autoScalingGroupName/%s",
				rt.client.GetEffectiveRegion(),
				accountID,
				*asg.AutoScalingGroupName)

			resource := Resource{
				ARN:  arn,
				Tags: make(map[string]string),
			}

			// Convert ASG tags
			for _, tag := range asg.Tags {
				if tag.Key != nil && tag.Value != nil {
					resource.Tags[*tag.Key] = *tag.Value
				}
			}

			resources = append(resources, resource)
		}
	}

	return resources, nil
}

// discoverSecretsManagerSecrets enumerates all Secrets Manager secrets directly
func (rt *ResourceTagger) discoverSecretsManagerSecrets(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	// List all secrets
	paginator := secretsmanager.NewListSecretsPaginator(rt.secretsClient, &secretsmanager.ListSecretsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list Secrets Manager secrets: %w", err)
		}

		for _, secret := range page.SecretList {
			if secret.ARN == nil {
				continue
			}

			resource := Resource{
				ARN:  *secret.ARN,
				Tags: make(map[string]string),
			}

			// Convert secret tags
			for _, tag := range secret.Tags {
				if tag.Key != nil && tag.Value != nil {
					resource.Tags[*tag.Key] = *tag.Value
				}
			}

			resources = append(resources, resource)
		}
	}

	return resources, nil
}

// discoverBackupVaults enumerates all AWS Backup vaults directly
func (rt *ResourceTagger) discoverBackupVaults(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	// List all backup vaults
	result, err := rt.backupClient.ListBackupVaults(ctx, &backup.ListBackupVaultsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list backup vaults: %w", err)
	}

	for _, vault := range result.BackupVaultList {
		if vault.BackupVaultArn == nil {
			continue
		}

		resource := Resource{
			ARN:  *vault.BackupVaultArn,
			Tags: make(map[string]string),
		}

		// Get vault tags
		tagsResult, err := rt.backupClient.ListTags(ctx, &backup.ListTagsInput{
			ResourceArn: vault.BackupVaultArn,
		})
		if err == nil && tagsResult.Tags != nil {
			for k, v := range tagsResult.Tags {
				resource.Tags[k] = v
			}
		}
		// Ignore error if vault has no tags

		resources = append(resources, resource)
	}

	return resources, nil
}

// discoverAdditionalBackupResources enumerates additional Backup resources directly
func (rt *ResourceTagger) discoverAdditionalBackupResources(ctx context.Context, resourceType string) ([]Resource, error) {
	// For backup resources beyond backup-vault, we rely primarily on the Resource Groups Tagging API
	// as these resources don't have simple direct enumeration APIs or require complex nested queries.
	// The Resource Groups Tagging API will catch these resources if they are tagged.
	// This method exists to maintain consistency but returns empty for direct enumeration.
	return []Resource{}, nil
}

// discoverAdditionalEC2Resources enumerates additional EC2 resources directly
func (rt *ResourceTagger) discoverAdditionalEC2Resources(ctx context.Context, resourceType string) ([]Resource, error) {
	var resources []Resource

	// Get account ID
	accountID, err := rt.client.GetAccountId(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get account ID: %w", err)
	}

	region := rt.client.GetEffectiveRegion()

	switch resourceType {
	case "ec2:vpc":
		// List all VPCs
		paginator := ec2.NewDescribeVpcsPaginator(rt.ec2Client, &ec2.DescribeVpcsInput{})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to describe VPCs: %w", err)
			}
			for _, vpc := range page.Vpcs {
				if vpc.VpcId == nil {
					continue
				}
				arn := fmt.Sprintf("arn:aws:ec2:%s:%s:vpc/%s", region, accountID, *vpc.VpcId)
				resource := Resource{ARN: arn, Tags: make(map[string]string)}
				for _, tag := range vpc.Tags {
					if tag.Key != nil && tag.Value != nil {
						resource.Tags[*tag.Key] = *tag.Value
					}
				}
				resources = append(resources, resource)
			}
		}

	case "ec2:subnet":
		// List all subnets
		paginator := ec2.NewDescribeSubnetsPaginator(rt.ec2Client, &ec2.DescribeSubnetsInput{})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to describe subnets: %w", err)
			}
			for _, subnet := range page.Subnets {
				if subnet.SubnetId == nil {
					continue
				}
				arn := fmt.Sprintf("arn:aws:ec2:%s:%s:subnet/%s", region, accountID, *subnet.SubnetId)
				resource := Resource{ARN: arn, Tags: make(map[string]string)}
				for _, tag := range subnet.Tags {
					if tag.Key != nil && tag.Value != nil {
						resource.Tags[*tag.Key] = *tag.Value
					}
				}
				resources = append(resources, resource)
			}
		}

	case "ec2:internet-gateway":
		// List all internet gateways
		paginator := ec2.NewDescribeInternetGatewaysPaginator(rt.ec2Client, &ec2.DescribeInternetGatewaysInput{})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to describe internet gateways: %w", err)
			}
			for _, igw := range page.InternetGateways {
				if igw.InternetGatewayId == nil {
					continue
				}
				arn := fmt.Sprintf("arn:aws:ec2:%s:%s:internet-gateway/%s", region, accountID, *igw.InternetGatewayId)
				resource := Resource{ARN: arn, Tags: make(map[string]string)}
				for _, tag := range igw.Tags {
					if tag.Key != nil && tag.Value != nil {
						resource.Tags[*tag.Key] = *tag.Value
					}
				}
				resources = append(resources, resource)
			}
		}

	case "ec2:route-table":
		// List all route tables
		paginator := ec2.NewDescribeRouteTablesPaginator(rt.ec2Client, &ec2.DescribeRouteTablesInput{})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to describe route tables: %w", err)
			}
			for _, rt := range page.RouteTables {
				if rt.RouteTableId == nil {
					continue
				}
				arn := fmt.Sprintf("arn:aws:ec2:%s:%s:route-table/%s", region, accountID, *rt.RouteTableId)
				resource := Resource{ARN: arn, Tags: make(map[string]string)}
				for _, tag := range rt.Tags {
					if tag.Key != nil && tag.Value != nil {
						resource.Tags[*tag.Key] = *tag.Value
					}
				}
				resources = append(resources, resource)
			}
		}

	case "ec2:network-acl":
		// List all network ACLs
		paginator := ec2.NewDescribeNetworkAclsPaginator(rt.ec2Client, &ec2.DescribeNetworkAclsInput{})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to describe network ACLs: %w", err)
			}
			for _, nacl := range page.NetworkAcls {
				if nacl.NetworkAclId == nil {
					continue
				}
				arn := fmt.Sprintf("arn:aws:ec2:%s:%s:network-acl/%s", region, accountID, *nacl.NetworkAclId)
				resource := Resource{ARN: arn, Tags: make(map[string]string)}
				for _, tag := range nacl.Tags {
					if tag.Key != nil && tag.Value != nil {
						resource.Tags[*tag.Key] = *tag.Value
					}
				}
				resources = append(resources, resource)
			}
		}

	case "ec2:dhcp-options":
		// List all DHCP options sets
		paginator := ec2.NewDescribeDhcpOptionsPaginator(rt.ec2Client, &ec2.DescribeDhcpOptionsInput{})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to describe DHCP options: %w", err)
			}
			for _, dhcpOptions := range page.DhcpOptions {
				if dhcpOptions.DhcpOptionsId == nil {
					continue
				}
				arn := fmt.Sprintf("arn:aws:ec2:%s:%s:dhcp-options/%s", region, accountID, *dhcpOptions.DhcpOptionsId)
				resource := Resource{ARN: arn, Tags: make(map[string]string)}
				for _, tag := range dhcpOptions.Tags {
					if tag.Key != nil && tag.Value != nil {
						resource.Tags[*tag.Key] = *tag.Value
					}
				}
				resources = append(resources, resource)
			}
		}

	case "ec2:network-interface":
		// List all network interfaces
		paginator := ec2.NewDescribeNetworkInterfacesPaginator(rt.ec2Client, &ec2.DescribeNetworkInterfacesInput{})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to describe network interfaces: %w", err)
			}
			for _, eni := range page.NetworkInterfaces {
				if eni.NetworkInterfaceId == nil {
					continue
				}
				arn := fmt.Sprintf("arn:aws:ec2:%s:%s:network-interface/%s", region, accountID, *eni.NetworkInterfaceId)
				resource := Resource{ARN: arn, Tags: make(map[string]string)}
				for _, tag := range eni.TagSet {
					if tag.Key != nil && tag.Value != nil {
						resource.Tags[*tag.Key] = *tag.Value
					}
				}
				resources = append(resources, resource)
			}
		}

	case "ec2:elastic-ip":
		// List all elastic IPs
		result, err := rt.ec2Client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{})
		if err != nil {
			return nil, fmt.Errorf("failed to describe elastic IPs: %w", err)
		}
		for _, address := range result.Addresses {
			if address.AllocationId == nil {
				continue
			}
			arn := fmt.Sprintf("arn:aws:ec2:%s:%s:elastic-ip/%s", region, accountID, *address.AllocationId)
			resource := Resource{ARN: arn, Tags: make(map[string]string)}
			for _, tag := range address.Tags {
				if tag.Key != nil && tag.Value != nil {
					resource.Tags[*tag.Key] = *tag.Value
				}
			}
			resources = append(resources, resource)
		}

	default:
		return []Resource{}, nil
	}

	return resources, nil
}

// discoverResourcesDirect enumerates resources directly using service-specific APIs
func (rt *ResourceTagger) discoverResourcesDirect(ctx context.Context, resourceType string) ([]Resource, error) {
	switch resourceType {
	case "ec2:instance":
		return rt.discoverEC2Instances(ctx)
	case "s3:bucket":
		return rt.discoverS3Buckets(ctx)
	case "ec2:security-group":
		return rt.discoverEC2SecurityGroups(ctx)
	case "sns:topic":
		return rt.discoverSNSTopics(ctx)
	case "sqs:queue":
		return rt.discoverSQSQueues(ctx)
	case "acm:certificate":
		return rt.discoverACMCertificates(ctx)
	case "kms:key":
		return rt.discoverKMSKeys(ctx)
	case "autoscaling:autoScalingGroup":
		return rt.discoverAutoScalingGroups(ctx)
	case "secretsmanager:secret":
		return rt.discoverSecretsManagerSecrets(ctx)
	case "backup:backup-vault":
		return rt.discoverBackupVaults(ctx)
	case "backup:recovery-point", "backup:backup-plan", "backup:framework", "backup:report-plan":
		return rt.discoverAdditionalBackupResources(ctx, resourceType)
	case "ec2:vpc", "ec2:subnet", "ec2:internet-gateway", "ec2:route-table", "ec2:network-acl", "ec2:dhcp-options", "ec2:network-interface", "ec2:elastic-ip":
		return rt.discoverAdditionalEC2Resources(ctx, resourceType)
	case "events:event-bus":
		eventBridgeDiscovery := NewEventBridgeResourceDiscovery(rt.client)
		return eventBridgeDiscovery.DiscoverEventBusResources(ctx)
	case "events:schedule-group":
		eventBridgeDiscovery := NewEventBridgeResourceDiscovery(rt.client)
		return eventBridgeDiscovery.DiscoverScheduleGroupResources(ctx)
	default:
		// For unsupported resource types, return empty slice
		// This allows gradual implementation of all resource types
		return []Resource{}, nil
	}
}

// isEndpointResolutionError checks if the error is related to endpoint resolution
func isEndpointResolutionError(err error) bool {
	return strings.Contains(err.Error(), "ResolveEndpointV2") || strings.Contains(err.Error(), "not found")
}

// DiscoverResources discovers resources using both Resource Groups Tagging API and direct enumeration
func (rt *ResourceTagger) DiscoverResources(ctx context.Context, resourceTypes []string) ([]Resource, error) {
	// Map to store resources by ARN to avoid duplicates
	resourceMap := make(map[string]Resource)

	// First, get resources from Resource Groups Tagging API (tagged resources)
	typeFilters := make([]string, len(resourceTypes))
	copy(typeFilters, resourceTypes)

	input := &resourcegroupstaggingapi.GetResourcesInput{
		ResourceTypeFilters: typeFilters,
	}

	paginator := resourcegroupstaggingapi.NewGetResourcesPaginator(rt.rgta, input)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			// If Resource Groups Tagging API is not available (endpoint resolution error),
			// continue with direct enumeration only
			if isEndpointResolutionError(err) {
				fmt.Printf("⚠️  Resource Groups Tagging API not available, using direct enumeration only\n")
				break
			}
			return nil, fmt.Errorf("failed to get resources page from Resource Groups API: %w", err)
		}

		for _, resourceMapping := range page.ResourceTagMappingList {
			resource := Resource{
				ARN:  aws.ToString(resourceMapping.ResourceARN),
				Tags: make(map[string]string),
			}

			// Convert AWS tags to our map format
			for _, tag := range resourceMapping.Tags {
				if tag.Key != nil && tag.Value != nil {
					resource.Tags[*tag.Key] = *tag.Value
				}
			}

			resourceMap[resource.ARN] = resource
		}
	}

	// Second, supplement with direct enumeration to catch untagged resources
	for _, resourceType := range resourceTypes {
		directResources, err := rt.discoverResourcesDirect(ctx, resourceType)
		if err != nil {
			// If direct enumeration fails due to endpoint resolution, skip this resource type
			if isEndpointResolutionError(err) {
				fmt.Printf("⚠️  Direct enumeration for %s not available, skipping\n", resourceType)
				continue
			}
			return nil, fmt.Errorf("failed to discover %s resources directly: %w", resourceType, err)
		}

		// Add or update resources from direct enumeration
		for _, resource := range directResources {
			// If resource already exists from Resource Groups API, keep the existing one
			// (it may have better tag information), otherwise add the new one
			if _, exists := resourceMap[resource.ARN]; !exists {
				resourceMap[resource.ARN] = resource
			}
		}
	}

	// Handle EventBridge resources specifically - these are not caught by the standard discovery methods
	// but need to be added if the resource types are included
	var eventBridgeResources []Resource

	for _, resourceType := range resourceTypes {
		if resourceType == "events:event-bus" || resourceType == "events:schedule-group" {
			// We need to create an EventBridgeResourceDiscovery instance to get these resources
			eventBridgeDiscovery := NewEventBridgeResourceDiscovery(rt.client)

			// Discover all EventBridge resources
			eventBridgeRes, err := eventBridgeDiscovery.DiscoverEventBridgeResources(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to discover EventBridge resources: %w", err)
			}

			eventBridgeResources = append(eventBridgeResources, eventBridgeRes...)
			break // We only need to do this once per discovery session
		}
	}

	// Merge EventBridge resources into the main resource map to avoid duplicates
	for _, resource := range eventBridgeResources {
		if _, exists := resourceMap[resource.ARN]; !exists {
			resourceMap[resource.ARN] = resource
		}
	}

	// Convert map back to slice
	var resources []Resource
	for _, resource := range resourceMap {
		resources = append(resources, resource)
	}

	return resources, nil
}

// TagResources tags multiple resources with the provided tags
func (rt *ResourceTagger) TagResources(ctx context.Context, arns []string, tags map[string]string) error {
	if len(arns) == 0 {
		return nil
	}

	// Convert tags to AWS SDK format
	awsTags := make(map[string]string)
	for k, v := range tags {
		awsTags[k] = v
	}

	// Resource Groups Tagging API allows up to 20 ARNs per call
	chunkSize := 20
	for i := 0; i < len(arns); i += chunkSize {
		end := i + chunkSize
		if end > len(arns) {
			end = len(arns)
		}

		chunk := arns[i:end]
		input := &resourcegroupstaggingapi.TagResourcesInput{
			ResourceARNList: chunk,
			Tags:            awsTags,
		}

		_, err := rt.rgta.TagResources(ctx, input)
		if err != nil {
			return fmt.Errorf("failed to tag resources chunk %d-%d: %w", i, end-1, err)
		}
	}

	return nil
}

// TagEventBridgeResources tags EventBridge event buses and schedule groups with provided tags
func (rt *ResourceTagger) TagEventBridgeResources(ctx context.Context, arns []string, tags map[string]string) error {
	// Create EventBridge resource discovery instance
	eventBridgeDiscovery := NewEventBridgeResourceDiscovery(rt.client)

	// Tag the resources using EventBridge client
	err := eventBridgeDiscovery.TagEventBridgeResources(ctx, arns, tags)
	if err != nil {
		return fmt.Errorf("failed to tag EventBridge resources: %w", err)
	}

	return nil
}

// GetResourceTags retrieves tags for a specific resource ARN
func (rt *ResourceTagger) GetResourceTags(ctx context.Context, arn string) (map[string]string, error) {
	input := &resourcegroupstaggingapi.GetResourcesInput{
		ResourceARNList: []string{arn},
	}

	result, err := rt.rgta.GetResources(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource tags: %w", err)
	}

	if len(result.ResourceTagMappingList) == 0 {
		return make(map[string]string), nil
	}

	tags := make(map[string]string)
	for _, tag := range result.ResourceTagMappingList[0].Tags {
		if tag.Key != nil && tag.Value != nil {
			tags[*tag.Key] = *tag.Value
		}
	}

	return tags, nil
}
