package cli

import (
	"os"
	"testing"

	"github.com/kesk-git/hive/contract/internal/flushseam"
)

// The tests skip the real flush to the medium, which costs milliseconds a call
// on macOS; the store and the journal test it themselves.
func TestMain(m *testing.M) {
	flushseam.Skip = true
	os.Exit(m.Run())
}
