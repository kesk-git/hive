//go:build windows

package store

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// SetFileInformationByHandle is not in the standard library's syscall
// package; it is reached through its lazy DLL loader.
var procSetFileInformationByHandle = kernel32.NewProc("SetFileInformationByHandle")

const (
	// deleteAccess is the right to delete a file, which a rename through its
	// handle needs.
	deleteAccess = 0x00010000

	// shareAll lets every other handle read, write and delete the file, so
	// that a handle of Hive's never blocks a replacement.
	shareAll = syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE

	// fileRenameInfoEx is FILE_INFO_BY_HANDLE_CLASS FileRenameInfoEx, with the
	// flags that replace the target while handles to it are open.
	fileRenameInfoEx            = 22
	fileRenameFlagReplace       = 0x1
	fileRenameFlagPosixSemantic = 0x2

	errorSharingViolation = syscall.Errno(32)
)

// renameWait is how long a rename that a handle outside Hive refuses is
// retried, and renamePause how long it waits between two attempts.
var (
	renameWait  = 2 * time.Second
	renamePause = 10 * time.Millisecond
)

// extendedPath is path in the form Windows takes for any length: absolute,
// and with the long-path prefix once it is too long for the short form, as
// the standard library's own file functions do.
func extendedPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if len(abs) < 248 || strings.HasPrefix(abs, `\\?\`) || strings.HasPrefix(abs, `\\.\`) {
		return abs
	}
	if strings.HasPrefix(abs, `\\`) {
		return `\\?\UNC\` + abs[2:]
	}
	return `\\?\` + abs
}

// openHandle opens the file at path with the access and creation disposition
// given, sharing it with every other handle.
func openHandle(path string, access, create uint32) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(extendedPath(path))
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := syscall.CreateFile(name, access, shareAll, nil, create, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// OpenRead opens the file at path for reading, as every reader of a home's
// files does: on Windows with read, write and delete sharing, so that its
// handle never blocks the replacement of the file (spec/records.md, Writing
// one record).
func OpenRead(path string) (*os.File, error) {
	return openHandle(path, syscall.GENERIC_READ, syscall.OPEN_EXISTING)
}

// ReadFile reads the whole file at path, as OpenRead opens it.
func ReadFile(path string) ([]byte, error) {
	f, err := OpenRead(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// createTemp creates the temporary file at tmp for writing and deleting,
// truncating a file a failed attempt left under that name. Deleting is the
// right a rename through the file's own handle needs.
func createTemp(tmp string) (*os.File, error) {
	return openHandle(tmp, syscall.GENERIC_WRITE|deleteAccess, syscall.CREATE_ALWAYS)
}

// publish renames the flushed temporary file f at tmp over the record at full
// through f's own handle, with POSIX semantics and replacement, which replaces
// the record while a reader holds the old one open, and then flushes f through
// that handle, as the flush of the directories is on Windows
// (spec/records.md, Writing one record). A rename that a handle outside Hive
// refuses is retried for a bounded time; if it is still refused the temporary
// file is removed and the old record stands.
func (s *Store) publish(f *os.File, tmp, full, rel string) error {
	if err := s.step("rename " + rel); err != nil {
		f.Close()
		return err
	}
	if err := renameOver(f, full); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("store: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("store: flushing %s: %w", rel, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("store: closing %s: %w", rel, err)
	}
	return nil
}

// fileRenameInfo is FILE_RENAME_INFO, with the flags of FileRenameInfoEx in
// its first word.
type fileRenameInfo struct {
	Flags          uint32
	RootDirectory  syscall.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

// renameOver renames the file open as f to the path to, replacing what is
// there, and retries it while a handle outside Hive refuses it.
func renameOver(f *os.File, to string) error {
	name, err := syscall.UTF16FromString(extendedPath(to))
	if err != nil {
		return err
	}
	// name ends in its NUL, which the buffer keeps and the length leaves out.
	size := unsafe.Offsetof(fileRenameInfo{}.FileName) + uintptr(len(name))*2
	buf := make([]uint64, (size+7)/8)
	info := (*fileRenameInfo)(unsafe.Pointer(&buf[0]))
	info.Flags = fileRenameFlagReplace | fileRenameFlagPosixSemantic
	info.FileNameLength = uint32((len(name) - 1) * 2)
	copy(unsafe.Slice(&info.FileName[0], len(name)), name)

	deadline := time.Now().Add(renameWait)
	for {
		r, _, err := procSetFileInformationByHandle.Call(f.Fd(), fileRenameInfoEx, uintptr(unsafe.Pointer(&buf[0])), size)
		if r != 0 {
			return nil
		}
		if !renameRefused(err) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(renamePause)
	}
}

// renameRefused reports whether err says a handle that something outside
// Hive holds, such as an indexer's or a scanner's, refused the rename, which
// may pass.
func renameRefused(err error) bool {
	return err == syscall.ERROR_ACCESS_DENIED || err == errorSharingViolation || err == errorLockViolation
}

// syncDir does nothing on Windows, which offers no directory handle that can
// be flushed: the flush of the renamed file in publish stands for it
// (spec/records.md, Writing one record).
func syncDir(dir string) error { return nil }

// syncFile flushes the file at path through a handle opened with write
// access.
func syncFile(path string) error {
	f, err := openHandle(path, syscall.GENERIC_WRITE, syscall.OPEN_EXISTING)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("store: flushing %s: %w", path, err)
	}
	return nil
}
