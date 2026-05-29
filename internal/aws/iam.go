// internal/aws/iam.go
package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
)

// IAMRole represents an IAM role with its metadata and tags
type IAMRole struct {
	Name string
	Path string
	Tags map[string]string
}

// IAMPolicy represents an IAM customer-managed policy with its metadata and tags
type IAMPolicy struct {
	Name string
	ARN  string
	Tags map[string]string
}

// IAMTagger handles IAM roles and policies tagging operations
type IAMTagger struct {
	client *Client
	iam    *iam.Client
}

// NewIAMTagger creates a new IAMTagger instance
func NewIAMTagger(client *Client) *IAMTagger {
	return &IAMTagger{
		client: client,
		iam:    iam.NewFromConfig(client.Config),
	}
}

// IsServiceLinkedRole checks if a role is a service-linked role
func IsServiceLinkedRole(roleName, rolePath string) bool {
	return strings.HasPrefix(roleName, "AWSServiceRoleFor") || strings.HasPrefix(rolePath, "/aws-service-role/")
}

// IsRoleNonModifiable checks if a role is known to be non-modifiable using heuristics
func IsRoleNonModifiable(roleName, rolePath string) bool {
	// AWS IAM Identity Center (SSO) creates AWSReservedSSO* roles which are not modifiable
	if strings.HasPrefix(roleName, "AWSReservedSSO") {
		return true
	}

	// Some reserved roles reside under /aws-reserved/ path
	if strings.HasPrefix(rolePath, "/aws-reserved/") {
		return true
	}

	return false
}

// DiscoverRoles discovers IAM roles with optional filtering
func (it *IAMTagger) DiscoverRoles(ctx context.Context, includeServiceLinked bool) ([]IAMRole, error) {
	var roles []IAMRole

	// Use paginator to handle large numbers of roles
	paginator := iam.NewListRolesPaginator(it.iam, &iam.ListRolesInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list roles: %w", err)
		}

		for _, role := range page.Roles {
			if role.RoleName == nil || role.Path == nil {
				continue
			}

			roleName := *role.RoleName
			rolePath := *role.Path

			// Skip service-linked roles unless explicitly included
			if !includeServiceLinked && IsServiceLinkedRole(roleName, rolePath) {
				continue
			}

			// Skip known non-modifiable roles
			if IsRoleNonModifiable(roleName, rolePath) {
				continue
			}

			// Get tags for this role
			tags, err := it.getRoleTags(ctx, roleName)
			if err != nil {
				// If we can't get tags, skip this role
				continue
			}

			roles = append(roles, IAMRole{
				Name: roleName,
				Path: rolePath,
				Tags: tags,
			})
		}
	}

	return roles, nil
}

// DiscoverPolicies discovers customer-managed IAM policies
func (it *IAMTagger) DiscoverPolicies(ctx context.Context) ([]IAMPolicy, error) {
	var policies []IAMPolicy

	// Use paginator to handle large numbers of policies
	paginator := iam.NewListPoliciesPaginator(it.iam, &iam.ListPoliciesInput{
		Scope: types.PolicyScopeTypeLocal, // Only customer-managed policies
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list policies: %w", err)
		}

		for _, policy := range page.Policies {
			if policy.PolicyName == nil || policy.Arn == nil {
				continue
			}

			policyName := *policy.PolicyName
			policyARN := *policy.Arn

			// Get tags for this policy
			tags, err := it.getPolicyTags(ctx, policyARN)
			if err != nil {
				// If we can't get tags, skip this policy
				continue
			}

			policies = append(policies, IAMPolicy{
				Name: policyName,
				ARN:  policyARN,
				Tags: tags,
			})
		}
	}

	return policies, nil
}

// getRoleTags retrieves tags for a specific role
func (it *IAMTagger) getRoleTags(ctx context.Context, roleName string) (map[string]string, error) {
	result, err := it.iam.ListRoleTags(ctx, &iam.ListRoleTagsInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list role tags: %w", err)
	}

	tags := make(map[string]string)
	for _, tag := range result.Tags {
		if tag.Key != nil && tag.Value != nil {
			tags[*tag.Key] = *tag.Value
		}
	}

	return tags, nil
}

// getPolicyTags retrieves tags for a specific policy
func (it *IAMTagger) getPolicyTags(ctx context.Context, policyARN string) (map[string]string, error) {
	result, err := it.iam.ListPolicyTags(ctx, &iam.ListPolicyTagsInput{
		PolicyArn: aws.String(policyARN),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list policy tags: %w", err)
	}

	tags := make(map[string]string)
	for _, tag := range result.Tags {
		if tag.Key != nil && tag.Value != nil {
			tags[*tag.Key] = *tag.Value
		}
	}

	return tags, nil
}

// TagRole tags an IAM role with the specified tags
func (it *IAMTagger) TagRole(ctx context.Context, roleName string, tags map[string]string) error {
	if len(tags) == 0 {
		return nil
	}

	// Convert tags to IAM tag format
	var iamTags []types.Tag
	for k, v := range tags {
		iamTags = append(iamTags, types.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	_, err := it.iam.TagRole(ctx, &iam.TagRoleInput{
		RoleName: aws.String(roleName),
		Tags:     iamTags,
	})
	if err != nil {
		return fmt.Errorf("failed to tag role %s: %w", roleName, err)
	}

	return nil
}

// TagPolicy tags an IAM policy with the specified tags
func (it *IAMTagger) TagPolicy(ctx context.Context, policyARN string, tags map[string]string) error {
	if len(tags) == 0 {
		return nil
	}

	// Convert tags to IAM tag format
	var iamTags []types.Tag
	for k, v := range tags {
		iamTags = append(iamTags, types.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	_, err := it.iam.TagPolicy(ctx, &iam.TagPolicyInput{
		PolicyArn: aws.String(policyARN),
		Tags:      iamTags,
	})
	if err != nil {
		return fmt.Errorf("failed to tag policy %s: %w", policyARN, err)
	}

	return nil
}

// TagRoles tags multiple roles in batch with retry logic
func (it *IAMTagger) TagRoles(ctx context.Context, roles []IAMRole, tags map[string]string, dryRun bool) (int, int, error) {
	tagged := 0
	skipped := 0

	for _, role := range roles {
		if dryRun {
			fmt.Printf("🧪 DRY-RUN would tag role: %s\n", role.Name)
			for k, v := range tags {
				fmt.Printf("      %s=%s\n", k, v)
			}
			continue
		}

		err := it.TagRole(ctx, role.Name, tags)
		if err != nil {
			skipped++
			fmt.Printf("❌ Failed to tag role: %s (reason: %v)\n", role.Name, err)
			continue
		}

		tagged++
		fmt.Printf("🏷️  Tagged role: %s\n", role.Name)
	}

	return tagged, skipped, nil
}

// TagPolicies tags multiple policies in batch with retry logic
func (it *IAMTagger) TagPolicies(ctx context.Context, policies []IAMPolicy, tags map[string]string, dryRun bool) (int, int, error) {
	tagged := 0
	skipped := 0

	for _, policy := range policies {
		if dryRun {
			fmt.Printf("🧪 DRY-RUN would tag policy: %s\n", policy.Name)
			for k, v := range tags {
				fmt.Printf("      %s=%s\n", k, v)
			}
			continue
		}

		err := it.TagPolicy(ctx, policy.ARN, tags)
		if err != nil {
			skipped++
			fmt.Printf("❌ Failed to tag policy: %s (reason: %v)\n", policy.Name, err)
			continue
		}

		tagged++
		fmt.Printf("🏷️  Tagged policy: %s\n", policy.Name)
	}

	return tagged, skipped, nil
}
