package cli

import "testing"

func TestReadmeCommandSurface(t *testing.T) {
	for _, args := range [][]string{
		{"readme"},
		{"readme", "build"},
		{"readme", "build", "terraform"},
		{"readme", "build", "tf"},
		{"readme", "build", "tofu"},
		{"readme", "init"},
		{"readme", "lint"},
		{"readme", "deps"},
		{"readme", "assets", "path"},
		{"readme", "assets", "sync"},
		{"readme", "assets", "cache", "ls"},
	} {
		cmd, _, err := rootCmd.Find(args)
		if err != nil || cmd == nil {
			t.Fatalf("find %v: cmd=%v err=%v", args, cmd, err)
		}
	}
}

func TestReadmeBuildTerraformFlags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"readme", "build", "terraform"})
	if err != nil {
		t.Fatalf("find readme build terraform: %v", err)
	}
	for _, name := range []string{"terraform-docs", "terraform-docs-version", "terraform-output", "terraform-docs-format", "no-install-terraform-docs"} {
		if cmd.Flag(name) == nil {
			t.Fatalf("readme build terraform flag --%s is missing", name)
		}
	}
}

func TestReadmeProvisioningFlags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"readme"})
	if err != nil {
		t.Fatalf("find readme: %v", err)
	}
	for _, name := range []string{"tools-dir", "tools-config", "make", "no-install-gomplate", "gomplate-version", "no-install-terraform-docs", "terraform-docs-version"} {
		if cmd.Flag(name) == nil {
			t.Fatalf("readme flag --%s is missing", name)
		}
	}
}

func TestDocsCommandSurface(t *testing.T) {
	for _, args := range [][]string{
		{"docs"},
		{"docs", "init"},
		{"docs", "targets"},
		{"docs", "terraform"},
		{"docs", "copyright-add"},
	} {
		cmd, _, err := rootCmd.Find(args)
		if err != nil || cmd == nil {
			t.Fatalf("find %v: cmd=%v err=%v", args, cmd, err)
		}
	}
}
