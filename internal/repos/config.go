package repos

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	if strings.TrimSpace(c.SchemaVersion) == "" {
		return fmt.Errorf("repos config schemaVersion is required")
	}
	if c.TemplateDirectory == "" {
		c.TemplateDirectory = ".template"
	}
	if !safeRepositoryPath(c.TemplateDirectory) || overlapsManagedRoot(c.TemplateDirectory) {
		return fmt.Errorf("repos config templateDirectory must be a safe non-reserved repository child, got %q", c.TemplateDirectory)
	}
	if c.DefaultPullBranch == "" {
		c.DefaultPullBranch = "master"
	}

	templateNames := map[string]struct{}{}
	templateRoutes := map[string]templateMigration{}
	for _, tmpl := range c.Templates {
		if strings.TrimSpace(tmpl.Name) == "" {
			return fmt.Errorf("repos config template name is required")
		}
		name := normalizeKey(tmpl.Name)
		if _, ok := templateNames[name]; ok {
			return fmt.Errorf("repos config duplicate template %q", tmpl.Name)
		}
		templateNames[name] = struct{}{}
		if !safeMarker(tmpl.Marker) {
			return fmt.Errorf("repos config template %q marker must be a safe non-.git path", tmpl.Name)
		}
		if strings.TrimSpace(tmpl.Repository) == "" {
			return fmt.Errorf("repos config template %q repository is required", tmpl.Name)
		}
		if err := validateBoilerplatePaths(tmpl); err != nil {
			return fmt.Errorf("repos config template %q %w", tmpl.Name, err)
		}
		if tmpl.Migration == "" {
			if tmpl.Versioned {
				return fmt.Errorf("repos config versioned template %q requires a migration route", tmpl.Name)
			}
			continue
		}
		route, version, err := parseMigrationRoute(tmpl.Migration)
		if err != nil {
			return fmt.Errorf("repos config template %q has invalid migration %q: %w", tmpl.Name, tmpl.Migration, err)
		}
		templateRoutes[name] = templateMigration{route: route, version: version}
	}

	planVersions := map[string]string{}
	canonicalPlanVersions := map[string]struct{}{}
	planRoutes := map[string]map[string]struct{}{}
	for _, plan := range c.MigrationPlans {
		if strings.TrimSpace(plan.Version) == "" {
			return fmt.Errorf("repos config migration version is required")
		}
		version := normalizeVersion(plan.Version)
		if _, ok := canonicalPlanVersions[version]; ok {
			return fmt.Errorf("repos config duplicate migration version %q", plan.Version)
		}
		if owner, ok := planVersions[version]; ok && owner != plan.Version {
			return fmt.Errorf("repos config migration version or alias %q collides with %q", plan.Version, owner)
		}
		canonicalPlanVersions[version] = struct{}{}
		planVersions[version] = plan.Version
		routes := map[string]struct{}{}
		for route, operations := range plan.Templates {
			normalizedRoute, err := normalizeMigrationRoute(route)
			if err != nil {
				return fmt.Errorf("repos config migration %q has invalid template route %q: %w", plan.Version, route, err)
			}
			if _, ok := routes[normalizedRoute]; ok {
				return fmt.Errorf("repos config migration %q has duplicate template route %q", plan.Version, route)
			}
			routes[normalizedRoute] = struct{}{}
			for _, op := range operations {
				if err := validateMigrationOperation(op); err != nil {
					return fmt.Errorf("repos config migration %q %w", plan.Version, err)
				}
			}
		}
		planRoutes[version] = routes
		for _, op := range plan.Common {
			if err := validateMigrationOperation(op); err != nil {
				return fmt.Errorf("repos config migration %q %w", plan.Version, err)
			}
		}
		for _, alias := range plan.Aliases {
			if strings.TrimSpace(alias) == "" {
				return fmt.Errorf("repos config migration %q has empty alias", plan.Version)
			}
			normalizedAlias := normalizeVersion(alias)
			if normalizedAlias == "" {
				return fmt.Errorf("repos config migration %q has invalid alias %q", plan.Version, alias)
			}
			if owner, ok := planVersions[normalizedAlias]; ok {
				if owner != plan.Version {
					return fmt.Errorf("repos config migration version or alias %q collides with %q", alias, owner)
				}
				continue
			}
			planVersions[normalizedAlias] = plan.Version
		}
	}

	declaredRoutes := map[string]map[string]struct{}{}
	for templateName, migration := range templateRoutes {
		canonicalVersion, ok := planVersions[migration.version]
		if !ok {
			return fmt.Errorf("repos config template %q migration %q does not resolve to a migration plan", templateName, migration.version)
		}
		canonicalVersion = normalizeVersion(canonicalVersion)
		routes := planRoutes[canonicalVersion]
		if _, ok := routes[migration.route]; !ok {
			return fmt.Errorf("repos config template %q migration route %q is not declared by migration %q", templateName, migration.route, canonicalVersion)
		}
		if declaredRoutes[canonicalVersion] == nil {
			declaredRoutes[canonicalVersion] = map[string]struct{}{}
		}
		declaredRoutes[canonicalVersion][migration.route] = struct{}{}
	}
	for version, routes := range planRoutes {
		for route := range routes {
			if _, ok := declaredRoutes[version][route]; !ok {
				return fmt.Errorf("repos config migration %q has unknown template route %q", version, route)
			}
		}
	}
	return nil
}

type templateMigration struct {
	route   string
	version string
}

// parseMigrationRoute splits a catalog migration reference of the form
// "template-route/version". The route is the key in MigrationPlan.Templates;
// the version may be a canonical migration version or one of its aliases.
func parseMigrationRoute(value string) (route, version string, err error) {
	if strings.TrimSpace(value) != value {
		return "", "", fmt.Errorf("must not contain leading or trailing whitespace")
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("must use route/version form")
	}
	route, err = normalizeMigrationRoute(parts[0])
	if err != nil {
		return "", "", err
	}
	if parts[1] == "" || strings.TrimSpace(parts[1]) != parts[1] {
		return "", "", fmt.Errorf("version is required")
	}
	version = normalizeVersion(parts[1])
	if version == "" {
		return "", "", fmt.Errorf("version is required")
	}
	return route, version, nil
}

func normalizeMigrationRoute(route string) (string, error) {
	if route == "" || strings.TrimSpace(route) != route || normalizeKey(route) != route || strings.ContainsAny(route, `/\\`) {
		return "", fmt.Errorf("route must be a lowercase template key")
	}
	return route, nil
}

func validateBoilerplatePaths(tmpl Template) error {
	if path := tmpl.BoilerplatePathPre510; path != "" {
		if !safeRepositoryPath(path) || !withinBlueprintRoot(path) {
			return fmt.Errorf("pre-v5.10 boilerplate must be within .github or .cloudopsworks, got %q", path)
		}
	}
	if path := tmpl.BoilerplatePathV510Plus; path != "" {
		if !safeRepositoryPath(path) || !strictDescendant(path, ".cloudopsworks") {
			return fmt.Errorf("v5.10 boilerplate must be a strict descendant of .cloudopsworks, got %q", path)
		}
		for _, protected := range []string{
			".cloudopsworks/_VERSION", ".cloudopsworks/hooks", ".cloudopsworks/vars", ".cloudopsworks/config",
		} {
			if pathsOverlap(path, protected) {
				return fmt.Errorf("v5.10 boilerplate must not overlap protected path %q", protected)
			}
		}
	}
	return nil
}

func validateMigrationOperation(op Operation) error {
	if err := validateOperationWhen(op.When); err != nil {
		return err
	}
	validateSource := func(path string, glob bool) error {
		if !safeMigrationPath(path, glob) || !withinBlueprintRoot(path) {
			return fmt.Errorf("has unsafe migration source %q", path)
		}
		return nil
	}
	validateDestination := func(path string) error {
		if !safeMigrationPath(path, false) || !withinPath(path, ".cloudopsworks") {
			return fmt.Errorf("has unsafe migration destination %q", path)
		}
		return nil
	}

	switch op.Action {
	case "ensureDir":
		if op.Destination == "" || op.Source != "" || len(op.Sources) != 0 || op.Message != "" || op.Optional {
			return fmt.Errorf("ensureDir requires only destination")
		}
		return validateDestination(op.Destination)
	case "move":
		if op.Source == "" || op.Destination == "" || len(op.Sources) != 0 || op.Message != "" {
			return fmt.Errorf("move requires source and destination")
		}
		if err := validateSource(op.Source, false); err != nil {
			return err
		}
		return validateDestination(op.Destination)
	case "moveMany":
		if op.Source != "" || op.Destination == "" || len(op.Sources) == 0 || op.Message != "" {
			return fmt.Errorf("moveMany requires sources and destination")
		}
		for _, source := range op.Sources {
			if err := validateSource(source, true); err != nil {
				return err
			}
		}
		return validateDestination(op.Destination)
	case "gitAdd":
		if op.Source != "" || op.Destination != "" || len(op.Sources) == 0 || op.Message != "" || op.Optional || op.When != "" {
			return fmt.Errorf("gitAdd requires sources only")
		}
		for _, source := range op.Sources {
			if err := validateSource(source, true); err != nil {
				return err
			}
		}
		return nil
	case "unsupported":
		if strings.TrimSpace(op.Message) == "" || op.Source != "" || op.Destination != "" || len(op.Sources) != 0 || op.Optional || op.When != "" {
			return fmt.Errorf("unsupported requires message only")
		}
		return nil
	default:
		return fmt.Errorf("has unsupported migration action %q", op.Action)
	}
}

func validateOperationWhen(when string) error {
	if when == "" {
		return nil
	}
	for _, prefix := range []string{"exists:", "missing:"} {
		if strings.HasPrefix(when, prefix) {
			path := strings.TrimPrefix(when, prefix)
			if safeMigrationPath(path, false) && withinBlueprintRoot(path) {
				return nil
			}
			break
		}
	}
	return fmt.Errorf("has invalid when condition %q", when)
}

func safeMarker(path string) bool {
	return safeRepositoryPath(path) && !strings.ContainsAny(path, "*?[")
}

func safeRepositoryPath(path string) bool {
	return safePath(path, false)
}

func safeMigrationPath(path string, allowGlob bool) bool {
	return safePath(path, allowGlob)
}

func safePath(path string, allowGlob bool) bool {
	if path == "" || filepath.IsAbs(path) || strings.TrimSpace(path) != path || strings.Contains(path, "\\") {
		return false
	}
	if !allowGlob && strings.ContainsAny(path, "*?[") {
		return false
	}
	if allowGlob {
		if _, err := filepath.Match(path, ""); err != nil {
			return false
		}
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "." || clean == ".." || clean != path {
		return false
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == ".git" || segment == ".." || segment == "" {
			return false
		}
	}
	return true
}

func withinPath(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}

func strictDescendant(path, root string) bool {
	return strings.HasPrefix(path, root+"/")
}

func withinBlueprintRoot(path string) bool {
	return withinPath(path, ".github") || withinPath(path, ".cloudopsworks")
}

func overlapsManagedRoot(path string) bool {
	return pathsOverlap(path, ".git") || pathsOverlap(path, ".github") || pathsOverlap(path, ".cloudopsworks")
}

func pathsOverlap(first, second string) bool {
	return withinPath(first, second) || withinPath(second, first)
}

func flattenMigrationOperations(templates map[string][]Operation) []Operation {
	var out []Operation
	for _, ops := range templates {
		out = append(out, ops...)
	}
	return out
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
