package readme

import "embed"

// defaultAssets are offline fallback copies of the canonical README assets.
// Canonical runtime-syncable assets live in GitHub under cloudopsworks/tronador.
// Use `tronador readme assets sync` to refresh local cache/project copies without
// rebuilding the tronador-cli binary.
//
//go:embed assets/README.yaml assets/README.md.gotmpl
var defaultAssets embed.FS

const (
	defaultTemplateAsset = "assets/README.md.gotmpl"
	defaultYAMLAsset     = "assets/README.yaml"
)
