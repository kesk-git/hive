package journal

import (
	"os"
	"testing"

	"github.com/kesk-git/hive/contract/internal/flushseam"
)

// The tests skip the real flush to the medium, which costs milliseconds a call
// on macOS; every step the recovery tests observe is kept.
func TestMain(m *testing.M) {
	flushseam.Skip()
	os.Exit(m.Run())
}

// An append made with the real flush is whole: the segment, its directory and
// the home's are flushed by the file system the tests run on.
func TestAppendWithTheRealFlush(t *testing.T) {
	flushseam.Real()
	t.Cleanup(flushseam.Skip)
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 0)
	append_(t, j, event(t, "pool", 1))
	if head, err := j.Head(); err != nil || head != 1 {
		t.Fatalf("Head = %d, %v", head, err)
	}
}
