//go:build !windows

package replacement

import "os"

func replacePath(source, destination string) error {
	return os.Rename(source, destination)
}

func replaceInRoot(root *os.Root, source, destination string) error {
	if root == nil {
		return os.ErrInvalid
	}
	return root.Rename(source, destination)
}
