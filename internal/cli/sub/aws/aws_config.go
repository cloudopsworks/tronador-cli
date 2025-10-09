// internal/cli/sub/aws/aws_config.go
package aws

import awsclient "tronador-cli/internal/aws"

// AWS configuration variables (shared with parent)
var awsConfig *awsclient.AWSConfig

// SetAWSConfig sets the shared AWS configuration variables
func SetAWSConfig(p, r, ara, arsn, arei string, ards int32) {
	awsConfig = &awsclient.AWSConfig{
		Profile:                p,
		Region:                 r,
		AssumeRoleArn:          ara,
		AssumeRoleSessionName:  arsn,
		AssumeRoleExternalId:   arei,
		AssumeRoleDurationSecs: ards,
	}
}

// buildAWSConfigFromFlags builds an AWS configuration from the shared variables
func buildAWSConfigFromFlags() awsclient.Config {
	return awsConfig.BuildAWSConfig()
}
