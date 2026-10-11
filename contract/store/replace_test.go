package store

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
)

var (
	task1 = RecordPath("item", id("task", 1))
	req1  = id("req", 1)
	req2  = id("req", 2)
)

func TestReplaceCreatesAndReplaces(t *testing.T) {
	home := newHome(t)
	s, clk, _ := open(t, home, "pool")
	w := lock(t, s)
	defer w.Release()

	// Spacing, member order and a revision the caller got wrong are not kept;
	// text is kept exactly.
	text := "Grüße   \"quoted\" \\ tab\t end"
	content := []byte("{ \"title\": \"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\t", "\\t").Replace(text) + "\",\n \"record_revision\": 99, \"n\": 1 }")
	got, err := w.Replace(req1, task1, content, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := record(t, map[string]any{"title": text, "n": int64(1)}, 1, "2026-10-11T08:00:00.000Z")
	if got.Revision != 1 || got.Path != task1 || got.Digest != canon.FileDigest(want[:len(want)-1]) {
		t.Errorf("Written = %+v", got)
	}
	data, err := os.ReadFile(s.abs(task1))
	if err != nil || !bytes.Equal(data, want) {
		t.Fatalf("on disk %q, %v; want %q", data, err, want)
	}
	if parsed(t, data)["title"] != text {
		t.Error("text was not kept as written")
	}
	rec, err := s.ReadRecord(task1)
	if err != nil || rec.Revision != 1 || rec.Digest != got.Digest || !bytes.Equal(rec.Data, want) {
		t.Fatalf("ReadRecord = %+v, %v", rec, err)
	}
	info, _ := os.Stat(s.abs(task1))
	if info.Mode().Perm() != fileMode {
		t.Errorf("record mode %v", info.Mode())
	}
	info, _ = os.Stat(filepath.Dir(s.abs(task1)))
	if info.Mode().Perm() != dirMode {
		t.Errorf("records directory mode %v", info.Mode())
	}

	clk.Advance(1500 * time.Millisecond)
	got, err = w.Replace(req2, task1, []byte(`{"title":"second","n":2}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	want = record(t, map[string]any{"title": "second", "n": int64(2)}, 2, "2026-10-11T08:00:01.500Z")
	data, _ = os.ReadFile(s.abs(task1))
	if got.Revision != 2 || !bytes.Equal(data, want) {
		t.Fatalf("second Replace = %+v, on disk %q", got, data)
	}
	if temps := temps(t, home); len(temps) != 0 {
		t.Errorf("temporary files left: %v", temps)
	}
	if want := []string{"pool/records/item/" + id("task", 1) + ".json"}; !slices.Equal(files(t, filepath.Join(home)), append([]string{"locks/pool.lock"}, want...)) {
		t.Errorf("files = %v", files(t, home))
	}
}

func TestReplaceRefusesAndWritesNothing(t *testing.T) {
	home := newHome(t)
	s, _, _ := open(t, home, "pool")
	w := lock(t, s)
	defer w.Release()
	if _, err := w.Replace(req1, task1, []byte(`{"n":1}`), 0); err != nil {
		t.Fatal(err)
	}
	bad := RecordPath("item", id("task", 2))
	write(t, s, bad, []byte("{\"n\":1,\"record_revision\":1,\n"))
	before := tree(t, home)

	tests := []struct {
		name     string
		path     string
		content  string
		expected int64
		want     error
	}{
		{"create over a present record", task1, `{"n":2}`, 0, ErrAlreadyExists},
		{"replace at another revision", task1, `{"n":2}`, 2, ErrRevisionConflict},
		{"replace an absent record", RecordPath("item", id("task", 3)), `{"n":2}`, 1, ErrRevisionConflict},
		{"replace a malformed record", bad, `{"n":2}`, 1, ErrRecordMalformed},
		{"a negative expected revision", task1, `{"n":2}`, -1, nil},
		{"content that is not an object", task1, `[1]`, 1, nil},
		{"content with a fraction", task1, `{"n":1.5}`, 1, nil},
		{"content that is not JSON", task1, `{"n":}`, 1, nil},
		{"content with a duplicate member", task1, `{"n":1,"n":2}`, 1, nil},
		{"a path outside records", "../home.json", `{}`, 0, nil},
		{"a path with upper case", "records/item/Task.json", `{}`, 0, nil},
		{"a path without .json", "records/item/x", `{}`, 0, nil},
		{"a temporary file", "records/item/x.json.req.tmp", `{}`, 0, nil},
		{"a receipt as a record", ReceiptPath(id("rcpt", 1)), `{}`, 0, nil},
		{"an outcome as a record", OutcomePath(req1), `{}`, 0, nil},
		{"an intent as a record", IntentPath(req1), `{}`, 0, nil},
		{"an empty path", "", `{}`, 0, nil},
		{"a request that is not one", task1, `{}`, 1, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := req2
			if tc.name == "a request that is not one" {
				req = "req_nope"
			}
			got, err := w.Replace(req, tc.path, []byte(tc.content), tc.expected)
			if err == nil {
				t.Fatalf("Replace = %+v, want an error", got)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error = %v; want %v", err, tc.want)
			}
			sameTree(t, "after a refused write", tree(t, home), before)
		})
	}
}

// Every replacement is interrupted at each of its steps. After the
// interruption the record is the whole old record or the whole new one, and
// the next writer removes what the interruption left.
func TestReplaceInterruptedAtEachStep(t *testing.T) {
	setup := func(t *testing.T) (*Store, []byte) {
		home := newHome(t)
		s, _, _ := open(t, home, "pool")
		w := lock(t, s)
		if _, err := w.Replace(req1, task1, []byte(`{"n":1}`), 0); err != nil {
			t.Fatal(err)
		}
		w.Release()
		old, _ := os.ReadFile(s.abs(task1))
		return s, old
	}
	s, _ := setup(t)
	steps := trace(t, s, func() {
		w := lock(t, s)
		defer w.Release()
		if _, err := w.Replace(req2, task1, []byte(`{"n":2}`), 1); err != nil {
			t.Fatal(err)
		}
	})
	want := []string{
		"create " + task1, "fill " + task1, "sync " + task1, "rename " + task1,
		"syncdir " + task1, "syncdir pool/records/", "syncdir pool/", "syncdir ./",
	}
	if !slices.Equal(steps, want) {
		t.Fatalf("steps = %v, want %v", steps, want)
	}
	newRecord := record(t, map[string]any{"n": int64(2)}, 2, "2026-10-11T08:00:00.000Z")

	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
			s, old := setup(t)
			w := lock(t, s)
			hit := faultAt(s, step, 1)
			_, err := w.Replace(req2, task1, []byte(`{"n":2}`), 1)
			w.Release()
			if !*hit || !errors.Is(err, errCrash) {
				t.Fatalf("the interruption did not happen: hit %v, err %v", *hit, err)
			}
			s.fault = nil
			wholeRecord(t, s, task1, old, newRecord)
			rec, err := s.ReadRecord(task1)
			if err != nil {
				t.Fatalf("a reader after the interruption: %v", err)
			}
			tmp := s.abs(task1) + "." + req2 + tempSuffix
			_, tmpErr := os.Stat(tmp)
			// Between its creation and its rename the temporary file is there
			// and the record is still the old one.
			between := !strings.HasPrefix(step, "create") && !strings.HasPrefix(step, "syncdir")
			if left := tmpErr == nil; left != between || (left && rec.Revision != 1) {
				t.Errorf("temporary file left %v at step %s, record at revision %d", left, step, rec.Revision)
			}

			// The next writer to take the lock removes the temporary file and
			// reads the record as it is; nothing rolls a single replacement
			// forward.
			after, err := os.ReadFile(s.abs(task1))
			if err != nil {
				t.Fatal(err)
			}
			w = lock(t, s)
			w.Release()
			if temps := temps(t, s.Dir); len(temps) != 0 {
				t.Errorf("temporary files after the next lock: %v", temps)
			}
			if now, _ := os.ReadFile(s.abs(task1)); !bytes.Equal(now, after) {
				t.Error("the next lock changed the record")
			}
			if _, err := os.Stat(filepath.Join(s.Dir, locksDir, "pool.lock")); err != nil {
				t.Error("the lock file was swept")
			}
		})
	}
}

// A temporary file, whole or partial, is never read as a record, and a
// writer sweeps it. A write that fails under pressure leaves the record as
// it was and no temporary file.
func TestTemporaryFilesAreNeverRecords(t *testing.T) {
	home := newHome(t)
	s, _, _ := open(t, home, "pool")
	w := lock(t, s)
	if _, err := w.Replace(req1, task1, []byte(`{"n":1}`), 0); err != nil {
		t.Fatal(err)
	}
	w.Release()
	whole := record(t, map[string]any{"n": int64(2)}, 2, "2026-10-11T08:00:00.000Z")
	write(t, s, task1+"."+req2+tempSuffix, whole)
	write(t, s, task1+"."+id("req", 3)+tempSuffix, whole[:len(whole)/2])
	write(t, s, "records/item/"+id("task", 9)+".json."+id("req", 4)+tempSuffix, nil)
	write(t, s, "tx/"+id("req", 5)+".json."+id("req", 5)+tempSuffix, []byte(`{"schema":"hive.tx-in`))
	rec, err := s.ReadRecord(task1)
	if err != nil || rec.Revision != 1 {
		t.Fatalf("ReadRecord with temporary files beside = %+v, %v", rec, err)
	}
	for _, rel := range []string{task1 + "." + req2 + tempSuffix, "records/item/" + id("task", 9) + ".json." + id("req", 4) + tempSuffix} {
		if _, err := s.ReadRecord(rel); err == nil {
			t.Errorf("a temporary file %s was read as a record", rel)
		}
	}
	if n := len(temps(t, home)); n != 4 {
		t.Fatalf("%d temporary files, want 4", n)
	}
	w = lock(t, s)
	defer w.Release()
	if temps := temps(t, home); len(temps) != 0 {
		t.Errorf("temporary files after the lock: %v", temps)
	}
	if rec, err := s.ReadRecord(task1); err != nil || rec.Revision != 1 {
		t.Errorf("the record after the sweep: %+v, %v", rec, err)
	}

	// Pressure: the temporary file cannot be created. The write is refused
	// whole, the old record stands and nothing is left behind. Root is not
	// refused by a directory's mode, so the case needs an ordinary user.
	if os.Geteuid() == 0 {
		t.Log("running as root: a closed directory does not refuse a write; the pressure case is not run")
		return
	}
	before := tree(t, home)
	dir := filepath.Dir(s.abs(task1))
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	_, err = w.Replace(req2, task1, []byte(`{"n":2}`), 1)
	os.Chmod(dir, dirMode)
	if err == nil {
		t.Fatal("a write into a closed directory succeeded")
	}
	if errors.Is(err, ErrRevisionConflict) || errors.Is(err, ErrRecordMalformed) {
		t.Errorf("a refused write reported the wrong thing: %v", err)
	}
	sameTree(t, "after a refused write", tree(t, home), before)
}

// A receipt and a recorded outcome are written only by a transaction, and
// read back whole.
func TestReadReceiptsAndOutcomes(t *testing.T) {
	home := newHome(t)
	s, _, _ := open(t, home, "pool")
	w := lock(t, s)
	defer w.Release()
	rel := ReceiptPath(rcpt1)
	if _, err := w.Commit(Tx{
		RequestID: req1,
		Receipts:  []Receipt{{rcpt1, []byte("{ \"b\": 2,\n\"a\": 1 }")}},
		Outcome:   []byte(`{"status":"ok"}`),
	}); err != nil {
		t.Fatal(err)
	}
	data, err := s.Read(rel)
	if err != nil || string(data) != "{\"a\":1,\"b\":2}\n" {
		t.Fatalf("Read = %q, %v", data, err)
	}
	if _, err := s.Read(OutcomePath(req2)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Read of a missing file: %v", err)
	}
	if _, err := s.ReadRecord(rel); err == nil {
		t.Error("a receipt was read as a record")
	}
	if _, err := s.Read("requests/x.tmp"); err == nil {
		t.Error("a temporary name was read")
	}
	write(t, s, OutcomePath(req2), []byte("{\"a\":1}"))
	if _, err := s.Read(OutcomePath(req2)); !errors.Is(err, ErrRecordMalformed) {
		t.Errorf("Read of a file without its line feed: %v", err)
	}
	// Nothing writes a receipt outside a transaction: the receipt of a second
	// request that names the same receipt is refused whole.
	before := tree(t, home)
	if _, err := w.Commit(Tx{RequestID: req2, Receipts: []Receipt{{rcpt1, []byte(`{"a":1,"b":2}`)}}, Outcome: []byte(`{"status":"ok"}`)}); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("a second write of the receipt: %v", err)
	}
	sameTree(t, "after a refused receipt", tree(t, home), before)
}

func TestReadRecordRefusesMalformedRecords(t *testing.T) {
	home := newHome(t)
	s, _, _ := open(t, home, "pool")
	if _, err := s.ReadRecord(task1); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing record: %v", err)
	}
	for name, data := range map[string]string{
		"empty":                      "",
		"only a line feed":           "\n",
		"no line feed":               `{"record_revision":1}`,
		"two line feeds":             "{\"record_revision\":1}\n\n",
		"byte-order mark":            "\xef\xbb\xbf{\"record_revision\":1}\n",
		"torn":                       "{\"record_revision\":1,\"a\":\n",
		"not an object":              "[1]\n",
		"no record_revision":         "{\"a\":1}\n",
		"record_revision zero":       "{\"record_revision\":0}\n",
		"record_revision a string":   "{\"record_revision\":\"1\"}\n",
		"record_revision a fraction": "{\"record_revision\":1.0}\n",
		"duplicate member":           "{\"record_revision\":1,\"record_revision\":2}\n",
		"invalid UTF-8":              "{\"record_revision\":1,\"a\":\"\xff\"}\n",
		"trailing content":           "{\"record_revision\":1} x\n",
	} {
		t.Run(name, func(t *testing.T) {
			write(t, s, task1, []byte(data))
			if _, err := s.ReadRecord(task1); !errors.Is(err, ErrRecordMalformed) {
				t.Errorf("error = %v; want ErrRecordMalformed", err)
			}
			if _, err := s.ReadRecord(task1); Code(err) != "record_malformed" {
				t.Error("Code is not record_malformed")
			}
		})
	}
	// A record not in canonical form is still a record; its digest is that
	// of its canonical form.
	write(t, s, task1, []byte("{ \"b\": 1,\n  \"record_revision\": 3 }\n"))
	rec, err := s.ReadRecord(task1)
	if err != nil || rec.Revision != 3 || rec.Digest != canon.FileDigest([]byte(`{"b":1,"record_revision":3}`)) {
		t.Errorf("ReadRecord = %+v, %v", rec, err)
	}
	if _, err := s.ReadRecord("records/item/" + strings.ToUpper(id("task", 1)) + ".json"); err == nil {
		t.Error("an upper-case path was read")
	}
}

// A store given a limit refuses a file over it whole, naming the limit and
// the size, and writes nothing: not the record, not an intent.
func TestSizeLimit(t *testing.T) {
	home := newHome(t)
	clk := clock.NewFake(epoch)
	at := stamp(t, clk)
	// The limit is exactly the size of the first record, so that record
	// passes and one byte more fails.
	limit := int64(len(record(t, map[string]any{"n": int64(1)}, 1, at)))
	s, err := Open(home, "pool", Options{Clock: clk, Journal: newJournal(), Limit: limit, Waiting: Waiting{Clock: clk, Limit: time.Millisecond, Pause: clk.Advance}})
	if err != nil {
		t.Fatal(err)
	}
	w := lock(t, s)
	defer w.Release()
	if _, err := w.Replace(req1, task1, []byte(`{"n":1}`), 0); err != nil {
		t.Fatal(err)
	}
	before := tree(t, home)
	big := []byte(`{"n":1,"x":"` + strings.Repeat("y", 10) + `"}`)
	_, err = w.Replace(req2, task1, big, 1)
	var se *SizeError
	if !errors.As(err, &se) || !errors.Is(err, ErrRecordTooLarge) || Code(err) != "record_too_large" {
		t.Fatalf("Replace over the limit: %v", err)
	}
	if want := int64(len(record(t, map[string]any{"n": int64(1), "x": strings.Repeat("y", 10)}, 2, at))); se.Path != task1 || se.Limit != limit || se.Actual != want {
		t.Errorf("SizeError = %+v, want actual %d", se, want)
	}
	sameTree(t, "after a refused record", tree(t, home), before)

	_, err = w.Commit(Tx{
		RequestID: req2,
		Receipts:  []Receipt{{id("rcpt", 1), []byte(`{"x":"` + strings.Repeat("y", 70) + `"}`)}},
		Outcome:   []byte(`{"ok":true}`),
	})
	if !errors.As(err, &se) || se.Path != ReceiptPath(id("rcpt", 1)) || se.Actual != 70+9 {
		t.Fatalf("a receipt over the limit: %v", err)
	}
	sameTree(t, "after a refused receipt", tree(t, home), before)

	_, err = w.Commit(Tx{
		RequestID: req2,
		Records:   []RecordWrite{{task1, []byte(`{"n":2}`), 1}, {RecordPath("item", id("task", 2)), big, 0}},
		Outcome:   []byte(`{"ok":true}`),
	})
	if !errors.As(err, &se) || se.Path != RecordPath("item", id("task", 2)) {
		t.Fatalf("Commit over the limit: %v", err)
	}
	sameTree(t, "after a refused transaction", tree(t, home), before)

	_, err = w.Commit(Tx{RequestID: req2, Records: []RecordWrite{{task1, []byte(`{"n":2}`), 1}}, Outcome: []byte(`{"x":"` + strings.Repeat("y", 70) + `"}`)})
	if !errors.As(err, &se) || se.Path != OutcomePath(req2) {
		t.Fatalf("Commit with an outcome over the limit: %v", err)
	}
	sameTree(t, "after a refused outcome", tree(t, home), before)

	// Without a limit the same record is written.
	s2, _, _ := open(t, newHome(t), "pool")
	w2 := lock(t, s2)
	defer w2.Release()
	if _, err := w2.Replace(req1, task1, big, 0); err != nil {
		t.Errorf("a store without a limit refused: %v", err)
	}
}
