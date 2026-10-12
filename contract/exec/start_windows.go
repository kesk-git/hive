package exec

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// winTree is a run's job object: the job the start created the process in,
// with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE and JOB_OBJECT_LIMIT_BREAKAWAY_OK, so
// every descendant is in it unless it asks to leave with
// CREATE_BREAKAWAY_FROM_JOB. A descendant that does leaves the tree, and Hive
// never relies on ending such a process through its starter's tree.
type winTree struct {
	process syscall.Handle
	job     syscall.Handle // zero for a resident, which is in no job
	// stdin is the pipe to a plain start's standard input: closing it is the
	// one request Windows offers a process that has no console.
	stdin io.Closer
	// console is the pseudoconsole of a terminal start.
	console *winConsole
	once    sync.Once
}

func (t *winTree) wait() Result {
	ev, err := syscall.WaitForSingleObject(t.process, infinite)
	if err != nil || ev != syscall.WAIT_OBJECT_0 {
		if err == nil {
			err = fmt.Errorf("exec: waiting returned %#x", ev)
		}
		return Result{Outcome: Lost, Err: err}
	}
	var code uint32
	if err := syscall.GetExitCodeProcess(t.process, &code); err != nil {
		return Result{Outcome: Lost, Err: err}
	}
	if code >= exceptionStatus {
		return Result{Outcome: Ended, Code: int64(code)}
	}
	return Result{Outcome: Exited, Code: int64(code)}
}

// interrupt writes Control-C, 0x03, to the pseudoconsole's input, which the
// pseudoconsole turns into a CTRL_C_EVENT for the processes attached to it.
func (t *winTree) interrupt() error {
	if t.console == nil {
		return errors.New("exec: a run without a terminal has no interrupt")
	}
	_, err := t.console.Write([]byte{0x03})
	return err
}

// request closes the pseudoconsole of a terminal start, which sends
// CTRL_CLOSE_EVENT to its processes, or for a plain start its standard input.
func (t *winTree) request() error {
	switch {
	case t.console != nil:
		return t.console.closeConsole()
	case t.stdin != nil:
		return t.stdin.Close()
	}
	return nil
}

// end ends the whole tree with TerminateJobObject.
func (t *winTree) end() error {
	if t.job == 0 {
		return errors.New("exec: a run in no job has no tree to end")
	}
	return call(procTerminateJobObject, uintptr(t.job), terminatedByInterrupt)
}

// empty says whether no process is left in the job.
func (t *winTree) empty() bool {
	if t.job == 0 {
		return true
	}
	n, err := activeProcesses(t.job)
	return err == nil && n == 0
}

// close releases Hive's handles. Closing the job ends what is still in it.
func (t *winTree) close() {
	t.once.Do(func() {
		if t.console != nil {
			t.console.close()
		}
		if t.process != 0 {
			syscall.CloseHandle(t.process)
		}
		if t.job != 0 {
			syscall.CloseHandle(t.job)
		}
	})
}

// maxPending is how much unread output of a terminal Hive keeps: what a
// terminal shows is its rendering and never a record, and a holder that does
// not read must not stop the pseudoconsole from closing.
const maxPending = 1 << 20

// winConsole is the controller side of a pseudoconsole: Hive keeps the
// writing end of the input pipe and the reading end of the output pipe. The
// output is read by a goroutine for as long as the pseudoconsole lives, which
// ClosePseudoConsole needs in order to return.
type winConsole struct {
	hpc syscall.Handle
	in  *os.File // writing end of the input pipe
	out *os.File // reading end of the output pipe

	mu      sync.Mutex
	cond    *sync.Cond
	pending []byte
	eof     bool
	closing sync.Once
	closed  error
}

func newWinConsole(hpc syscall.Handle, in, out *os.File) *winConsole {
	c := &winConsole{hpc: hpc, in: in, out: out}
	c.cond = sync.NewCond(&c.mu)
	go c.pump()
	return c
}

func (c *winConsole) pump() {
	buf := make([]byte, 32*1024)
	for {
		n, err := c.out.Read(buf)
		c.mu.Lock()
		c.pending = append(c.pending, buf[:n]...)
		if over := len(c.pending) - maxPending; over > 0 {
			c.pending = c.pending[over:]
		}
		if err != nil {
			c.eof = true
		}
		c.cond.Broadcast()
		c.mu.Unlock()
		if err != nil {
			return
		}
	}
}

func (c *winConsole) Read(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.pending) == 0 {
		if c.eof {
			return 0, io.EOF
		}
		c.cond.Wait()
	}
	n := copy(b, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

func (c *winConsole) Write(b []byte) (int, error) { return c.in.Write(b) }

func (c *winConsole) Resize(w Window) error {
	r, _, _ := procResizePseudoConsole.Call(uintptr(c.hpc), coord(w))
	if r != 0 {
		return fmt.Errorf("exec: ResizePseudoConsole: HRESULT %#x", uint32(r))
	}
	return nil
}

// closeConsole closes the pseudoconsole while the output goes on being
// drained, and sends CTRL_CLOSE_EVENT to its processes.
func (c *winConsole) closeConsole() error {
	c.closing.Do(func() {
		procClosePseudoConsole.Call(uintptr(c.hpc))
	})
	return c.closed
}

// close closes the console and Hive's ends of the pipes.
func (c *winConsole) close() {
	c.closeConsole()
	c.in.Close()
	c.out.Close()
}

// Close implements io.Closer for the holder of the terminal.
func (c *winConsole) Close() error { c.close(); return nil }

// handle is the handle of a file, which stays valid while the file is open.
func handle(f *os.File) syscall.Handle { return syscall.Handle(f.Fd()) }

// inheritable returns a copy of the handle that a child can inherit.
func inheritable(h syscall.Handle) (syscall.Handle, error) {
	self, _ := syscall.GetCurrentProcess()
	var dup syscall.Handle
	if err := syscall.DuplicateHandle(self, h, self, &dup, 0, true, syscall.DUPLICATE_SAME_ACCESS); err != nil {
		return 0, err
	}
	return dup, nil
}

// envBlock is the environment as the UTF-16 block CreateProcessW takes: each
// variable ends in a NUL, and the block in one more. The variables are already
// sorted by name without regard to case.
func envBlock(env []string) ([]uint16, error) {
	var block []uint16
	for _, kv := range env {
		u, err := syscall.UTF16FromString(kv)
		if err != nil {
			return nil, err
		}
		block = append(block, u...)
	}
	if len(env) == 0 {
		block = append(block, 0)
	}
	return append(block, 0), nil
}

// startPlatform starts the run with one CreateProcessW call through syscall,
// with CREATE_UNICODE_ENVIRONMENT; CREATE_NO_WINDOW for a plain start; never
// CREATE_NEW_PROCESS_GROUP, which turns Control-C off for the new process;
// and an extended startup block whose attribute list carries the job the
// process is created in (PROC_THREAD_ATTRIBUTE_JOB_LIST), the handle list
// and, for a terminal start, a pseudoconsole. A resident start there is
// DETACHED_PROCESS, CREATE_BREAKAWAY_FROM_JOB and CREATE_SUSPENDED, in no job
// of its own; before it is resumed it is checked with IsProcessInJob, naming
// no job, to be in no job at all, and if it is in one, or the check fails, it
// is ended unresumed and the start is refused. A resident is reported as
// started only after it was resumed outside every job.
//
// For a terminal start the process inherits no handle at all: the
// pseudoconsole is its terminal, and the start passes no handle list.
func startPlatform(p plan) (*started, *StartError) {
	var closers []func()
	defer func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}()
	fail := func(err error) (*started, *StartError) {
		return nil, &StartError{Reason: StartFailed, Err: err}
	}
	resident := p.spec.Start == ResidentStart
	terminal := p.spec.Start == TerminalStart

	appName, err := syscall.UTF16PtrFromString(p.path)
	if err != nil {
		return fail(err)
	}
	cmdline, err := syscall.UTF16PtrFromString(windowsCommandLine(p.path, p.args))
	if err != nil {
		return fail(err)
	}
	dir, err := syscall.UTF16PtrFromString(p.spec.Dir)
	if err != nil {
		return fail(err)
	}
	env, err := envBlock(p.env)
	if err != nil {
		return fail(err)
	}

	st := &started{}
	tree := &winTree{}
	var si startupInfoEx
	si.Cb = uint32(unsafe.Sizeof(si))
	var inherit []syscall.Handle
	var parents []*os.File // Hive's ends, closed if the start fails
	failed := true
	defer func() {
		if failed {
			for _, f := range parents {
				f.Close()
			}
			if tree.console != nil {
				tree.console.close()
			}
		}
	}()

	var job syscall.Handle
	if !resident {
		job, err = newJob()
		if err != nil {
			return fail(fmt.Errorf("exec: creating the job object: %w", err))
		}
		closers = append(closers, func() {
			if failed {
				syscall.CloseHandle(job)
			}
		})
		tree.job = job
	}

	flags := uint32(syscall.CREATE_UNICODE_ENVIRONMENT | extendedStartupInfoPresent)
	attrs := 0
	if !resident {
		attrs++ // the job
	}
	if terminal {
		attrs++ // the pseudoconsole
	} else {
		attrs++ // the handle list
	}

	if terminal {
		inR, inW, err := os.Pipe()
		if err != nil {
			return fail(err)
		}
		outR, outW, err := os.Pipe()
		if err != nil {
			inR.Close()
			inW.Close()
			return fail(err)
		}
		if procCreatePseudoConsole.Find() != nil {
			inR.Close()
			inW.Close()
			outR.Close()
			outW.Close()
			return fail(errors.New("exec: this Windows has no pseudoconsole; it needs Windows 10 version 1809 or later"))
		}
		var hpc syscall.Handle
		r, _, _ := procCreatePseudoConsole.Call(coord(p.spec.Window), uintptr(handle(inR)), uintptr(handle(outW)), 0, uintptr(unsafe.Pointer(&hpc)))
		// The pseudoconsole holds its own ends; Hive closes its copies.
		inR.Close()
		outW.Close()
		if r != 0 {
			inW.Close()
			outR.Close()
			return fail(fmt.Errorf("exec: CreatePseudoConsole: HRESULT %#x", uint32(r)))
		}
		tree.console = newWinConsole(hpc, inW, outR)
		st.term = tree.console
	} else {
		var std [3]syscall.Handle
		for i, s := range []Stream{p.spec.Stdin, p.spec.Stdout, p.spec.Stderr} {
			child, parent, err := openWindowsStream(s, i == 0)
			if err != nil {
				return fail(err)
			}
			if parent != nil {
				parents = append(parents, parent)
			}
			dup, err := inheritable(handle(child))
			child.Close()
			if err != nil {
				return fail(err)
			}
			closers = append(closers, func() { syscall.CloseHandle(dup) })
			std[i] = dup
			if parent != nil {
				switch i {
				case 0:
					st.stdin = parent
					tree.stdin = parent
				case 1:
					st.stdout = parent
				case 2:
					st.stderr = parent
				}
			}
		}
		si.Flags |= syscall.STARTF_USESTDHANDLES
		si.StdInput, si.StdOutput, si.StdErr = std[0], std[1], std[2]
		for _, h := range std {
			dup := false
			for _, seen := range inherit {
				dup = dup || seen == h
			}
			if !dup {
				inherit = append(inherit, h)
			}
		}
		if resident {
			flags |= detachedProcess | createBreakawayFromJob | createSuspended
		} else {
			flags |= createNoWindow
		}
	}

	list, err := newAttributeList(attrs)
	if err != nil {
		return fail(fmt.Errorf("exec: the attribute list: %w", err))
	}
	closers = append(closers, list.delete)
	if !resident {
		jobs := []syscall.Handle{job}
		if err := list.update(procThreadAttributeJobList, uintptr(unsafe.Pointer(&jobs[0])), unsafe.Sizeof(jobs[0])*uintptr(len(jobs))); err != nil {
			return fail(fmt.Errorf("exec: the job attribute: %w", err))
		}
		defer runtime.KeepAlive(jobs)
	}
	if terminal {
		if err := list.update(procThreadAttributePseudoConsole, uintptr(tree.console.hpc), unsafe.Sizeof(tree.console.hpc)); err != nil {
			return fail(fmt.Errorf("exec: the pseudoconsole attribute: %w", err))
		}
	} else if err := list.update(procThreadAttributeHandleList, uintptr(unsafe.Pointer(&inherit[0])), unsafe.Sizeof(inherit[0])*uintptr(len(inherit))); err != nil {
		return fail(fmt.Errorf("exec: the handle list: %w", err))
	}
	si.AttributeList = list.pointer()

	var pi syscall.ProcessInformation
	err = syscall.CreateProcess(appName, cmdline, nil, nil, !terminal, flags, &env[0], dir, &si.StartupInfo, &pi)
	runtime.KeepAlive(inherit)
	runtime.KeepAlive(list)
	if err != nil {
		if resident && errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
			return nil, &StartError{Reason: BreakawayRefused, Err: err}
		}
		return fail(err)
	}
	tree.process = pi.Process
	thread := syscall.Handle(pi.Thread)

	if resident {
		// A breakaway can leave the inner job and keep the outer one without
		// the call failing, so the success of the call proves nothing: the
		// process must be in no job at all before it runs.
		var inJob int32
		if err := call(procIsProcessInJob, uintptr(tree.process), 0, uintptr(unsafe.Pointer(&inJob))); err != nil || inJob != 0 {
			syscall.TerminateProcess(tree.process, 1)
			syscall.CloseHandle(thread)
			syscall.CloseHandle(tree.process)
			if err == nil {
				err = errors.New("the process is in a job that does not let it leave")
			}
			return nil, &StartError{Reason: BreakawayRefused, Err: err}
		}
		if r, _, err := procResumeThread.Call(uintptr(thread)); int32(r) == -1 {
			syscall.TerminateProcess(tree.process, 1)
			syscall.CloseHandle(thread)
			syscall.CloseHandle(tree.process)
			return fail(fmt.Errorf("exec: resuming the process: %w", err))
		}
	}
	syscall.CloseHandle(thread)
	failed = false
	st.tree = tree
	if start, err := processStart(tree.process); err == nil {
		st.id = ProcessID{PID: int(pi.ProcessId), Start: start}
	} else {
		st.id = ProcessID{PID: int(pi.ProcessId)}
	}
	return st, nil
}

// openWindowsStream opens the child's end of a stream, and returns Hive's end
// of a pipe, nil for the others.
func openWindowsStream(s Stream, input bool) (child *os.File, parent *os.File, err error) {
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
