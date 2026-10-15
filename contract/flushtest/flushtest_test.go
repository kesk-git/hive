package flushtest

import (
	"testing"

	"github.com/kesk-git/hive/contract/internal/flushseam"
)

// Skip called from a test binary turns the real flush off, and the test puts
// the real flush back so that it leaves no state behind.
func TestSkipTurnsTheFlushOff(t *testing.T) {
	flushseam.Real()
	t.Cleanup(flushseam.Real)
	if flushseam.Skipped() {
		t.Fatal("the flush is skipped before Skip is called")
	}
	Skip()
	if !flushseam.Skipped() {
		t.Fatal("Skip did not turn the flush off")
	}
	flushseam.Real()
	if flushseam.Skipped() {
		t.Fatal("Real did not restore the real flush")
	}
}
