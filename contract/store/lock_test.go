package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/clock"
)

// waiting returns a Waiting run by a fake clock, and a count of the pauses
// taken, which move that clock.
func waiting(limit time.Duration) (Waiting, *clock.Fake, *int) {
	clk := clock.NewFake(epoch)
	pauses := new(int)
	w := Waiting{Clock: clk, Limit: limit, Pause: func(d time.Duration) {
		*pauses++
		clk.Advance(d)
	}}
	return w, clk, pauses
}

// Two commands contend for one store's lock: the second waits the bounded
// time and stops with lock_held, then takes the lock once the first lets go.
func TestLockContention(t *testing.T) {
	home := newHome(t)
	w, clk, pauses := waiting(100 * time.Millisecond)

	first, err := Acquire(home, "pool", false, w)
	if err != nil {
		t.Fatal(err)
	}
	start := clk.Now()
	second, err := Acquire(home, "pool", false, w)
	if !errors.Is(err, ErrLockHeld) || second != nil {
		t.Fatalf("second Acquire = %v, %v; want ErrLockHeld", second, err)
	}
	if Code(err) != "lock_held" {
		t.Errorf("Code = %q", Code(err))
	}
	if waited := clk.Now().Sub(start); waited < 100*time.Millisecond || waited > 100*time.Millisecond+lockPause {
		t.Errorf("waited %v, want the bounded %v", waited, 100*time.Millisecond)
	}
	if *pauses == 0 {
		t.Error("the waiter never paused")
	}
	// Another name is another lock.
	other, err := Acquire(home, "spawning-pool", false, w)
	if err != nil {
		t.Fatal(err)
	}
	other.Release()

	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	*pauses = 0
	second, err = Acquire(home, "pool", false, w)
	if err != nil {
		t.Fatal(err)
	}
	if *pauses != 0 {
		t.Error("a free lock was waited for")
	}
	second.Release()
	if err := second.Release(); err != nil {
		t.Errorf("releasing twice: %v", err)
	}
}

// Readers that need several records to agree share the lock; a writer waits
// for all of them, and they wait for a writer.
func TestSharedLocks(t *testing.T) {
	home := newHome(t)
	w, _, _ := waiting(50 * time.Millisecond)

	a, err := Acquire(home, "pool", true, w)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Acquire(home, "pool", true, w)
	if err != nil {
		t.Fatalf("a second shared lock: %v", err)
	}
	if !a.Shared() || !b.Shared() || a.Name() != "pool" {
		t.Errorf("Shared/Name wrong: %v %v %q", a.Shared(), b.Shared(), a.Name())
	}
	if _, err := Acquire(home, "pool", false, w); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("exclusive against shared: %v", err)
	}
	a.Release()
	if _, err := Acquire(home, "pool", false, w); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("exclusive against one remaining shared: %v", err)
	}
	b.Release()
	x, err := Acquire(home, "pool", false, w)
	if err != nil {
		t.Fatal(err)
	}
	if x.Shared() {
		t.Error("an exclusive lock says it is shared")
	}
	if _, err := Acquire(home, "pool", true, w); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("shared against exclusive: %v", err)
	}
	x.Release()
	c, err := Acquire(home, "pool", true, w)
	if err != nil {
		t.Fatal(err)
	}
	c.Release()
}

// A lock file left by a crashed process is not stale: the operating system
// released its lock, and the next command locks the file afresh, at once,
// without touching its content.
func TestLockFileLeftByACrashIsNotStale(t *testing.T) {
	home := newHome(t)
	w, _, pauses := waiting(50 * time.Millisecond)
	locks := filepath.Join(home, locksDir)
	if err := os.Mkdir(locks, dirMode); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(locks, "pool.lock")
	if err := os.WriteFile(path, []byte("pid 4242, long gone\n"), fileMode); err != nil {
		t.Fatal(err)
	}
	l, err := Acquire(home, "pool", false, w)
	if err != nil {
		t.Fatalf("a left-over lock file blocked: %v", err)
	}
	if *pauses != 0 {
		t.Error("a left-over lock file was waited for")
	}
	l.Release()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "pid 4242, long gone\n" {
		t.Errorf("the lock file was changed or removed: %q, %v", data, err)
	}
}

func TestAcquireNamesAndModes(t *testing.T) {
	home := newHome(t)
	w, _, _ := waiting(time.Millisecond)
	for _, name := range []string{"", "Pool", "pool.lock", "../x", "a b", "journal/x"} {
		if _, err := Acquire(home, name, false, w); err == nil {
			t.Errorf("Acquire(%q) succeeded", name)
		}
	}
	for _, name := range []string{"journal", "pool", "spawning-pool", "ui"} {
		l, err := Acquire(home, name, false, w)
		if err != nil {
			t.Fatalf("Acquire(%q): %v", name, err)
		}
		l.Release()
		info, err := os.Stat(filepath.Join(home, locksDir, name+".lock"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != fileMode {
			t.Errorf("%s.lock mode %v", name, info.Mode())
		}
	}
	info, err := os.Stat(filepath.Join(home, locksDir))
	if err != nil || info.Mode().Perm() != dirMode {
		t.Errorf("locks/ mode %v, %v", info.Mode(), err)
	}
	if _, err := CheckHome(home); err != nil {
		t.Errorf("the home with locks is refused: %v", err)
	}
}

// The zero Waiting waits DefaultLockWait and the default pause is a real one;
// the store's Lock and LockShared go through Acquire with its own Waiting.
func TestWaitingDefaults(t *testing.T) {
	var w Waiting
	if w.limit() != DefaultLockWait {
		t.Errorf("limit = %v", w.limit())
	}
	if _, ok := w.clock().(clock.System); !ok {
		t.Errorf("clock = %T", w.clock())
	}
	before := time.Now()
	w.pause(time.Millisecond)
	if time.Since(before) < time.Millisecond {
		t.Error("the default pause did not pause")
	}

	home := newHome(t)
	s, _, _ := open(t, home, "pool")
	wr := lock(t, s)
	if _, err := s.Lock(); !errors.Is(err, ErrLockHeld) {
		t.Errorf("second Lock: %v", err)
	}
	if _, err := s.LockShared(); !errors.Is(err, ErrLockHeld) {
		t.Errorf("LockShared against Lock: %v", err)
	}
	wr.Release()
	sh, err := s.LockShared()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lock(); !errors.Is(err, ErrLockHeld) {
		t.Errorf("Lock against LockShared: %v", err)
	}
	sh.Release()
	wr = lock(t, s)
	wr.Release()
	if _, err := wr.Replace(id("req", 1), RecordPath("item", id("task", 1)), []byte(`{}`), 0); !errors.Is(err, errReleased) {
		t.Errorf("Replace after Release: %v", err)
	}
	if _, err := wr.WriteOnce(id("req", 1), ReceiptPath(id("rcpt", 1)), []byte(`{}`)); !errors.Is(err, errReleased) {
		t.Errorf("WriteOnce after Release: %v", err)
	}
	if _, err := wr.Commit(Tx{}); !errors.Is(err, errReleased) {
		t.Errorf("Commit after Release: %v", err)
	}
	if err := wr.Release(); err != nil {
		t.Errorf("Release twice: %v", err)
	}
}
