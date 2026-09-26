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

// fileRenameInformation is FILE_RENAME_INFORMATION. RootDirectory makes the
// destination name relative to an already-open directory handle, which avoids
// reopening a mutable path after the layout has been pinned.
type fileRenameInformation struct {
	ReplaceIfExists uint32
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [1]uint16
}

var (
	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	replaceFileW = kernel32.NewProc("ReplaceFileW")
	moveFileExW  = kernel32.NewProc("MoveFileExW")

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
		result, _, callErr := moveFileExW.Call(
			uintptr(unsafe.Pointer(source)),
			uintptr(unsafe.Pointer(destination)),
			0,
		)
		if result == 0 {
			return result, windowsCallError(callErr)
		}
		return result, nil
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
	directory, err := root.Open(".")
	if err != nil {
		return fmt.Errorf("open replacement directory: %w", err)
	}
	defer directory.Close()
	connection, err := directory.SyscallConn()
	if err != nil {
		return fmt.Errorf("get replacement directory handle: %w", err)
	}
	var renameErr error
	err = connection.Control(func(directoryHandle uintptr) {
		renameErr = replaceRootedWindowsHandle(windows.Handle(directoryHandle), source, destination)
	})
	if err != nil {
		return fmt.Errorf("lock replacement directory handle: %w", err)
	}
	if renameErr != nil {
		return fmt.Errorf("replace config atomically: %w", renameErr)
	}
	return nil
}

func rootedLeafName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `\\/:`+"\x00")
}

const fileRenameInformationEx = 65

var (
	ntdll                = syscall.NewLazyDLL("ntdll.dll")
	ntOpenFile           = ntdll.NewProc("NtOpenFile")
	windowsCloseHandle   = windows.CloseHandle
	windowsOpenForRename = func(directory windows.Handle, source string) (windows.Handle, error) {
		name, err := windows.NewNTUnicodeString(source)
		if err != nil {
			return 0, err
		}
		attributes := windows.OBJECT_ATTRIBUTES{
			Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
			RootDirectory: directory,
			ObjectName:    name,
		}
		var handle windows.Handle
		var status windows.IO_STATUS_BLOCK
		result, _, _ := ntOpenFile.Call(
			uintptr(unsafe.Pointer(&handle)),
			uintptr(windows.SYNCHRONIZE|windows.DELETE),
			uintptr(unsafe.Pointer(&attributes)),
			uintptr(unsafe.Pointer(&status)),
			uintptr(windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE),
			uintptr(windows.FILE_OPEN_REPARSE_POINT|windows.FILE_OPEN_FOR_BACKUP_INTENT|windows.FILE_SYNCHRONOUS_IO_NONALERT),
		)
		if result != 0 {
			return 0, windows.NTStatus(result).Errno()
		}
		return handle, nil
	}
	windowsSetRenameInfo = func(source, directory windows.Handle, destination string, flags, informationClass uint32) error {
		name, err := windows.UTF16FromString(destination)
		if err != nil {
			return err
		}
		name = name[:len(name)-1]
		var prototype fileRenameInformation
		size := int(unsafe.Offsetof(prototype.FileName)) + len(name)*2
		buffer := make([]byte, size)
		info := (*fileRenameInformation)(unsafe.Pointer(&buffer[0]))
		info.ReplaceIfExists = flags
		info.RootDirectory = directory
		info.FileNameLength = uint32(len(name) * 2)
		copy(unsafe.Slice(&info.FileName[0], len(name)), name)
		var status windows.IO_STATUS_BLOCK
		return windows.NtSetInformationFile(source, &status, &buffer[0], uint32(len(buffer)), informationClass)
	}
)

func replaceRootedWindowsHandle(directory windows.Handle, source, destination string) error {
	handle, err := windowsOpenForRename(directory, source)
	if err != nil {
		return fmt.Errorf("open replacement source relative to pinned directory: %w", windowsCallError(err))
	}
	defer windowsCloseHandle(handle)
	flags := uint32(windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS)
	err = windowsSetRenameInfo(handle, directory, destination, flags, fileRenameInformationEx)
	if err == nil {
		return nil
	}
	if !renameInfoFallback(err) {
		return fmt.Errorf("rename destination relative to pinned directory: %w", windowsCallError(err))
	}
	// Old Windows/filesystems do not implement the extended information class
	// or POSIX semantics. The legacy class remains rooted and replace-existing.
	err = windowsSetRenameInfo(handle, directory, destination, windows.FILE_RENAME_REPLACE_IF_EXISTS, windows.FileRenameInformation)
	if err != nil {
		return fmt.Errorf("rename destination relative to pinned directory: %w", windowsCallError(err))
	}
	return nil
}

func renameInfoFallback(err error) bool {
	status, ok := err.(windows.NTStatus)
	return ok && (status == windows.STATUS_INVALID_INFO_CLASS || status == windows.STATUS_INVALID_PARAMETER || status == windows.STATUS_NOT_SUPPORTED)
}

func windowsCallError(err error) error {
	if err == nil {
		return errors.New("Windows API returned failure without an error code")
	}
	if status, ok := err.(windows.NTStatus); ok {
		return status.Errno()
	}
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return errors.New("Windows API returned failure without an error code")
	}
	return err
}
