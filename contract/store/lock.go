package store

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/kesk-git/hive/contract/clock"
)

// DefaultLockWait is how long a command waits for a transaction lock when
// its Waiting sets no limit.
const DefaultLockWait = 5 * time.Second

// lockPause is how long a waiter pauses between two attempts.
const lockPause = 10 * time.Millisecond

// journalLock is the name of the journal's transaction lock.
const journalLock = "journal"

// Waiting says how a command waits for a lock. The zero value waits
// DefaultLockWait by the system clock, pausing with time.Sleep. A test gives
// its own clock and a pause that moves it, so the wait is run by control.
type Waiting struct {
	Clock clock.Clock
	Limit time.Duration
	Pause func(time.Duration)
}

func (w Waiting) clock() clock.Clock {
	if w.Clock == nil {
		return clock.System{}
	}
	return w.Clock
}

func (w Waiting) limit() time.Duration {
	if w.Limit <= 0 {
		return DefaultLockWait
	}
	return w.Limit
}

func (w Waiting) pause(d time.Duration) {
	if w.Pause == nil {
		time.Sleep(d)
		return
	}
	w.Pause(d)
}

// Lock is a held transaction lock (spec/records.md, Locks): an advisory lock
// on locks/<name>.lock, which the operating system releases when the process
// ends. A crash therefore leaves nothing to clean, and a lock file left on
// disk is never stale: the next command locks it afresh. The file is never
// removed, because a lock on a removed file would guard nothing.
type Lock struct {
	f      *os.File
	name   string
	shared bool
}

// Acquire takes the lock named name in the home at dir: the name of a
// package for its store, or journal for the home's journal. An exclusive
// lock waits for every other holder, a shared one for an exclusive holder
// only. It waits as w says and then fails with ErrLockHeld.
func Acquire(dir, name string, shared bool, w Waiting) (*Lock, error) {
	if name != journalLock && !slices.Contains(storePackages, name) {
		return nil, fmt.Errorf("store: %q is not a lock name", name)
	}
	locks := filepath.Join(dir, locksDir)
	if err := os.MkdirAll(locks, dirMode); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(locks, name+".lock"), os.O_RDWR|os.O_CREATE, fileMode)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	how := syscall.LOCK_EX | syscall.LOCK_NB
	if shared {
		how = syscall.LOCK_SH | syscall.LOCK_NB
	}
	clk := w.clock()
	deadline := clk.Now().Add(w.limit())
	for {
		err := syscall.Flock(int(f.Fd()), how)
		if err == nil {
			return &Lock{f, name, shared}, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			f.Close()
			return nil, fmt.Errorf("store: locking %s: %w", name, err)
		}
		if !clk.Now().Before(deadline) {
			f.Close()
			return nil, fmt.Errorf("%w: %s", ErrLockHeld, name)
		}
		w.pause(lockPause)
	}
}

// Name is the name the lock was taken under.
func (l *Lock) Name() string { return l.name }

// Shared reports whether the lock is held shared.
func (l *Lock) Shared() bool { return l.shared }

// Release releases the lock. Releasing twice is harmless.
func (l *Lock) Release() error {
	if l.f == nil {
		return nil
	}
	f := l.f
	l.f = nil
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return f.Close()
}
