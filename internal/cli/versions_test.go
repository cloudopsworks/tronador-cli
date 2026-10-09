package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestVersionsToolFlagsAreInheritedByAliasesAndSubcommands(t *testing.T) {
	flags := []string{"tools-dir", "tools-config", "no-install-tools", "allow-network", "tool-version", "tool-path"}
	for _, name := range flags {
		if versionsCmd.PersistentFlags().Lookup(name) == nil {
			t.Fatalf("versions lacks persistent --%s", name)
		}
	}
	for _, path := range [][]string{
		{"versions", "hotfix", "start"},
		{"gf", "release", "finish"},
		{"flow", "tag"},
	} {
		command, _, err := rootCmd.Find(path)
		if err != nil {
			t.Fatalf("find %s: %v", strings.Join(path, " "), err)
		}
		for _, name := range flags {
			if command.InheritedFlags().Lookup(name) == nil {
				t.Fatalf("%s does not inherit --%s", strings.Join(path, " "), name)
			}
		}
	}
}

func TestVersionsGuardFailuresPrintOnceForAliases(t *testing.T) {
	oldGitFlow, oldGitHubFlow := versionsGitFlow, versionsGitHubFlow
	t.Cleanup(func() {
		versionsGitFlow, versionsGitHubFlow = oldGitFlow, oldGitHubFlow
		rootCmd.SetArgs(nil)
	})

	var stderr bytes.Buffer
	rootCmd.SetArgs([]string{"gf", "init", "--gitflow", "--githubflow"})
	rootCmd.SetErr(&stderr)
	defer rootCmd.SetErr(nil)

	_, err := rootCmd.ExecuteC()
	if err == nil {
		t.Fatal("versions init with conflicting workflow flags unexpectedly succeeded")
	}
	const want = "only one of --gitflow, --githubflow, --trunkbased, or --trunk may be used"
	if got := strings.Count(stderr.String(), want); got != 1 {
		t.Fatalf("stderr mentions guard failure %d times; want once:\n%s", got, stderr.String())
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

func TestVersionsReleaseStartUsesGitVersionCalculatedVersion(t *testing.T) {
	release := newVersionsReleaseCommand()
	start, _, err := release.Find([]string{"start"})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	start.SetOut(&output)
	if err := start.Help(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "GitVersion's calculated MajorMinorPatch") {
		t.Fatalf("release start help omits GitVersion version source:\n%s", output.String())
	}
	for _, flag := range []string{"patch", "minor", "major"} {
		if start.Flags().Lookup(flag) != nil {
			t.Fatalf("release start unexpectedly exposes --%s", flag)
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
		if start.Flags().Lookup(flag) != nil {
			t.Fatalf("release start unexpectedly exposes --%s", flag)
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
	if !strings.Contains(output.String(), "Dry-run plan for versions mutate (no changes made):") || !strings.Contains(output.String(), "1. Run the mutate workflow") {
		t.Fatalf("dry-run output = %q", output.String())
	}
}

func TestVersionsDryRunPlanIncludesSubcommandAndResolvedArguments(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"versions", "feature", "start"})
	if err != nil {
		t.Fatal(err)
	}
	plan := versionsActionPlan(command, command.Use, []string{"demo"})
	var output bytes.Buffer
	writeVersionsPlan(&output, versionsCommandLabel(command, command.Use), plan)
	for _, want := range []string{"Dry-run plan for versions feature start (no changes made):", "1. Select the feature base branch", "3. Create and check out feature/demo"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("dry-run plan missing %q:\n%s", want, output.String())
		}
	}
}

func TestVersionsReleaseStartDryRunPlanUsesGitVersionConfiguration(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"versions", "release", "start"})
	if err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(versionsActionPlan(command, command.Use, nil), "\n")
	for _, want := range []string{"GitVersion's calculated MajorMinorPatch unchanged", "configuration determines the version increment"} {
		if !strings.Contains(plan, want) {
			t.Fatalf("release start dry-run plan missing %q:\n%s", want, plan)
		}
	}
}

func TestVersionsTagDryRunDoesNotRequireRepositoryOrMutate(t *testing.T) {
	restoreVersionsToolOptions(t)
	cache := filepath.Join(t.TempDir(), "missing-cache")
	badConfig := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(badConfig, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	versionsToolsDir, versionsToolsConfig = cache, badConfig
	versionsAllowNetwork = true
	versionsToolVersions = []string{"malformed"}
	versionsToolPaths = []string{"also-malformed"}
	command := newVersionsTagCommand()
	command.Flags().Bool("dry-run", true, "")
	var output bytes.Buffer
	command.SetOut(&output)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Dry-run plan for versions tag (no changes made):") || !strings.Contains(output.String(), "Create the annotated tag locally") {
		t.Fatalf("dry-run output = %q", output.String())
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("dry-run created tool cache: %v", err)
	}
}

func TestVersionsTagCLIUsesProjectStyleToolResolution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtures")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}

	t.Run("explicit path", func(t *testing.T) {
		repo := setupVersionsTagRepo(t)
		restoreVersionsToolOptions(t)
		versionsWorkDir, versionsGitPath, versionsMainBranch = repo, gitPath, "main"
		versionsNoInstallTools = true
		versionsToolPaths = []string{"gitversion=" + writeCLITool(t, t.TempDir(), "gitversion", "1.2.3")}
		stdout, _ := executeVersionsTagCommand(t)
		if stdout != "v1.2.3\n" {
			t.Fatalf("stdout = %q", stdout)
		}
	})

	t.Run("PATH wins over configured version", func(t *testing.T) {
		repo := setupVersionsTagRepo(t)
		restoreVersionsToolOptions(t)
		bin := t.TempDir()
		writeCLITool(t, bin, "gitversion", "2.3.4")
		t.Setenv("PATH", bin)
		versionsWorkDir, versionsGitPath, versionsMainBranch = repo, gitPath, "main"
		versionsToolVersions = []string{"gitversion=99.0.0"}
		stdout, _ := executeVersionsTagCommand(t)
		if stdout != "v2.3.4\n" {
			t.Fatalf("stdout = %q", stdout)
		}
	})

	t.Run("cache without network", func(t *testing.T) {
		repo := setupVersionsTagRepo(t)
		restoreVersionsToolOptions(t)
		cache := t.TempDir()
		writeCLITool(t, cache, "gitversion", "3.4.5")
		t.Setenv("PATH", t.TempDir())
		versionsWorkDir, versionsGitPath, versionsMainBranch = repo, gitPath, "main"
		versionsToolsDir, versionsNoInstallTools = cache, true
		stdout, _ := executeVersionsTagCommand(t)
		if stdout != "v3.4.5\n" {
			t.Fatalf("stdout = %q", stdout)
		}
	})

	t.Run("missing tool is actionable and does not create cache", func(t *testing.T) {
		repo := setupVersionsTagRepo(t)
		restoreVersionsToolOptions(t)
		cache := filepath.Join(t.TempDir(), "missing-cache")
		t.Setenv("PATH", t.TempDir())
		versionsWorkDir, versionsGitPath, versionsMainBranch = repo, gitPath, "main"
		versionsToolsDir = cache
		_, _, err := executeVersionsTagCommandError(t)
		if err == nil || !strings.Contains(err.Error(), "--allow-network") || !strings.Contains(err.Error(), "gitversion") {
			t.Fatalf("error = %v; want actionable GitVersion error", err)
		}
		if _, statErr := os.Stat(cache); !os.IsNotExist(statErr) {
			t.Fatalf("missing-tool path created cache: %v", statErr)
		}
	})

	t.Run("download uses configured version and keeps stdout clean", func(t *testing.T) {
		repo := setupVersionsTagRepo(t)
		restoreVersionsToolOptions(t)
		requested := make(chan string, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requested <- r.URL.Path
			_, _ = w.Write([]byte("#!/bin/sh\nprintf '4.5.6\\n'\n"))
		}))
		defer server.Close()
		downloadURL := server.URL + "/{version}/gitversion"
		config := fmt.Sprintf(`{"tools":[{"name":"gitversion","executable":"gitversion","default_version":"1.0.0","url_template":%q,"format":"binary","platform_overrides":{%q:{"url_template":%q,"format":"binary"}}}]}`,
			downloadURL, runtime.GOOS+"/"+runtime.GOARCH, downloadURL)
		configPath := filepath.Join(t.TempDir(), "tools.json")
		if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", t.TempDir())
		versionsWorkDir, versionsGitPath, versionsMainBranch = repo, gitPath, "main"
		versionsToolsDir, versionsToolsConfig = filepath.Join(t.TempDir(), "cache"), configPath
		versionsAllowNetwork = true
		versionsToolVersions = []string{"gitversion=7.8.9"}
		stdout, stderr := executeVersionsTagCommand(t)
		if stdout != "v4.5.6\n" {
			t.Fatalf("stdout = %q", stdout)
		}
		if path := <-requested; path != "/7.8.9/gitversion" {
			t.Fatalf("request path = %q", path)
		}
		if !strings.Contains(stderr, "Installing gitversion 7.8.9") {
			t.Fatalf("stderr = %q; want install diagnostic", stderr)
		}
	})
}

func TestVersionsWorkflowRejectsMalformedToolAssignments(t *testing.T) {
	restoreVersionsToolOptions(t)
	versionsWorkDir = t.TempDir()
	versionsToolPaths = []string{"gitversion"}
	command := &cobra.Command{Use: "test"}
	if _, err := newVersionsWorkflow(command, false); err == nil || !strings.Contains(err.Error(), "--tool-path expects name=value") {
		t.Fatalf("malformed assignment error = %v", err)
	}
}

func TestVersionsMainBranchOverrideIsAvailableToWorkflowCommands(t *testing.T) {
	if versionsCmd.PersistentFlags().Lookup("main-branch") == nil {
		t.Fatal("versions command lacks --main-branch")
	}
}

func TestVersionsPurgeStopsBeforeActionWhenLegacyWayOfWorkIsAmbiguous(t *testing.T) {
	dir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = dir
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
		}
	}
	runGit("init")
	runGit("config", "user.email", "test@example.test")
	runGit("config", "user.name", "Test")
	config := filepath.Join(dir, ".cloudopsworks")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"gitversion_gitflow.yaml":    "mode: duplicate\n",
		"gitversion_githubflow.yaml": "mode: duplicate\n",
		"gitversion_trunkbased.yaml": "mode: trunk\n",
		"gitversion.yaml":            "mode: duplicate\n",
	} {
		if err := os.WriteFile(filepath.Join(config, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit("add", ".cloudopsworks")
	runGit("commit", "-m", "ambiguous legacy selectors")

	oldWorkDir, oldGitPath, oldMain := versionsWorkDir, versionsGitPath, versionsMainBranch
	versionsWorkDir, versionsGitPath, versionsMainBranch = dir, "git", ""
	t.Cleanup(func() {
		versionsWorkDir, versionsGitPath, versionsMainBranch = oldWorkDir, oldGitPath, oldMain
	})
	actionCalled := false
	command := versionsPurgeAction("purge", "purge", "purge", cobra.NoArgs, func(context.Context, *versions.Workflows, []string) error {
		actionCalled = true
		return nil
	})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "matched 2 checked-in selectors") {
		t.Fatalf("purge detection error = %v", err)
	}
	if actionCalled {
		t.Fatal("purge action ran after ambiguous WayOfWork detection")
	}
}

func TestVersionsPurgeStopsBeforeActionWhenLegacySelectorIsMissing(t *testing.T) {
	dir := t.TempDir()
	cliGit(t, dir, "init")
	cliGit(t, dir, "config", "user.email", "test@example.test")
	cliGit(t, dir, "config", "user.name", "Test")
	config := filepath.Join(dir, ".cloudopsworks")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"gitversion_gitflow.yaml":    "mode: gitflow\n",
		"gitversion_trunkbased.yaml": "mode: trunk\n",
		"gitversion.yaml":            "mode: githubflow\n",
	} {
		if err := os.WriteFile(filepath.Join(config, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cliGit(t, dir, "add", ".cloudopsworks")
	cliGit(t, dir, "commit", "-m", "missing legacy selector")

	oldWorkDir, oldGitPath, oldMain := versionsWorkDir, versionsGitPath, versionsMainBranch
	versionsWorkDir, versionsGitPath, versionsMainBranch = dir, "git", ""
	t.Cleanup(func() {
		versionsWorkDir, versionsGitPath, versionsMainBranch = oldWorkDir, oldGitPath, oldMain
	})
	actionCalled := false
	command := versionsPurgeAction("purge", "purge", "purge", cobra.NoArgs, func(context.Context, *versions.Workflows, []string) error {
		actionCalled = true
		return nil
	})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "gitversion_githubflow.yaml") {
		t.Fatalf("purge missing-selector error = %v", err)
	}
	if actionCalled {
		t.Fatal("purge action ran after missing selector detection")
	}
}

func TestVersionsPurgeStopsBeforeActionForValidAndMalformedHeaders(t *testing.T) {
	for _, test := range []struct {
		name   string
		active string
	}{
		{
			name:   "valid-before-malformed",
			active: "# Agents: WayOfWork=githubflow\n# Agents: WayOfWork=gitflow # trailing junk\nmode: active\n",
		},
		{
			name:   "malformed-before-valid",
			active: "# Agents: WayOfWork=gitflow # trailing junk\n# Agents: WayOfWork=githubflow\nmode: active\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			cliGit(t, dir, "init")
			cliGit(t, dir, "config", "user.email", "test@example.test")
			cliGit(t, dir, "config", "user.name", "Test")
			config := filepath.Join(dir, ".cloudopsworks")
			if err := os.MkdirAll(config, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, content := range map[string]string{
				"gitversion_gitflow.yaml":    "mode: gitflow\n",
				"gitversion_githubflow.yaml": "mode: githubflow\n",
				"gitversion_trunkbased.yaml": "mode: trunk\n",
				"gitversion.yaml":            test.active,
			} {
				if err := os.WriteFile(filepath.Join(config, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cliGit(t, dir, "add", ".cloudopsworks")
			cliGit(t, dir, "commit", "-m", "mixed WayOfWork headers")

			oldWorkDir, oldGitPath, oldMain := versionsWorkDir, versionsGitPath, versionsMainBranch
			versionsWorkDir, versionsGitPath, versionsMainBranch = dir, "git", ""
			t.Cleanup(func() {
				versionsWorkDir, versionsGitPath, versionsMainBranch = oldWorkDir, oldGitPath, oldMain
			})
			actionCalled := false
			command := versionsPurgeAction("purge", "purge", "purge", cobra.NoArgs, func(context.Context, *versions.Workflows, []string) error {
				actionCalled = true
				return nil
			})
			if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "malformed WayOfWork header") {
				t.Fatalf("purge mixed-header detection error = %v", err)
			}
			if actionCalled {
				t.Fatal("purge action ran after malformed extra WayOfWork header")
			}
		})
	}
}

func TestVersionsPurgeStopsBeforeActionForStagedConfigCancelledInWorktree(t *testing.T) {
	dir := t.TempDir()
	cliGit(t, dir, "init")
	cliGit(t, dir, "config", "user.email", "test@example.test")
	cliGit(t, dir, "config", "user.name", "Test")
	config := filepath.Join(dir, ".cloudopsworks")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	active := "# Agents: WayOfWork=githubflow\nmode: active\n"
	for name, content := range map[string]string{
		"gitversion_gitflow.yaml":    "mode: gitflow\n",
		"gitversion_githubflow.yaml": "mode: githubflow\n",
		"gitversion_trunkbased.yaml": "mode: trunk\n",
		"gitversion.yaml":            active,
	} {
		if err := os.WriteFile(filepath.Join(config, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cliGit(t, dir, "add", ".cloudopsworks")
	cliGit(t, dir, "commit", "-m", "add workflow metadata")
	activePath := filepath.Join(config, "gitversion.yaml")
	if err := os.WriteFile(activePath, []byte("# Agents: WayOfWork=trunkbased\nmode: staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cliGit(t, dir, "add", ".cloudopsworks/gitversion.yaml")
	if err := os.WriteFile(activePath, []byte(active), 0o644); err != nil {
		t.Fatal(err)
	}

	oldWorkDir, oldGitPath, oldMain := versionsWorkDir, versionsGitPath, versionsMainBranch
	versionsWorkDir, versionsGitPath, versionsMainBranch = dir, "git", ""
	t.Cleanup(func() {
		versionsWorkDir, versionsGitPath, versionsMainBranch = oldWorkDir, oldGitPath, oldMain
	})
	actionCalled := false
	command := versionsPurgeAction("purge", "purge", "purge", cobra.NoArgs, func(context.Context, *versions.Workflows, []string) error {
		actionCalled = true
		return nil
	})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "gitversion.yaml has staged changes relative to HEAD") {
		t.Fatalf("purge staged-config detection error = %v", err)
	}
	if actionCalled {
		t.Fatal("purge action ran after staged config was cancelled in the worktree")
	}
}

func cliGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func setupCLIHeaderlessPurgeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	cliGit(t, root, "init", "--bare", remote)
	repo := filepath.Join(root, "repo")
	cliGit(t, root, "clone", remote, repo)
	cliGit(t, repo, "config", "user.email", "test@example.test")
	cliGit(t, repo, "config", "user.name", "Test")
	cliGit(t, repo, "checkout", "-b", "master")
	config := filepath.Join(repo, ".cloudopsworks")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	selectors := map[string]string{
		"gitversion_gitflow.yaml":    "mode: ContinuousDelivery\nlegacy-selector: gitflow\n",
		"gitversion_githubflow.yaml": "mode: ContinuousDelivery\nlegacy-selector: githubflow\n",
		"gitversion_trunkbased.yaml": "mode: ContinuousDelivery\nlegacy-selector: trunkbased\n",
	}
	for name, content := range selectors {
		if err := os.WriteFile(filepath.Join(config, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(config, "gitversion.yaml"), []byte(selectors["gitversion_githubflow.yaml"]), 0o644); err != nil {
		t.Fatal(err)
	}
	cliGit(t, repo, "add", ".cloudopsworks")
	cliGit(t, repo, "commit", "-m", "add headerless GitHubFlow config")
	cliGit(t, repo, "push", "-u", "origin", "master")
	cliGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/master")
	cliGit(t, repo, "remote", "set-head", "origin", "master")
	cliGit(t, repo, "checkout", "-b", "feature/stale")
	if err := os.WriteFile(filepath.Join(repo, "feature.txt"), []byte("merged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cliGit(t, repo, "add", "feature.txt")
	cliGit(t, repo, "commit", "-m", "add merged feature")
	cliGit(t, repo, "push", "-u", "origin", "feature/stale")
	cliGit(t, repo, "checkout", "--no-guess", "master")
	cliGit(t, repo, "merge", "--no-ff", "feature/stale", "-m", "merge feature")
	cliGit(t, repo, "push", "origin", "master")
	cliGit(t, repo, "checkout", "--no-guess", "feature/stale")
	return repo
}

func TestVersionsFeaturePurgeCLIHandlesHeaderlessGitHubFlowOnMaster(t *testing.T) {
	for _, override := range []bool{false, true} {
		name := "implicit-primary"
		if override {
			name = "main-branch-master"
		}
		t.Run(name, func(t *testing.T) {
			repo := setupCLIHeaderlessPurgeRepo(t)
			oldWorkDir, oldGitPath, oldMain := versionsWorkDir, versionsGitPath, versionsMainBranch
			versionsWorkDir, versionsGitPath, versionsMainBranch = repo, "git", ""
			t.Cleanup(func() {
				versionsWorkDir, versionsGitPath, versionsMainBranch = oldWorkDir, oldGitPath, oldMain
			})

			command := &cobra.Command{Use: "versions"}
			command.PersistentFlags().StringVar(&versionsMainBranch, "main-branch", "", "primary branch")
			command.AddCommand(newVersionsFeatureCommand())
			args := []string{"feature", "purge", "stale"}
			if override {
				args = append(args, "--main-branch", "master")
			}
			command.SetArgs(args)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if current := strings.TrimSpace(cliGit(t, repo, "branch", "--show-current")); current != "master" {
				t.Fatalf("current branch = %q, want master", current)
			}
			for _, ref := range []string{"refs/heads/feature/stale", "refs/remotes/origin/feature/stale"} {
				check := exec.Command("git", "show-ref", "--verify", "--quiet", ref)
				check.Dir = repo
				if err := check.Run(); err == nil {
					t.Fatalf("purged ref remains: %s", ref)
				}
			}
		})
	}
}

func restoreVersionsToolOptions(t *testing.T) {
	t.Helper()
	oldWorkDir, oldGitPath, oldMainBranch := versionsWorkDir, versionsGitPath, versionsMainBranch
	oldToolsDir, oldToolsConfig := versionsToolsDir, versionsToolsConfig
	oldNoInstall, oldAllowNetwork := versionsNoInstallTools, versionsAllowNetwork
	oldVersions := append([]string(nil), versionsToolVersions...)
	oldPaths := append([]string(nil), versionsToolPaths...)
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() {
		versionsWorkDir, versionsGitPath, versionsMainBranch = oldWorkDir, oldGitPath, oldMainBranch
		versionsToolsDir, versionsToolsConfig = oldToolsDir, oldToolsConfig
		versionsNoInstallTools, versionsAllowNetwork = oldNoInstall, oldAllowNetwork
		versionsToolVersions, versionsToolPaths = oldVersions, oldPaths
	})
	versionsToolsDir, versionsToolsConfig = "", ""
	versionsNoInstallTools, versionsAllowNetwork = false, false
	versionsToolVersions, versionsToolPaths = nil, nil
}

func setupVersionsTagRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	repo := filepath.Join(root, "repo")
	cliGit(t, root, "init", "--bare", remote)
	cliGit(t, root, "init", repo)
	cliGit(t, repo, "config", "user.email", "test@example.test")
	cliGit(t, repo, "config", "user.name", "Test")
	cliGit(t, repo, "checkout", "-b", "main")
	configDir := filepath.Join(repo, ".cloudopsworks")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "gitversion.yaml"), []byte("# Agents: WayOfWork=githubflow\nmode: ContinuousDelivery\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cliGit(t, repo, "add", ".")
	cliGit(t, repo, "commit", "-m", "initial")
	cliGit(t, repo, "remote", "add", "origin", remote)
	cliGit(t, repo, "push", "-u", "origin", "main")
	cliGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	return repo
}

func writeCLITool(t *testing.T, dir, name, output string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' '"+output+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func executeVersionsTagCommand(t *testing.T) (string, string) {
	t.Helper()
	stdout, stderr, err := executeVersionsTagCommandError(t)
	if err != nil {
		t.Fatal(err)
	}
	return stdout, stderr
}

func executeVersionsTagCommandError(t *testing.T) (string, string, error) {
	t.Helper()
	command := newVersionsTagCommand()
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs(nil)
	err := command.Execute()
	return stdout.String(), stderr.String(), err
}
