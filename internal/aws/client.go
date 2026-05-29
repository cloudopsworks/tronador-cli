// internal/aws/client.go
package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Config holds AWS configuration options
type Config struct {
	Profile                string
	Region                 string
	AssumeRoleArn          string
	AssumeRoleSessionName  string
	AssumeRoleExternalId   string
	AssumeRoleDurationSecs int32
}

// Client wraps AWS SDK clients with common configuration
type Client struct {
	Config aws.Config
	STS    *sts.Client
}

// NewClient creates a new AWS client with optional assume role
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	// Load base AWS config
	var opts []func(*config.LoadOptions) error

	if cfg.Profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(cfg.Profile))
	}

	if cfg.Region != "" {
		opts = append(opts, config.WithRegion(cfg.Region))
	}

	// If assume role is requested, use AWS SDK V2 assume role credentials options
	if cfg.AssumeRoleArn != "" {
		sessionName := cfg.AssumeRoleSessionName
		if sessionName == "" {
			sessionName = fmt.Sprintf("tronador-cli-%d", time.Now().Unix())
		}

		assumeRoleOptions := func(options *stscreds.AssumeRoleOptions) {
			options.RoleSessionName = sessionName
			if cfg.AssumeRoleDurationSecs > 0 {
				options.Duration = time.Duration(cfg.AssumeRoleDurationSecs) * time.Second
			}
			if cfg.AssumeRoleExternalId != "" {
				options.ExternalID = &cfg.AssumeRoleExternalId
			}
		}

		// Load base config first to get the STS client
		baseConfig, err := config.LoadDefaultConfig(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to load base AWS config: %w", err)
		}

		// Create assume role provider with proper STS client
		assumeRoleProvider := stscreds.NewAssumeRoleProvider(
			sts.NewFromConfig(baseConfig),
			cfg.AssumeRoleArn,
			assumeRoleOptions,
		)

		opts = append(opts, config.WithCredentialsProvider(assumeRoleProvider))
	}

	awsConfig, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		// Check if this is an endpoint resolution error for assume role
		if cfg.AssumeRoleArn != "" && isEndpointResolutionError(err) {
			return nil, fmt.Errorf("assume role failed - STS endpoint not available: %w", err)
		}
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := &Client{
		Config: awsConfig,
		STS:    sts.NewFromConfig(awsConfig),
	}

	return client, nil
}

// GetEffectiveRegion returns the effective region being used
func (c *Client) GetEffectiveRegion() string {
	return c.Config.Region
}

// GetAccountId retrieves the current AWS account ID
func (c *Client) GetAccountId(ctx context.Context) (string, error) {
	result, err := c.STS.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("failed to get caller identity: %w", err)
	}

	if result.Account == nil {
		return "", fmt.Errorf("caller identity returned nil account")
	}

	return *result.Account, nil
}

// GetCallerArn retrieves the current caller ARN
func (c *Client) GetCallerArn(ctx context.Context) (string, error) {
	result, err := c.STS.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("failed to get caller identity: %w", err)
	}

	if result.Arn == nil {
		return "", fmt.Errorf("caller identity returned nil arn")
	}

	return *result.Arn, nil
}
