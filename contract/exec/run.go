package exec

import (
	"errors"
	"io"
	"runtime"
	"sync"
	"time"
)

// tree is a started run's tree on one platform: the process group on macOS
// and Linux, the job object on Windows. The platform implements it.
type tree interface {
	// wait blocks until the run's own process ends and reports how, from the
	// operating system.
	wait() Result
	// interrupt writes the terminal's interrupt character, Control-C, to the
	// terminal's input. Only a terminal start has one.
	interrupt() error
	// request asks the tree to end: SIGTERM to the group, and a hang-up for a
	// terminal, on macOS and Linux; on Windows the pseudoconsole is closed for
	// a terminal start and standard input for a plain one.
	request() error
	// end ends the whole tree: SIGKILL to the group, or TerminateJobObject.
	end() error
	// empty says whether no process of the tree is left.
	empty() bool
	// closeEnds says whether close ends what is still in the tree.
	closeEnds() bool
	// close releases what Hive holds, except what the reaper of a run
	// waited for still uses, which the reaper releases itself: on Windows
	// the job, which ends what is in it.
	close()
}

// started is what the platform made of a start.
type started struct {
	tree   tree
	id     ProcessID
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	term   Terminal
}

// plan is a run after its checks: everything the platform needs to start it.
type plan struct {
	spec Spec
	// path is the resolved, absolute path; the first word of the arguments.
	path string
	// args is the argument list after the first word.
	args []string
	// env is the child's environment, built from nothing.
	env []string
}

// Run is a started run.
type Run struct {
	// Path is the program as resolved: an absolute path.
	Path string
	// ID identifies the process: its id with its start time.
	ID ProcessID

	started
	kind StartKind

	done chan struct{}

	mu     sync.Mutex
	result Result
	ending bool
	stopMu sync.Mutex
	closed bool
}

// Start starts the run, or says why it was not started: every refusal is a
// *StartError, and nothing runs then. Before anything starts the program is
// found by the lookup rules, the arguments are checked against the platform's
// limits, and the child's environment is built from nothing.
func Start(spec Spec) (*Run, error) {
	if err := spec.check(); err != nil {
		return nil, err
	}
	goos := runtime.GOOS
	if err := checkArguments(goos, spec.Args); err != nil {
		err.Program = spec.Program
		return nil, err
	}
	path, serr := lookup(spec.Program)
	if serr != nil {
		return nil, serr
	}
	env, err := buildEnv(envInput{goos: goos, environ: ownEnviron(), config: spec.Config, terminal: spec.Start == TerminalStart})
	if err != nil {
		return nil, &StartError{Reason: Refused, Program: spec.Program, Err: err}
	}
	p := plan{spec: spec, path: path, args: spec.Args, env: env}
	if serr := checkLimits(p); serr != nil {
		serr.Program = spec.Program
		return nil, serr
	}
	st, serr := startPlatform(p)
	if serr != nil {
		serr.Program = spec.Program
		return nil, serr
	}
	r := &Run{Path: path, ID: st.id, started: *st, kind: spec.Start, done: make(chan struct{})}
	if spec.Start != ResidentStart {
		go r.reap()
	}
	return r, nil
}

// reap waits for the run's own process and records its result.
func (r *Run) reap() {
	res := r.tree.wait()
	r.mu.Lock()
	if r.ending {
		res = Result{Outcome: StoppedByHive, Code: res.Code, Signal: res.Signal}
	}
	r.result = res
	r.mu.Unlock()
	close(r.done)
}

// Stdin is the pipe to the program's standard input, or nil where the stream
// is not a pipe.
func (r *Run) Stdin() io.WriteCloser { return r.stdin }

// Stdout is the pipe from the program's standard output, or nil.
func (r *Run) Stdout() io.ReadCloser { return r.stdout }

// Stderr is the pipe from the program's standard error, or nil.
func (r *Run) Stderr() io.ReadCloser { return r.stderr }

// Terminal is the controller side of a terminal start, or nil.
func (r *Run) Terminal() Terminal { return r.term }

// Done is closed when the run's own process has ended. A resident start is
// not waited for, and its Done is never closed.
func (r *Run) Done() <-chan struct{} { return r.done }

// Wait waits for the run's process to end and returns its result. It does
// not close the pipes: a caller reads the program's output until it ends
// first, and then waits. A resident start is not waited for, and Wait on one
// returns at once with the result lost.
func (r *Run) Wait() Result {
	if r.kind == ResidentStart {
		return Result{Outcome: Lost, Err: errors.New("exec: a resident start is not waited for")}
	}
	<-r.done
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.result
}

// stopped is how long Stop waits, after the end, for the tree to be gone.
const stopWait = 10 * time.Second

// pollEvery is how often Stop looks at the tree.
const pollEvery = 10 * time.Millisecond

// Stop stops the run in up to three steps, each taken only after the one
// before has had the grace period: for a terminal start the interrupt,
// Control-C written to its input; the request, which asks the tree to end;
// and the end, which ends the whole tree. A step is not taken once the tree
// is gone.
//
// Before it ends the tree, Stop calls ending, which records that Hive is
// ending it, so that the result is read from that record and never inferred
// from an exit code. An error from ending stops Stop before anything is
// ended, and is returned. A tree Hive ended reads StoppedByHive, whatever
// exit code or signal the end gave. A resident start is stopped through its
// own command, and Stop on one is refused.
func (r *Run) Stop(g Grace, ending func() error) (Result, error) {
	if r.kind == ResidentStart {
		return Result{}, errors.New("exec: a resident start is stopped through its own command")
	}
	r.stopMu.Lock()
	defer r.stopMu.Unlock()
	if r.gone() {
		return r.Wait(), nil
	}
	if r.term != nil {
		_ = r.tree.interrupt()
		if r.goneWithin(g.Interrupt) {
			return r.Wait(), nil
		}
	}
	_ = r.tree.request()
	if r.goneWithin(g.Request) {
		return r.Wait(), nil
	}
	if ending != nil {
		if err := ending(); err != nil {
			return Result{}, err
		}
	}
	r.markEnding()
	if err := r.tree.end(); err != nil {
		return Result{}, err
	}
	r.goneWithin(stopWait)
	res := r.Wait()
	return res, nil
}

// markEnding records that Hive is ending the tree, so that the result is read
// from the record: a result already read becomes StoppedByHive too.
func (r *Run) markEnding() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ending = true
	if r.result.Outcome != "" {
		r.result = Result{Outcome: StoppedByHive, Code: r.result.Code, Signal: r.result.Signal}
	}
}

// gone says whether the run's process has ended and no process of its tree
// is left.
func (r *Run) gone() bool {
	select {
	case <-r.done:
		return r.tree.empty()
	default:
		return false
	}
}

// goneWithin waits up to d for the tree to be gone.
func (r *Run) goneWithin(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if r.gone() {
			return true
		}
		left := time.Until(deadline)
		if left <= 0 {
			return false
		}
		time.Sleep(min(left, pollEvery))
	}
}

// Close releases what Hive holds of the run: its pipes, its terminal and, on
// Windows, its job, which ends what is still in it. For a resident start it
// releases the handle of a process it does not wait for and ends nothing.
func (r *Run) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()
	if r.kind != ResidentStart && r.tree.closeEnds() && !r.tree.empty() {
		r.markEnding()
	}
	for _, c := range []io.Closer{r.stdin, r.stdout, r.stderr} {
		if c != nil {
			c.Close()
		}
	}
	if c, ok := r.term.(io.Closer); ok {
		c.Close()
	}
	r.tree.close()
	return nil
}
