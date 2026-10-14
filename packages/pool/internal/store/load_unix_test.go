//go:build unix

package store

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// within runs f and fails the test if it has not returned in time, as a read
// that opened a FIFO would not.
func within(t testing.TB, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the read is held by what stands at a record path")
	}
}

// A FIFO at a record path is never opened: a read by identity and a list both
// name it as malformed and read the others around it.
func TestAFIFOAtARecordPathIsNamedAndNeverOpened(t *testing.T) {
	st, dir := newStore(t)
	k, _ := Lookup("hold")
	good := fixture(t, k, "active-parked.json")
	good["record_revision"] = int64(1)
	id := good["hold_id"].(string)
	put(t, dir, "hold", id, append(mustEncode(t, good), '\n'))
	if err := syscall.Mkfifo(filepath.Join(dir, "pool", "records", "hold", "hold_fifo.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	within(t, func() {
		if l, om, err := Load(st, "hold", "hold_fifo"); err != nil || l != nil || om == nil || *om != (Omission{"hold_fifo", ReasonMalformed}) {
			t.Errorf("by identity: %+v, %+v, %v", l, om, err)
		}
		all, omitted, err := LoadAll(st, "hold")
		if err != nil || len(all) != 1 || all[0].ID != id || len(omitted) != 1 || omitted[0] != (Omission{"hold_fifo", ReasonMalformed}) {
			t.Errorf("the list: %+v, %+v, %v", all, omitted, err)
		}
	})
}
