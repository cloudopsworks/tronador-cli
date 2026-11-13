package cli

import (
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"tronador-cli/internal/banner"
)

var cliCommit = ""

// SetCommit injects the build commit from the root main package.
func SetCommit(value string) {
	cliCommit = strings.TrimSpace(value)
}

// currentCommit prefers the release ldflag and falls back to the VCS revision
// Go stamps into locally built binaries.
func currentCommit() string {
	commit := cliCommit
	if commit == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, s := range info.Settings {
				if s.Key == "vcs.revision" {
					commit = s.Value
				}
			}
		}
	}
	if len(commit) > 7 {
		commit = commit[:7]
	}
	return commit
}

// bannerTerminal reports whether out is an interactive terminal that should
// get the decorated banner, and how to render it. Pipes, files, and test
// buffers get plain output so scripts can keep parsing it.
func bannerTerminal(out io.Writer) (banner.Options, bool) {
	f, ok := out.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) || os.Getenv("TERM") == "dumb" {
		return banner.Options{}, false
	}
	opts := banner.Options{Color: banner.ANSI256}
	if width, _, err := term.GetSize(int(f.Fd())); err == nil {
		opts.Width = width
	}
	switch strings.ToLower(os.Getenv("COLORTERM")) {
	case "truecolor", "24bit":
		opts.Color = banner.TrueColor
	}
	if os.Getenv("WT_SESSION") != "" { // Windows Terminal supports 24-bit color.
		opts.Color = banner.TrueColor
	}
	if _, set := os.LookupEnv("NO_COLOR"); set || !enableVirtualTerminal(f) {
		opts.Color = banner.NoColor
	}
	// COLORFGBG is "fg;bg" (or "fg;default;bg"); ANSI 7 and 15 are light grays.
	if parts := strings.Split(os.Getenv("COLORFGBG"), ";"); len(parts) > 1 {
		switch parts[len(parts)-1] {
		case "7", "15":
			opts.LightBackground = true
		}
	}
	return opts, true
}

// bannerInfo lists the build details shown beneath the title.
func bannerInfo() []string {
	info := []string{"Version: " + currentVersion()}
	if commit := currentCommit(); commit != "" {
		info = append(info, "Commit:  "+commit)
	}
	return append(info, "Built:   "+runtime.GOOS+"/"+runtime.GOARCH+" ("+runtime.Version()+")")
}

// printBanner writes the banner when out is a terminal and reports whether it
// did. Rendering errors are swallowed: the banner is decoration only.
func printBanner(out io.Writer) bool {
	opts, ok := bannerTerminal(out)
	if !ok {
		return false
	}
	return banner.Render(out, bannerInfo(), opts) == nil
}

func init() {
	// Top-level help opens with the banner; subcommand help stays compact.
	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd == rootCmd {
			printBanner(cmd.OutOrStdout())
		}
		defaultHelp(cmd, args)
	})
}
