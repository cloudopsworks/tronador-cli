//go:build windows

package replacement

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestReplacePathUsesReplaceFileForExistingDestination(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "temporary")
	destination := filepath.Join(directory, "destination")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalReplace, originalMove := windowsReplaceFile, windowsMoveFile
	t.Cleanup(func() {
		windowsReplaceFile = originalReplace
		windowsMoveFile = originalMove
	})
	var gotDestination, gotSource string
	windowsReplaceFile = func(destination, source *uint16) (uintptr, error) {
		gotDestination = syscall.UTF16ToString((*[1 << 20]uint16)(unsafe.Pointer(destination))[:])
		gotSource = syscall.UTF16ToString((*[1 << 20]uint16)(unsafe.Pointer(source))[:])
		return 1, nil
	}
	windowsMoveFile = func(*uint16, *uint16) (uintptr, error) {
		t.Fatal("MoveFileExW called for an existing destination")
		return 0, nil
	}

	if err := replacePath(source, destination); err != nil {
		t.Fatal(err)
	}
	if gotDestination != destination || gotSource != source {
		t.Fatalf("ReplaceFileW paths = %q, %q; want %q, %q", gotDestination, gotSource, destination, source)
	}
}

func TestReplacePathUsesMoveFileForInitialCreation(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "temporary")
	destination := filepath.Join(directory, "destination")
	originalReplace, originalMove := windowsReplaceFile, windowsMoveFile
	t.Cleanup(func() {
		windowsReplaceFile = originalReplace
		windowsMoveFile = originalMove
	})
	windowsReplaceFile = func(*uint16, *uint16) (uintptr, error) {
		t.Fatal("ReplaceFileW called for a missing destination")
		return 0, nil
	}
	var gotDestination, gotSource string
	windowsMoveFile = func(source, destination *uint16) (uintptr, error) {
		gotSource = syscall.UTF16ToString((*[1 << 20]uint16)(unsafe.Pointer(source))[:])
		gotDestination = syscall.UTF16ToString((*[1 << 20]uint16)(unsafe.Pointer(destination))[:])
		return 1, nil
	}

	if err := replacePath(source, destination); err != nil {
		t.Fatal(err)
	}
	if gotDestination != destination || gotSource != source {
		t.Fatalf("MoveFileExW paths = %q, %q; want %q, %q", gotDestination, gotSource, destination, source)
	}
}

func TestReplacePathReportsZeroErrno(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "destination")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalReplace := windowsReplaceFile
	t.Cleanup(func() { windowsReplaceFile = originalReplace })
	windowsReplaceFile = func(*uint16, *uint16) (uintptr, error) {
		return 0, syscall.Errno(0)
	}

	err := replacePath(filepath.Join(directory, "temporary"), destination)
	if err == nil || !strings.Contains(err.Error(), "without an error code") {
		t.Fatalf("replacePath error = %v, want meaningful zero-errno error", err)
	}
}

func TestWindowsCallErrorPreservesError(t *testing.T) {
	want := errors.New("failure")
	if got := windowsCallError(want); !errors.Is(got, want) {
		t.Fatalf("windowsCallError = %v, want %v", got, want)
	}
}
