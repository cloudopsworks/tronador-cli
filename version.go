package main

import "strings"

// version and commit are populated by release builds through GoReleaser ldflags:
// -X main.version={{.Version}} -X main.commit={{.Commit}}
var (
	version = "dev"
	commit  = ""
)

func buildVersion() string {
	// Keep main.commit as a live GoReleaser ldflag target while preserving the
	// existing `tronador-cli version` output shape of printing only the version.
	_ = strings.TrimSpace(commit)

	value := strings.TrimSpace(version)
	if value == "" {
		return "dev"
	}
	return value
}
