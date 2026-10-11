package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
)

var (
	task2 = RecordPath("item", id("task", 2))
	ws1   = "registry/workspaces/" + id("ws", 1) + ".json"
	rcpt1 = id("rcpt", 1)
	rcpt2 = id("rcpt", 2)
	evt1  = id("evt", 1)
	evt2  = id("evt", 2)
	req9  = id("req", 9)
)

// fullTx is a transaction that does everything one can: it replaces a
// record, creates two, writes two receipts, appends two events and records
// its outcome.
func fullTx() Tx {
	return Tx{
		RequestID: req9,
		Records: []RecordWrite{
			{task1, []byte(`{"n":2,"title":"replaced"}`), 1},
			{task2, []byte(`{"n":1,"title":"created"}`), 0},
			{ws1, []byte(`{"slug":"alpha"}`), 0},
		},
		Receipts: []Receipt{
			{rcpt1, []byte(`{"receipt_id":"` + rcpt1 + `","kind":"pool.item.created"}`)},
			{rcpt2, []byte(`{"receipt_id":"` + rcpt2 + `","kind":"pool.item.created"}`)},
		},
		Events: []Event{
			{evt1, []byte(`{"event_id":"` + evt1 + `","kind":"pool.item.created"}`)},
			{evt2, []byte(`{"event_id":"` + evt2 + `","kind":"pool.item.created"}`)},
		},
		Outcome: []byte(`{"status":"ok","request_id":"` + req9 + `"}`),
	}
}

// seeded is a store with task1 at revision 1, the state fullTx starts from.
func seeded(t *testing.T) (*Store, *fakeJournal) {
	t.Helper()
	s, _, j := open(t, newHome(t), "pool")
	w := lock(t, s)
	if _, err := w.Replace(req1, task1, []byte(`{"n":1,"title":"first"}`), 0); err != nil {
		t.Fatal(err)
	}
	w.Release()
	return s, j
}

// committed is the state of a store after fullTx ran without interruption,
// and the journal's content.
func committed(t *testing.T) (map[string]string, []string) {
	t.Helper()
	s, j := seeded(t)
	w := lock(t, s)
	defer w.Release()
	if _, err := w.Commit(fullTx()); err != nil {
		t.Fatal(err)
	}
	return tree(t, s.Dir), j.order
}

func TestCommitWritesEverythingInOrder(t *testing.T) {
	s, j := seeded(t)
	w := lock(t, s)
	defer w.Release()
	var got *Committed
	steps := trace(t, s, func() {
		var err error
		if got, err = w.Commit(fullTx()); err != nil {
			t.Fatal(err)
		}
	})

	// The order of spec/records.md: the intent, the records, the receipts, the
	// events, the outcome, then the intent is removed.
	var order []string
	for _, step := range steps {
		if strings.HasPrefix(step, "create ") || strings.HasPrefix(step, "events ") || strings.HasPrefix(step, "remove ") {
			order = append(order, step)
		}
	}
	want := []string{
		"create " + IntentPath(req9),
		"create " + task1, "create " + task2, "create " + ws1,
		"create " + ReceiptPath(rcpt1), "create " + ReceiptPath(rcpt2),
		"events " + IntentPath(req9),
		"create " + OutcomePath(req9),
		"remove " + IntentPath(req9),
	}
	if !slices.Equal(order, want) {
		t.Errorf("order = %v\nwant %v", order, want)
	}

	wantRecords := []Written{
		{task1, 2, canon.FileDigest([]byte(`{"n":2,"record_revision":2,"title":"replaced","updated_at":"2026-10-11T08:00:00.000Z"}`))},
		{task2, 1, canon.FileDigest([]byte(`{"n":1,"record_revision":1,"title":"created","updated_at":"2026-10-11T08:00:00.000Z"}`))},
		{ws1, 1, canon.FileDigest([]byte(`{"record_revision":1,"slug":"alpha","updated_at":"2026-10-11T08:00:00.000Z"}`))},
	}
	if !slices.Equal(got.Records, wantRecords) {
		t.Errorf("Committed = %+v\nwant %+v", got.Records, wantRecords)
	}
	for _, r := range wantRecords {
		rec, err := s.ReadRecord(r.Path)
		if err != nil || rec.Revision != r.Revision || rec.Digest != r.Digest {
			t.Errorf("%s on disk = %+v, %v", r.Path, rec, err)
		}
	}
	for _, id := range []string{rcpt1, rcpt2} {
		data, err := s.Read(ReceiptPath(id))
		if err != nil || parsed(t, data)["receipt_id"] != id {
			t.Errorf("receipt %s = %q, %v", id, data, err)
		}
	}
	if data, err := s.Read(OutcomePath(req9)); err != nil || parsed(t, data)["status"] != "ok" {
		t.Errorf("outcome = %q, %v", data, err)
	}
	if !slices.Equal(j.order, []string{evt1, evt2}) || j.appends != 1 {
		t.Errorf("journal = %v after %d appends", j.order, j.appends)
	}
	if !bytes.Equal(j.lines[evt1], []byte(`{"event_id":"`+evt1+`","kind":"pool.item.created"}`)) {
		t.Errorf("event content %q is not canonical", j.lines[evt1])
	}
	if _, err := os.Stat(s.abs(IntentPath(req9))); !errNotExist(err) {
		t.Error("the intent remains")
	}
	if temps := temps(t, s.Dir); len(temps) != 0 {
		t.Errorf("temporary files: %v", temps)
	}
	wantFiles := []string{
		"locks/pool.lock",
		"pool/receipts/" + rcpt1 + ".json", "pool/receipts/" + rcpt2 + ".json",
		"pool/records/item/" + id("task", 1) + ".json", "pool/records/item/" + id("task", 2) + ".json",
		"pool/registry/workspaces/" + id("ws", 1) + ".json",
		"pool/requests/" + req9 + ".json",
	}
	if got := files(t, s.Dir); !slices.Equal(got, wantFiles) {
		t.Errorf("files = %v", got)
	}
}

// A transaction with one record and nothing else is still a transaction.
func TestCommitSmallest(t *testing.T) {
	s, j := seeded(t)
	w := lock(t, s)
	defer w.Release()
	got, err := w.Commit(Tx{RequestID: req9, Outcome: []byte(`{"status":"ok"}`)})
	if err != nil || len(got.Records) != 0 {
		t.Fatalf("Commit = %+v, %v", got, err)
	}
	if j.appends != 0 {
		t.Error("the journal was touched without events")
	}
	if _, err := s.Read(OutcomePath(req9)); err != nil {
		t.Error(err)
	}
}

// Every check of step 2 fails before anything is written: no record, no
// receipt, no event, no outcome and no intent.
func TestCommitChecksBeforeWriting(t *testing.T) {
	s, j := seeded(t)
	w := lock(t, s)
	defer w.Release()
	write(t, s, ReceiptPath(id("rcpt", 7)), []byte("{}\n"))
	write(t, s, OutcomePath(id("req", 7)), []byte("{}\n"))
	write(t, s, RecordPath("item", id("task", 7)), []byte("{\"record_revision\":1,\n"))
	before := tree(t, s.Dir)

	tests := []struct {
		name   string
		change func(tx *Tx)
		want   error
	}{
		{"a record at another revision", func(tx *Tx) { tx.Records[0].Expected = 2 }, ErrRevisionConflict},
		{"a record to replace that is absent", func(tx *Tx) { tx.Records[1].Expected = 1 }, ErrRevisionConflict},
		{"a record to create that is present", func(tx *Tx) { tx.Records[0].Expected = 0 }, ErrAlreadyExists},
		{"a record that is malformed", func(tx *Tx) { tx.Records[1].Path = RecordPath("item", id("task", 7)); tx.Records[1].Expected = 1 }, ErrRecordMalformed},
		{"a receipt that is present", func(tx *Tx) { tx.Receipts[0].ID = id("rcpt", 7) }, ErrAlreadyExists},
		{"an outcome that is present", func(tx *Tx) { tx.RequestID = id("req", 7) }, ErrAlreadyExists},
		{"the same record twice", func(tx *Tx) { tx.Records[1].Path = task1; tx.Records[1].Expected = 1 }, nil},
		{"the same receipt twice", func(tx *Tx) { tx.Receipts[1].ID = rcpt1 }, nil},
		{"the same event twice", func(tx *Tx) { tx.Events[1].ID = evt1 }, nil},
		{"a request that is not one", func(tx *Tx) { tx.RequestID = "req_x" }, nil},
		{"a receipt whose id is not one", func(tx *Tx) { tx.Receipts[0].ID = id("task", 3) }, nil},
		{"an event whose id is not one", func(tx *Tx) { tx.Events[0].ID = id("rcpt", 3) }, nil},
		{"a record path among the receipts", func(tx *Tx) { tx.Records[1].Path = ReceiptPath(id("rcpt", 8)) }, nil},
		{"record content that is not an object", func(tx *Tx) { tx.Records[2].Content = []byte(`"alpha"`) }, nil},
		{"receipt content that is not JSON", func(tx *Tx) { tx.Receipts[1].Content = []byte(`{`) }, nil},
		{"event content with a fraction", func(tx *Tx) { tx.Events[1].Content = []byte(`{"n":0.5}`) }, nil},
		{"no outcome", func(tx *Tx) { tx.Outcome = nil }, nil},
		{"an outcome that is not an object", func(tx *Tx) { tx.Outcome = []byte(`true`) }, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := fullTx()
			tc.change(&tx)
			got, err := w.Commit(tx)
			if err == nil {
				t.Fatalf("Commit = %+v, want an error", got)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error = %v; want %v", err, tc.want)
			}
			sameTree(t, "after a refused transaction", tree(t, s.Dir), before)
			if j.appends != 0 {
				t.Error("the journal was touched")
			}
		})
	}

	// Events without a journal to append them to.
	s2, _ := seeded(t)
	s2.journal = nil
	w2 := lock(t, s2)
	defer w2.Release()
	before = tree(t, s2.Dir)
	if _, err := w2.Commit(fullTx()); !errors.Is(err, ErrNoJournal) {
		t.Fatalf("Commit without a journal: %v", err)
	}
	sameTree(t, "after a refused transaction", tree(t, s2.Dir), before)
	tx := fullTx()
	tx.Events = nil
	if _, err := w2.Commit(tx); err != nil {
		t.Errorf("a transaction without events needs no journal: %v", err)
	}
}

// The transaction is interrupted at each of its steps. After every
// interruption each record is whole; the next command to take the lock
// completes the transaction, so the store ends exactly as an uninterrupted
// run leaves it, with every event once; and completing it again changes
// nothing.
func TestCommitInterruptedAtEachStep(t *testing.T) {
	s, _ := seeded(t)
	w := lock(t, s)
	steps := trace(t, s, func() {
		if _, err := w.Commit(fullTx()); err != nil {
			t.Fatal(err)
		}
	})
	w.Release()
	if len(steps) < 30 {
		t.Fatalf("only %d steps: %v", len(steps), steps)
	}
	done, doneEvents := committed(t)
	untouched := func() map[string]string {
		s, _ := seeded(t)
		return tree(t, s.Dir)
	}()
	old := record(t, map[string]any{"n": int64(1), "title": "first"}, 1, "2026-10-11T08:00:00.000Z")
	replaced := record(t, map[string]any{"n": int64(2), "title": "replaced"}, 2, "2026-10-11T08:00:00.000Z")
	created := record(t, map[string]any{"n": int64(1), "title": "created"}, 1, "2026-10-11T08:00:00.000Z")
	// The intent is on disk from its rename on. Before that the transaction
	// never happened; from then on recovery completes it.
	durable := slices.Index(steps, "syncdir "+IntentPath(req9))

	for i, step := range steps {
		t.Run(step, func(t *testing.T) {
			s, j := seeded(t)
			w := lock(t, s)
			hit := faultAt(s, step, 1)
			_, err := w.Commit(fullTx())
			w.Release()
			if !*hit || !errors.Is(err, errCrash) {
				t.Fatalf("the interruption did not happen: hit %v, err %v", *hit, err)
			}
			s.fault = nil

			// Whatever the step, every record is the whole old one or the whole
			// new one, and a reader sees no half.
			wholeRecord(t, s, task1, old, replaced)
			wholeRecord(t, s, task2, nil, created)
			if _, err := s.ReadRecord(task1); err != nil {
				t.Fatal(err)
			}
			intentThere := false
			if _, err := os.Stat(s.abs(IntentPath(req9))); err == nil {
				intentThere = true
			}
			if intentThere != (i >= durable) {
				t.Errorf("intent present %v after step %d %s", intentThere, i, step)
			}
			want, wantEvents := done, doneEvents
			if i < durable {
				want, wantEvents = untouched, nil
			}

			// The next lock completes the transaction, or finds nothing to
			// complete.
			w = lock(t, s)
			w.Release()
			sameTree(t, "after recovery", tree(t, s.Dir), want)
			if !slices.Equal(j.order, wantEvents) {
				t.Errorf("journal after recovery = %v, want %v", j.order, wantEvents)
			}
			if _, err := os.Stat(s.abs(IntentPath(req9))); !errNotExist(err) {
				t.Error("the intent remains after recovery")
			}

			// Rolling forward again changes nothing.
			appends := j.appends
			w = lock(t, s)
			w.Release()
			sameTree(t, "after a second recovery", tree(t, s.Dir), want)
			if j.appends != appends {
				t.Error("a second recovery appended events again")
			}
		})
	}
}

// Recovery itself is interrupted at each of its steps, and the lock after
// that completes it: roll-forward is idempotent at every point.
func TestRecoveryInterruptedAtEachStep(t *testing.T) {
	want, wantEvents := committed(t)
	interrupted := func(t *testing.T) (*Store, *fakeJournal) {
		t.Helper()
		s, j := seeded(t)
		w := lock(t, s)
		faultAt(s, "create "+task2, 1) // the intent and task1 are written
		if _, err := w.Commit(fullTx()); !errors.Is(err, errCrash) {
			t.Fatal(err)
		}
		w.Release()
		s.fault = nil
		return s, j
	}
	s, _ := interrupted(t)
	steps := trace(t, s, func() { lock(t, s).Release() })
	if slices.Contains(steps, "create "+task1) {
		t.Error("recovery rewrote a record already at the intended digest")
	}
	if !slices.Contains(steps, "create "+task2) || !slices.Contains(steps, "events "+IntentPath(req9)) {
		t.Errorf("recovery steps = %v", steps)
	}
	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
			s, j := interrupted(t)
			hit := faultAt(s, step, 1)
			if _, err := s.Lock(); !*hit || !errors.Is(err, errCrash) {
				t.Fatalf("the interruption did not happen: hit %v, err %v", *hit, err)
			}
			s.fault = nil
			// The failed Lock released the lock: the next one is taken at once.
			w := lock(t, s)
			w.Release()
			sameTree(t, "after recovery", tree(t, s.Dir), want)
			if !slices.Equal(j.order, wantEvents) {
				t.Errorf("journal = %v", j.order)
			}
		})
	}
}

// After the intent is written, what recovery finds on disk decides: a record
// at the intended digest is left, one at the expected revision or absent as
// expected is written, and anything else stops the store for that
// transaction, naming the intent, with nothing written and the intent kept.
func TestRecoveryDecidesByWhatItFinds(t *testing.T) {
	want, _ := committed(t)
	afterIntent := func(t *testing.T) (*Store, *fakeJournal) {
		t.Helper()
		s, j := seeded(t)
		w := lock(t, s)
		faultAt(s, "create "+task1, 1)
		if _, err := w.Commit(fullTx()); !errors.Is(err, errCrash) {
			t.Fatal(err)
		}
		w.Release()
		s.fault = nil
		return s, j
	}
	replaced := record(t, map[string]any{"n": int64(2), "title": "replaced"}, 2, "2026-10-11T08:00:00.000Z")

	t.Run("records already at the intended digest are left", func(t *testing.T) {
		s, _ := afterIntent(t)
		write(t, s, task1, replaced)
		steps := trace(t, s, func() { lock(t, s).Release() })
		if slices.Contains(steps, "create "+task1) {
			t.Error("the record was rewritten")
		}
		sameTree(t, "after recovery", tree(t, s.Dir), want)
	})
	t.Run("a record at the intended digest but spaced otherwise is left", func(t *testing.T) {
		s, _ := afterIntent(t)
		spaced := bytes.Replace(replaced, []byte(`,"title"`), []byte(` , "title"`), 1)
		write(t, s, task1, spaced)
		lock(t, s).Release()
		if data, _ := os.ReadFile(s.abs(task1)); !bytes.Equal(data, spaced) {
			t.Error("the record was rewritten")
		}
	})
	t.Run("a receipt and an outcome already written are left", func(t *testing.T) {
		s, _ := afterIntent(t)
		write(t, s, ReceiptPath(rcpt1), []byte(`{"kind":"pool.item.created","receipt_id":"`+rcpt1+`"}`+"\n"))
		write(t, s, OutcomePath(req9), []byte(`{"request_id":"`+req9+`","status":"ok"}`+"\n"))
		steps := trace(t, s, func() { lock(t, s).Release() })
		for _, rel := range []string{ReceiptPath(rcpt1), OutcomePath(req9)} {
			if slices.Contains(steps, "create "+rel) {
				t.Errorf("%s was rewritten", rel)
			}
		}
		sameTree(t, "after recovery", tree(t, s.Dir), want)
	})

	stops := []struct {
		name   string
		change func(t *testing.T, s *Store)
		names  string
	}{
		{"a record at another revision", func(t *testing.T, s *Store) {
			write(t, s, task1, record(t, map[string]any{"n": int64(5)}, 5, "2026-10-11T08:00:00.000Z"))
		}, task1},
		{"a record at the intended revision with other content", func(t *testing.T, s *Store) {
			write(t, s, task1, record(t, map[string]any{"n": int64(7)}, 2, "2026-10-11T08:00:00.000Z"))
		}, task1},
		{"a record expected absent that is present with other content", func(t *testing.T, s *Store) {
			write(t, s, task2, record(t, map[string]any{"n": int64(7)}, 1, "2026-10-11T08:00:00.000Z"))
		}, task2},
		{"a record expected at a revision that is absent", func(t *testing.T, s *Store) {
			os.Remove(s.abs(task1))
		}, task1},
		{"a record that is malformed", func(t *testing.T, s *Store) {
			write(t, s, task1, []byte("{\"record_revision\":1,\n"))
		}, task1},
		{"a receipt present with other content", func(t *testing.T, s *Store) {
			write(t, s, ReceiptPath(rcpt2), []byte("{\"other\":true}\n"))
		}, ReceiptPath(rcpt2)},
		{"a receipt that is malformed", func(t *testing.T, s *Store) {
			write(t, s, ReceiptPath(rcpt2), []byte("{"))
		}, ReceiptPath(rcpt2)},
		{"an outcome present with other content", func(t *testing.T, s *Store) {
			write(t, s, OutcomePath(req9), []byte("{\"status\":\"failed\"}\n"))
		}, OutcomePath(req9)},
	}
	for _, tc := range stops {
		t.Run(tc.name, func(t *testing.T) {
			s, j := afterIntent(t)
			tc.change(t, s)
			before := tree(t, s.Dir)
			for i := 0; i < 2; i++ {
				w, err := s.Lock()
				if !errors.Is(err, ErrRecordMalformed) || w != nil {
					t.Fatalf("Lock %d = %v, %v; want ErrRecordMalformed", i, w, err)
				}
				if !strings.Contains(err.Error(), IntentPath(req9)) || !strings.Contains(err.Error(), tc.names) {
					t.Errorf("error %q names neither the intent nor %s", err, tc.names)
				}
				if Code(err) != "record_malformed" {
					t.Errorf("Code = %q", Code(err))
				}
				sameTree(t, "after a stopped recovery", tree(t, s.Dir), before)
				if j.appends != 0 {
					t.Error("events were appended")
				}
			}
			// The stopped lock was released: a shared reader is not kept out.
			l, err := s.LockShared()
			if err != nil {
				t.Fatal(err)
			}
			l.Release()
		})
	}
}

// A torn or otherwise malformed intent stops the store, naming it, and is
// never discarded. A torn temporary file of an intent is not an intent and
// is removed.
func TestTornIntent(t *testing.T) {
	s, _ := seeded(t)
	w := lock(t, s)
	whole, err := s.encodeIntent(plan(t, s, fullTx()))
	if err != nil {
		t.Fatal(err)
	}
	w.Release()
	rel := IntentPath(req9)

	torn := map[string][]byte{
		"cut in half":                        whole[:len(whole)/2],
		"cut before its line feed":           whole[:len(whole)-1],
		"empty":                              nil,
		"not JSON":                           []byte("intent\n"),
		"not an object":                      []byte("[]\n"),
		"another schema":                     bytes.Replace(whole, []byte(intentSchema), []byte("hive.other"), 1),
		"another version":                    bytes.Replace(whole, []byte(`"schema_version":"1.0"`), []byte(`"schema_version":"1.1"`), 1),
		"another package":                    bytes.Replace(whole, []byte(`"package":"pool"`), []byte(`"package":"shadow"`), 1),
		"another request":                    bytes.Replace(whole, []byte(`"request_id":"`+req9), []byte(`"request_id":"`+req2), 1),
		"a digest that does not match":       bytes.Replace(whole, []byte(`"title":"replaced"`), []byte(`"title":"other"`), 1),
		"an unknown member":                  bytes.Replace(whole, []byte(`"schema":`), []byte(`"extra":1,"schema":`), 1),
		"no outcome":                         bytes.Replace(whole, []byte(`"outcome":`), []byte(`"outcomes":`), 1),
		"a record without an expected state": bytes.Replace(whole, []byte(`"expected":"revision","expected_revision":1`), []byte(`"expected":"maybe"`), 1),
		"a record with an expected revision of zero": bytes.Replace(whole, []byte(`"expected_revision":1`), []byte(`"expected_revision":0`), 1),
		"an event without its id":                    bytes.ReplaceAll(whole, []byte(`"event_id":"`+evt1+`"`), []byte(`"event_id":"x"`)),
	}
	for name, data := range torn {
		t.Run(name, func(t *testing.T) {
			s, j := seeded(t)
			write(t, s, rel, data)
			before := tree(t, s.Dir)
			for i := 0; i < 2; i++ {
				w, err := s.Lock()
				if !errors.Is(err, ErrRecordMalformed) || w != nil {
					t.Fatalf("Lock = %v, %v; want ErrRecordMalformed", w, err)
				}
				if !strings.Contains(err.Error(), rel) {
					t.Errorf("error %q does not name the intent", err)
				}
				sameTree(t, "after a stopped recovery", tree(t, s.Dir), before)
				if j.appends != 0 {
					t.Error("events were appended")
				}
			}
		})
	}
	t.Run("the whole intent rolls forward", func(t *testing.T) {
		want, wantEvents := committed(t)
		s, j := seeded(t)
		write(t, s, rel, whole)
		lock(t, s).Release()
		sameTree(t, "after recovery", tree(t, s.Dir), want)
		if !slices.Equal(j.order, wantEvents) {
			t.Errorf("journal = %v", j.order)
		}
	})
	t.Run("a torn temporary file of an intent is removed", func(t *testing.T) {
		s, j := seeded(t)
		write(t, s, rel+"."+req9+tempSuffix, whole[:len(whole)/2])
		write(t, s, "tx/notes.txt", []byte("not an intent"))
		before := tree(t, s.Dir)
		lock(t, s).Release()
		delete(before, "pool/tx/"+req9+".json."+req9+tempSuffix)
		sameTree(t, "after the sweep", tree(t, s.Dir), before)
		if j.appends != 0 {
			t.Error("events were appended")
		}
	})
}

// plan plans a transaction against s, failing the test if it cannot.
func plan(t *testing.T, s *Store, tx Tx) *intent {
	t.Helper()
	in, err := s.plan(tx)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// The intent as written reads back as the intent that was planned.
func TestIntentRoundTrip(t *testing.T) {
	s, _ := seeded(t)
	in := plan(t, s, fullTx())
	data, err := s.encodeIntent(in)
	if err != nil {
		t.Fatal(err)
	}
	if !canon.IsCanonical(data[:len(data)-1]) {
		t.Error("the intent is not in canonical form")
	}
	write(t, s, IntentPath(req9), data)
	back, err := s.readIntent(IntentPath(req9))
	if err != nil {
		t.Fatal(err)
	}
	if back.request != in.request || len(back.records) != 3 || len(back.receipts) != 2 || len(back.events) != 2 {
		t.Fatalf("read back %+v", back)
	}
	for i, r := range in.records {
		b := back.records[i]
		if b.rel != r.rel || b.absent != r.absent || b.expected != r.expected || b.digest != r.digest || !bytes.Equal(b.data, r.data) {
			t.Errorf("record %d read back %+v, want %+v", i, b, r)
		}
	}
	if back.records[0].absent || back.records[0].expected != 1 || !back.records[1].absent {
		t.Errorf("expected states read back wrong: %+v %+v", back.records[0], back.records[1])
	}
	for i, r := range in.receipts {
		if b := back.receipts[i]; b.rel != r.rel || !b.absent || !bytes.Equal(b.data, r.data) {
			t.Errorf("receipt %d read back %+v", i, b)
		}
	}
	for i, e := range in.events {
		if b := back.events[i]; b.ID != e.ID || !bytes.Equal(b.Content, e.Content) {
			t.Errorf("event %d read back %+v", i, b)
		}
	}
	if back.outcome.rel != OutcomePath(req9) || !bytes.Equal(back.outcome.data, in.outcome.data) {
		t.Errorf("outcome read back %+v", back.outcome)
	}
	// The spec's words: a created record states that it is expected absent.
	doc := parsed(t, data)
	rec := doc["records"].([]any)[1].(map[string]any)
	if rec["expected"] != "absent" || rec["expected_revision"] != nil || rec["path"] != task2 {
		t.Errorf("created record entry = %v", rec)
	}
	rec = doc["records"].([]any)[0].(map[string]any)
	if rec["expected"] != "revision" || rec["expected_revision"] != int64(1) {
		t.Errorf("replaced record entry = %v", rec)
	}
}

// A journal that refuses stops the transaction after the records and
// receipts, with the intent kept; the next lock tries again.
func TestJournalFailureKeepsTheIntent(t *testing.T) {
	want, wantEvents := committed(t)
	s, j := seeded(t)
	w := lock(t, s)
	j.fail = errors.New("journal down")
	if _, err := w.Commit(fullTx()); err == nil || !strings.Contains(err.Error(), "journal down") {
		t.Fatalf("Commit = %v", err)
	}
	w.Release()
	if _, err := os.Stat(s.abs(IntentPath(req9))); err != nil {
		t.Fatal("the intent was discarded")
	}
	if _, err := s.Lock(); err == nil {
		t.Fatal("recovery succeeded while the journal is down")
	}
	j.fail = nil
	lock(t, s).Release()
	sameTree(t, "after recovery", tree(t, s.Dir), want)
	if !slices.Equal(j.order, wantEvents) || j.appends != 3 {
		t.Errorf("journal = %v after %d appends", j.order, j.appends)
	}
}

// Two intents left behind are both completed, in name order.
func TestRecoveryCompletesEveryIntent(t *testing.T) {
	s, j := seeded(t)
	w := lock(t, s)
	first := plan(t, s, fullTx())
	firstData, _ := s.encodeIntent(first)
	second := Tx{RequestID: req2, Records: []RecordWrite{{RecordPath("item", id("task", 5)), []byte(`{"n":5}`), 0}}, Outcome: []byte(`{"status":"ok"}`)}
	secondData, _ := s.encodeIntent(plan(t, s, second))
	w.Release()
	write(t, s, IntentPath(req9), firstData)
	write(t, s, IntentPath(req2), secondData)
	lock(t, s).Release()
	if entries, _ := os.ReadDir(filepath.Join(s.dir, txDir)); len(entries) != 0 {
		t.Errorf("intents remain: %v", entries)
	}
	for _, rel := range []string{task1, task2, ws1, RecordPath("item", id("task", 5)), ReceiptPath(rcpt1), OutcomePath(req9), OutcomePath(req2)} {
		if _, err := s.Read(rel); err != nil && !errors.Is(err, ErrRecordMalformed) {
			t.Errorf("%s: %v", rel, err)
		}
	}
	if rec, err := s.ReadRecord(task1); err != nil || rec.Revision != 2 {
		t.Errorf("task1 = %+v, %v", rec, err)
	}
	if !slices.Equal(j.order, []string{evt1, evt2}) {
		t.Errorf("journal = %v", j.order)
	}
}
