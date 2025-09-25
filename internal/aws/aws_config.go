// internal/cli/aws_config.go
package aws

// AWSConfig holds common AWS configuration for CLI commands
type AWSConfig struct {
	Profile                string
	Region                 string
	AssumeRoleArn          string
	AssumeRoleSessionName  string
	AssumeRoleExternalId   string
	AssumeRoleDurationSecs int32
}

// BuildAWSConfig builds an AWS client configuration from the shared variables
func (ac *AWSConfig) BuildAWSConfig() Config {
	return Config{
		Profile:                ac.Profile,
		Region:                 ac.Region,
		AssumeRoleArn:          ac.AssumeRoleArn,
		AssumeRoleSessionName:  ac.AssumeRoleSessionName,
		AssumeRoleExternalId:   ac.AssumeRoleExternalId,
		AssumeRoleDurationSecs: ac.AssumeRoleDurationSecs,
	}
}

// SetAWSConfig sets the shared AWS configuration variables
func SetAWSConfig(p, r, ara, arsn, arei string, ards int32) *AWSConfig {
	return &AWSConfig{
		Profile:                p,
		Region:                 r,
		AssumeRoleArn:          ara,
		AssumeRoleSessionName:  arsn,
		AssumeRoleExternalId:   arei,
		AssumeRoleDurationSecs: ards,
	}
}
