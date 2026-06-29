package docs

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripANSI(t *testing.T) {
	got := StripANSI("\x1b[32;01mreadme/build\x1b[0m Build README")
	if got != "readme/build Build README" {
		t.Fatalf("StripANSI() = %q", got)
	}
}

func TestTargetsWritesFencedMakeHelp(t *testing.T) {
	dir := t.TempDir()
	fakeMake := writeExecutable(t, dir, "make", `#!/bin/sh
printf '\033[32;01mreadme/build\033[0m Create README\n'
`)
	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Targets(context.Background(), TargetsOptions{MakePath: fakeMake}); err != nil {
		t.Fatalf("Targets() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "docs", "targets.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "## Makefile Targets\n```\nreadme/build Create README\n```\n"
	if string(got) != want {
		t.Fatalf("targets.md = %q, want %q", got, want)
	}
}

func TestTerraformWritesOutput(t *testing.T) {
	dir := t.TempDir()
	fake := writeExecutable(t, dir, "terraform-docs", `#!/bin/sh
printf '# Terraform Docs\n'
`)
	runner, err := NewRunner(Options{WorkDir: dir, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Terraform(context.Background(), TerraformOptions{TerraformDocsPath: fake}); err != nil {
		t.Fatalf("Terraform() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "docs", "terraform.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# Terraform Docs\n" {
		t.Fatalf("terraform.md = %q", got)
	}
}

func TestCopyrightDryRunRequiresDescriptionAndPrintsCommand(t *testing.T) {
	dir := t.TempDir()
	var out strings.Builder
	runner, err := NewRunner(Options{WorkDir: dir, DryRun: true, Stdout: &out, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.CopyrightAdd(context.Background(), CopyrightOptions{}); err == nil {
		t.Fatalf("CopyrightAdd() succeeded without description")
	}
	if err := runner.CopyrightAdd(context.Background(), CopyrightOptions{SoftwareDescription: "Test software"}); err != nil {
		t.Fatalf("CopyrightAdd() dry run error = %v", err)
	}
	if !strings.Contains(out.String(), "DRY-RUN:") || !strings.Contains(out.String(), "--copyright-software-description") {
		t.Fatalf("dry-run output = %q", out.String())
	}
}

func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
