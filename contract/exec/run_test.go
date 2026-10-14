package exec

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/envelope"
)

// A program runs in the directory the operation names, and under the
// resolved path as its own name, so that a link acts as the command whose
// name it was started under.
func TestRunDirectoryAndName(t *testing.T) {
	dir := fakeDir(t, "fakeprog")
	work := t.TempDir()
	out, res := runOutput(t, withFake(t, Spec{Dir: work}, "cwd"))
	if !res.Passed() {
		t.Fatalf("%v", res)
	}
	got, _ := filepath.EvalSymlinks(strings.TrimSpace(out))
	want, _ := filepath.EvalSymlinks(work)
	if got != want {
		t.Errorf("the working directory is %q, want %q", got, want)
	}
	out, _ = runOutput(t, withFake(t, Spec{}, "argv0"))
	exe := "fakeprog"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if got := strings.TrimSpace(out); got != filepath.Join(dir, exe) {
		t.Errorf("the program's own name is %q, want %q", got, filepath.Join(dir, exe))
	}
}

// Standard streams are pipes, named files or the null device, and bytes pass
// untranslated: nothing changes a line ending or decodes text.
func TestStreams(t *testing.T) {
	fakeDir(t, "fakeprog")
	// Output bytes of both line endings and a NUL arrive as they were written.
	out, res := runOutput(t, withFake(t, Spec{}, "bytes"))
	if !res.Passed() || hexOf(out) != hexOf("a\r\nb\nc\x00d\r\n") {
		t.Errorf("%q, %v", out, res)
	}
	// Input through a pipe is echoed untouched.
	payload := "x\r\ny\nz\x00\xff\xfe\r"
	spec := withFake(t, Spec{Stdin: Pipe(), Stdout: Pipe()}, "echo")
	run, err := Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	defer run.Close()
	go func() {
		io.WriteString(run.Stdin(), payload)
		run.Stdin().Close()
	}()
	got, _ := io.ReadAll(run.Stdout())
	if string(got) != payload || !run.Wait().Passed() {
		t.Errorf("echoed %q", got)
	}
	// Files: input read from a file, output created and truncated, or appended to.
	work := t.TempDir()
	in, outFile := filepath.Join(work, "in.bin"), filepath.Join(work, "out.bin")
	os.WriteFile(in, []byte(payload), 0o600)
	os.WriteFile(outFile, []byte("old content that is longer than the payload\n"), 0o600)
	run2 := fake(t, Spec{Stdin: File(in), Stdout: File(outFile)}, "echo")
	if res := run2.Wait(); !res.Passed() {
		t.Fatal(res)
	}
	if data, _ := os.ReadFile(outFile); string(data) != payload {
		t.Errorf("file output %q", data)
	}
	run3 := fake(t, Spec{Stdin: File(in), Stdout: AppendFile(outFile)}, "echo")
	run3.Wait()
	if data, _ := os.ReadFile(outFile); string(data) != payload+payload {
		t.Errorf("appended output %q", data)
	}
	// The null device: input is empty, output is discarded.
	run4 := fake(t, Spec{Stdin: Null(), Stdout: Null()}, "echo")
	if res := run4.Wait(); !res.Passed() {
		t.Error(res)
	}
	if run4.Stdin() != nil || run4.Stdout() != nil || run4.Stderr() != nil {
		t.Error("a pipe was made for a stream that is not one")
	}
	// A standard error pipe is separate from output.
	run5 := fake(t, Spec{Stdout: Null(), Stderr: Pipe()}, "needstty")
	data, _ := io.ReadAll(run5.Stderr())
	if res := run5.Wait(); res.Outcome != Exited || res.Code != 1 || !strings.Contains(string(data), "not a terminal") {
		t.Errorf("%v %q", res, data)
	}
}

// Each way a run can end gives the result the table names.
func TestResults(t *testing.T) {
	fakeDir(t, "fakeprog")
	for _, code := range []string{"0", "1", "7", "63"} {
		run := fake(t, Spec{}, "exit", code)
		want, _ := strconv.Atoi(code)
		if res := run.Wait(); res.Outcome != Exited || res.Code != int64(want) {
			t.Errorf("exit %s: %v", code, res)
		}
	}
	if runtime.GOOS != "windows" {
		run := fake(t, Spec{}, "selfkill")
		res := run.Wait()
		if res.Outcome != Ended || res.Signal != "killed" || res.Code != 9 {
			t.Errorf("a signal: %+v", res)
		}
	} else {
		run := fake(t, Spec{}, "selfkill")
		res := run.Wait()
		if res.Outcome != Ended || uint32(res.Code) < 0xC0000000 {
			t.Errorf("an exception: %+v", res)
		}
	}
	// A project's validation command passes only by exiting with 0.
	for res, want := range map[Result]bool{
		{Outcome: Exited, Code: 0}: true, {Outcome: Exited, Code: 1}: false, {Outcome: Ended}: false,
		{Outcome: StoppedByHive}: false, {Outcome: Lost}: false, {Outcome: NotStarted}: false,
	} {
		if res.Passed() != want {
			t.Errorf("%v passes: %v", res, res.Passed())
		}
	}
}

// A run that was not started is each of: not found, found and not usable,
// refused by the lookup rules, or its start call failed.
func TestNotStarted(t *testing.T) {
	fakeDir(t, "fakeprog")
	spec := withFake(t, Spec{}, "exit", "0")
	spec.Program.Name = "missing-program"
	_, err := Start(spec)
	serr, ok := err.(*StartError)
	if !ok || serr.Reason != Missing || serr.Result().Outcome != NotStarted {
		t.Fatalf("%v", err)
	}
	// A working directory that does not exist is a start call that failed.
	spec = withFake(t, Spec{Dir: filepath.Join(t.TempDir(), "nowhere")}, "exit", "0")
	_, err = Start(spec)
	if serr, ok := err.(*StartError); !ok || serr.Reason != StartFailed {
		t.Errorf("%v", err)
	}
	// The exit states of the operation that made the run.
	for _, tc := range []struct {
		err    StartError
		status envelope.Status
		code   string
	}{
		{StartError{Reason: Missing, Program: Program{Core, "git"}}, envelope.Unavailable, "dependency_missing"},
		{StartError{Reason: Missing, Program: Program{Hive, "hive-pool"}}, envelope.Unavailable, "dependency_missing"},
		{StartError{Reason: Missing, Program: Program{Platform, "security"}}, envelope.Unavailable, "platform_utility_missing"},
		{StartError{Reason: NotExecutable, Program: Program{Core, "gh"}, File: "/x/gh"}, envelope.Unavailable, "dependency_unusable"},
		{StartError{Reason: BatchFile, Program: Program{Core, "claude"}, File: `C:\npm\claude.cmd`}, envelope.Unavailable, "dependency_unusable"},
		{StartError{Reason: BatchFile, Program: Program{Platform, "security"}, File: "s.cmd"}, envelope.Unavailable, "platform_utility_unusable"},
		{StartError{Reason: BreakawayRefused, Program: Program{Optional, "chrome-devtools-axi"}, Err: errors.New("denied")}, envelope.Unavailable, "dependency_unusable"},
		{StartError{Reason: StartFailed, Program: Program{Core, "codex"}, Err: errors.New("denied")}, envelope.Unavailable, "dependency_unusable"},
		{StartError{Reason: TooLarge, Program: Program{Core, "git"}, Limit: 10, Actual: 20}, envelope.TooLarge, "record_too_large"},
		{StartError{Reason: Refused, Program: Program{Core, "git"}, Err: errors.New("x")}, envelope.Failed, "internal"},
	} {
		f := tc.err.Failure()
		if f.Status != tc.status || f.Err.Code != tc.code || f.Check() != nil {
			t.Errorf("%s: %+v", tc.err.Reason, f)
		}
	}
	// A batch file's failure names the file in its diagnostic, never in its
	// message.
	if f := (&StartError{Reason: BatchFile, Program: Program{Core, "claude"}, File: `C:\npm\claude.cmd`}).Failure(); strings.Contains(f.Err.Message, `C:\npm\claude.cmd`) || !strings.Contains(strings.Join(f.Diagnostic().Chain, "\n"), `C:\npm\claude.cmd`) {
		t.Errorf("%q, %q", f.Err.Message, f.Diagnostic().Chain)
	}
}

// An external program that gave no result is unknown where it may have had an
// effect outside Hive and unavailable where it could have had none; one that
// exited is read by its own contract.
func TestExternalMapping(t *testing.T) {
	git := Program{Core, "git"}
	util := Program{Platform, "security"}
	if code, f := (Result{Outcome: Exited, Code: 128}).External(git, true); f != nil || code != 128 {
		t.Errorf("%d %v", code, f)
	}
	for _, r := range []Result{{Outcome: Ended, Signal: "killed", Code: 9}, {Outcome: StoppedByHive}, {Outcome: Lost, Err: errors.New("wait")}} {
		_, f := r.External(git, true)
		if f == nil || f.Status != envelope.Unknown || f.Err.Code != "outcome_unknown" || f.Err.Retryable || f.Check() != nil {
			t.Errorf("%v with an effect: %+v", r, f)
		}
		_, f = r.External(git, false)
		if f == nil || f.Status != envelope.Unavailable || f.Err.Code != "dependency_unusable" {
			t.Errorf("%v without: %+v", r, f)
		}
		_, f = r.External(util, false)
		if f == nil || f.Err.Code != "platform_utility_unusable" {
			t.Errorf("%v utility: %+v", r, f)
		}
	}
}

// Another Hive command is read from its complete response and never from its
// exit code: a run that leaves none agreeing with its exit code is unknown.
func TestHiveResult(t *testing.T) {
	const id = "req_00000000000000000000000001"
	resp := &envelope.Response{
		RequestID: id, Command: "hive-pool", Operation: "echo", Status: envelope.Ok, CompletedAt: "2026-10-11T08:00:00.000Z",
		Responder: envelope.Responder{Package: "pool", Command: "hive-pool", ImplementationVersion: "0.1.0", ContractVersion: envelope.ContractVersion},
		Receipts:  []envelope.Receipt{}, Body: map[string]any{},
	}
	data, err := resp.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if got, f := (Result{Outcome: Exited, Code: 0}).HiveResult(data, id); f != nil || got == nil || got.Status != envelope.Ok {
		t.Errorf("%+v %v", got, f)
	}
	for name, c := range map[string]struct {
		res    Result
		output []byte
	}{
		"an exit code that disagrees":   {Result{Outcome: Exited, Code: 1}, data},
		"no output":                     {Result{Outcome: Exited, Code: 0}, nil},
		"output cut short":              {Result{Outcome: Exited, Code: 0}, data[:len(data)/2]},
		"another request":               {Result{Outcome: Exited, Code: 0}, []byte(strings.Replace(string(data), id, "req_00000000000000000000000002", 1))},
		"a run a signal ended":          {Result{Outcome: Ended, Signal: "killed", Code: 9}, data},
		"a run Hive ended":              {Result{Outcome: StoppedByHive}, data},
		"a run that was lost":           {Result{Outcome: Lost, Err: errors.New("x")}, data},
		"a run that was not started":    {Result{Outcome: NotStarted}, data},
		"an exit code of a signal-like": {Result{Outcome: Exited, Code: 137}, data},
	} {
		got, f := c.res.HiveResult(c.output, id)
		if got != nil || f == nil || f.Status != envelope.Unknown || f.Err.Code != "outcome_unknown" {
			t.Errorf("%s: %+v %v", name, got, f)
		}
	}
}

// A run's process is identified by its id together with its start time, read
// in process, and a process that is another has another identity.
func TestProcessIdentity(t *testing.T) {
	fakeDir(t, "fakeprog")
	a := fake(t, Spec{Stdout: Pipe()}, "hold")
	readLine(t, a.Stdout())
	b := fake(t, Spec{Stdout: Pipe()}, "hold")
	readLine(t, b.Stdout())
	if a.ID.PID == 0 || a.ID.Start == 0 || a.ID == b.ID || a.ID.PID == b.ID.PID {
		t.Errorf("%+v %+v", a.ID, b.ID)
	}
	again, err := identify(a.ID.PID)
	if err != nil || again != a.ID {
		t.Errorf("read again: %+v, %v; started as %+v", again, err, a.ID)
	}
	self, err := identify(os.Getpid())
	if err != nil || self.Start == 0 || self.PID != os.Getpid() {
		t.Errorf("%+v %v", self, err)
	}
	if self.Start == a.ID.Start && self.PID == a.ID.PID {
		t.Error("two processes share an identity")
	}
	_ = time.Second
}

// stuckTree is a tree whose processes the end does not end, as a process in
// uninterruptible sleep is not.
type stuckTree struct{}

func (stuckTree) wait() Result     { select {} }
func (stuckTree) interrupt() error { return nil }
func (stuckTree) request() error   { return nil }
func (stuckTree) end() error       { return nil }
func (stuckTree) empty() bool      { return false }
func (stuckTree) release()         {}
func (stuckTree) closeEnds() bool  { return false }
func (stuckTree) close()           {}

// A tree still not gone after the end is not reported stopped: Stop returns
// an error rather than waiting for a process that has not ended.
func TestStopReportsATreeTheEndDidNotEnd(t *testing.T) {
	defer func(d time.Duration) { stopWait = d }(stopWait)
	stopWait = 50 * time.Millisecond
	run := &Run{started: started{tree: stuckTree{}}, done: make(chan struct{})}
	finished := make(chan error, 1)
	go func() {
		_, err := run.Stop(Grace{}, nil)
		finished <- err
	}()
	select {
	case err := <-finished:
		if err == nil {
			t.Error("Stop reported a tree that is not gone as stopped")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop waited for a process that has not ended")
	}
}
