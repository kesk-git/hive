//go:build unix

package exec

import (
	"errors"
	"os"
	"sync/atomic"
	"syscall"
	"time"
)

// unixTree is a run's process group: the group the start created, which is
// the process's own id. A descendant that leaves it on purpose, as a daemon
// does by starting a session of its own, leaves the tree, and Hive never
// relies on ending such a process through its starter's tree.
//
// The group's number belongs to the tree only while a process of the group
// lives: once none does, the number may name any group. So the tree is
// signaled only until it has once been seen gone, and never when the process
// of that number is now another one. While the leader is unreaped its number
// cannot be taken; once it is reaped, and a descendant keeps the group, the
// group is watched until it is seen gone, so that its number is never
// signaled after it could have been taken, whether or not the process that
// took it can still be read.
type unixTree struct {
	proc *os.Process
	pgid int
	// id is the identity of the group's leader as started.
	id ProcessID
	// over is set once the group has been seen gone, or its number has been
	// seen taken by another process.
	over atomic.Bool
	// resident is a start no reaper waits for.
	resident bool
	// term is the controller side of a terminal start.
	term *os.File
}

func (t *unixTree) wait() Result {
	res := t.status()
	// The leader is reaped, and its number is the group's only while a
	// process of the group lives: look at once, while that is least doubtful,
	// and go on looking while a descendant keeps the group.
	if !t.empty() {
		go t.watch()
	}
	return res
}

// watchEvery is how often a group whose leader has ended is looked at.
const watchEvery = 100 * time.Millisecond

// watch looks at the group until it is seen gone, which empty records.
func (t *unixTree) watch() {
	tick := time.NewTicker(watchEvery)
	defer tick.Stop()
	for range tick.C {
		if t.empty() {
			return
		}
	}
}

func (t *unixTree) status() Result {
	state, err := t.proc.Wait()
	if err != nil {
		return Result{Outcome: Lost, Err: err}
	}
	status, ok := state.Sys().(syscall.WaitStatus)
	switch {
	case !ok:
		return Result{Outcome: Lost, Err: errors.New("exec: the wait status is not readable")}
	case status.Exited():
		return Result{Outcome: Exited, Code: int64(status.ExitStatus())}
	case status.Signaled():
		return Result{Outcome: Ended, Code: int64(status.Signal()), Signal: status.Signal().String()}
	}
	return Result{Outcome: Lost, Err: errors.New("exec: the process neither exited nor was ended by a signal")}
}

// interrupt writes Control-C, 0x03, to the terminal's input, which its line
// discipline turns into SIGINT for the foreground process group.
func (t *unixTree) interrupt() error {
	if t.term == nil {
		return errors.New("exec: a run without a terminal has no interrupt")
	}
	_, err := t.term.Write([]byte{0x03})
	return err
}

// request sends SIGTERM to the group, and for a terminal start also closes
// the controller side, which hangs the terminal up (SIGHUP).
func (t *unixTree) request() error {
	err := t.signal(syscall.SIGTERM)
	if t.term != nil {
		t.term.Close()
	}
	return err
}

// end sends SIGKILL to the group.
func (t *unixTree) end() error { return t.signal(syscall.SIGKILL) }

// signal sends the signal to the group, unless it is gone: signal 0 asks only
// whether a process is left in it.
func (t *unixTree) signal(sig syscall.Signal) error {
	if t.over.Load() {
		return nil
	}
	if cur, err := identify(t.pgid); err == nil && t.id.Start != 0 && cur != t.id {
		t.over.Store(true)
		return nil
	}
	err := syscall.Kill(-t.pgid, sig)
	if gone(err) {
		t.over.Store(true)
		return nil
	}
	return err
}

// empty says whether the group has no process left.
func (t *unixTree) empty() bool {
	if t.over.Load() {
		return true
	}
	if t.signal(0) != nil {
		return false
	}
	return t.over.Load()
}

// gone says whether the error of a signal to the group says that no process
// of it is left to signal: ESRCH, or EPERM, which macOS gives for a group of
// processes that have ended and are not yet reaped. Hive started every
// process of its tree as its own user, so a permission it lacks is not one it
// could have been refused.
func gone(err error) bool {
	return errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EPERM)
}

func (t *unixTree) closeEnds() bool { return false }

// close releases the process of a resident start. The process of any other
// start is released when the reaper has waited for it.
func (t *unixTree) close() {
	if t.resident && t.proc != nil {
		t.proc.Release()
	}
}
