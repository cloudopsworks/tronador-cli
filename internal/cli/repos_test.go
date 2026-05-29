package cli

import "testing"

func TestReposUpgradeCommandIsSingleFullWorkflowSurface(t *testing.T) {
	cmd := newReposUpgradeCommand()
	if got := len(cmd.Commands()); got != 0 {
		names := make([]string, 0, got)
		for _, child := range cmd.Commands() {
			names = append(names, child.Name())
		}
		t.Fatalf("upgrade exposed subcommands %v; want a single upgrade [version] workflow", names)
	}
	if cmd.Use != "upgrade [version]" {
		t.Fatalf("Use = %q, want upgrade [version]", cmd.Use)
	}
	if err := cmd.Args(cmd, nil); err != nil {
		t.Fatalf("upgrade should allow no version: %v", err)
	}
	if err := cmd.Args(cmd, []string{"v5.10.1"}); err != nil {
		t.Fatalf("upgrade should allow one optional version: %v", err)
	}
	if err := cmd.Args(cmd, []string{"v5.10.1", "extra"}); err == nil {
		t.Fatalf("upgrade should reject more than one version argument")
	}
}
