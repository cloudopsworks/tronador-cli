package repos

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

//go:embed default_config.json
var defaultConfigFS embed.FS

const defaultConfigName = "default_config.json"

// Config is the configuration-driven catalog for repository template operations.
// New template types or migration versions should be added to JSON instead of
// branching CLI command code.
type Config struct {
	SchemaVersion     string          `json:"schemaVersion"`
	TemplateDirectory string          `json:"templateDirectory"`
	DefaultPullBranch string          `json:"defaultPullBranch"`
	Templates         []Template      `json:"templates"`
	MigrationPlans    []MigrationPlan `json:"migrationPlans"`
}

// Template describes one supported repository template marker and behavior.
type Template struct {
	Name                    string `json:"name"`
	Description             string `json:"description"`
	Marker                  string `json:"marker"`
	Repository              string `json:"repository"`
	Merge                   bool   `json:"merge"`
	Versioned               bool   `json:"versioned"`
	CICD                    bool   `json:"cicd"`
	Boilerplate             bool   `json:"boilerplate"`
	BoilerplatePathPre510   string `json:"boilerplatePathPre510"`
	BoilerplatePathV510Plus string `json:"boilerplatePathV510Plus"`
	AgentsOverride          bool   `json:"agentsOverride"`
	Migration               string `json:"migration"`
}

// MigrationPlan contains declarative file operations for a repository layout
// migration, such as 5.10, 5.11, or 5.12.
type MigrationPlan struct {
	Version     string                 `json:"version"`
	Aliases     []string               `json:"aliases"`
	Description string                 `json:"description"`
	Common      []Operation            `json:"common"`
	Templates   map[string][]Operation `json:"templates"`
}

// Operation is a declarative file or shell-adjacent action used by migrations.
type Operation struct {
	Action      string   `json:"action"`
	Source      string   `json:"source"`
	Sources     []string `json:"sources"`
	Destination string   `json:"destination"`
	Optional    bool     `json:"optional"`
	When        string   `json:"when"`
	Message     string   `json:"message"`
}

// LoadConfig loads the embedded default configuration or an override JSON file.
func LoadConfig(path string) (*Config, error) {
	var data []byte
	var err error
	if path == "" {
		data, err = defaultConfigFS.ReadFile(defaultConfigName)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read repos config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse repos config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate checks the config shape before commands use it.
func (c *Config) Validate() error {
	if c.SchemaVersion == "" {
		return fmt.Errorf("repos config schemaVersion is required")
	}
	if c.TemplateDirectory == "" {
		c.TemplateDirectory = ".template"
	}
	if c.DefaultPullBranch == "" {
		c.DefaultPullBranch = "master"
	}

	seen := map[string]struct{}{}
	for _, tmpl := range c.Templates {
		if tmpl.Name == "" {
			return fmt.Errorf("repos config template name is required")
		}
		name := normalizeKey(tmpl.Name)
		if _, ok := seen[name]; ok {
			return fmt.Errorf("repos config duplicate template %q", tmpl.Name)
		}
		seen[name] = struct{}{}
		if tmpl.Marker == "" {
			return fmt.Errorf("repos config template %q marker is required", tmpl.Name)
		}
		if tmpl.Repository == "" {
			return fmt.Errorf("repos config template %q repository is required", tmpl.Name)
		}
	}

	plans := map[string]struct{}{}
	for _, plan := range c.MigrationPlans {
		if plan.Version == "" {
			return fmt.Errorf("repos config migration version is required")
		}
		version := normalizeVersion(plan.Version)
		if _, ok := plans[version]; ok {
			return fmt.Errorf("repos config duplicate migration version %q", plan.Version)
		}
		plans[version] = struct{}{}
	}
	return nil
}

// FindTemplate finds a template by configured name.
func (c *Config) FindTemplate(name string) (Template, bool) {
	want := normalizeKey(name)
	for _, tmpl := range c.Templates {
		if normalizeKey(tmpl.Name) == want {
			return tmpl, true
		}
	}
	return Template{}, false
}

// FindMigrationPlan finds a migration plan by version or alias.
func (c *Config) FindMigrationPlan(version string) (MigrationPlan, bool) {
	want := normalizeVersion(version)
	for _, plan := range c.MigrationPlans {
		if normalizeVersion(plan.Version) == want {
			return plan, true
		}
		for _, alias := range plan.Aliases {
			if normalizeVersion(alias) == want {
				return plan, true
			}
		}
	}
	return MigrationPlan{}, false
}

func normalizeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeVersion(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimSuffix(v, "+")
	v = strings.ReplaceAll(v, ".", "")
	return v
}
