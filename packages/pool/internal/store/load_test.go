package store

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/ids"
	cstore "github.com/kesk-git/hive/contract/store"
)

// newStore opens the Pool's store in a home with nothing in it.
func newStore(t testing.TB) (*cstore.Store, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	homeID, err := ids.Mint(clock.System{}, ids.Home)
	if err != nil {
		t.Fatal(err)
	}
	home, err := canon.Encode(map[string]any{
		"schema": "hive.home", "schema_version": "1.0", "record_revision": int64(1),
		"home_id": homeID, "role": "hive", "name": "A test home", "parent": map[string]any{"kind": "none"},
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z",
		"writer": map[string]any{"package": "dispatch", "command": "hive-fleet", "operation": "provision", "implementation_version": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "home.json"), append(home, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := cstore.Open(dir, Package, cstore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return st, dir
}

// put writes a record file as a writer would have left it.
func put(t testing.TB, dir, kind, id string, content []byte) {
	t.Helper()
	path := filepath.Join(dir, "pool", "records", kind, id+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A record that is absent is not found; one that cannot be read, breaks its
// schema, is of a version the build does not read or is filed under another
// identity is named and omitted, and a list reads the others around it.
func TestARecordThatCannotBeReadIsNamedAndNeverGuessed(t *testing.T) {
	st, dir := newStore(t)
	k, _ := Lookup("hold")
	good := fixture(t, k, "active-parked.json")
	file := func(change func(m map[string]any)) []byte {
		m := parse(t, mustEncode(t, good))
		m["record_revision"] = int64(1)
		change(m)
		return append(mustEncode(t, m), '\n')
	}
	id := good["hold_id"].(string)
	put(t, dir, "hold", id, file(func(map[string]any) {}))
	put(t, dir, "hold", "hold_broken", []byte("{not json}\n"))
	put(t, dir, "hold", "hold_norevision", mustEncode(t, map[string]any{"schema": "pool.hold"}))
	put(t, dir, "hold", "hold_schema", file(func(m map[string]any) { delete(m, "reason"); m["hold_id"] = "hold_schema" }))
	put(t, dir, "hold", "hold_newer", file(func(m map[string]any) { m["schema_version"] = "1.5"; m["hold_id"] = "hold_newer" }))
	put(t, dir, "hold", "hold_other", file(func(m map[string]any) {}))
	if err := os.WriteFile(filepath.Join(dir, "pool", "records", "hold", "notes.txt"), []byte("not a record"), 0o600); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "hold", "Bad Name", []byte("{}"))

	if _, _, err := Load(st, "hold", "hold_absent"); !errors.Is(err, ErrNotFound) {
		t.Errorf("an absent record: %v", err)
	}
	if _, _, err := Load(st, "hold", "../x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("an identity that is no file name: %v", err)
	}
	l, om, err := Load(st, "hold", id)
	if err != nil || om != nil || l.Revision != 1 || l.Data["hold_id"] != id {
		t.Errorf("a good record: %+v, %v, %v", l, om, err)
	}
	want := map[string]string{
		"hold_broken": ReasonMalformed, "hold_norevision": ReasonMalformed, "hold_schema": ReasonMalformed,
		"hold_newer": ReasonUnsupported, "hold_other": ReasonMalformed,
	}
	for id, reason := range want {
		if l, om, err := Load(st, "hold", id); err != nil || l != nil || om == nil || *om != (Omission{id, reason}) {
			t.Errorf("%s: %+v, %+v, %v", id, l, om, err)
		}
	}

	all, omitted, err := LoadAll(st, "hold")
	if err != nil || len(all) != 1 || all[0].ID != id {
		t.Fatalf("the list: %+v, %v", all, err)
	}
	var got []string
	for _, o := range omitted {
		got = append(got, o.ID+" "+o.Reason)
		if want[o.ID] != o.Reason && o.ID != "Bad Name" {
			t.Errorf("%s is omitted as %s", o.ID, o.Reason)
		}
	}
	if !slices.IsSorted(got) || len(omitted) != len(want)+1 {
		t.Errorf("omitted %v", got)
	}
	if all, omitted, err := LoadAll(st, "item"); err != nil || len(all)+len(omitted) != 0 {
		t.Errorf("a kind with no directory: %v %v %v", all, omitted, err)
	}
	if _, _, err := Load(st, "nothing", "x"); err == nil {
		t.Error("a kind the Pool does not store")
	}
}

func mustEncode(t testing.TB, v any) []byte {
	t.Helper()
	out, err := canon.Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
