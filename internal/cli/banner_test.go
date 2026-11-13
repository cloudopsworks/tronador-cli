package cli

import (
	"bytes"
	"strings"
	"testing"
)

// Non-terminal writers (pipes, files, buffers) must keep the plain output
// that scripts parse.
func TestVersionPlainWhenNotTerminal(t *testing.T) {
	SetVersion("v1.2.3")
	defer SetVersion("dev")
	for _, args := range [][]string{{"version"}, {"version", "--short"}} {
		var out bytes.Buffer
		rootCmd.SetOut(&out)
		rootCmd.SetArgs(args)
		err := rootCmd.Execute()
		rootCmd.SetOut(nil)
		rootCmd.SetArgs(nil)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if out.String() != "v1.2.3\n" {
			t.Errorf("%v printed %q, want plain version", args, out.String())
		}
	}
	versionCmd.Flags().Set("short", "false")
}

func TestRootHelpPlainWhenNotTerminal(t *testing.T) {
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"--help"})
	err := rootCmd.Execute()
	rootCmd.SetOut(nil)
	rootCmd.SetArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "░██") || strings.Contains(out.String(), "\x1b[") {
		t.Fatal("help written to a non-terminal must not include the banner")
	}
	if !strings.HasPrefix(out.String(), "Tronador CLI is a command-line tool") {
		t.Fatalf("unexpected help start: %q", out.String()[:min(80, out.Len())])
	}
}

func TestBannerTerminalRejectsNonFiles(t *testing.T) {
	if _, ok := bannerTerminal(&bytes.Buffer{}); ok {
		t.Fatal("buffers are not terminals")
	}
}
