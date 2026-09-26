package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"tronador-cli/internal/versions"
)

func TestVersionsCommandIsRootNamespaceWithCompatibilityAliases(t *testing.T) {
	for _, name := range []string{"versions", "gitflow", "gf", "githubflow", "flow"} {
		command, _, err := rootCmd.Find([]string{name})
		if err != nil || command != versionsCmd {
			t.Fatalf("root alias %q = %v, %v; want versions command", name, command, err)
		}
	}
	initCommand, _, err := rootCmd.Find([]string{"versions", "init"})
	if err != nil || initCommand == nil {
		t.Fatalf("versions init not found: %v", err)
	}
	for _, name := range []string{"gitflow", "githubflow", "trunkbased", "trunk"} {
		if initCommand.Flags().Lookup(name) == nil {
			t.Fatalf("init flag --%s missing", name)
		}
	}
}

func TestVersionsWayOfWorkAcceptsTrunkAliasAndRejectsMultipleFlags(t *testing.T) {
	oldGitFlow, oldGitHubFlow, oldTrunkBased, oldTrunk := versionsGitFlow, versionsGitHubFlow, versionsTrunkBased, versionsTrunk
	t.Cleanup(func() {
		versionsGitFlow, versionsGitHubFlow, versionsTrunkBased, versionsTrunk = oldGitFlow, oldGitHubFlow, oldTrunkBased, oldTrunk
	})
	versionsGitFlow, versionsGitHubFlow, versionsTrunkBased, versionsTrunk = false, false, false, true
	got, err := versionsWayOfWork()
	if err != nil || got != versions.WayOfWorkTrunkBased {
		t.Fatalf("trunk alias = %q, %v", got, err)
	}
	for _, test := range []struct {
		name                                   string
		gitflow, githubflow, trunkbased, trunk bool
	}{
		{name: "gitflow-githubflow", gitflow: true, githubflow: true},
		{name: "gitflow-trunk", gitflow: true, trunk: true},
		{name: "githubflow-trunkbased", githubflow: true, trunkbased: true},
		{name: "literal-trunk-alias-pair", trunkbased: true, trunk: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			versionsGitFlow, versionsGitHubFlow = test.gitflow, test.githubflow
			versionsTrunkBased, versionsTrunk = test.trunkbased, test.trunk
			if _, err := versionsWayOfWork(); err == nil {
				t.Fatal("multiple workflow flags accepted")
			}
		})
	}
}

func TestVersionsWorkflowCommandGrammar(t *testing.T) {
	cases := []struct {
		path []string
		ok   [][]string
		bad  [][]string
	}{
		{[]string{"versions", "feature", "start"}, [][]string{{"name"}}, [][]string{{}, {"one", "two"}}},
		{[]string{"versions", "feature", "publish"}, [][]string{{}, {"name"}}, [][]string{{"one", "two"}}},
		{[]string{"versions", "feature", "finish"}, [][]string{{}, {"name"}}, [][]string{{"one", "two"}}},
		{[]string{"versions", "feature", "purge"}, [][]string{{}, {"name"}}, [][]string{{"one", "two"}}},
		{[]string{"versions", "hotfix", "start"}, [][]string{{}}, [][]string{{"v1.2.3"}}},
		{[]string{"versions", "hotfix", "publish"}, [][]string{{}}, [][]string{{"v1.2.3"}}},
		{[]string{"versions", "hotfix", "finish"}, [][]string{{}}, [][]string{{"v1.2.3"}}},
		{[]string{"versions", "hotfix", "purge"}, [][]string{{}, {"1.2.3"}}, [][]string{{"one", "two"}}},
		{[]string{"versions", "release", "start"}, [][]string{{}}, [][]string{{"patch"}}},
		{[]string{"versions", "release", "publish"}, [][]string{{}, {"v1.2.3"}}, [][]string{{"one", "two"}}},
		{[]string{"versions", "release", "finish"}, [][]string{{}}, [][]string{{"v1.2.3"}}},
		{[]string{"versions", "release", "purge"}, [][]string{{}, {"v1.2.3"}}, [][]string{{"one", "two"}}},
		{[]string{"versions", "support", "start"}, [][]string{{"v1.2.3"}}, [][]string{{}, {"one", "two"}}},
		{[]string{"versions", "support", "publish"}, [][]string{{}, {"v1.2.3"}}, [][]string{{"one", "two"}}},
		{[]string{"versions", "support", "purge"}, [][]string{{}, {"v1.2.3"}}, [][]string{{"one", "two"}}},
		{[]string{"versions", "tag"}, [][]string{{}, {"deploy-test"}}, [][]string{{"one", "two"}}},
	}
	for _, test := range cases {
		t.Run(strings.Join(test.path, " "), func(t *testing.T) {
			command, _, err := rootCmd.Find(test.path)
			if err != nil || command == nil {
				t.Fatalf("find command: %v", err)
			}
			for _, args := range test.ok {
				if err := command.Args(command, args); err != nil {
					t.Fatalf("Args(%q) = %v", args, err)
				}
			}
			for _, args := range test.bad {
				if err := command.Args(command, args); err == nil {
					t.Fatalf("Args(%q) unexpectedly accepted", args)
				}
			}
		})
	}
}

func TestVersionsReleaseStartRequiresExactlyOneBumpFlag(t *testing.T) {
	for _, test := range []struct {
		patch, minor, major bool
		want                string
		valid               bool
	}{
		{patch: true, want: "patch", valid: true}, {minor: true, want: "minor", valid: true}, {major: true, want: "major", valid: true}, {}, {patch: true, minor: true},
	} {
		got, err := releaseBumpKind(test.patch, test.minor, test.major)
		if test.valid && (err != nil || got != test.want) {
			t.Fatalf("releaseBumpKind = %q, %v; want %q", got, err, test.want)
		}
		if !test.valid && err == nil {
			t.Fatalf("releaseBumpKind(%t,%t,%t) unexpectedly accepted", test.patch, test.minor, test.major)
		}
	}
}

func TestVersionsHelpDocumentsRepositorySensitiveCapabilities(t *testing.T) {
	for _, test := range []struct {
		command *cobra.Command
		wants   []string
	}{
		{versionsCmd, []string{"not the project-version generator", "develop", "GitFlow"}},
		{newVersionsFeatureCommand(), []string{"GitFlow", "GitHub Flow", "trunk-based", "develop", "main"}},
		{newVersionsReleaseCommand(), []string{"GitFlow", "GitHub Flow", "trunk-based", "breadcrumbs", "--local"}},
		{newVersionsSupportCommand(), []string{"only", "WayOfWork=gitflow"}},
		{newVersionsTagCommand(), []string{"MajorMinorPatch", "SemVer", "--publish", "+deploy"}},
	} {
		var output bytes.Buffer
		test.command.SetOut(&output)
		if err := test.command.Help(); err != nil {
			t.Fatal(err)
		}
		for _, want := range test.wants {
			if !strings.Contains(output.String(), want) {
				t.Fatalf("help for %s missing %q:\n%s", test.command.Name(), want, output.String())
			}
		}
	}
}

func TestVersionsWorkflowFlagsAreScopedToTheirCapabilities(t *testing.T) {
	for _, path := range [][]string{{"versions", "hotfix", "finish"}, {"versions", "release", "finish"}} {
		command, _, err := rootCmd.Find(path)
		if err != nil || command.Flags().Lookup("local") == nil {
			t.Fatalf("%s lacks --local: %v", strings.Join(path, " "), err)
		}
	}
	start, _, err := rootCmd.Find([]string{"versions", "release", "start"})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"patch", "minor", "major"} {
		if start.Flags().Lookup(flag) == nil {
			t.Fatalf("release start lacks --%s", flag)
		}
	}
	tag, _, err := rootCmd.Find([]string{"versions", "tag"})
	if err != nil || tag.Flags().Lookup("publish") == nil {
		t.Fatalf("tag lacks --publish: %v", err)
	}
}

func TestVersionsWorkflowDryRunNeverCallsAction(t *testing.T) {
	called := false
	command := versionsAction("mutate", "mutate", "mutate", cobra.NoArgs, func(context.Context, *versions.Workflows, []string) error {
		called = true
		return nil
	})
	command.Flags().Bool("dry-run", true, "")
	var output bytes.Buffer
	command.SetOut(&output)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("workflow action was called in dry-run")
	}
	if !strings.Contains(output.String(), "dry-run: would run versions mutate") {
		t.Fatalf("dry-run output = %q", output.String())
	}
}

func TestVersionsTagDryRunDoesNotRequireRepositoryOrMutate(t *testing.T) {
	command := newVersionsTagCommand()
	command.Flags().Bool("dry-run", true, "")
	var output bytes.Buffer
	command.SetOut(&output)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "dry-run: would run versions tag") {
		t.Fatalf("dry-run output = %q", output.String())
	}
}

func TestVersionsMainBranchOverrideIsAvailableToWorkflowCommands(t *testing.T) {
	if versionsCmd.PersistentFlags().Lookup("main-branch") == nil {
		t.Fatal("versions command lacks --main-branch")
	}
}
