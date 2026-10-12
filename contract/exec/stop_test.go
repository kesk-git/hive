package exec

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Stopping takes up to three steps, and a tree Hive ended reads
// stopped_by_hive whatever its exit code; the whole tree, a grandchild
// included, is ended by the third step.
func TestStopEndsTheWholeTree(t *testing.T) {
	fakeDir(t, "fakeprog")
	run := fake(t, Spec{Stdout: Pipe()}, "tree")
	line := readLine(t, run.Stdout())
	childPID, err := strconv.Atoi(strings.TrimPrefix(line, "child "))
	if err != nil {
		t.Fatalf("%q", line)
	}
	child, err := identify(childPID)
	if err != nil || !alive(child) {
		t.Fatalf("the grandchild is not running: %v", err)
	}
	recorded := 0
	res, err := run.Stop(Grace{}, func() error {
		// The record is made before the tree is ended.
		if !alive(child) || !alive(run.ID) {
			t.Error("the tree was ended before it was recorded")
		}
		recorded++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != StoppedByHive || recorded != 1 {
		t.Errorf("%+v, recorded %d", res, recorded)
	}
	waitGone(t, child)
	waitGone(t, run.ID)
	// Stopping again changes nothing.
	if again, err := run.Stop(Grace{}, func() error { t.Error("recorded again"); return nil }); err != nil || again.Outcome != StoppedByHive {
		t.Errorf("%+v %v", again, err)
	}
}

// A resident start outlives the command that made it.
func TestResidentOutlivesItsStarter(t *testing.T) {
	fakeDir(t, "fakeprog")
	work := t.TempDir()
	idFile := filepath.Join(work, "resident.pid")
	starter := fake(t, Spec{}, "starter", idFile)
	if res := starter.Wait(); !res.Passed() {
		data, _ := os.ReadFile(idFile)
		t.Fatalf("the starter: %v %s", res, data)
	}
	data, err := os.ReadFile(idFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatalf("%q", data)
	}
	id, err := identify(pid)
	if err != nil {
		t.Fatalf("the resident did not outlive its starter: %v", err)
	}
	defer kill(id)
	if !alive(id) || alive(starter.ID) {
		t.Error("the resident is not running or the starter is")
	}
	// A resident is not waited for and is stopped through its own command.
	if _, err := (&Run{kind: ResidentStart}).Stop(Grace{}, nil); err == nil {
		t.Error("Stop ended a resident")
	}
}
