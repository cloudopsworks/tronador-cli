package readme

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadmeIncludesAreTrimmedAndDeduplicated(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "README.yaml"), `name: test
include:
  - " docs/targets.md "
  - docs/targets.md
  - ""
  - docs/custom.md
`)
	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	got, err := runner.ReadmeIncludes(context.Background())
	if err != nil {
		t.Fatalf("ReadmeIncludes() error = %v", err)
	}
	want := []string{"docs/targets.md", "docs/custom.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ReadmeIncludes() = %v, want %v", got, want)
	}
}

func TestBuildRunsMakeForTargetsInclude(t *testing.T) {
	dir := t.TempDir()
	makePath := writeExecutable(t, dir, "make", `#!/bin/sh
printf '%s\n' "$1" > make-target.txt
mkdir -p docs
printf '## Targets\n' > "$1"
`)
	gomplate := fakeGomplate(t, dir)
	mustWrite(t, filepath.Join(dir, "README.yaml"), "name: test\ninclude:\n  - docs/targets.md\n")
	runner, err := NewRunner(Options{WorkDir: dir, MakePath: makePath, GomplatePath: gomplate, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Build(context.Background()); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "make-target.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "docs/targets.md\n" {
		t.Fatalf("make target = %q", got)
	}
}

func TestBuildDryRunPreparesIncludeDirectoriesBeforeGenerators(t *testing.T) {
	dir := t.TempDir()
	makePath := writeExecutable(t, dir, "make", "#!/bin/sh\nexit 0\n")
	terraformDocs := writeExecutable(t, dir, "terraform-docs", "#!/bin/sh\nexit 0\n")
	gomplate := fakeGomplate(t, dir)
	var stdout strings.Builder
	mustWrite(t, filepath.Join(dir, "README.yaml"), "name: test\ninclude:\n  - docs/targets.md\n  - docs/terraform.md\n")
	mustWrite(t, filepath.Join(dir, "variables.tf"), "variable \"name\" {}\n")
	runner, err := NewRunner(Options{
		WorkDir:           dir,
		MakePath:          makePath,
		GomplatePath:      gomplate,
		TerraformDocsPath: terraformDocs,
		DryRun:            true,
		Stdout:            &stdout,
		Stderr:            io.Discard,
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Build(context.Background()); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	out := stdout.String()
	mkdir := "DRY-RUN: mkdir -p " + filepath.Join(dir, "docs")
	makeTarget := "DRY-RUN: " + makePath + " docs/targets.md"
	terraformTarget := "DRY-RUN: " + terraformDocs + " md . > docs/terraform.md"
	if strings.Count(out, mkdir) != 1 {
		t.Fatalf("mkdir output count = %d, want 1:\n%s", strings.Count(out, mkdir), out)
	}
	if strings.Index(out, mkdir) > strings.Index(out, makeTarget) || strings.Index(out, makeTarget) > strings.Index(out, terraformTarget) {
		t.Fatalf("dry-run operations are out of order:\n%s", out)
	}
}

func TestBuildTargetsIncludePropagatesMakeFailure(t *testing.T) {
	dir := t.TempDir()
	makePath := writeExecutable(t, dir, "make", "#!/bin/sh\nexit 2\n")
	gomplate := fakeGomplate(t, dir)
	mustWrite(t, filepath.Join(dir, "README.yaml"), "name: test\ninclude:\n  - docs/targets.md\n")
	runner, err := NewRunner(Options{WorkDir: dir, MakePath: makePath, GomplatePath: gomplate, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Build(context.Background()); err == nil {
		t.Fatal("Build() succeeded when the required docs/targets.md Make target failed")
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", "targets.md")); !os.IsNotExist(err) {
		t.Fatalf("required target failure should not create a placeholder, stat error = %v", err)
	}
}

func TestBuildUnknownIncludeUsesSuccessfulMakeTarget(t *testing.T) {
	dir := t.TempDir()
	makePath := writeExecutable(t, dir, "make", `#!/bin/sh
mkdir -p "$(dirname "$1")"
printf '## Custom\n' > "$1"
`)
	gomplate := fakeGomplate(t, dir)
	var stderr strings.Builder
	mustWrite(t, filepath.Join(dir, "README.yaml"), "name: test\ninclude:\n  - docs/custom.md\n")
	runner, err := NewRunner(Options{WorkDir: dir, MakePath: makePath, GomplatePath: gomplate, Stdout: io.Discard, Stderr: &stderr})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Build(context.Background()); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "docs", "custom.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "## Custom\n" {
		t.Fatalf("custom include = %q", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected warning: %s", stderr.String())
	}
}

func TestBuildUnknownIncludeMakeFailureCreatesWarningPlaceholder(t *testing.T) {
	dir := t.TempDir()
	makePath := writeExecutable(t, dir, "make", `#!/bin/sh
printf 'missing target\n' >&2
exit 2
`)
	gomplate := fakeGomplate(t, dir)
	var stderr strings.Builder
	mustWrite(t, filepath.Join(dir, "README.yaml"), "name: test\ninclude:\n  - docs/unsupported.md\n")
	runner, err := NewRunner(Options{WorkDir: dir, MakePath: makePath, GomplatePath: gomplate, Stdout: io.Discard, Stderr: &stderr})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Build(context.Background()); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	placeholder, err := os.ReadFile(filepath.Join(dir, "docs", "unsupported.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<!-- WARNING:", "docs/unsupported.md", "not supported yet"} {
		if !strings.Contains(string(placeholder), want) {
			t.Fatalf("placeholder missing %q: %s", want, placeholder)
		}
	}
	for _, want := range []string{"WARNING:", "docs/unsupported.md", "not supported yet"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr missing %q: %s", want, stderr.String())
		}
	}
}

func TestIncludeGateSupportsCustomRegisteredHandler(t *testing.T) {
	dir := t.TempDir()
	gomplate := fakeGomplate(t, dir)
	mustWrite(t, filepath.Join(dir, "README.yaml"), "name: test\ninclude:\n  - docs/custom.md\n")
	gate := NewIncludeGate(func(context.Context, *Runner, string) error {
		t.Fatal("fallback handler called")
		return nil
	})
	gate.Register("docs/custom.md", func(_ context.Context, runner *Runner, entry string) error {
		return os.WriteFile(filepath.Join(runner.Opts.WorkDir, entry), []byte("custom handler\n"), 0o644)
	})
	runner, err := NewRunner(Options{WorkDir: dir, IncludeGate: gate, GomplatePath: gomplate, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Build(context.Background()); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "docs", "custom.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "custom handler\n" {
		t.Fatalf("custom include = %q", got)
	}
}

func TestDepsResolvesTerraformDocsOnlyWhenIncluded(t *testing.T) {
	dir := t.TempDir()
	gomplate := writeExecutable(t, dir, "gomplate", "#!/bin/sh\nexit 0\n")
	terraformDocs := writeExecutable(t, dir, "terraform-docs", "#!/bin/sh\nexit 0\n")
	var stdout strings.Builder
	mustWrite(t, filepath.Join(dir, "README.yaml"), "name: test\ninclude:\n  - docs/terraform.md\n")
	runner, err := NewRunner(Options{
		WorkDir:           dir,
		GomplatePath:      gomplate,
		TerraformDocsPath: terraformDocs,
		Stdout:            &stdout,
		Stderr:            io.Discard,
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Deps(context.Background()); err != nil {
		t.Fatalf("Deps() error = %v", err)
	}
	for _, want := range []string{"gomplate: " + gomplate, "terraform-docs: " + terraformDocs} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("deps output missing %q: %s", want, stdout.String())
		}
	}
}

func fakeGomplate(t *testing.T, dir string) string {
	t.Helper()
	return writeExecutable(t, dir, "gomplate", `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--out" ]; then shift; out="$1"; fi
  shift || true
done
printf 'generated\n' > "$out"
`)
}
