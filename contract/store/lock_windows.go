//go:build windows

package store

import (
	"os"
	"syscall"
	"unsafe"
)

// LockFileEx and UnlockFileEx are reached through the standard library's lazy
// DLL loader: the standard library's syscall package names neither.
var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	lockfileFailImmediately = 0x1
	lockfileExclusiveLock   = 0x2

	// errorLockViolation is what LockFileEx reports for a range another
	// handle holds when it is told not to wait.
	errorLockViolation = syscall.Errno(33)

	// The lock covers the file's whole range: its length is the largest the
	// call takes, in both halves.
	wholeLow, wholeHigh = 0xFFFFFFFF, 0xFFFFFFFF
)

// tryLock makes one attempt, without waiting in the call
// (LOCKFILE_FAIL_IMMEDIATELY), to lock the whole range of f with LockFileEx:
// exclusively, or shared when shared is set.
func tryLock(f *os.File, shared bool) error {
	flags := uintptr(lockfileFailImmediately)
	if !shared {
		flags |= lockfileExclusiveLock
	}
	var ol syscall.Overlapped
	r, _, err := procLockFileEx.Call(f.Fd(), flags, 0, wholeLow, wholeHigh, uintptr(unsafe.Pointer(&ol)))
	if r == 0 {
		return err
	}
	return nil
}

// lockBusy reports whether err from tryLock says another holder has the lock,
// as opposed to a failure to lock at all.
func lockBusy(err error) bool { return err == errorLockViolation }

// unlock releases the lock of f with UnlockFileEx, over the range tryLock
// locked.
func unlock(f *os.File) {
	var ol syscall.Overlapped
	procUnlockFileEx.Call(f.Fd(), 0, wholeLow, wholeHigh, uintptr(unsafe.Pointer(&ol)))
}
