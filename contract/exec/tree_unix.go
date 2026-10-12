//go:build unix

package exec

import (
	"errors"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// unixTree is a run's process group: the group the start created, which is
// the process's own id. A descendant that leaves it on purpose, as a daemon
// does by starting a session of its own, leaves the tree, and Hive never
// relies on ending such a process through its starter's tree.
//
// The group's number is the leader's id, and no process or group can be
// given it while the leader, Hive's own child, exists, ended or not, until
// Hive reaps it. So the leader is held unreaped for as long as Hive holds the
// run, and every signal goes to a group whose number cannot have been given
// to another. Hive never looks at whether the group is empty: letting the run
// go is one last SIGKILL to the group and then the reap, after which the
// group is never signaled again. A resident start is let go without being
// ended.
type unixTree struct {
	proc *os.Process
	pgid int
	// resident is a start no reaper waits for.
	resident bool
	// term is the controller side of a terminal start.
	term *os.File

	// mu orders the signals against the release.
	mu sync.Mutex
	// exited is set once the leader's end has been observed.
	exited bool
	// released is set once Hive has let the run go: the leader is reaped as
	// soon as it has ended.
	released bool
}

// The codes of a child's end in the siginfo waitid fills, the same on macOS
// and Linux.
const (
	cldExited = 1
	cldKilled = 2
	cldDumped = 3
)

// pPID is waitid's P_PID: the id names one process.
const pPID = 1

// wait observes the leader's end without reaping it, and reaps it at once
// where the run is already let go.
func (t *unixTree) wait() Result {
	res := t.observe()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.exited = true
	if t.released {
		t.proc.Wait()
	}
	return res
}

// observe waits for the leader to end, with WNOWAIT so that it stays
// unreaped, and reports how it ended.
func (t *unixTree) observe() Result {
	var info siginfo
	for {
		_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, pPID, uintptr(t.pgid), uintptr(unsafe.Pointer(&info)), syscall.WEXITED|syscall.WNOWAIT, 0, 0)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return Result{Outcome: Lost, Err: errno}
		}
		break
	}
	switch info.Code {
	case cldExited:
		return Result{Outcome: Exited, Code: int64(info.Status)}
	case cldKilled, cldDumped:
		sig := syscall.Signal(info.Status)
		return Result{Outcome: Ended, Code: int64(sig), Signal: sig.String()}
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

// signal sends the signal to the group while Hive holds the run.
func (t *unixTree) signal(sig syscall.Signal) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.released {
		return nil
	}
	if err := syscall.Kill(-t.pgid, sig); err != nil && !gone(err) {
		return err
	}
	return nil
}

// empty says whether the leader has ended. The group is not looked at: what
// is left of it is ended when the run is let go.
func (t *unixTree) empty() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.exited
}

// gone says whether the error of a signal to the group says that no process
// of it is left to signal: ESRCH, or EPERM, which macOS gives for a group of
// processes that have ended and are not yet reaped. Hive started every
// process of its tree as its own user, so a permission it lacks is not one it
// could have been refused.
func gone(err error) bool {
	return errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EPERM)
}

// closeEnds says that close ends what is left of the group, except for a
// resident start.
func (t *unixTree) closeEnds() bool { return !t.resident }

// release lets the run go: one last SIGKILL to the group, and the reap of
// the leader, now if it has ended and otherwise as soon as it ends. A
// resident start is let go without being ended: Hive releases its process,
// which nothing waits for.
func (t *unixTree) release() {
	if t.resident {
		if t.proc != nil {
			t.proc.Release()
		}
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.released {
		return
	}
	t.released = true
	syscall.Kill(-t.pgid, syscall.SIGKILL)
	if t.exited {
		t.proc.Wait()
	}
}

// close lets the run go.
func (t *unixTree) close() { t.release() }
