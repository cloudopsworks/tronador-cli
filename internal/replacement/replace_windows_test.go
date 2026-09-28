//go:build windows

package replacement

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
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
	for _, test := range []struct {
		name      string
		existing  bool
		operation string
		configure func()
	}{
		{
			name:      "ReplaceFileW",
			existing:  true,
			operation: "ReplaceFileW",
			configure: func() {
				windowsReplaceFile = func(*uint16, *uint16) (uintptr, error) { return 0, syscall.Errno(0) }
			},
		},
		{
			name:      "MoveFileExW",
			operation: "MoveFileExW",
			configure: func() {
				windowsMoveFile = func(*uint16, *uint16) (uintptr, error) { return 0, syscall.Errno(0) }
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			destination := filepath.Join(directory, "destination")
			if test.existing {
				if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			originalReplace, originalMove := windowsReplaceFile, windowsMoveFile
			t.Cleanup(func() {
				windowsReplaceFile = originalReplace
				windowsMoveFile = originalMove
			})
			test.configure()

			err := replacePath(filepath.Join(directory, "temporary"), destination)
			if err == nil || !strings.Contains(err.Error(), test.operation) || !strings.Contains(err.Error(), "without an error code") {
				t.Fatalf("replacePath error = %v, want meaningful %s zero-errno error", err, test.operation)
			}
		})
	}
}

func TestWindowsCallErrorPreservesError(t *testing.T) {
	want := errors.New("failure")
	if got := windowsCallError(want); !errors.Is(got, want) {
		t.Fatalf("windowsCallError = %v, want %v", got, want)
	}
}

func TestReplaceRootedWindowsHandleUsesPinnedDirectoryAndExtendedRename(t *testing.T) {
	originalOpen, originalSet, originalClose := windowsOpenForRename, windowsSetRenameInfo, windowsCloseHandle
	t.Cleanup(func() {
		windowsOpenForRename, windowsSetRenameInfo, windowsCloseHandle = originalOpen, originalSet, originalClose
	})
	windowsCloseHandle = func(windows.Handle) error { return nil }
	const directory = windows.Handle(41)
	const source = windows.Handle(42)
	windowsOpenForRename = func(gotDirectory windows.Handle, gotSource string) (windows.Handle, error) {
		if gotDirectory != directory || gotSource != ".tronador-temp" {
			t.Fatalf("open = (%v, %q), want (%v, %q)", gotDirectory, gotSource, directory, ".tronador-temp")
		}
		return source, nil
	}
	var calls int
	windowsSetRenameInfo = func(gotSource, gotDirectory windows.Handle, target string, flags, class uint32) error {
		calls++
		if gotSource != source || gotDirectory != directory || target != "gitversion.yaml" {
			t.Fatalf("rename = (%v, %v, %q)", gotSource, gotDirectory, target)
		}
		if flags != windows.FILE_RENAME_REPLACE_IF_EXISTS|windows.FILE_RENAME_POSIX_SEMANTICS || class != fileRenameInformationEx {
			t.Fatalf("rename flags/class = %#x/%d", flags, class)
		}
		return nil
	}
	if err := replaceRootedWindowsHandle(directory, ".tronador-temp", "gitversion.yaml"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("rename calls = %d, want 1", calls)
	}
}

func TestReplaceRootedWindowsHandleFallsBackOnlyForUnsupportedExtendedRename(t *testing.T) {
	originalOpen, originalSet, originalClose := windowsOpenForRename, windowsSetRenameInfo, windowsCloseHandle
	t.Cleanup(func() {
		windowsOpenForRename, windowsSetRenameInfo, windowsCloseHandle = originalOpen, originalSet, originalClose
	})
	windowsCloseHandle = func(windows.Handle) error { return nil }
	windowsOpenForRename = func(windows.Handle, string) (windows.Handle, error) { return windows.Handle(9), nil }
	var got [][2]uint32
	windowsSetRenameInfo = func(_ windows.Handle, _ windows.Handle, _ string, flags, class uint32) error {
		got = append(got, [2]uint32{flags, class})
		if len(got) == 1 {
			return windows.STATUS_INVALID_INFO_CLASS
		}
		return nil
	}
	if err := replaceRootedWindowsHandle(8, ".tronador-temp", "gitversion.yaml"); err != nil {
		t.Fatal(err)
	}
	want := [][2]uint32{{windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS, fileRenameInformationEx}, {windows.FILE_RENAME_REPLACE_IF_EXISTS, windows.FileRenameInformation}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rename calls = %#v, want %#v", got, want)
	}
}

func TestReplaceRootedWindowsHandleDoesNotFallbackForOperationalFailure(t *testing.T) {
	originalOpen, originalSet, originalClose := windowsOpenForRename, windowsSetRenameInfo, windowsCloseHandle
	t.Cleanup(func() {
		windowsOpenForRename, windowsSetRenameInfo, windowsCloseHandle = originalOpen, originalSet, originalClose
	})
	windowsCloseHandle = func(windows.Handle) error { return nil }
	windowsOpenForRename = func(windows.Handle, string) (windows.Handle, error) { return windows.Handle(9), nil }
	calls := 0
	windowsSetRenameInfo = func(windows.Handle, windows.Handle, string, uint32, uint32) error {
		calls++
		return windows.STATUS_ACCESS_DENIED
	}
	if err := replaceRootedWindowsHandle(8, ".tronador-temp", "gitversion.yaml"); err == nil {
		t.Fatal("replace unexpectedly succeeded")
	}
	if calls != 1 {
		t.Fatalf("rename calls = %d, want 1", calls)
	}
}

func TestWindowsCallErrorNormalizesNTStatus(t *testing.T) {
	if got, want := windowsCallError(windows.STATUS_ACCESS_DENIED), windows.STATUS_ACCESS_DENIED.Errno(); !errors.Is(got, want) {
		t.Fatalf("windowsCallError = %v, want errno %v", got, want)
	}
}
