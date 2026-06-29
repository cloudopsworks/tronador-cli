package cli

import "testing"

func TestReadmeCommandSurface(t *testing.T) {
	for _, args := range [][]string{
		{"readme"},
		{"readme", "build"},
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

func TestReadmeProvisioningFlags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"readme"})
	if err != nil {
		t.Fatalf("find readme: %v", err)
	}
	for _, name := range []string{"tools-dir", "tools-config", "no-install-gomplate", "gomplate-version"} {
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
