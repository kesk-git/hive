//go:build unix

package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// alive says whether the process of the identity is still the one running:
// the id is in use with the same start time.
func alive(id ProcessID) bool {
	if syscall.Kill(id.PID, 0) != nil {
		return false
	}
	now, err := identify(id.PID)
	return err == nil && now == id
}

// kill ends the process of the identity, if it is still running.
func kill(id ProcessID) {
	if alive(id) {
		syscall.Kill(id.PID, syscall.SIGKILL)
	}
}

// waitGone waits for the process to be gone.
func waitGone(t testing.TB, id ProcessID) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if !alive(id) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d is still running", id.PID)
}

// Nothing else Hive holds open reaches the child: a descriptor the process
// was started with, which is not close-on-exec, is closed when the program
// starts.
func TestNoOtherDescriptorReachesTheChild(t *testing.T) {
	fakeDir(t, "fakeprog")
	fds := make([]int, 0, 3)
	for _, name := range []string{os.DevNull, os.DevNull, os.DevNull} {
		fd, err := syscall.Open(name, syscall.O_RDONLY, 0) // no O_CLOEXEC, as an inherited one is
		if err != nil {
			t.Fatal(err)
		}
		fds = append(fds, fd)
	}
	defer func() {
		for _, fd := range fds {
			syscall.Close(fd)
		}
	}()
	out, res := runOutput(t, withFake(t, Spec{Stdin: Pipe()}, "fds"))
	if !res.Passed() {
		t.Fatalf("%v", res)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("the child holds descriptors %q", out)
	}
	// The same through a terminal start.
	term := fake(t, Spec{Start: TerminalStart}, "fds")
	got := readUntil(t, term.Terminal(), "\n")
	if strings.TrimSpace(got) != "" {
		t.Errorf("the child of a terminal start holds descriptors %q", got)
	}
	// And through a resident start, which writes where it is told.
	file := filepath.Join(t.TempDir(), "fds.txt")
	resident := fake(t, Spec{Start: ResidentStart, Stdout: File(file)}, "fds")
	_ = resident
	for i := 0; i < 200; i++ {
		if data, _ := os.ReadFile(file); len(data) > 0 {
			if strings.TrimSpace(string(data)) != "" {
				t.Errorf("the resident holds descriptors %q", data)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the resident wrote nothing")
}

// A plain start is a process group of its own; a resident and a terminal start
// are sessions of their own.
func TestStartsGroupsAndSessions(t *testing.T) {
	fakeDir(t, "fakeprog")
	me, _ := getsid(0)
	out, res := runOutput(t, withFake(t, Spec{}, "ids"))
	if !res.Passed() {
		t.Fatal(res)
	}
	var pid, pgid, sid int
	fmt.Sscanf(strings.TrimSpace(out), "pid=%d pgid=%d sid=%d", &pid, &pgid, &sid)
	if pid == 0 || pgid != pid || sid != me {
		t.Errorf("a plain start: %s (Hive's session %d)", out, me)
	}
}

// A descendant that leaves the tree on purpose, by starting a session of its
// own, is not ended through its starter's tree.
func TestADescendantThatLeavesTheTreeIsNotEnded(t *testing.T) {
	fakeDir(t, "fakeprog")
	run := fake(t, Spec{Stdout: Pipe()}, "tree", "leave")
	childPID, _ := strconv.Atoi(strings.TrimPrefix(readLine(t, run.Stdout()), "child "))
	child, err := identify(childPID)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(child.PID, syscall.SIGKILL)
	if res, err := run.Stop(Grace{}, nil); err != nil || res.Outcome != StoppedByHive {
		t.Fatalf("%+v %v", res, err)
	}
	time.Sleep(100 * time.Millisecond)
	if !alive(child) {
		t.Error("a descendant that left the tree was ended with it")
	}
}

// The steps wait for their grace: a program that ends at the request is not
// ended, and reads as its own end; one that ignores the request is ended after
// the grace; and one that would exit 0 on the request is still stopped_by_hive
// once Hive ended it.
func TestStopStepsAndGrace(t *testing.T) {
	fakeDir(t, "fakeprog")
	// Ends at the request (SIGTERM): the signal that ended it, not Hive.
	run := fake(t, Spec{Stdout: Pipe()}, "hold")
	readLine(t, run.Stdout())
	ended := false
	res, err := run.Stop(Grace{Request: 5 * time.Second}, func() error { ended = true; return nil })
	if err != nil || res.Outcome != Ended || res.Signal != "terminated" || ended {
		t.Errorf("%+v %v ended=%v", res, err, ended)
	}
	// Ignores the request: ended after its grace.
	start := time.Now()
	run = fake(t, Spec{Stdout: Pipe()}, "holdterm")
	readLine(t, run.Stdout())
	start = time.Now()
	res, err = run.Stop(Grace{Request: 400 * time.Millisecond}, nil)
	if err != nil || res.Outcome != StoppedByHive || time.Since(start) < 400*time.Millisecond {
		t.Errorf("%+v %v after %v", res, err, time.Since(start))
	}
	if res.Signal != "killed" {
		t.Errorf("the end gave %q", res.Signal)
	}
	// Would exit 0 on the request, slowly: with no grace Hive ends it first.
	run = fake(t, Spec{Stdout: Pipe()}, "slowterm")
	readLine(t, run.Stdout())
	res, err = run.Stop(Grace{}, nil)
	if err != nil || res.Outcome != StoppedByHive {
		t.Errorf("%+v %v", res, err)
	}
	// With a grace long enough it exits 0 by itself and Hive ends nothing.
	run = fake(t, Spec{Stdout: Pipe()}, "slowterm")
	readLine(t, run.Stdout())
	res, err = run.Stop(Grace{Request: 5 * time.Second}, func() error { t.Error("ended a program that was exiting"); return nil })
	if err != nil || res.Outcome != Exited || res.Code != 0 {
		t.Errorf("%+v %v", res, err)
	}
	// A record that fails stops Stop before anything is ended.
	run = fake(t, Spec{Stdout: Pipe()}, "holdterm")
	readLine(t, run.Stdout())
	failed := fmt.Errorf("the record cannot be made")
	if _, err := run.Stop(Grace{}, func() error { return failed }); err != failed {
		t.Errorf("%v", err)
	}
	if !alive(run.ID) {
		t.Error("the tree was ended without being recorded")
	}
}

// A resident start is in a session of its own.
func TestResidentIsInASessionOfItsOwn(t *testing.T) {
	fakeDir(t, "fakeprog")
	idFile := filepath.Join(t.TempDir(), "resident.pid")
	starter := fake(t, Spec{}, "starter", idFile)
	if res := starter.Wait(); !res.Passed() {
		t.Fatal(res)
	}
	data, _ := os.ReadFile(idFile)
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatalf("%q", data)
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	sid, _ := getsid(pid)
	mine, _ := getsid(0)
	if sid != pid || sid == mine {
		t.Errorf("the resident's session is %d; its id is %d and Hive's session is %d", sid, pid, mine)
	}
}

// A group's number is the tree's only while a process of the group lives. A
// tree whose number has been taken by another process is not signaled and
// reads as gone, and so is one whose group was seen gone.
func TestAGroupNumberTakenByAnotherProcessIsNotSignaled(t *testing.T) {
	fakeDir(t, "fakeprog")
	run := fake(t, Spec{Stdout: Pipe()}, "hold")
	readLine(t, run.Stdout())
	defer kill(run.ID)
	real := run.tree.(*unixTree)
	stale := &unixTree{pgid: real.pgid, id: ProcessID{PID: real.id.PID, Start: real.id.Start + 1}}
	if !stale.empty() {
		t.Error("a group whose number another process holds is not gone")
	}
	if err := stale.end(); err != nil || !alive(run.ID) {
		t.Errorf("the other process was signaled: %v", err)
	}
	if err := stale.request(); err != nil || !alive(run.ID) {
		t.Errorf("the other process was asked to end: %v", err)
	}
	if real.empty() {
		t.Error("the group of a running process is gone")
	}

	// Once the group has been seen gone it is never signaled again, whoever
	// takes its number.
	res, err := run.Stop(Grace{}, nil)
	if err != nil || res.Outcome != StoppedByHive {
		t.Fatalf("%+v %v", res, err)
	}
	if !real.empty() || !real.over.Load() {
		t.Error("a stopped group is not gone")
	}
	other := fake(t, Spec{Stdout: Pipe()}, "hold")
	readLine(t, other.Stdout())
	defer kill(other.ID)
	reused := &unixTree{pgid: other.tree.(*unixTree).pgid, id: other.ID}
	reused.over.Store(true)
	if err := reused.end(); err != nil || !alive(other.ID) {
		t.Errorf("a group seen gone was signaled: %v", err)
	}
}

// A group whose leader has ended and which a descendant keeps is watched, so
// that it is seen gone when the descendant ends and is never signaled after,
// without Stop having looked; until then Stop still ends the descendant.
func TestAGroupKeptByADescendantIsWatchedUntilGone(t *testing.T) {
	fakeDir(t, "fakeprog")
	spawn := func() (*Run, ProcessID) {
		run := fake(t, Spec{Stdout: Pipe()}, "tree")
		pid, _ := strconv.Atoi(strings.TrimPrefix(readLine(t, run.Stdout()), "child "))
		child, err := identify(pid)
		if err != nil {
			t.Fatal(err)
		}
		syscall.Kill(run.ID.PID, syscall.SIGKILL)
		if res := run.Wait(); res.Outcome != Ended {
			t.Fatalf("%+v", res)
		}
		return run, child
	}

	run, child := spawn()
	defer kill(child)
	tree := run.tree.(*unixTree)
	if tree.over.Load() {
		t.Fatal("a group a descendant keeps is gone")
	}
	syscall.Kill(child.PID, syscall.SIGKILL)
	deadline := time.Now().Add(5 * time.Second)
	for !tree.over.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the group was not seen gone when its last process ended")
		}
		time.Sleep(10 * time.Millisecond)
	}

	run, child = spawn()
	defer kill(child)
	if _, err := run.Stop(Grace{}, nil); err != nil {
		t.Fatal(err)
	}
	waitGone(t, child)
}
