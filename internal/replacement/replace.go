// Package replacement contains small filesystem primitives for replacing a
// completed same-directory temporary file with its destination.
package replacement

import "os"

// Replace installs source at destination. Callers must close source before
// calling Replace and keep source in the same directory as destination.
// Replacement errors leave the operating system state unspecified on Windows, so
// callers must report the error and clean up source only when it still exists.
func Replace(source, destination string) error {
	return replace(source, destination)
}

// ReplaceInRoot installs source at destination without resolving either name
// outside root. Source and destination must name entries in the pinned root.
// On Windows this is a rooted namespace rename/replace, not ReplaceFileW's
// metadata-preserving replacement transaction; callers prepare the temporary
// file's intended mode before the rename. Callers must close source before
// calling ReplaceInRoot.
func ReplaceInRoot(root *os.Root, source, destination string) error {
	return replaceInRoot(root, source, destination)
}

var replace = replacePath
