//go:build unix

package testsupport

import (
	"os"
	"testing"
)

// GrantOthers opens the entry at path to everyone, as mode says.
func GrantOthers(t testing.TB, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
