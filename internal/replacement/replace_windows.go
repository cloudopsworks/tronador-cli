//go:build windows

package replacement

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32     = windows.NewLazySystemDLL("kernel32.dll")
	replaceFileW = kernel32.NewProc("ReplaceFileW")

	windowsReplaceFile = func(destination, source *uint16) (uintptr, error) {
		result, _, callErr := replaceFileW.Call(
			uintptr(unsafe.Pointer(destination)),
			uintptr(unsafe.Pointer(source)),
			0,
			0,
			0,
			0,
		)
		if result == 0 {
			return result, windowsCallError(callErr)
		}
		return result, nil
	}
	windowsMoveFile = func(source, destination *uint16) (uintptr, error) {
		if err := windows.MoveFileEx(source, destination, 0); err != nil {
			return 0, err
		}
		return 1, nil
	}
)

// replacePath uses ReplaceFileW for an existing destination. Unlike the Go
// runtime's MoveFileEx replacement path, ReplaceFileW is the Windows API that
// combines save/new, rename-old, rename-new, and delete-old replacement steps.
// For an initial creation there is no old destination to replace, so a
// same-directory MoveFileExW creates the destination instead. A reported
// Windows replacement failure can leave either path changed; callers must stop
// and report the error rather than assuming either file was preserved.
func replacePath(source, destination string) error {
	sourceUTF16, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return fmt.Errorf("encode replacement source: %w", err)
	}
	destinationUTF16, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return fmt.Errorf("encode replacement destination: %w", err)
	}
	_, statErr := os.Lstat(destination)
	if errors.Is(statErr, os.ErrNotExist) {
		result, callErr := windowsMoveFile(sourceUTF16, destinationUTF16)
		if result == 0 {
			return fmt.Errorf("create destination with MoveFileExW: %w", windowsCallError(callErr))
		}
		return nil
	}
	if statErr != nil {
		return fmt.Errorf("inspect replacement destination: %w", statErr)
	}
	result, callErr := windowsReplaceFile(destinationUTF16, sourceUTF16)
	if result == 0 {
		return fmt.Errorf("replace destination with ReplaceFileW: %w", windowsCallError(callErr))
	}
	return nil
}

func replaceInRoot(root *os.Root, source, destination string) error {
	if root == nil {
		return os.ErrInvalid
	}
	if !rootedLeafName(source) || !rootedLeafName(destination) {
		return fmt.Errorf("rooted replacement names must be non-empty leaf names: %w", os.ErrInvalid)
	}
	if err := root.Rename(source, destination); err != nil {
		return fmt.Errorf("replace config in root: %w", err)
	}
	return nil
}

func rootedLeafName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `\\/:`+"\x00")
}

func windowsCallError(err error) error {
	if err == nil {
		return errors.New("Windows API returned failure without an error code")
	}
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return errors.New("Windows API returned failure without an error code")
	}
	return err
}
