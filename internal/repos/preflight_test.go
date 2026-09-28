package repos

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests deliberately call the immutable preflight rather than Stack: the
// contract under test is that rejection happens before Clean, staging, or the
// release-marker write can be reached.
func TestStackMutationPreflightRejectsUnsafeTreesBeforeExecution(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string)
		opts  StackOptions
	}{
		{
			name: "migration destination ancestor symlink",
			setup: func(t *testing.T, dir string) {
				mustWrite(t, filepath.Join(dir, ".github", "vars", "inner.yaml"), "x: y\n")
				mustSymlink(t, filepath.Join(dir, "outside"), filepath.Join(dir, ".cloudopsworks"))
			},
			opts: StackOptions{Template: Template{Name: "go", Merge: true, Versioned: true}, State: preflightLegacyState(""), V510Plus: "v5.10"},
		},
		{
			name: "migration marker destination symlink after ensure directory",
			setup: func(t *testing.T, dir string) {
				mustWrite(t, filepath.Join(dir, ".github", ".golang"), "legacy marker\n")
				mustWrite(t, filepath.Join(dir, "outside"), "x\n")
				mustSymlink(t, filepath.Join(dir, "outside"), filepath.Join(dir, ".cloudopsworks", ".golang"))
			},
			opts: StackOptions{Template: Template{Name: "go", Merge: true, Versioned: true}, State: preflightLegacyState(""), V510Plus: "v5.10"},
		},
		{
			name: "empty migration directory destination collision",
			setup: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, ".github", "vars"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(dir, ".cloudopsworks", "vars"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			opts: StackOptions{Template: Template{Name: "go", Merge: true, Versioned: true}, State: preflightLegacyState(""), V510Plus: "v5.10"},
		},
		{
			name: "migration source symlink",
			setup: func(t *testing.T, dir string) {
				mustWrite(t, filepath.Join(dir, "outside"), "not a migration source\n")
				mustSymlink(t, filepath.Join(dir, "outside"), filepath.Join(dir, ".github", ".golang"))
			},
			opts: StackOptions{Template: Template{Name: "go", Merge: true, Versioned: true}, State: preflightLegacyState(""), V510Plus: "v5.10"},
		},
		{
			name: "template workflow source symlink",
			setup: func(t *testing.T, dir string) {
				mustWrite(t, filepath.Join(dir, "outside"), "name: no\n")
				mustSymlink(t, filepath.Join(dir, "outside"), filepath.Join(dir, ".template", ".github", "workflows", "build.yml"))
			},
			opts: StackOptions{Template: Template{Name: "go", Merge: true, Versioned: true}, State: preflightLegacyState(""), V510Plus: "v5.10"},
		},
		{
			name: "template workflow source ancestor symlink",
			setup: func(t *testing.T, dir string) {
				mustWrite(t, filepath.Join(dir, "outside", "workflows", "build.yml"), "name: no\n")
				mustSymlink(t, filepath.Join(dir, "outside"), filepath.Join(dir, ".template", ".github"))
			},
			opts: StackOptions{Template: Template{Name: "go", Merge: true, Versioned: true}, State: preflightLegacyState(""), V510Plus: "v5.10"},
		},
		{
			name: "issue template source ancestor symlink",
			setup: func(t *testing.T, dir string) {
				mustWrite(t, filepath.Join(dir, "outside", "ISSUE_TEMPLATE", "bug.yml"), "name: bug\n")
				mustSymlink(t, filepath.Join(dir, "outside"), filepath.Join(dir, ".template", ".github"))
			},
			opts: StackOptions{Template: Template{Name: "go", Merge: true, Versioned: true}, State: preflightLegacyState(""), V510Plus: "v5.10"},
		},
		{
			name: "migration glob source ancestor symlink",
			setup: func(t *testing.T, dir string) {
				mustWrite(t, filepath.Join(dir, "outside", "vars", "inputs-dev.yaml"), "x: y\n")
				mustSymlink(t, filepath.Join(dir, "outside"), filepath.Join(dir, ".github"))
			},
			opts: StackOptions{Template: Template{Name: "go", Merge: true, Versioned: true}, State: preflightLegacyState(""), V510Plus: "v5.10"},
		},
		{
			name: "stale workflow symlink when target empty",
			setup: func(t *testing.T, dir string) {
				mustWrite(t, filepath.Join(dir, "outside"), "x\n")
				mustSymlink(t, filepath.Join(dir, "outside"), filepath.Join(dir, ".github", "workflows", "stale.yml"))
			},
			opts: StackOptions{Template: Template{Name: "go", Merge: true, Versioned: true}, State: preflightLegacyState(""), V510Plus: "v5.10"},
		},
		{
			name: "stale hook symlink when target empty",
			setup: func(t *testing.T, dir string) {
				mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "hooks", "target.sh"), "target\n")
				mustWrite(t, filepath.Join(dir, "outside"), "x\n")
				mustSymlink(t, filepath.Join(dir, "outside"), filepath.Join(dir, ".cloudopsworks", "hooks", "stale.sh"))
			},
			opts: StackOptions{Template: Template{Name: "go", Merge: true, Versioned: true}, State: preflightV510State(""), V510Plus: "v5.10"},
		},
		{
			name: "stale boilerplate symlink when target empty",
			setup: func(t *testing.T, dir string) {
				mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "boilerplate", "target.sh"), "target\n")
				mustWrite(t, filepath.Join(dir, "outside"), "x\n")
				mustSymlink(t, filepath.Join(dir, "outside"), filepath.Join(dir, ".cloudopsworks", "boilerplate", "stale.sh"))
			},
			opts: StackOptions{Template: Template{Name: "go", Merge: true, Versioned: true, Boilerplate: true, BoilerplatePathV510Plus: ".cloudopsworks/boilerplate"}, State: preflightV510State(""), V510Plus: "v5.10"},
		},
		{
			name: "post migration cicd destination symlink",
			setup: func(t *testing.T, dir string) {
				mustWrite(t, filepath.Join(dir, "outside"), "x\n")
				mustSymlink(t, filepath.Join(dir, "outside"), filepath.Join(dir, ".cloudopsworks", "cloudopsworks-ci.yaml"))
			},
			opts: StackOptions{Template: Template{Name: "go", Merge: true, Versioned: true, CICD: true}, State: preflightLegacyState(""), V510Plus: "v5.10"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(t, dir)
			r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			tt.opts.State.WorkDir = dir
			before := mutationSentinels(t, dir)
			err = r.preflightStackDestinations(tt.opts, cloudOpsworksConfigPlan{})
			if err == nil {
				t.Fatal("preflight unexpectedly accepted unsafe tree")
			}
			if !strings.Contains(err.Error(), "unsafe") && !strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "collision") {
				t.Fatalf("error should identify unsafe mutation tree: %v", err)
			}
			if after := mutationSentinels(t, dir); after != before {
				t.Fatalf("preflight mutated repository\nbefore=%q\nafter=%q", before, after)
			}
		})
	}
}

func TestMigrateRejectsAncestorConflictingResolvedDestinationsBeforeMutation(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	box := filepath.Join(dir, ".github", "box", "first.txt")
	second := filepath.Join(dir, ".github", "second.txt")
	mustWrite(t, box, "first bytes\n")
	mustWrite(t, second, "second bytes\n")
	beforeBox := mustRead(t, box)
	beforeSecond := mustRead(t, second)

	r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	r.Config = &Config{MigrationPlans: []MigrationPlan{{
		Version: "test",
		Templates: map[string][]Operation{"go": {
			{Action: "move", Source: ".github/box", Destination: ".cloudopsworks/box"},
			{Action: "move", Source: ".github/second.txt", Destination: ".cloudopsworks/box"},
		}},
	}}}

	err = r.Migrate("go", "test")
	if err == nil || !strings.Contains(err.Error(), "destination claim conflict") {
		t.Fatalf("Migrate() error = %v, want destination claim conflict", err)
	}
	if got := mustRead(t, box); got != beforeBox {
		t.Fatalf("box bytes changed: %q, want %q", got, beforeBox)
	}
	if got := mustRead(t, second); got != beforeSecond {
		t.Fatalf("second bytes changed: %q, want %q", got, beforeSecond)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".cloudopsworks")); !os.IsNotExist(err) {
		t.Fatalf("migration created destination before rejecting conflict: %v", err)
	}
}

func TestMigrateRejectsExistingResolvedDestinationBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, dir, destination string)
	}{
		{
			name: "regular file",
			setup: func(t *testing.T, _ string, destination string) {
				mustWrite(t, destination, "destination bytes\n")
			},
		},
		{
			name: "directory",
			setup: func(t *testing.T, _ string, destination string) {
				if err := os.MkdirAll(destination, 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink",
			setup: func(t *testing.T, dir, destination string) {
				mustWrite(t, filepath.Join(dir, "outside"), "outside bytes\n")
				mustSymlink(t, filepath.Join(dir, "outside"), destination)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			runGit(t, dir, "init")
			source := filepath.Join(dir, ".github", "result")
			destination := filepath.Join(dir, ".cloudopsworks", "result")
			mustWrite(t, source, "source bytes\n")
			tc.setup(t, dir, destination)
			beforeSource := mustRead(t, source)
			beforeDestination, err := os.Lstat(destination)
			if err != nil {
				t.Fatal(err)
			}

			r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			r.Config = &Config{MigrationPlans: []MigrationPlan{{
				Version: "test",
				Templates: map[string][]Operation{"go": {
					{Action: "move", Source: ".github/result", Destination: ".cloudopsworks"},
				}},
			}}}

			err = r.Migrate("go", "test")
			if err == nil || !strings.Contains(err.Error(), "destination collision") {
				t.Fatalf("Migrate() error = %v, want destination collision", err)
			}
			if got := mustRead(t, source); got != beforeSource {
				t.Fatalf("source bytes changed: %q, want %q", got, beforeSource)
			}
			afterDestination, err := os.Lstat(destination)
			if err != nil {
				t.Fatalf("destination was removed: %v", err)
			}
			if afterDestination.Mode() != beforeDestination.Mode() {
				t.Fatalf("destination mode changed: %v, want %v", afterDestination.Mode(), beforeDestination.Mode())
			}
		})
	}
}

func TestMigrateAllowsMoveIntoExistingDirectoryWithVacantResolvedDestination(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	mustWrite(t, filepath.Join(dir, ".github", "source.txt"), "source bytes\n")
	if err := os.MkdirAll(filepath.Join(dir, ".cloudopsworks"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	r.Config = &Config{MigrationPlans: []MigrationPlan{{
		Version: "test",
		Templates: map[string][]Operation{"go": {
			{Action: "move", Source: ".github/source.txt", Destination: ".cloudopsworks"},
		}},
	}}}
	if err := r.Migrate("go", "test"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "source.txt")); got != "source bytes\n" {
		t.Fatalf("resolved directory destination bytes = %q", got)
	}
}

func TestMigrateAllowsEnsureParentBeforeExistingChildSource(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	source := filepath.Join(dir, ".cloudopsworks", "aws", "source.txt")
	mustWrite(t, source, "source bytes\n")
	r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	r.Config = &Config{MigrationPlans: []MigrationPlan{{Version: "test", Templates: map[string][]Operation{"go": {
		{Action: "ensureDir", Destination: ".cloudopsworks"},
		{Action: "move", Source: ".cloudopsworks/aws", Destination: ".github/migrated"},
	}}}}}
	if err := r.Migrate("go", "test"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "migrated", "source.txt")); got != "source bytes\n" {
		t.Fatalf("migrated child source = %q", got)
	}
}

func TestMigrateRejectsEnsureInsideSourceBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	source := filepath.Join(dir, ".github", "source", "file.txt")
	mustWrite(t, source, "source bytes\n")
	r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	r.Config = &Config{MigrationPlans: []MigrationPlan{{Version: "test", Templates: map[string][]Operation{"go": {
		{Action: "ensureDir", Destination: ".github/source/generated"},
		{Action: "move", Source: ".github/source", Destination: ".cloudopsworks/moved"},
	}}}}}
	err = r.Migrate("go", "test")
	if err == nil || !strings.Contains(err.Error(), "ensure/source dependency conflict") {
		t.Fatalf("Migrate() error = %v, want ensure/source dependency conflict", err)
	}
	if got := mustRead(t, source); got != "source bytes\n" {
		t.Fatalf("source changed before rejection: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".cloudopsworks")); !os.IsNotExist(err) {
		t.Fatalf("migration created destination before rejecting dependency: %v", err)
	}
}

func TestApplyVersionedTemplateMigratesBeforeWorkflowReplacement(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	mustWrite(t, filepath.Join(dir, ".github", "source.txt"), "legacy source bytes\n")
	mustWrite(t, filepath.Join(dir, ".github", "workflows", "build.yml"), "legacy workflow bytes\n")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "build.yml"), "target workflow bytes\n")
	mustWrite(t, filepath.Join(dir, ".template", "Makefile"), "all:\n\t@true\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v5.10.0\n")
	r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cfg := *r.Config
	cfg.MigrationPlans = []MigrationPlan{{Version: "510", Templates: map[string][]Operation{"go": {
		{Action: "move", Source: ".github/source.txt", Destination: ".cloudopsworks"},
		{Action: "move", Source: ".github/workflows/build.yml", Destination: ".cloudopsworks/legacy-build.yml"},
	}}}}
	r.Config = &cfg
	state := RepositoryState{WorkDir: dir, BlueprintPath: ".github", VersionFile: ".github/_VERSION", Pre510: true}
	if err := r.applyVersionedTemplate(Template{Name: "go", Versioned: true}, state, "v5.10"); err != nil {
		t.Fatalf("applyVersionedTemplate() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "source.txt")); got != "legacy source bytes\n" {
		t.Fatalf("legacy root migration = %q", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "legacy-build.yml")); got != "legacy workflow bytes\n" {
		t.Fatalf("workflow migration after replacement = %q", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "workflows", "build.yml")); got != "target workflow bytes\n" {
		t.Fatalf("workflow replacement = %q", got)
	}
}

func TestMigrateRejectsEnsureAtResolvedMoveDestinationBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	source := filepath.Join(dir, ".github", "source.txt")
	mustWrite(t, source, "source bytes\n")
	r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	r.Config = &Config{MigrationPlans: []MigrationPlan{{Version: "test", Templates: map[string][]Operation{"go": {
		{Action: "ensureDir", Destination: ".cloudopsworks/source.txt"},
		{Action: "move", Source: ".github/source.txt", Destination: ".cloudopsworks"},
	}}}}}
	err = r.Migrate("go", "test")
	if err == nil || !strings.Contains(err.Error(), "ensure/destination dependency conflict") {
		t.Fatalf("Migrate() error = %v, want ensure/destination dependency conflict", err)
	}
	if got := mustRead(t, source); got != "source bytes\n" {
		t.Fatalf("source changed before rejection: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".cloudopsworks")); !os.IsNotExist(err) {
		t.Fatalf("migration created destination before rejecting dependency: %v", err)
	}
}

func TestMigrateResolvesEnsureDirectoryBeforeMoveDeterministically(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	mustWrite(t, filepath.Join(dir, ".github", "source.txt"), "source bytes\n")
	r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	r.Config = &Config{MigrationPlans: []MigrationPlan{{Version: "test", Templates: map[string][]Operation{"go": {
		{Action: "ensureDir", Destination: ".cloudopsworks/final"},
		{Action: "move", Source: ".github/source.txt", Destination: ".cloudopsworks/final"},
	}}}}}
	if err := r.Migrate("go", "test"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "final", "source.txt")); got != "source bytes\n" {
		t.Fatalf("resolved ensure destination bytes = %q", got)
	}
}

func TestMigrateRejectsMoveThenEnsureDestinationBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	source := filepath.Join(dir, ".github", "source.txt")
	mustWrite(t, source, "source bytes\n")
	r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	r.Config = &Config{MigrationPlans: []MigrationPlan{{Version: "test", Templates: map[string][]Operation{"go": {
		{Action: "move", Source: ".github/source.txt", Destination: ".cloudopsworks/final"},
		{Action: "ensureDir", Destination: ".cloudopsworks/final"},
	}}}}}
	err = r.Migrate("go", "test")
	if err == nil || !strings.Contains(err.Error(), "move/ensure dependency conflict") {
		t.Fatalf("Migrate() error = %v, want move/ensure dependency conflict", err)
	}
	if got := mustRead(t, source); got != "source bytes\n" {
		t.Fatalf("source changed before rejection: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".cloudopsworks")); !os.IsNotExist(err) {
		t.Fatalf("migration created destination before rejecting dependency: %v", err)
	}
}

func TestStackMigratesLegacySourcesBeforeWorkflowReplacement(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "tronador-cli-test@example.com")
	runGit(t, dir, "config", "user.name", "tronador-cli test")
	mustWrite(t, filepath.Join(dir, ".github", "source.txt"), "legacy source bytes\n")
	mustWrite(t, filepath.Join(dir, ".github", "workflows", "build.yml"), "legacy workflow bytes\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "build.yml"), "uses: cloudopsworks/blueprints/cd/checkout@v5.10\n")
	mustWrite(t, filepath.Join(dir, ".template", "Makefile"), "all:\n\t@true\n")
	mustWrite(t, filepath.Join(dir, ".template", ".cloudopsworks", "_VERSION"), "v5.10.0\n")

	r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cfg := *r.Config
	cfg.MigrationPlans = []MigrationPlan{{Version: "510", Templates: map[string][]Operation{"go": {
		{Action: "move", Source: ".github/source.txt", Destination: ".cloudopsworks"},
		{Action: "move", Source: ".github/workflows/build.yml", Destination: ".cloudopsworks/legacy-build.yml"},
	}}}}
	r.Config = &cfg
	state := RepositoryState{WorkDir: dir, BlueprintPath: ".github", VersionFile: ".github/_VERSION", Pre510: true, Version: "v5.9.0"}
	if err := r.Stack(context.Background(), StackOptions{Template: Template{Name: "go", Merge: true, Versioned: true}, State: state, PullBranch: "test", TemplateHash: "target", V510Plus: "v5.10"}); err != nil {
		t.Fatalf("Stack() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "source.txt")); got != "legacy source bytes\n" {
		t.Fatalf("legacy source migration = %q", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".cloudopsworks", "legacy-build.yml")); got != "legacy workflow bytes\n" {
		t.Fatalf("workflow source was replaced before migration: %q", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".github", "workflows", "build.yml")); !strings.Contains(got, "@v5.10") {
		t.Fatalf("target workflow was not installed: %q", got)
	}
}

func TestMigrateRejectsOverlappingSourceClaimsBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	parentLeaf := filepath.Join(dir, ".github", "box", "first.txt")
	child := filepath.Join(dir, ".github", "box", "second.txt")
	mustWrite(t, parentLeaf, "first bytes\n")
	mustWrite(t, child, "second bytes\n")
	beforeParent, beforeChild := mustRead(t, parentLeaf), mustRead(t, child)

	r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	r.Config = &Config{MigrationPlans: []MigrationPlan{{
		Version: "test",
		Templates: map[string][]Operation{"go": {
			{Action: "move", Source: ".github/box", Destination: ".cloudopsworks/box"},
			{Action: "move", Source: ".github/box/second.txt", Destination: ".cloudopsworks/second.txt"},
		}},
	}}}

	err = r.Migrate("go", "test")
	if err == nil || !strings.Contains(err.Error(), "source claim conflict") {
		t.Fatalf("Migrate() error = %v, want source claim conflict", err)
	}
	if got := mustRead(t, parentLeaf); got != beforeParent {
		t.Fatalf("parent source bytes changed: %q, want %q", got, beforeParent)
	}
	if got := mustRead(t, child); got != beforeChild {
		t.Fatalf("child source bytes changed: %q, want %q", got, beforeChild)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".cloudopsworks")); !os.IsNotExist(err) {
		t.Fatalf("migration created destination before rejecting source conflict: %v", err)
	}
}

func TestMigrateRejectsSourceDestinationDependenciesBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	box := filepath.Join(dir, ".github", "box", "first.txt")
	second := filepath.Join(dir, ".github", "second.txt")
	mustWrite(t, box, "box bytes\n")
	mustWrite(t, second, "second bytes\n")
	beforeBox, beforeSecond := mustRead(t, box), mustRead(t, second)

	r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	r.Config = &Config{MigrationPlans: []MigrationPlan{{
		Version: "test",
		Templates: map[string][]Operation{"go": {
			{Action: "move", Source: ".github/box", Destination: ".cloudopsworks/box"},
			{Action: "move", Source: ".github/second.txt", Destination: ".github/box/new.txt"},
		}},
	}}}

	err = r.Migrate("go", "test")
	if err == nil || !strings.Contains(err.Error(), "source/destination dependency conflict") {
		t.Fatalf("Migrate() error = %v, want source/destination dependency conflict", err)
	}
	if got := mustRead(t, box); got != beforeBox {
		t.Fatalf("box bytes changed: %q, want %q", got, beforeBox)
	}
	if got := mustRead(t, second); got != beforeSecond {
		t.Fatalf("second bytes changed: %q, want %q", got, beforeSecond)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".cloudopsworks")); !os.IsNotExist(err) {
		t.Fatalf("migration created destination before rejecting dependency: %v", err)
	}
}

func TestRunOperationsSelectsConditionsBeforeMutations(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".github", "source.txt"), "source\n")
	r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.runOperations([]Operation{
		{Action: "ensureDir", Destination: ".cloudopsworks", When: "missing:.cloudopsworks"},
		{Action: "move", Source: ".github/source.txt", Destination: ".cloudopsworks/source.txt", When: "exists:.cloudopsworks"},
	})
	if err != nil {
		t.Fatalf("runOperations() error = %v", err)
	}
	if !exists(filepath.Join(dir, ".github", "source.txt")) {
		t.Fatal("later operation activated from an earlier mutation")
	}
}

func TestMigrateSelectsCommonAndTemplateConditionsBeforeMutations(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".github", "source.txt"), "source\n")
	r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cfg := *r.Config
	cfg.MigrationPlans = []MigrationPlan{{
		Version:   "test",
		Common:    []Operation{{Action: "ensureDir", Destination: ".cloudopsworks", When: "missing:.cloudopsworks"}},
		Templates: map[string][]Operation{"go": {{Action: "move", Source: ".github/source.txt", Destination: ".cloudopsworks/source.txt", When: "exists:.cloudopsworks"}}},
	}}
	r.Config = &cfg
	if err := r.Migrate("go", "test"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if !exists(filepath.Join(dir, ".github", "source.txt")) {
		t.Fatal("template operation activated from common migration mutation")
	}
}

func TestMigrationOperationsUsesCommonForExecutableRoutesOnly(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	r.Config = &Config{MigrationPlans: []MigrationPlan{{
		Version: "test",
		Common:  []Operation{{Action: "ensureDir", Destination: ".cloudopsworks"}},
		Templates: map[string][]Operation{
			"go":      {{Action: "ensureDir", Destination: ".cloudopsworks/go"}},
			"android": {{Action: "unsupported", Message: "not available"}},
		},
	}}}

	_, executable, err := r.migrationOperations("go", "test")
	if err != nil {
		t.Fatalf("migrationOperations(go) error = %v", err)
	}
	if len(executable) != 2 || executable[0].Destination != ".cloudopsworks" || executable[1].Destination != ".cloudopsworks/go" {
		t.Fatalf("executable route operations = %#v, want common followed by route operations", executable)
	}

	_, unsupported, err := r.migrationOperations("android", "test")
	if err != nil {
		t.Fatalf("migrationOperations(android) error = %v", err)
	}
	if len(unsupported) != 1 || unsupported[0].Action != "unsupported" {
		t.Fatalf("unsupported route operations = %#v, want route operation only", unsupported)
	}
}

func TestPushRejectsWhitespaceAlteredStagedPath(t *testing.T) {
	for _, path := range []string{" .cloudopsworks/payload", ".cloudopsworks/payload\n"} {
		t.Run(strings.ReplaceAll(path, "\n", "newline"), func(t *testing.T) {
			dir := t.TempDir()
			runGit(t, dir, "init")
			mustWrite(t, filepath.Join(dir, path), "unowned\n")
			runGit(t, dir, "add", "--", path)
			r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			err = r.Push(context.Background(), Template{}, RepositoryState{})
			if err == nil || !strings.Contains(err.Error(), "whitespace-altered") {
				t.Fatalf("Push() error = %v, want whitespace path rejection", err)
			}
			if staged := runGit(t, dir, "diff", "--cached", "--name-only", "-z"); staged != path+"\x00" {
				t.Fatalf("Push() changed caller index: %q", staged)
			}
		})
	}
}

func TestUpgradeVersionDryRunUsesIsolatedTargetCheckout(t *testing.T) {
	for _, tc := range []struct {
		name     string
		workflow string
		wantErr  bool
	}{
		{
			name:     "success",
			workflow: "uses: cloudopsworks/blueprints/cd/checkout@v5.10\n",
		},
		{
			name:     "target analysis failure",
			workflow: "name: no active blueprint reference\n",
			wantErr:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newDryRunUpgradeFixture(t)
			sentinel := filepath.Join(dir, ".template", "caller-sentinel")
			beforeBytes, err := os.ReadFile(sentinel)
			if err != nil {
				t.Fatal(err)
			}
			beforeInfo, err := os.Lstat(sentinel)
			if err != nil {
				t.Fatal(err)
			}
			beforeHead := runGit(t, dir, "rev-parse", "HEAD")
			beforeTree := runGit(t, dir, "write-tree")
			beforeIndex := runGit(t, dir, "ls-files", "-s")
			beforeStatus := runGit(t, dir, "status", "--porcelain=v1", "-z")

			gitPath := dryRunTemplateGit(t, tc.workflow)
			out := new(strings.Builder)
			runner, err := NewRunner(Options{WorkDir: dir, DryRun: true, GitPath: gitPath, Stdout: out, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			ghPath, ghInvocation := failingGH(t)
			runner.Opts.GHPath = ghPath
			runner.gitClient = &fakeGitClient{owner: "example", repo: "fixture"}

			err = runner.UpgradeVersion(context.Background(), "fixture")
			if tc.wantErr {
				if err == nil {
					t.Fatal("UpgradeVersion() succeeded with an invalid target workflow")
				}
			} else if err != nil {
				t.Fatalf("UpgradeVersion() error = %v", err)
			}
			if !strings.Contains(out.String(), "DRY-RUN (target analysis:") {
				t.Fatalf("dry-run did not execute target analysis:\n%s", out.String())
			}
			if _, err := os.Lstat(ghInvocation); !os.IsNotExist(err) {
				t.Fatalf("dry-run invoked gh despite its side-effect sentinel: %v", err)
			}
			checkout := dryRunAnalysisDirectory(t, out.String())
			if filepath.Clean(checkout) == filepath.Clean(dir) {
				t.Fatalf("target analysis used caller workdir instead of an isolated checkout: %s", checkout)
			}
			if _, err := os.Lstat(checkout); !os.IsNotExist(err) {
				t.Fatalf("temporary target checkout remains after dry-run: %s (err=%v)", checkout, err)
			}

			afterBytes, err := os.ReadFile(sentinel)
			if err != nil {
				t.Fatalf("caller .template sentinel missing: %v", err)
			}
			afterInfo, err := os.Lstat(sentinel)
			if err != nil {
				t.Fatal(err)
			}
			if string(afterBytes) != string(beforeBytes) || afterInfo.Mode() != beforeInfo.Mode() {
				t.Fatalf("caller .template sentinel changed: bytes=%q mode=%v; want bytes=%q mode=%v", afterBytes, afterInfo.Mode(), beforeBytes, beforeInfo.Mode())
			}
			if got := runGit(t, dir, "rev-parse", "HEAD"); got != beforeHead {
				t.Fatalf("HEAD changed: %q != %q", got, beforeHead)
			}
			if got := runGit(t, dir, "write-tree"); got != beforeTree {
				t.Fatalf("index tree changed: %q != %q", got, beforeTree)
			}
			if got := runGit(t, dir, "ls-files", "-s"); got != beforeIndex {
				t.Fatalf("index entries changed:\n%s\nwant:\n%s", got, beforeIndex)
			}
			if got := runGit(t, dir, "status", "--porcelain=v1", "-z"); got != beforeStatus {
				t.Fatalf("worktree status changed: %q != %q", got, beforeStatus)
			}
		})
	}
}

func TestRecoverDryRunSkipsGHEvenWithOrigin(t *testing.T) {
	dir := newDryRunUpgradeFixture(t)
	beforeHead := runGit(t, dir, "rev-parse", "HEAD")
	beforeTree := runGit(t, dir, "write-tree")
	beforeIndex := runGit(t, dir, "ls-files", "-s")
	beforeStatus := runGit(t, dir, "status", "--porcelain=v1", "-z")

	out := new(strings.Builder)
	gitPath := dryRunTemplateGit(t, "uses: cloudopsworks/blueprints/cd/checkout@v5.10\n")
	ghPath, ghInvocation := failingGH(t)
	runner, err := NewRunner(Options{WorkDir: dir, DryRun: true, PullBranch: "fixture", GitPath: gitPath, GHPath: ghPath, Stdout: out, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	runner.gitClient = &fakeGitClient{owner: "example", repo: "fixture"}
	for index := range runner.Config.Templates {
		if runner.Config.Templates[index].Name == "go" {
			runner.Config.Templates[index].CICD = false
		}
	}

	if err := runner.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() dry-run error = %v", err)
	}
	if !strings.Contains(out.String(), "Skipping gh repo set-default in dry-run") {
		t.Fatalf("Recover() did not take the dry-run gh skip branch:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "DRY-RUN (target analysis:") {
		t.Fatalf("Recover() did not analyse target checkout:\n%s", out.String())
	}
	if _, err := os.Lstat(ghInvocation); !os.IsNotExist(err) {
		t.Fatalf("Recover() dry-run invoked gh despite side-effect sentinel: %v", err)
	}
	if got := runGit(t, dir, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("Recover() changed HEAD: %q != %q", got, beforeHead)
	}
	if got := runGit(t, dir, "write-tree"); got != beforeTree {
		t.Fatalf("Recover() changed index tree: %q != %q", got, beforeTree)
	}
	if got := runGit(t, dir, "ls-files", "-s"); got != beforeIndex {
		t.Fatalf("Recover() changed index entries:\n%s\nwant:\n%s", got, beforeIndex)
	}
	if got := runGit(t, dir, "status", "--porcelain=v1", "-z"); got != beforeStatus {
		t.Fatalf("Recover() changed worktree status: %q != %q", got, beforeStatus)
	}
}

func TestUpgradeEffectsRejectWhitespacePathsWithoutNormalizingJournal(t *testing.T) {
	for _, raw := range []string{" .cloudopsworks/payload", ".cloudopsworks/payload ", ".cloudopsworks/payload\n"} {
		t.Run(strings.ReplaceAll(strings.ReplaceAll(raw, " ", "space"), "\n", "newline"), func(t *testing.T) {
			effects := newUpgradeEffects()
			if err := effects.add(".cloudopsworks/known.yaml"); err != nil {
				t.Fatal(err)
			}
			if _, err := safeRelativePaths([]string{raw}); err == nil {
				t.Fatalf("safeRelativePaths accepted whitespace-altered path %q", raw)
			}
			if err := effects.add(raw); err == nil {
				t.Fatalf("upgradeEffects.add accepted whitespace-altered path %q", raw)
			}
			if _, err := effects.list(); err == nil {
				t.Fatal("upgradeEffects.list hid the invalid path error")
			}
			if len(effects.paths) != 1 {
				t.Fatalf("journal changed after rejecting %q: %#v", raw, effects.paths)
			}
			if _, ok := effects.paths[".cloudopsworks/known.yaml"]; !ok {
				t.Fatalf("journal lost its original path after rejecting %q: %#v", raw, effects.paths)
			}
			if _, ok := effects.paths[strings.TrimSpace(raw)]; ok {
				t.Fatalf("journal recorded normalized substitute for %q: %#v", raw, effects.paths)
			}
		})
	}
}

func TestEvalTemplateVersionRejectsSymlinkedWorkflowAncestors(t *testing.T) {
	for _, tc := range []struct {
		name string
		link string
	}{
		{name: "github ancestor", link: ".github"},
		{name: "workflows ancestor", link: ".github/workflows"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, runner, _ := configUpgradeRunner(t, false)
			outside := filepath.Join(dir, "outside")
			workflow := filepath.Join(outside, "workflows", "build.yml")
			if tc.link == ".github/workflows" {
				workflow = filepath.Join(outside, "build.yml")
			}
			mustWrite(t, workflow, "uses: cloudopsworks/blueprints/cd/checkout@v5.10\n")
			link := filepath.Join(dir, ".template", filepath.FromSlash(tc.link))
			if err := os.RemoveAll(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if _, err := runner.EvalTemplateVersion(); err == nil || !strings.Contains(err.Error(), "unsafe") {
				t.Fatalf("EvalTemplateVersion() error = %v, want unsafe symlink rejection", err)
			}
		})
	}

	dir, runner, _ := configUpgradeRunner(t, false)
	mustWrite(t, filepath.Join(dir, ".template", ".github", "workflows", "safe.yml"), "blueprint_ref: v5.10\n")
	if got, err := runner.EvalTemplateVersion(); err != nil || got != "v5.10" {
		t.Fatalf("EvalTemplateVersion() = %q, %v; want safe v5.10 workflow", got, err)
	}
}

func newDryRunUpgradeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "tests@example.invalid")
	runGit(t, dir, "config", "user.name", "tests")
	runGit(t, dir, "remote", "add", "origin", "https://github.com/example/fixture.git")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", "_VERSION"), "v5.10.0\n")
	mustWrite(t, filepath.Join(dir, ".cloudopsworks", ".golang"), "")
	mustWrite(t, filepath.Join(dir, ".template", "caller-sentinel"), "caller-owned bytes\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "fixture")
	return dir
}

func dryRunTemplateGit(t *testing.T, workflow string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "git")
	script := "#!/bin/sh\nset -eu\n" +
		"case \"$1\" in\n" +
		"clone)\n  dest=\"$3\"\n  mkdir -p \"$dest/.github/workflows\" \"$dest/.cloudopsworks\"\n" +
		"  cat > \"$dest/.github/workflows/build.yml\" <<'EOF'\n" + workflow + "EOF\n" +
		"  printf 'v5.10.1\\n' > \"$dest/.cloudopsworks/_VERSION\"\n  printf 'all:\\n\\t@true\\n' > \"$dest/Makefile\"\n  ;;\n" +
		"checkout) ;;\nrev-parse) printf 'fixture-hash\\n' ;;\n*) echo \"unexpected git invocation: $*\" >&2; exit 1 ;;\nesac\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func failingGH(t *testing.T) (path, invocation string) {
	t.Helper()
	dir := t.TempDir()
	path = filepath.Join(dir, "gh")
	invocation = filepath.Join(dir, "invoked")
	script := "#!/bin/sh\nset -eu\nprintf invoked > '" + invocation + "'\nexit 99\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, invocation
}

func dryRunAnalysisDirectory(t *testing.T, output string) string {
	t.Helper()
	const clone = " clone "
	start := strings.Index(output, clone)
	if start == -1 {
		t.Fatalf("target analysis clone absent from output:\n%s", output)
	}
	rest := output[start+len(clone):]
	// The first clone argument is the URL; its second argument is the isolated
	// checkout destination. Paths created by os.MkdirTemp contain no spaces.
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		t.Fatalf("target analysis clone is malformed:\n%s", output)
	}
	destination := parts[1]
	end := strings.Index(destination, ")")
	if end == -1 {
		return destination
	}
	return destination[:end]
}

func TestStackMutationPreflightSkipsExistingNonDestructiveCopyDestinations(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".template", ".github", "ISSUE_TEMPLATE", "bug.yml.disabled"), "name: Bug\n")
	mustWrite(t, filepath.Join(dir, "outside"), "x\n")
	mustSymlink(t, filepath.Join(dir, "outside"), filepath.Join(dir, ".github", "ISSUE_TEMPLATE", "bug.yml"))

	r, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	opts := StackOptions{
		Template: Template{Name: "go", Merge: true, Versioned: true},
		State:    preflightV510State(dir),
		V510Plus: "v5.10",
	}
	if err := r.preflightStackDestinations(opts, cloudOpsworksConfigPlan{}); err != nil {
		t.Fatalf("preflight rejected destination execution leaves untouched: %v", err)
	}
}

func preflightLegacyState(dir string) RepositoryState {
	return RepositoryState{WorkDir: dir, BlueprintPath: ".github", VersionFile: ".github/_VERSION", Pre510: true, Version: "v5.9.0"}
}
func preflightV510State(dir string) RepositoryState {
	return RepositoryState{WorkDir: dir, BlueprintPath: ".cloudopsworks", VersionFile: ".cloudopsworks/_VERSION", Version: "v5.10.0"}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func mutationSentinels(t *testing.T, root string) string {
	t.Helper()
	var paths []string
	for _, rel := range []string{".github/workflows", ".cloudopsworks/_VERSION", ".github/_VERSION"} {
		if info, err := os.Lstat(filepath.Join(root, rel)); err == nil {
			paths = append(paths, rel+":"+info.Mode().String())
		}
	}
	return strings.Join(paths, ",")
}
