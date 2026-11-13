package main

import "strings"

// version and commit are populated by release builds through GoReleaser ldflags:
// -X main.version={{.Version}} -X main.commit={{.Commit}}
var (
	version = "dev"
	commit  = ""
)

func buildVersion() string {
	value := strings.TrimSpace(version)
	if value == "" {
		return "dev"
	}
	return value
}

func buildCommit() string {
	return strings.TrimSpace(commit)
}
