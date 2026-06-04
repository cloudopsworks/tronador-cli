package cli

import "testing"

func TestReposCommandAliasesRepo(t *testing.T) {
	if !hasString(reposCmd.Aliases, "repo") {
		t.Fatalf("repos aliases = %v, want repo", reposCmd.Aliases)
	}
}

func TestRepoAndReposRouteToSameAvailableCommand(t *testing.T) {
	reposAvailable, _, err := rootCmd.Find([]string{"repos", "available"})
	if err != nil {
		t.Fatalf("find repos available: %v", err)
	}
	repoAvailable, _, err := rootCmd.Find([]string{"repo", "available"})
	if err != nil {
		t.Fatalf("find repo available: %v", err)
	}
	reposAvail, _, err := rootCmd.Find([]string{"repos", "avail"})
	if err != nil {
		t.Fatalf("find repos avail: %v", err)
	}
	if repoAvailable != reposAvailable {
		t.Fatalf("repo available routed to %p, want repos available %p", repoAvailable, reposAvailable)
	}
	if reposAvail != reposAvailable {
		t.Fatalf("repos avail routed to %p, want repos available %p", reposAvail, reposAvailable)
	}
}

func TestReposAvailableAliasesAvail(t *testing.T) {
	cmd := newReposAvailableCommand()
	if !hasString(cmd.Aliases, "avail") {
		t.Fatalf("available aliases = %v, want avail", cmd.Aliases)
	}
}

func TestReposCommandSurfaceKeepsMigrateInternal(t *testing.T) {
	if findReposChild("migrate") != nil {
		t.Fatalf("repos migrate is exposed; migration must remain an internal workflow step")
	}
	if findReposChild("recover") == nil {
		t.Fatalf("repos recover is not exposed")
	}
	if err := reposCmd.Args(reposCmd, []string{"migrate"}); err == nil {
		t.Fatalf("repos migrate should be rejected as an unknown public command")
	}
}

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

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func findReposChild(name string) interface{} {
	for _, child := range reposCmd.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}
