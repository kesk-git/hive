package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kesk-git/hive/contract/internal/flushseam"
)

// The tests skip the real flush to the medium, which costs milliseconds a call
// on macOS; every step the recovery tests observe is kept.
func TestMain(m *testing.M) {
	flushseam.Skip = true
	os.Exit(m.Run())
}

// withRealFlush runs the rest of a test with the real flush, which the other
// tests skip.
func withRealFlush(t testing.TB) {
	t.Helper()
	was := flushseam.Skip
	flushseam.Skip = false
	t.Cleanup(func() { flushseam.Skip = was })
}

// A record written with the real flush is whole, and the file and directory
// flushes themselves succeed on the file system the tests run on.
func TestWriteWithTheRealFlush(t *testing.T) {
	withRealFlush(t)
	home := newHome(t)
	s, _, _ := open(t, home, "pool")
	if err := s.writeFile("pool/a.json", "req1", []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(home, "pool", "a.json"))
	if err != nil || string(got) != "{}\n" {
		t.Fatalf("record = %q, %v", got, err)
	}
	f, err := os.Create(filepath.Join(t.TempDir(), "f"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := Flush(f); err != nil {
		t.Errorf("Flush: %v", err)
	}
	if err := SyncDir("test", home); err != nil {
		t.Errorf("SyncDir: %v", err)
	}
}
