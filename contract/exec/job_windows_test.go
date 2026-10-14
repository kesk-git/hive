package exec

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var procAssignProcessToJobObject = testKernel32.NewProc("AssignProcessToJobObject")

const (
	synchronize       = 0x00100000
	processTerminate  = 0x0001
	waitTimeoutStatus = 0x102
)

// alive says whether the process of the identity is still the one running:
// the id is in use with the same creation time.
func alive(id ProcessID) bool {
	h, err := syscall.OpenProcess(processQueryLimitedInformation|synchronize, false, uint32(id.PID))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	if ev, _ := syscall.WaitForSingleObject(h, 0); ev != waitTimeoutStatus {
		return false
	}
	start, err := processStart(h)
	return err == nil && start == id.Start
}

// kill ends the process of the identity, if it is still running.
func kill(id ProcessID) {
	if !alive(id) {
		return
	}
	if h, err := syscall.OpenProcess(processTerminate, false, uint32(id.PID)); err == nil {
		syscall.TerminateProcess(h, 1)
		syscall.CloseHandle(h)
	}
}

// The command line is built as syscall.EscapeArg quotes it.
func TestCommandLineIsQuotedAsEscapeArgQuotesIt(t *testing.T) {
	for _, a := range awkwardArguments {
		if got, want := string(appendEscapeArg(nil, a)), syscall.EscapeArg(a); got != want {
			t.Errorf("%q: %q, EscapeArg gives %q", a, got, want)
		}
	}
	if got, want := windowsCommandLine(`C:\x\a.exe`, awkwardArguments), `C:\x\a.exe `+strings.Join(escapeAll(awkwardArguments), " "); got != want {
		t.Errorf("%q", got)
	}
}

func escapeAll(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = syscall.EscapeArg(a)
	}
	return out
}

// A batch file of the name is refused and named, and a program that is found
// as <name>.exe is preferred to it.
func TestABatchFileIsNeverRun(t *testing.T) {
	dir := fakeDir(t)
	for _, name := range []string{"shim.cmd", "other.bat"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("@echo off\r\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, file := range map[string]string{"shim": "shim.cmd", "other": "other.bat"} {
		_, err := Start(Spec{Program: Program{Project, name}, Dir: dir})
		serr, ok := err.(*StartError)
		if !ok || serr.Reason != BatchFile || filepath.Base(serr.File) != file {
			t.Errorf("%s: %v", name, err)
		}
	}
	// With a native executable of the name on the search path, that is run.
	fakeDir(t, "fakeprog", "shim")
	if err := os.WriteFile(filepath.Join(startPath, "shim.cmd"), []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := withFake(t, Spec{}, "exit", "0")
	spec.Program.Name = "shim"
	run, err := Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	defer run.Close()
	if res := run.Wait(); !res.Passed() || !strings.EqualFold(filepath.Base(run.Path), "shim.exe") {
		t.Errorf("%v %s", res, run.Path)
	}
}

// The job of a run has the limits the specification names.
func TestTheJobOfARun(t *testing.T) {
	fakeDir(t, "fakeprog")
	run := fake(t, Spec{Stdout: Pipe()}, "hold")
	readLine(t, run.Stdout())
	var info jobExtendedLimit
	job := run.tree.(*winTree).job
	if err := call(procQueryInformationJobObject, uintptr(job), jobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), 0); err != nil {
		t.Fatal(err)
	}
	if info.Basic.LimitFlags&jobObjectLimitKillOnJobClose == 0 || info.Basic.LimitFlags&jobObjectLimitBreakawayOK == 0 {
		t.Errorf("limit flags %#x", info.Basic.LimitFlags)
	}
	// Closing Hive's last handle ends what is in the job, a grandchild included.
	tree := fake(t, Spec{Stdout: Pipe()}, "tree")
	childPID, _ := strconv.Atoi(strings.TrimPrefix(readLine(t, tree.Stdout()), "child "))
	child, err := identify(childPID)
	if err != nil {
		t.Fatal(err)
	}
	tree.Close()
	waitGone(t, child)
	waitGone(t, tree.ID)
	if res := tree.Wait(); res.Outcome != StoppedByHive {
		t.Errorf("a tree the close ended reads %+v", res)
	}
}

// A descendant that asks to leave the job leaves the tree, and is not ended
// with it.
func TestADescendantThatBreaksAwayIsNotEnded(t *testing.T) {
	fakeDir(t, "fakeprog")
	run := fake(t, Spec{Stdout: Pipe()}, "tree", "leave")
	childPID, _ := strconv.Atoi(strings.TrimPrefix(readLine(t, run.Stdout()), "child "))
	child, err := identify(childPID)
	if err != nil {
		t.Fatal(err)
	}
	defer kill(child)
	if res, err := run.Stop(Grace{}, nil); err != nil || res.Outcome != StoppedByHive {
		t.Fatalf("%+v %v", res, err)
	}
	time.Sleep(100 * time.Millisecond)
	if !alive(child) {
		t.Error("a descendant that broke away was ended with the tree")
	}
}

// A process another program ended with TerminateProcess reports the code that
// program chose and reads as exited; one that ended with an exception status
// was ended by the platform.
func TestResultsOnWindows(t *testing.T) {
	fakeDir(t, "fakeprog")
	run := fake(t, Spec{Stdout: Pipe()}, "hold")
	readLine(t, run.Stdout())
	h, err := syscall.OpenProcess(processTerminate, false, uint32(run.ID.PID))
	if err != nil {
		t.Fatal(err)
	}
	syscall.TerminateProcess(h, 5)
	syscall.CloseHandle(h)
	if res := run.Wait(); res.Outcome != Exited || res.Code != 5 {
		t.Errorf("%+v", res)
	}
	ended := fake(t, Spec{}, "selfkill")
	if res := ended.Wait(); res.Outcome != Ended || uint32(res.Code) != 0xC0000999 {
		t.Errorf("%+v", res)
	}
	// A tree Hive ended reads stopped_by_hive whatever status the end gave.
	held := fake(t, Spec{Stdout: Pipe()}, "hold")
	readLine(t, held.Stdout())
	res, err := held.Stop(Grace{}, nil)
	if err != nil || res.Outcome != StoppedByHive || uint32(res.Code) != terminatedByInterrupt {
		t.Errorf("%+v %v", res, err)
	}
}

// The child inherits exactly its standard handles: a handle Hive holds that
// is inheritable is not among them.
func TestTheChildInheritsOnlyItsStandardHandles(t *testing.T) {
	fakeDir(t, "fakeprog")
	file := filepath.Join(t.TempDir(), "leak.txt")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := syscall.Handle(f.Fd())
	if err := syscall.SetHandleInformation(h, syscall.HANDLE_FLAG_INHERIT, syscall.HANDLE_FLAG_INHERIT); err != nil {
		t.Fatal(err)
	}
	out, res := runOutput(t, withFake(t, Spec{}, "writehandle", strconv.FormatUint(uint64(h), 10)))
	if !res.Passed() || strings.Contains(out, "written=6") {
		t.Errorf("%q %v", out, res)
	}
	if data, _ := os.ReadFile(file); len(data) != 0 {
		t.Errorf("the child wrote %q to a handle of Hive's", data)
	}
}

// denyingJob makes a job that lets nothing in it break away.
func denyingJob(t testing.TB) syscall.Handle {
	t.Helper()
	h, _, err := procCreateJobObject.Call(0, 0)
	if h == 0 {
		t.Fatal(err)
	}
	var info jobExtendedLimit
	info.Basic.LimitFlags = jobObjectLimitKillOnJobClose
	if err := call(procSetInformationJobObject, h, jobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.CloseHandle(syscall.Handle(h)) })
	return syscall.Handle(h)
}

func assign(t testing.TB, job syscall.Handle, run *Run) {
	t.Helper()
	if err := call(procAssignProcessToJobObject, uintptr(job), uintptr(run.tree.(*winTree).process)); err != nil {
		t.Fatal(err)
	}
}

// residentFrom starts a starter inside the jobs given, outermost first, and
// returns what it wrote: the resident's id, or the error of its start.
func residentFrom(t testing.TB, jobs ...syscall.Handle) string {
	t.Helper()
	work := t.TempDir()
	idFile, goFile := filepath.Join(work, "resident.pid"), filepath.Join(work, "go")
	starter := fake(t, Spec{}, "starter", idFile, goFile)
	for _, job := range jobs {
		assign(t, job, starter)
	}
	if err := os.WriteFile(goFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	starter.Wait()
	data, _ := os.ReadFile(idFile)
	if pid, err := strconv.Atoi(string(data)); err == nil {
		if id, err := identify(pid); err == nil {
			t.Cleanup(func() { kill(id) })
		}
	}
	return string(data)
}

// A resident start is refused, running nothing and reported as nothing
// detached, under a job that denies breakaway and under an allowing job inside
// a denying one; where the jobs around allow it, it is started outside every
// job.
func TestResidentRefusedWhereTheJobDeniesBreakaway(t *testing.T) {
	fakeDir(t, "fakeprog")
	got := residentFrom(t)
	if _, err := strconv.Atoi(got); err != nil {
		t.Fatalf("a resident under Hive's own job was refused: %q", got)
	}
	if got := residentFrom(t, denyingJob(t)); !strings.HasPrefix(got, "error:") || !strings.Contains(got, "breakaway") {
		t.Errorf("under a job that denies breakaway: %q", got)
	}
	outer := denyingJob(t)
	inner, err := newJob() // allows breakaway
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(inner)
	if got := residentFrom(t, outer, inner); !strings.HasPrefix(got, "error:") || !strings.Contains(got, "breakaway") {
		t.Errorf("under an allowing job inside a denying one: %q", got)
	}
}
