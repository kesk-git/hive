//go:build unix

package exec

import (
	"bytes"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"slices"
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

// withSentinels opens n uniquely identified files at non-standard descriptors
// without close-on-exec, as a descriptor Hive's starter left open is, and
// hands their identities to f; the files are closed when f returns. Each
// start gets sentinels of its own, because a start marks every descriptor of
// the process close-on-exec.
func withSentinels(t testing.TB, n int, f func(ids []string)) {
	t.Helper()
	dir := t.TempDir()
	var ids []string
	for i := 0; i < n; i++ {
		path := filepath.Join(dir, fmt.Sprintf("sentinel-%d", i))
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		fd, err := syscall.Open(path, syscall.O_RDONLY, 0) // no O_CLOEXEC
		if err != nil {
			t.Fatal(err)
		}
		defer syscall.Close(fd)
		var st syscall.Stat_t
		if err := syscall.Fstat(fd, &st); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, identity(&st))
	}
	f(ids)
}

// held reads what the fds fake printed: the identities it holds. A line
// without the fake's prefix is no report.
func held(t testing.TB, line string) []string {
	t.Helper()
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "fds:")
	if !ok {
		t.Fatalf("no report from the child: %q", line)
	}
	return strings.Fields(rest)
}

// Nothing else Hive holds open reaches the child: a descriptor the process
// was started with, which is not close-on-exec, is closed when the program
// starts. What crosses a start is measured by files the parent holds open
// under identities of their own, and which of them the child holds; a
// descriptor the child's own runtime opens after the start is not one.
func TestNoOtherDescriptorReachesTheChild(t *testing.T) {
	dir := fakeDir(t, "fakeprog")
	// The control: with Hive's start bypassed, the detector sees every
	// sentinel the child inherits.
	withSentinels(t, 3, func(ids []string) {
		cmd := osexec.Command(filepath.Join(dir, "fakeprog"), append([]string{fakeFlag, "fds"}, ids...)...)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		if got := held(t, string(out)); !slices.Equal(got, ids) {
			t.Fatalf("the control child holds %q, it was started with %q", got, ids)
		}
	})
	withSentinels(t, 3, func(ids []string) {
		out, res := runOutput(t, withFake(t, Spec{Stdin: Pipe()}, "fds", ids...))
		if !res.Passed() {
			t.Fatalf("%v", res)
		}
		if got := held(t, out); len(got) != 0 {
			t.Errorf("the child holds %q", got)
		}
	})
	// The same through a terminal start.
	withSentinels(t, 3, func(ids []string) {
		term := fake(t, Spec{Start: TerminalStart}, "fds", ids...)
		if got := held(t, readUntil(t, term.Terminal(), "\n")); len(got) != 0 {
			t.Errorf("the child of a terminal start holds %q", got)
		}
	})
	// And through a resident start, which writes where it is told.
	withSentinels(t, 3, func(ids []string) {
		file := filepath.Join(t.TempDir(), "fds.txt")
		fake(t, Spec{Start: ResidentStart, Stdout: File(file)}, "fds", ids...)
		for i := 0; i < 200; i++ {
			if data, _ := os.ReadFile(file); strings.Contains(string(data), "\n") {
				if got := held(t, string(data)); len(got) != 0 {
					t.Errorf("the resident holds %q", got)
				}
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Error("the resident wrote nothing")
	})
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
	// Would exit 0 on the request, slowly: with no grace Hive ends it first,
	// unless it has already exited by the time Hive looks, and once Hive
	// ended it the exit it was making does not count.
	run = fake(t, Spec{Stdout: Pipe()}, "slowterm")
	readLine(t, run.Stdout())
	ended = false
	res, err = run.Stop(Grace{}, func() error { ended = true; return nil })
	if err != nil || ended && res.Outcome != StoppedByHive || !ended && (res.Outcome != Exited || res.Code != 0) {
		t.Errorf("%+v %v ended=%v", res, err, ended)
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

// A helper that outlives its leader keeps running while Hive holds the run,
// and the leader is held unreaped, so that the group's number stays the
// run's: the leader's id still names it, with the start time it was started
// with. Letting the run go, by Close or by Stop, ends the helper with one
// last SIGKILL to the group and reaps the leader.
func TestAHelperRunsUntilTheRunIsLetGo(t *testing.T) {
	fakeDir(t, "fakeprog")
	for name, letGo := range map[string]func(*testing.T, *Run){
		"close": func(t *testing.T, run *Run) {
			if err := run.Close(); err != nil {
				t.Fatal(err)
			}
		},
		"stop": func(t *testing.T, run *Run) {
			res, err := run.Stop(Grace{}, func() error { t.Error("the end was recorded for a run whose process had ended"); return nil })
			if err != nil || res.Outcome != Ended || res.Signal != "killed" {
				t.Fatalf("%+v %v", res, err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			run := fake(t, Spec{Stdout: Pipe()}, "tree")
			pid, _ := strconv.Atoi(strings.TrimPrefix(readLine(t, run.Stdout()), "child "))
			helper, err := identify(pid)
			if err != nil {
				t.Fatal(err)
			}
			defer kill(helper)
			syscall.Kill(run.ID.PID, syscall.SIGKILL)
			if res := run.Wait(); res.Outcome != Ended || res.Signal != "killed" {
				t.Fatalf("%+v", res)
			}
			time.Sleep(100 * time.Millisecond)
			if !alive(helper) {
				t.Fatal("the helper was ended before the run was let go")
			}
			if now, err := identify(run.ID.PID); err != nil || now != run.ID {
				t.Fatalf("the id of the ended leader names %+v, %v; it was started as %+v", now, err, run.ID)
			}
			letGo(t, run)
			waitGone(t, helper)
			waitGone(t, run.ID)
		})
	}
}

// Once the run is let go and its leader reaped, its group is never signaled
// again, whatever its number comes to name: here another run's group, put in
// its place as the number would be if it were given again.
func TestAGroupLetGoIsNeverSignaledAgain(t *testing.T) {
	fakeDir(t, "fakeprog")
	run := fake(t, Spec{Stdout: Pipe()}, "holdterm")
	readLine(t, run.Stdout())
	if res, err := run.Stop(Grace{}, nil); err != nil || res.Outcome != StoppedByHive {
		t.Fatalf("%+v %v", res, err)
	}
	waitGone(t, run.ID)
	other := fake(t, Spec{Stdout: Pipe()}, "hold")
	readLine(t, other.Stdout())
	run.tree.(*unixTree).pgid = other.tree.(*unixTree).pgid
	if _, err := run.Stop(Grace{}, nil); err != nil {
		t.Error(err)
	}
	run.tree.request()
	run.tree.end()
	run.tree.release()
	run.Close()
	time.Sleep(100 * time.Millisecond)
	if !alive(other.ID) {
		t.Error("the group of a run let go was signaled")
	}
}

// A terminal program that reads nothing does not hold Stop behind the
// interrupt: the holder's input fills the terminal and its write blocks, so
// the interrupt cannot be written, and Stop still goes on to the request and
// the end once the interrupt's grace is over.
func TestAnInterruptThatCannotBeWrittenDoesNotHoldStop(t *testing.T) {
	fakeDir(t, "fakeprog")
	run := fake(t, Spec{Start: TerminalStart}, "holdterm")
	readUntil(t, run.Terminal(), "ready")
	go run.Terminal().Write(bytes.Repeat([]byte("input the program never reads\n"), 1<<17))
	time.Sleep(200 * time.Millisecond)
	stopped := make(chan error, 1)
	go func() {
		res, err := run.Stop(Grace{Interrupt: 100 * time.Millisecond, Request: 100 * time.Millisecond}, nil)
		if err == nil && res.Outcome == "" {
			err = fmt.Errorf("Stop returned no result")
		}
		stopped <- err
	}()
	select {
	case err := <-stopped:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Stop is held behind an interrupt that cannot be written")
	}
}
