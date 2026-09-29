package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

func TestVersionsReleaseStartDefaultsToMinorAndAllowsOneBumpFlag(t *testing.T) {
	for _, test := range []struct {
		patch, minor, major bool
		want                string
		valid               bool
	}{
		{patch: true, want: "patch", valid: true}, {minor: true, want: "minor", valid: true}, {major: true, want: "major", valid: true}, {want: "minor", valid: true}, {patch: true, minor: true},
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

func TestVersionsReleaseStartHelpDocumentsMinorDefault(t *testing.T) {
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
	if !strings.Contains(output.String(), "defaults to --minor") {
		t.Fatalf("release start help omits default bump:\n%s", output.String())
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
