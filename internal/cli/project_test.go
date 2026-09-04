package cli

import (
	"bytes"
	"strings"
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
	versionCmd, _, err := rootCmd.Find([]string{"project", "version"})
	if err != nil || versionCmd == nil || versionCmd.Name() != "version" || versionCmd.Flag("snapshot") == nil || versionCmd.Flag("plain") == nil {
		t.Fatalf("project version command is missing version flags: cmd=%v err=%v", versionCmd, err)
	}
	if cmd.SilenceUsage != true || cmd.SilenceErrors != true {
		t.Fatalf("project command must keep structured errors clean")
	}
}

func TestProjectSnapshotIsLimitedToVersion(t *testing.T) {
	if err := validateSnapshotCapability("version", true); err != nil {
		t.Fatalf("version snapshot validation = %v", err)
	}
	for _, capability := range []string{"detect", "capabilities", "init"} {
		if err := validateSnapshotCapability(capability, true); err == nil {
			t.Fatalf("%s unexpectedly accepted --snapshot", capability)
		}
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

func TestProjectPlainIsLimitedToVersion(t *testing.T) {
	if err := validatePlainCapability("version", true); err != nil {
		t.Fatalf("version plain validation = %v", err)
	}
	for _, capability := range []string{"detect", "capabilities", "init"} {
		if err := validatePlainCapability(capability, true); err == nil {
			t.Fatalf("%s unexpectedly accepted --plain", capability)
		}
	}
}

func TestProjectVersionHelpDocumentsOnlyVersionOptions(t *testing.T) {
	var output bytes.Buffer
	projectVersionCmd.SetOut(&output)
	t.Cleanup(func() { projectVersionCmd.SetOut(nil) })
	if err := projectVersionCmd.Help(); err != nil {
		t.Fatalf("version help: %v", err)
	}
	help := output.String()
	for _, want := range []string{"Generate and write the detected project's version.", "--plain", "Node and Python projects", "--snapshot", "Java", "MajorMinorPatch", "dry-run"} {
		if !strings.Contains(help, want) {
			t.Fatalf("version help missing %q:\n%s", want, help)
		}
	}
	for _, unwanted := range []string{"--engine", "--yes"} {
		if strings.Contains(help, unwanted) {
			t.Fatalf("version help includes unrelated %q:\n%s", unwanted, help)
		}
	}
}
