// Package replacement contains small filesystem primitives for replacing a
// completed same-directory temporary file with its destination.
package replacement

// Replace installs source at destination. Callers must close source before
// calling Replace and keep source in the same directory as destination.
// Replacement errors leave the operating system state unspecified on Windows, so
// callers must report the error and clean up source only when it still exists.
func Replace(source, destination string) error {
	return replace(source, destination)
}

var replace = replacePath
