package repos

import "testing"

func TestLoadDefaultConfigPassesSemanticValidation(t *testing.T) {
	if _, err := LoadConfig(""); err != nil {
		t.Fatalf("LoadConfig(default) error = %v", err)
	}
}

func TestConfigValidationRejectsUnsafeCatalogPaths(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"template directory root", func(c *Config) { c.TemplateDirectory = "." }},
		{"template directory traversal", func(c *Config) { c.TemplateDirectory = ".cloudopsworks/.." }},
		{"template directory git", func(c *Config) { c.TemplateDirectory = ".git/config" }},
		{"template directory managed overlap", func(c *Config) { c.TemplateDirectory = ".github/templates" }},
		{"marker git", func(c *Config) { c.Templates[0].Marker = ".git/config" }},
		{"marker traversal", func(c *Config) { c.Templates[0].Marker = ".cloudopsworks/.." }},
		{"v510 boilerplate root", func(c *Config) { c.Templates[0].BoilerplatePathV510Plus = ".cloudopsworks" }},
		{"v510 boilerplate version overlap", func(c *Config) { c.Templates[0].BoilerplatePathV510Plus = ".cloudopsworks/_VERSION/generated" }},
		{"v510 boilerplate hooks overlap", func(c *Config) { c.Templates[0].BoilerplatePathV510Plus = ".cloudopsworks/hooks" }},
		{"v510 boilerplate vars overlap", func(c *Config) { c.Templates[0].BoilerplatePathV510Plus = ".cloudopsworks/vars/helm" }},
		{"v510 boilerplate config overlap", func(c *Config) { c.Templates[0].BoilerplatePathV510Plus = ".cloudopsworks/config" }},
		{"pre510 boilerplate source directory", func(c *Config) { c.Templates[0].BoilerplatePathPre510 = "src/boilerplate" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validCatalogConfig()
			test.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want rejection")
			}
		})
	}
}

func TestConfigValidationRejectsMalformedMigrationOperations(t *testing.T) {
	tests := []struct {
		name string
		op   Operation
	}{
		{"unknown action", Operation{Action: "delete", Source: ".github/file"}},
		{"move without destination", Operation{Action: "move", Source: ".github/file"}},
		{"move with glob", Operation{Action: "move", Source: ".github/*.yaml", Destination: ".cloudopsworks"}},
		{"moveMany missing sources", Operation{Action: "moveMany", Destination: ".cloudopsworks"}},
		{"moveMany malformed glob", Operation{Action: "moveMany", Sources: []string{".github/["}, Destination: ".cloudopsworks"}},
		{"source git", Operation{Action: "move", Source: ".git/config", Destination: ".cloudopsworks"}},
		{"source root", Operation{Action: "move", Source: ".", Destination: ".cloudopsworks"}},
		{"source escape", Operation{Action: "move", Source: ".github/../.git/config", Destination: ".cloudopsworks"}},
		{"destination github workflow", Operation{Action: "move", Source: ".github/file", Destination: ".github/workflows"}},
		{"destination escape", Operation{Action: "move", Source: ".github/file", Destination: ".cloudopsworks/../.github"}},
		{"git add invalid fields", Operation{Action: "gitAdd", Source: ".github/file"}},
		{"unsupported missing message", Operation{Action: "unsupported"}},
		{"invalid when", Operation{Action: "ensureDir", Destination: ".cloudopsworks/generated", When: "always"}},
		{"when traversal", Operation{Action: "ensureDir", Destination: ".cloudopsworks/generated", When: "exists:.github/../.git/config"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validCatalogConfig()
			cfg.MigrationPlans = []MigrationPlan{{Version: "510", Common: []Operation{test.op}}}
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want rejection")
			}
		})
	}
}

func TestConfigValidationAllowsSupportedMigrationSchemas(t *testing.T) {
	cfg := validCatalogConfig()
	cfg.MigrationPlans = []MigrationPlan{{
		Version: "510",
		Common: []Operation{
			{Action: "ensureDir", Destination: ".cloudopsworks/generated"},
			{Action: "move", Source: ".github/file", Destination: ".cloudopsworks", Optional: true, When: "exists:.github/file"},
			{Action: "moveMany", Sources: []string{".github/*.yaml"}, Destination: ".cloudopsworks", Optional: true},
			{Action: "gitAdd", Sources: []string{".github"}},
			{Action: "unsupported", Message: "not available"},
		},
	}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func validCatalogConfig() Config {
	return Config{
		SchemaVersion:     "1",
		TemplateDirectory: ".template",
		Templates: []Template{{
			Name:                    "go",
			Marker:                  ".golang",
			Repository:              "cloudopsworks/go-app-template",
			BoilerplatePathPre510:   ".cloudopsworks",
			BoilerplatePathV510Plus: ".cloudopsworks/boilerplate",
		}},
	}
}

func TestConfigValidationRejectsMigrationReferenceSemanticErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{
			name: "aliases collide across plans",
			mutate: func(c *Config) {
				c.MigrationPlans = []MigrationPlan{
					{Version: "510", Aliases: []string{"5.10"}, Templates: map[string][]Operation{"go": {}}},
					{Version: "511", Aliases: []string{"v5.10"}, Templates: map[string][]Operation{"go": {}}},
				}
			},
		},
		{
			name: "alias collides with other canonical version",
			mutate: func(c *Config) {
				c.MigrationPlans = []MigrationPlan{
					{Version: "510", Aliases: []string{"511"}, Templates: map[string][]Operation{"go": {}}},
					{Version: "511", Templates: map[string][]Operation{"go": {}}},
				}
			},
		},
		{
			name: "unknown plan template route",
			mutate: func(c *Config) {
				c.Templates[0].Migration = "go/510"
				c.MigrationPlans = []MigrationPlan{{Version: "510", Templates: map[string][]Operation{"unknown": {}}}}
			},
		},
		{
			name: "unresolved template migration version",
			mutate: func(c *Config) {
				c.Templates[0].Migration = "go/999"
				c.MigrationPlans = []MigrationPlan{{Version: "510", Templates: map[string][]Operation{"go": {}}}}
			},
		},
		{
			name: "unresolved template migration route",
			mutate: func(c *Config) {
				c.Templates[0].Migration = "go/510"
				c.MigrationPlans = []MigrationPlan{{Version: "510", Templates: map[string][]Operation{"java": {}}}}
			},
		},
		{
			name: "malformed template migration",
			mutate: func(c *Config) {
				c.Templates[0].Migration = "Go/510/extra"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validCatalogConfig()
			test.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want rejection")
			}
		})
	}
}

func TestConfigValidationAcceptsTemplateMigrationAlias(t *testing.T) {
	cfg := validCatalogConfig()
	cfg.Templates[0].Migration = "go/v5.10"
	cfg.MigrationPlans = []MigrationPlan{{
		Version:   "510",
		Aliases:   []string{"5.10", "v5.10"},
		Templates: map[string][]Operation{"go": {}},
	}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	route, version, err := parseMigrationRoute(cfg.Templates[0].Migration)
	if err != nil || route != "go" || version != "510" {
		t.Fatalf("parseMigrationRoute() = (%q, %q, %v), want (go, 510, nil)", route, version, err)
	}
}

func TestConfigValidationRequiresMigrationForVersionedTemplates(t *testing.T) {
	cfg := validCatalogConfig()
	cfg.Templates[0].Versioned = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted versioned template without migration")
	}
}

func TestConfigValidationResolvesArbitraryMigrationAliasToCanonicalPlan(t *testing.T) {
	cfg := validCatalogConfig()
	cfg.Templates[0].Versioned = true
	cfg.Templates[0].Migration = "go/stable"
	cfg.MigrationPlans = []MigrationPlan{{
		Version:   "510",
		Aliases:   []string{"stable"},
		Templates: map[string][]Operation{"go": {}},
	}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestConfigValidationRejectsArbitraryMigrationAliasCollision(t *testing.T) {
	cfg := validCatalogConfig()
	cfg.Templates[0].Migration = "go/stable"
	cfg.MigrationPlans = []MigrationPlan{
		{Version: "510", Aliases: []string{"stable"}, Templates: map[string][]Operation{"go": {}}},
		{Version: "511", Aliases: []string{"stable"}, Templates: map[string][]Operation{"go": {}}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted alias collision")
	}
}
