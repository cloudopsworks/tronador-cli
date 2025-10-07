// internal/utils/tags.go
package utils

import (
	"fmt"
	"regexp"
	"strings"
)

// TagConfig holds the configuration for building organization tags
type TagConfig struct {
	Organization     string
	OrganizationUnit string
	ApplicationName  string
	ApplicationType  string
	ManagedBy        string
	FullNameSep      string
}

// ValidateTagConfig validates that all required tag configuration fields are present
func ValidateTagConfig(cfg TagConfig) error {
	if cfg.Organization == "" {
		return fmt.Errorf("organization is required")
	}
	if cfg.OrganizationUnit == "" {
		return fmt.Errorf("organization-unit is required")
	}
	if cfg.ApplicationName == "" {
		return fmt.Errorf("application-name is required")
	}
	if cfg.ApplicationType == "" {
		return fmt.Errorf("application-type is required")
	}
	if cfg.ManagedBy == "" {
		cfg.ManagedBy = "manual" // Default value
	}
	if cfg.FullNameSep == "" {
		cfg.FullNameSep = "-" // Default separator
	}
	return nil
}

// SanitizeForFullname sanitizes input strings for use in organization-full-name
// Replaces runs of whitespace with the separator and trims leading/trailing separators
func SanitizeForFullname(input, separator string) string {
	if input == "" {
		return ""
	}

	// Replace runs of whitespace with single separator
	whitespaceRegex := regexp.MustCompile(`\s+`)
	sanitized := whitespaceRegex.ReplaceAllString(input, separator)

	// Trim leading and trailing separators
	sanitized = strings.Trim(sanitized, separator)

	return sanitized
}

// BuildOrganizationFullName builds the organization-full-name tag value
func BuildOrganizationFullName(cfg TagConfig) string {
	fullOrg := SanitizeForFullname(cfg.Organization, cfg.FullNameSep)
	fullUnit := SanitizeForFullname(cfg.OrganizationUnit, cfg.FullNameSep)
	fullApp := SanitizeForFullname(cfg.ApplicationName, cfg.FullNameSep)
	fullType := SanitizeForFullname(cfg.ApplicationType, cfg.FullNameSep)

	return fmt.Sprintf("%s%s%s%s%s%s%s",
		fullOrg, cfg.FullNameSep,
		fullUnit, cfg.FullNameSep,
		fullApp, cfg.FullNameSep,
		fullType)
}

// BuildStandardTags creates the standard set of organization tags
func BuildStandardTags(cfg TagConfig) map[string]string {
	orgFullName := BuildOrganizationFullName(cfg)

	return map[string]string{
		"organization":           cfg.Organization,
		"organization-unit":      cfg.OrganizationUnit,
		"application-name":       cfg.ApplicationName,
		"application-type":       cfg.ApplicationType,
		"managed-by":             cfg.ManagedBy,
		"organization-full-name": orgFullName,
	}
}

// HasManagedByIaC checks if a resource has the managed-by=iac tag
func HasManagedByIaC(tags map[string]string) bool {
	// check both managed-by and managed_by tags:
	managedBy, exists := tags["managed-by"]
	managedByAlt, existsAlt := tags["managed_by"]
	return (exists && managedBy == "iac") || (existsAlt && managedByAlt == "iac")
}

// IsUntagged checks if a resource has zero tags
func IsUntagged(tags map[string]string) bool {
	return len(tags) == 0
}

// ShouldSkipResource determines if a resource should be skipped based on tags and reapply mode
func ShouldSkipResource(tags map[string]string, reapply bool) (bool, string) {
	// Always skip if managed by IaC
	if HasManagedByIaC(tags) {
		return true, "managed by IaC"
	}

	// New eligibility logic:
	// - If resource has no tags -> eligible for tagging
	// - If resource has tags but contains "managed-by=iac" -> not eligible (already handled above)
	// - If resource has tags but does NOT contain "managed-by=iac" -> eligible for tagging

	// Resources are eligible for tagging regardless of existing tags (except managed-by=iac)
	// The reapply flag now only affects whether we show additional messaging
	return false, ""
}
