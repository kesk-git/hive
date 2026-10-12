//go:build windows

package store

import (
	"bytes"
	"testing"
)

// A record replaced while a reader holds the old one open: the replacement
// goes through, the reader keeps the whole old record, and the next read is
// the whole new one (spec/records.md, Writing one record).
func TestReplaceWhileARecordIsOpen(t *testing.T) {
	home := newHome(t)
	s, _, _ := open(t, home, "pool")
	w := lock(t, s)
	defer w.Release()
	if _, err := w.Replace(req1, task1, []byte(`{"n":1}`), 0); err != nil {
		t.Fatal(err)
	}
	old, err := ReadFile(s.abs(task1))
	if err != nil {
		t.Fatal(err)
	}
	f, err := OpenRead(s.abs(task1))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := w.Replace(req2, task1, []byte(`{"n":2}`), 1); err != nil {
		t.Fatalf("a replacement while a reader holds the record: %v", err)
	}
	held := make([]byte, len(old))
	if n, err := f.ReadAt(held, 0); n != len(old) || !bytes.Equal(held, old) {
		t.Errorf("the reader's handle reads %q, %v; want the old record %q", held[:n], err, old)
	}
	rec, err := s.ReadRecord(task1)
	if err != nil || rec.Revision != 2 {
		t.Errorf("the record after the replacement: %+v, %v", rec, err)
	}
	if n := len(temps(t, home)); n != 0 {
		t.Errorf("%d temporary files left", n)
	}
}
