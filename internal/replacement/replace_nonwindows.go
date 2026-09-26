//go:build !windows

package replacement

import "os"

func replacePath(source, destination string) error {
	return os.Rename(source, destination)
}
