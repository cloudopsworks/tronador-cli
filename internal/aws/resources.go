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
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
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

// discoverResourcesDirect enumerates resources directly using service-specific APIs
func (rt *ResourceTagger) discoverResourcesDirect(ctx context.Context, resourceType string) ([]Resource, error) {
	switch resourceType {
	case "ec2:instance":
		return rt.discoverEC2Instances(ctx)
	case "s3:bucket":
		return rt.discoverS3Buckets(ctx)
	case "ec2:security-group":
		return rt.discoverEC2SecurityGroups(ctx)
	default:
		// For unsupported resource types, return empty slice
		// This allows gradual implementation of all resource types
		return []Resource{}, nil
	}
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
