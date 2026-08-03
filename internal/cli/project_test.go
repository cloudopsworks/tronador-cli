package cli

import (
	"testing"

	projectpkg "tronador-cli/internal/project"
)

func TestProjectCommandExposesNamespaceFreeGrammar(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"project"})
	if err != nil {
		t.Fatalf("find project command: %v", err)
	}
	if cmd == nil || cmd.Name() != "project" {
		t.Fatalf("project command not found: %#v", cmd)
	}
	if cmd.Flag("workdir") == nil || cmd.Flag("json") == nil || cmd.Flag("allow-network") == nil {
		t.Fatalf("project command is missing shared flags")
	}
	if cmd.SilenceUsage != true || cmd.SilenceErrors != true {
		t.Fatalf("project command must keep structured errors clean")
	}
}

func TestProjectAssignmentParsing(t *testing.T) {
	values, err := parseAssignments([]string{"terraform=1.9.8", "tofu=1.8.3"}, "tool-version")
	if err != nil {
		t.Fatal(err)
	}
	if values["terraform"] != "1.9.8" || values["tofu"] != "1.8.3" {
		t.Fatalf("assignments = %#v", values)
	}
	if _, err := parseAssignments([]string{"terraform"}, "tool-version"); err == nil {
		t.Fatal("malformed assignment unexpectedly accepted")
	}
}

func TestProjectCapabilityFlagNames(t *testing.T) {
	flags := capabilityFlagNames([]projectpkg.FlagDefinition{{Name: "json"}, {Name: "dry-run"}})
	if flags != "--json, --dry-run" {
		t.Fatalf("flag names = %q", flags)
	}
}
