package cli

import "testing"

func TestIACCommandExposesModuleAsPrimarySurface(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"iac", "module"})
	if err != nil {
		t.Fatalf("find iac module: %v", err)
	}
	if cmd == nil || cmd.Name() != "module" {
		t.Fatalf("iac module command not found: %#v", cmd)
	}
}

func TestIACModuleKeepsModuleVersionsAliases(t *testing.T) {
	primary := newIACModuleVersionsCommand()
	if primary.Use != "module" {
		t.Fatalf("Use = %q, want module", primary.Use)
	}
	for _, alias := range []string{"module-versions", "module_versions"} {
		if !hasString(primary.Aliases, alias) {
			t.Fatalf("aliases = %v, want %s", primary.Aliases, alias)
		}
	}

	moduleCmd, _, err := rootCmd.Find([]string{"iac", "module"})
	if err != nil {
		t.Fatalf("find iac module: %v", err)
	}
	moduleVersionsCmd, _, err := rootCmd.Find([]string{"iac", "module-versions"})
	if err != nil {
		t.Fatalf("find iac module-versions alias: %v", err)
	}
	moduleUnderscoreCmd, _, err := rootCmd.Find([]string{"iac", "module_versions"})
	if err != nil {
		t.Fatalf("find iac module_versions alias: %v", err)
	}
	if moduleVersionsCmd != moduleCmd || moduleUnderscoreCmd != moduleCmd {
		t.Fatalf("aliases did not resolve to primary command")
	}
}
