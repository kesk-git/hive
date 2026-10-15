package budget_test

import (
	"path/filepath"
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/budget"
)

func (h *home) read(id string) (*budget.Record, error) {
	h.t.Helper()
	st, err := cstore.Open(h.dir, "pool", cstore.Options{})
	if err != nil {
		h.t.Fatal(err)
	}
	return budget.Read(st, id)
}

// The reader gives each projection's entry, in the order the record keeps them,
// the revision it read, and no entry for a projection the record holds none for.
func TestTheReaderGivesTheEntriesOfTheRecord(t *testing.T) {
	h := seed(t, "")
	r, err := h.read(homeID)
	if err != nil {
		t.Fatal(err)
	}
	want := []budget.Entry{{ProjectionID: hive, MaxBytes: 18432}, {ProjectionID: subject, MaxBytes: 9216}, {ProjectionID: fleet, MaxBytes: 4608}}
	if r.Revision != 1 || len(r.Projections) != len(want) {
		t.Fatalf("revision %d, entries %v", r.Revision, r.Projections)
	}
	for i, e := range want {
		if r.Projections[i] != e {
			t.Errorf("entry %d is %v, want %v", i, r.Projections[i], e)
		}
	}
	if max, ok := r.Max(subject); !ok || max != 9216 {
		t.Errorf("Max(%s) = %d, %v", subject, max, ok)
	}
	if _, ok := r.Max(unknown); ok {
		t.Errorf("Max names an entry for %s", unknown)
	}
}

// A home with no configuration, one that cannot be read and one that is another
// home's are each a failure, and none is read as an empty configuration.
func TestTheReaderRefusesWhatItCannotRead(t *testing.T) {
	absent := newHome(t)
	if _, err := absent.read(homeID); envelope.AsFailure(err) == nil || envelope.AsFailure(err).Err.Code != envelope.CodeNotFound {
		t.Errorf("an absent configuration: %v", err)
	}

	garbled := newHome(t)
	path := filepath.Join(garbled.dir, "pool", "records", "budget", "memory.json")
	garbled.put(path, []byte("not a record\n"))
	if _, err := garbled.read(homeID); envelope.AsFailure(err) == nil || envelope.AsFailure(err).Err.Code != envelope.CodeRecordMalformed {
		t.Errorf("a record that cannot be read: %v", err)
	}

	if _, err := seed(t, "").read("home_01jc7d8e9f0g1h2j3k4m5n6p7z"); envelope.AsFailure(err) == nil || envelope.AsFailure(err).Err.Code != envelope.CodeRecordMalformed {
		t.Errorf("another home's record: %v", err)
	}
}
