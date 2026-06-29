package tools

import "embed"

// defaultConfigFS contains the built-in tool metadata used when no runtime overrides exist.
//
//go:embed assets/tools.json
var defaultConfigFS embed.FS
