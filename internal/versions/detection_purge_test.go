package versions

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func setupLegacyDetectionRepo(t *testing.T, selectors map[WayOfWork]string, active string) string {
	t.Helper()
	dir := t.TempDir()
	gitTest(t, dir, "init")
	gitTest(t, dir, "config", "user.email", "test@example.test")
	gitTest(t, dir, "config", "user.name", "Test")
	config := filepath.Join(dir, cloudOpsWorksDir)
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, wow := range []WayOfWork{WayOfWorkGitFlow, WayOfWorkGitHubFlow, WayOfWorkTrunkBased} {
		writeFile(t, filepath.Join(config, wow.selectorFileName()), selectors[wow])
	}
	writeFile(t, filepath.Join(config, "gitversion.yaml"), active)
	gitTest(t, dir, "add", cloudOpsWorksDir)
	gitTest(t, dir, "commit", "-m", "add workflow configuration")
	return dir
}

func distinctLegacySelectors() map[WayOfWork]string {
	return map[WayOfWork]string{
		WayOfWorkGitFlow:    "mode: ContinuousDelivery\nlegacy-selector: gitflow\n",
		WayOfWorkGitHubFlow: "mode: ContinuousDelivery\nlegacy-selector: githubflow\n",
		WayOfWorkTrunkBased: "mode: ContinuousDelivery\nlegacy-selector: trunkbased\n",
	}
}

func TestCurrentWayOfWorkDetectsUniqueCheckedInLegacySelectors(t *testing.T) {
	selectors := distinctLegacySelectors()
	for _, want := range []WayOfWork{WayOfWorkGitFlow, WayOfWorkGitHubFlow, WayOfWorkTrunkBased} {
		t.Run(string(want), func(t *testing.T) {
			dir := setupLegacyDetectionRepo(t, selectors, selectors[want])
			runner, err := NewRunner(Options{WorkDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			if got, err := runner.CurrentWayOfWork(); err != nil || got != want {
				t.Fatalf("CurrentWayOfWork() = %q, %v; want %q", got, err, want)
			}
			if got, err := runner.CurrentWayOfWorkForPurge(context.Background()); err != nil || got != want {
				t.Fatalf("CurrentWayOfWorkForPurge() = %q, %v; want %q", got, err, want)
			}
		})
	}
}

func TestCurrentWayOfWorkForPurgePrefersOneValidExplicitHeader(t *testing.T) {
	duplicate := "mode: duplicate\n"
	selectors := map[WayOfWork]string{
		WayOfWorkGitFlow: duplicate, WayOfWorkGitHubFlow: duplicate,
		WayOfWorkTrunkBased: "mode: trunk\n",
	}
	dir := setupLegacyDetectionRepo(t, selectors, "# Agents: WayOfWork=githubflow\nmode: active\n")
	runner, err := NewRunner(Options{WorkDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := runner.CurrentWayOfWorkForPurge(context.Background()); err != nil || got != WayOfWorkGitHubFlow {
		t.Fatalf("explicit detection = %q, %v", got, err)
	}

	// Selector state is irrelevant when a clean, valid explicit declaration is
	// authoritative; selector cleanliness is required only for legacy fallback.
	selector := filepath.Join(dir, cloudOpsWorksDir, WayOfWorkGitFlow.selectorFileName())
	writeFile(t, selector, "mode: staged-selector\n")
	gitTest(t, dir, "add", filepath.ToSlash(filepath.Join(cloudOpsWorksDir, WayOfWorkGitFlow.selectorFileName())))
	writeFile(t, selector, duplicate)
	if got, err := runner.CurrentWayOfWorkForPurge(context.Background()); err != nil || got != WayOfWorkGitHubFlow {
		t.Fatalf("explicit detection with dirty irrelevant selector = %q, %v", got, err)
	}
	if err := os.Remove(filepath.Join(dir, cloudOpsWorksDir, WayOfWorkTrunkBased.selectorFileName())); err != nil {
		t.Fatal(err)
	}
	if got, err := runner.CurrentWayOfWorkForPurge(context.Background()); err != nil || got != WayOfWorkGitHubFlow {
		t.Fatalf("explicit detection with missing and dirty irrelevant selectors = %q, %v", got, err)
	}
}

func TestCurrentWayOfWorkForPurgeRejectsUnreliableLegacyDetection(t *testing.T) {
	t.Run("ambiguous", func(t *testing.T) {
		duplicate := "mode: duplicate\n"
		selectors := map[WayOfWork]string{
			WayOfWorkGitFlow: duplicate, WayOfWorkGitHubFlow: duplicate,
			WayOfWorkTrunkBased: "mode: trunk\n",
		}
		dir := setupLegacyDetectionRepo(t, selectors, duplicate)
		runner, _ := NewRunner(Options{WorkDir: dir})
		if _, err := runner.CurrentWayOfWorkForPurge(context.Background()); err == nil || !strings.Contains(err.Error(), "matched 2 checked-in selectors") {
			t.Fatalf("ambiguous detection error = %v", err)
		}
		if got, err := runner.CurrentWayOfWork(); err != nil || got != WayOfWorkGitFlow {
			t.Fatalf("non-purge fallback = %q, %v", got, err)
		}
	})

	t.Run("no-match", func(t *testing.T) {
		dir := setupLegacyDetectionRepo(t, distinctLegacySelectors(), "mode: unknown\n")
		runner, _ := NewRunner(Options{WorkDir: dir})
		if _, err := runner.CurrentWayOfWorkForPurge(context.Background()); err == nil || !strings.Contains(err.Error(), "matched 0 checked-in selectors") {
			t.Fatalf("unmatched detection error = %v", err)
		}
	})

	t.Run("modified-selector", func(t *testing.T) {
		selectors := distinctLegacySelectors()
		dir := setupLegacyDetectionRepo(t, selectors, selectors[WayOfWorkGitHubFlow])
		modified := selectors[WayOfWorkGitHubFlow] + "modified: true\n"
		writeFile(t, filepath.Join(dir, cloudOpsWorksDir, WayOfWorkGitHubFlow.selectorFileName()), modified)
		runner, _ := NewRunner(Options{WorkDir: dir})
		if _, err := runner.CurrentWayOfWorkForPurge(context.Background()); err == nil || !strings.Contains(err.Error(), "gitversion_githubflow.yaml has worktree changes relative to HEAD") {
			t.Fatalf("modified selector detection error = %v", err)
		}
	})

	t.Run("modified-active-config", func(t *testing.T) {
		selectors := distinctLegacySelectors()
		dir := setupLegacyDetectionRepo(t, selectors, selectors[WayOfWorkGitHubFlow])
		writeFile(t, filepath.Join(dir, cloudOpsWorksDir, "gitversion.yaml"), selectors[WayOfWorkTrunkBased])
		runner, _ := NewRunner(Options{WorkDir: dir})
		if _, err := runner.CurrentWayOfWorkForPurge(context.Background()); err == nil || !strings.Contains(err.Error(), "gitversion.yaml has worktree changes relative to HEAD") {
			t.Fatalf("modified active config detection error = %v", err)
		}
	})

	t.Run("staged-active-config-cancelled-in-worktree", func(t *testing.T) {
		selectors := distinctLegacySelectors()
		active := selectors[WayOfWorkGitHubFlow]
		dir := setupLegacyDetectionRepo(t, selectors, active)
		path := filepath.Join(dir, cloudOpsWorksDir, "gitversion.yaml")
		writeFile(t, path, selectors[WayOfWorkTrunkBased])
		gitTest(t, dir, "add", filepath.ToSlash(filepath.Join(cloudOpsWorksDir, "gitversion.yaml")))
		writeFile(t, path, active)
		runner, _ := NewRunner(Options{WorkDir: dir})
		if _, err := runner.CurrentWayOfWorkForPurge(context.Background()); err == nil || !strings.Contains(err.Error(), "gitversion.yaml has staged changes relative to HEAD") {
			t.Fatalf("staged active config detection error = %v", err)
		}
	})

	t.Run("staged-selector-cancelled-in-worktree", func(t *testing.T) {
		selectors := distinctLegacySelectors()
		dir := setupLegacyDetectionRepo(t, selectors, selectors[WayOfWorkGitHubFlow])
		name := WayOfWorkGitHubFlow.selectorFileName()
		path := filepath.Join(dir, cloudOpsWorksDir, name)
		writeFile(t, path, "mode: staged-selector\n")
		gitTest(t, dir, "add", filepath.ToSlash(filepath.Join(cloudOpsWorksDir, name)))
		writeFile(t, path, selectors[WayOfWorkGitHubFlow])
		runner, _ := NewRunner(Options{WorkDir: dir})
		if _, err := runner.CurrentWayOfWorkForPurge(context.Background()); err == nil || !strings.Contains(err.Error(), name+" has staged changes relative to HEAD") {
			t.Fatalf("staged selector detection error = %v", err)
		}
	})

	t.Run("staged-selector-mode-cancelled-in-worktree", func(t *testing.T) {
		selectors := distinctLegacySelectors()
		dir := setupLegacyDetectionRepo(t, selectors, selectors[WayOfWorkGitHubFlow])
		gitTest(t, dir, "config", "core.filemode", "true")
		name := WayOfWorkGitFlow.selectorFileName()
		path := filepath.Join(dir, cloudOpsWorksDir, name)
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
		gitTest(t, dir, "add", filepath.ToSlash(filepath.Join(cloudOpsWorksDir, name)))
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		runner, _ := NewRunner(Options{WorkDir: dir})
		if _, err := runner.CurrentWayOfWorkForPurge(context.Background()); err == nil || !strings.Contains(err.Error(), name+" has staged changes relative to HEAD") {
			t.Fatalf("staged selector mode detection error = %v", err)
		}
	})

	t.Run("git-status-check-error", func(t *testing.T) {
		selectors := distinctLegacySelectors()
		dir := setupLegacyDetectionRepo(t, selectors, selectors[WayOfWorkGitHubFlow])
		fakeGit := filepath.Join(t.TempDir(), "git")
		writeFile(t, fakeGit, "#!/bin/sh\necho status-check-failed >&2\nexit 2\n")
		if err := os.Chmod(fakeGit, 0o755); err != nil {
			t.Fatal(err)
		}
		runner, err := NewRunner(Options{WorkDir: dir, GitPath: fakeGit})
		if err != nil {
			t.Fatal(err)
		}
		_, err = runner.CurrentWayOfWorkForPurge(context.Background())
		if err == nil || !strings.Contains(err.Error(), "check staged state") || !strings.Contains(err.Error(), "status-check-failed") {
			t.Fatalf("git status check error = %v", err)
		}
	})

	t.Run("duplicate-headers", func(t *testing.T) {
		active := "# Agents: WayOfWork=githubflow\n# Agents: WayOfWork=gitflow\nmode: active\n"
		dir := setupLegacyDetectionRepo(t, distinctLegacySelectors(), active)
		runner, _ := NewRunner(Options{WorkDir: dir})
		if _, err := runner.CurrentWayOfWorkForPurge(context.Background()); err == nil || !strings.Contains(err.Error(), "ambiguous WayOfWork headers") {
			t.Fatalf("duplicate header detection error = %v", err)
		}
	})

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
			dir := setupLegacyDetectionRepo(t, distinctLegacySelectors(), test.active)
			runner, _ := NewRunner(Options{WorkDir: dir})
			if _, err := runner.CurrentWayOfWorkForPurge(context.Background()); err == nil || !strings.Contains(err.Error(), "malformed WayOfWork header") {
				t.Fatalf("mixed valid/malformed header detection error = %v", err)
			}
		})
	}
}

func setupHeaderlessGitHubFlowPurgeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	gitTest(t, root, "init", "--bare", remote)
	repo := filepath.Join(root, "repo")
	gitTest(t, root, "clone", remote, repo)
	gitTest(t, repo, "config", "user.email", "test@example.test")
	gitTest(t, repo, "config", "user.name", "Test")
	gitTest(t, repo, "checkout", "-b", "master")
	selectors := distinctLegacySelectors()
	config := filepath.Join(repo, cloudOpsWorksDir)
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, wow := range []WayOfWork{WayOfWorkGitFlow, WayOfWorkGitHubFlow, WayOfWorkTrunkBased} {
		writeFile(t, filepath.Join(config, wow.selectorFileName()), selectors[wow])
	}
	writeFile(t, filepath.Join(config, "gitversion.yaml"), selectors[WayOfWorkGitHubFlow])
	gitTest(t, repo, "add", cloudOpsWorksDir)
	gitTest(t, repo, "commit", "-m", "add headerless GitHubFlow config")
	gitTest(t, repo, "push", "-u", "origin", "master")
	gitTest(t, remote, "symbolic-ref", "HEAD", "refs/heads/master")
	gitTest(t, repo, "remote", "set-head", "origin", "master")

	gitTest(t, repo, "checkout", "-b", "feature/stale")
	writeFile(t, filepath.Join(repo, "feature.txt"), "merged feature\n")
	gitTest(t, repo, "add", "feature.txt")
	gitTest(t, repo, "commit", "-m", "add merged feature")
	gitTest(t, repo, "push", "-u", "origin", "feature/stale")
	gitTest(t, repo, "checkout", "--no-guess", "master")
	gitTest(t, repo, "merge", "--no-ff", "feature/stale", "-m", "merge feature")
	gitTest(t, repo, "push", "origin", "master")
	gitTest(t, repo, "checkout", "--no-guess", "feature/stale")
	return repo
}

func TestHeaderlessGitHubFlowFeaturePurgeUsesMasterNotDevelop(t *testing.T) {
	for _, mainBranch := range []string{"", "master"} {
		name := "implicit-primary"
		if mainBranch != "" {
			name = "main-branch-override"
		}
		t.Run(name, func(t *testing.T) {
			repo := setupHeaderlessGitHubFlowPurgeRepo(t)
			runner, err := NewRunner(Options{WorkDir: repo, MainBranch: mainBranch})
			if err != nil {
				t.Fatal(err)
			}
			wow, err := runner.CurrentWayOfWorkForPurge(context.Background())
			if err != nil || wow != WayOfWorkGitHubFlow {
				t.Fatalf("purge detection = %q, %v", wow, err)
			}
			workflow, err := runner.Workflow(wow)
			if err != nil {
				t.Fatal(err)
			}
			if err = workflow.FeaturePurge(context.Background(), "stale"); err != nil {
				t.Fatal(err)
			}
			assertBranchAbsent(t, repo, "feature/stale")
			if current := strings.TrimSpace(gitTest(t, repo, "branch", "--show-current")); current != "master" {
				t.Fatalf("current branch = %q, want master", current)
			}
			develop := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/remotes/origin/develop")
			develop.Dir = repo
			if err := develop.Run(); err == nil {
				t.Fatal("unexpected origin/develop")
			}
		})
	}
}
