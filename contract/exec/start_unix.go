//go:build unix

package exec

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

// checkLimits refuses an argument list over the platform's limits before
// anything starts.
func checkLimits(p plan) *StartError {
	total, perString := limits()
	return checkUnixLimits(append([]string{p.path}, p.args...), p.env, total, perString)
}

// openStream opens the child's end of a stream of a plain or resident start,
// and returns the parent's end of a pipe, nil for the others.
func openStream(s Stream, input bool) (child *os.File, parent *os.File, err error) {
	switch s.kind {
	case streamPipe:
		r, w, err := os.Pipe()
		if err != nil {
			return nil, nil, err
		}
		if input {
			return r, w, nil
		}
		return w, r, nil
	case streamFile:
		if input {
			f, err := os.Open(s.path)
			return f, nil, err
		}
		f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		return f, nil, err
	case streamAppend:
		if input {
			return nil, nil, errors.New("exec: standard input is not appended to")
		}
		f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
		return f, nil, err
	}
	f, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	return f, nil, err
}

// startPlatform starts the run through the standard library's process API:
// a plain start in a new process group of its own (Setpgid), a terminal
// start on a pseudo-terminal in a new session with that terminal as its
// controlling terminal (Setsid and Setctty), a resident start in a new
// session (setsid). The first word of the arguments is the resolved path, so
// that a link acts as the command whose name it was started under.
func startPlatform(p plan) (*started, *StartError) {
	closeInherited()
	fail := func(err error) (*started, *StartError) {
		var errno syscall.Errno
		if errors.As(err, &errno) && errno == syscall.E2BIG {
			total, _ := limits()
			size := unixSize(append([]string{p.path}, p.args...), p.env)
			return nil, &StartError{Reason: TooLarge, Limit: min(total, size-1), Actual: size, Err: err}
		}
		return nil, &StartError{Reason: StartFailed, Err: err}
	}
	attr := &os.ProcAttr{Dir: p.spec.Dir, Env: p.env}
	var opened []*os.File
	closeAll := func() {
		for _, f := range opened {
			f.Close()
		}
	}
	st := &started{}
	var master *os.File
	switch p.spec.Start {
	case TerminalStart:
		m, slave, err := openPTY()
		if err != nil {
			return fail(err)
		}
		master = m
		if err := setWindow(master, p.spec.Window); err != nil {
			master.Close()
			slave.Close()
			return fail(fmt.Errorf("exec: setting the window size: %w", err))
		}
		opened = append(opened, slave)
		attr.Files = []*os.File{slave, slave, slave}
		attr.Sys = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	default:
		var parents [3]*os.File
		for i, s := range []Stream{p.spec.Stdin, p.spec.Stdout, p.spec.Stderr} {
			child, parent, err := openStream(s, i == 0)
			if err != nil {
				closeAll()
				for _, f := range parents {
					if f != nil {
						f.Close()
					}
				}
				return fail(err)
			}
			opened = append(opened, child)
			attr.Files = append(attr.Files, child)
			parents[i] = parent
		}
		sys := &syscall.SysProcAttr{Setpgid: true}
		if p.spec.Start == ResidentStart {
			sys = &syscall.SysProcAttr{Setsid: true}
		}
		attr.Sys = sys
		if parents[0] != nil {
			st.stdin = parents[0]
		}
		if parents[1] != nil {
			st.stdout = parents[1]
		}
		if parents[2] != nil {
			st.stderr = parents[2]
		}
	}
	proc, err := os.StartProcess(p.path, append([]string{p.path}, p.args...), attr)
	closeAll()
	if err != nil {
		if master != nil {
			master.Close()
		}
		for _, c := range []io.Closer{st.stdin, st.stdout, st.stderr} {
			if c != nil && !isNil(c) {
				c.Close()
			}
		}
		return fail(err)
	}
	st.id, _ = identify(proc.Pid)
	st.tree = &unixTree{proc: proc, pgid: proc.Pid, resident: p.spec.Start == ResidentStart, term: master}
	if master != nil {
		st.term = &pty{master: master}
	}
	return st, nil
}

// isNil says whether a closer holds a nil *os.File.
func isNil(c io.Closer) bool {
	f, ok := c.(*os.File)
	return ok && f == nil
}
