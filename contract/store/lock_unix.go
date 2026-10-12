//go:build unix

package store

import (
	"os"
	"syscall"
)

// tryLock makes one attempt, without waiting in the call, to lock the whole
// of f with flock: exclusively, or shared when shared is set.
func tryLock(f *os.File, shared bool) error {
	how := syscall.LOCK_EX | syscall.LOCK_NB
	if shared {
		how = syscall.LOCK_SH | syscall.LOCK_NB
	}
	return syscall.Flock(int(f.Fd()), how)
}

// lockBusy reports whether err from tryLock says another holder has the lock,
// as opposed to a failure to lock at all.
func lockBusy(err error) bool { return err == syscall.EWOULDBLOCK || err == syscall.EAGAIN }

// unlock releases the lock of f.
func unlock(f *os.File) { syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
