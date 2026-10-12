//go:build unix

package exec

import (
	"errors"
	"os"
	"syscall"
)

// unixTree is a run's process group: the group the start created, which is
// the process's own id. A descendant that leaves it on purpose, as a daemon
// does by starting a session of its own, leaves the tree, and Hive never
// relies on ending such a process through its starter's tree.
type unixTree struct {
	proc *os.Process
	pgid int
	// term is the controller side of a terminal start.
	term *os.File
}

func (t *unixTree) wait() Result {
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
	err := syscall.Kill(-t.pgid, syscall.SIGTERM)
	if t.term != nil {
		t.term.Close()
	}
	if gone(err) {
		return nil
	}
	return err
}

// end sends SIGKILL to the group.
func (t *unixTree) end() error {
	err := syscall.Kill(-t.pgid, syscall.SIGKILL)
	if gone(err) {
		return nil
	}
	return err
}

// empty says whether the group has no process left.
func (t *unixTree) empty() bool {
	return gone(syscall.Kill(-t.pgid, 0))
}

// gone says whether the error of a signal to the group says that no process
// of it is left to signal: ESRCH, or EPERM, which macOS gives for a group of
// processes that have ended and are not yet reaped. Hive started every
// process of its tree as its own user, so a permission it lacks is not one it
// could have been refused.
func gone(err error) bool {
	return errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EPERM)
}

func (t *unixTree) close() {
	if t.proc != nil {
		t.proc.Release()
	}
}
