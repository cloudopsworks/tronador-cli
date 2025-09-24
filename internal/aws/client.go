// internal/aws/client.go
package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
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

	awsConfig, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := &Client{
		Config: awsConfig,
		STS:    sts.NewFromConfig(awsConfig),
	}

	// If assume role is requested, get temporary credentials
	if cfg.AssumeRoleArn != "" {
		if err := client.assumeRole(ctx, cfg); err != nil {
			return nil, fmt.Errorf("failed to assume role: %w", err)
		}
	}

	return client, nil
}

// assumeRole assumes the specified role and updates the client config with temporary credentials
func (c *Client) assumeRole(ctx context.Context, cfg Config) error {
	sessionName := cfg.AssumeRoleSessionName
	if sessionName == "" {
		sessionName = fmt.Sprintf("tronador-cli-%d", time.Now().Unix())
	}

	input := &sts.AssumeRoleInput{
		RoleArn:         aws.String(cfg.AssumeRoleArn),
		RoleSessionName: aws.String(sessionName),
		DurationSeconds: aws.Int32(cfg.AssumeRoleDurationSecs),
	}

	if cfg.AssumeRoleExternalId != "" {
		input.ExternalId = aws.String(cfg.AssumeRoleExternalId)
	}

	result, err := c.STS.AssumeRole(ctx, input)
	if err != nil {
		return fmt.Errorf("assume role failed: %w", err)
	}

	if result.Credentials == nil {
		return fmt.Errorf("assume role returned nil credentials")
	}

	// Update the config with temporary credentials
	c.Config.Credentials = credentials.NewStaticCredentialsProvider(
		*result.Credentials.AccessKeyId,
		*result.Credentials.SecretAccessKey,
		*result.Credentials.SessionToken,
	)

	// Update STS client to use new credentials
	c.STS = sts.NewFromConfig(c.Config)

	return nil
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
