package versions

import (
	_ "embed"
)

// version contains the current version of tronador-cli
//
//go:embed VERSION
var Version string

func GetVersion() string {
	return Version
}
