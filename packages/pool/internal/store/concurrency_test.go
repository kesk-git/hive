package store_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
)

// held is a writer whose transaction is held between the intent and the first
// file it writes, with the store's lock taken. release lets it go on.
type held struct {
	dir      string
	blocked  chan struct{}
	release  chan struct{}
	done     chan result
	released bool
}

type result struct {
	run *run
	err error
}

// hold starts the request of the scenario over a copy of its home and returns
// once its transaction is durable and the writer is stopped inside it, holding
// the lock. The writer is stopped by the store's fault seam, which waits on a
// channel: nothing is timed.
func hold(t *testing.T, p *prepared) *held {
	t.Helper()
	at := firstWrite(p.clean.steps)
	if at < 0 {
		t.Fatal("the request writes no file after its intent")
	}
	h := &held{dir: clone(t, p.before), blocked: make(chan struct{}), release: make(chan struct{}), done: make(chan result, 1)}
	count := 0
	go func() {
		r, err := execute(h.dir, p.now, p.req, p.data, func(string) error {
			if count++; count-1 == at {
				close(h.blocked)
				<-h.release
			}
			return nil
		}, cstore.Waiting{})
		h.done <- result{r, err}
	}()
	select {
	case <-h.blocked:
	case r := <-h.done:
		t.Fatalf("the first writer ended before it was held: %+v, %v", r.run, r.err)
	}
	return h
}

// finish lets the held writer go on and returns what it answered.
func (h *held) finish(t *testing.T) *envelope.Response {
	t.Helper()
	if !h.released {
		h.released = true
		close(h.release)
	}
	r := <-h.done
	if r.err != nil {
		t.Fatal(r.err)
	}
	return r.run.resp
}

// second starts another writer's request over the same home, which finds the
// store's lock held and waits for it by the pause alone: the pause says the
// writer has found the lock busy, and holds it there until proceed is closed.
// The result arrives on the channel.
func second(dir string, p *prepared, r *req) (found <-chan struct{}, proceed func(), done <-chan result) {
	foundc, gate, out := make(chan struct{}), make(chan struct{}), make(chan result, 1)
	var once bool
	go func() {
		waiting := cstore.Waiting{Clock: clock.NewFake(p.now), Limit: time.Hour, Pause: func(time.Duration) {
			if !once {
				once = true
				close(foundc)
			}
			<-gate
		}}
		run, err := execute(dir, p.now, r, r.bytes(), nil, waiting)
		out <- result{run, err}
	}()
	return foundc, func() { close(gate) }, out
}

// conflictOf reports whether the response is the conflict a stale expectation
// returns, with nothing in it.
func conflictOf(resp *envelope.Response) []string {
	var bad []string
	if resp.Status != envelope.Conflict || resp.Error == nil || resp.Error.Code != envelope.CodeRevisionConflict {
		bad = append(bad, fmt.Sprintf("the answer is %s %+v, not conflict revision_conflict", resp.Status, resp.Error))
	}
	if resp.Body != nil {
		bad = append(bad, "a conflict carries a body")
	}
	if len(resp.Receipts) != 0 {
		bad = append(bad, "a conflict carries receipts")
	}
	return bad
}

// stale is the request of a second writer that read what the first one
// replaces before the first wrote it: the same operation, from another caller,
// expecting a record the first replaces at the revision it was at. It expects
// what the first request expects, where that names a record the first
// replaces, and otherwise the first record the operation replaced. It reports
// whether the writer replaces a record at all.
func stale(p *prepared) (*req, bool) {
	replaced := p.replaced()
	b := p.req.again()
	if len(replaced) == 0 {
		return b, false
	}
	for _, e := range b.expected {
		if slices.Contains(replaced, e) {
			return b, true
		}
	}
	pick := replaced[0]
	for _, r := range replaced {
		if p.sc.stale != "" && kindPath(strings.TrimPrefix(r.schema, "pool."), r.id) == p.sc.stale {
			pick = r
		}
	}
	b.expected = append(b.expected, pick)
	return b, true
}

// kept checks that the transaction of the first writer stands whole after the
// second: no record of its intent is older than the intent made it, and the
// receipts and the outcome are as it wrote them.
func kept(all map[string][]byte, in *intent) []string {
	var bad []string
	digest := func(data []byte) string {
		d, err := canon.RecordDigest(data)
		if err != nil {
			return err.Error()
		}
		return d
	}
	for _, f := range in.Records {
		data, ok := all[f.Path]
		switch {
		case !ok:
			bad = append(bad, "record lost: "+f.Path)
		case revisionOf(data) < f.Expected+1:
			bad = append(bad, fmt.Sprintf("record %s is at revision %d; the first writer wrote %d", f.Path, revisionOf(data), f.Expected+1))
		case revisionOf(data) == f.Expected+1 && digest(data) != f.Digest:
			bad = append(bad, "record "+f.Path+" is at the revision the first writer wrote and not its words")
		}
	}
	once := append([]intentFile{}, in.Receipts...)
	if in.Outcome != nil {
		once = append(once, *in.Outcome)
	}
	for _, f := range once {
		if data, ok := all[f.Path]; !ok || digest(data) != f.Digest {
			bad = append(bad, "lost or changed: "+f.Path)
		}
	}
	return bad
}

// Two writers under the lock lose no update. The first is held inside its
// transaction; the second, which read the record the first replaces before the
// first wrote it, waits for the lock and, once the first has committed,
// receives revision_conflict with nothing of its own written and the first
// writer's words and fields as they were. Where the writer only creates
// records, the second creates its own beside them.
func TestTwoWritersOfEveryWriterLoseNoUpdate(t *testing.T) {
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			p := prepareFor(t, sc)
			before := files(p.before)

			t.Run("a stale expectation, held at the lock", func(t *testing.T) {
				b, guarded := stale(p)
				h := hold(t, p)
				in, err := readIntent(files(h.dir), p.req.id)
				if err != nil || in == nil {
					t.Fatalf("the held writer has no intent: %v", err)
				}
				found, proceed, done := second(h.dir, p, b)
				select {
				case <-found:
				case r := <-done:
					t.Fatalf("the second writer ended while the first held the lock: %+v, %v", r.run, r.err)
				}
				a := h.finish(t)
				if a.Status != p.clean.resp.Status {
					t.Errorf("the first writer is answered %s, a clean run %s", a.Status, p.clean.resp.Status)
				}
				proceed()
				r := <-done
				if r.err != nil {
					t.Fatal(r.err)
				}
				all := files(h.dir)
				events, err := journalEvents(h.dir)
				if err != nil {
					t.Fatal(err)
				}
				check(t, "the first writer's transaction", completeOnce(all, events, in))
				if guarded {
					check(t, "the second writer", conflictOf(r.run.resp))
					if got := shapeOf(before, all, p.beforeEv, events); !sameShape(got, p.cleanShape) {
						t.Errorf("the second writer changed the home: it wrote %s, the first alone %s", got, p.cleanShape)
					}
					return
				}
				// A writer that replaces nothing leaves the second its own records.
				switch r.run.resp.Status {
				case envelope.Ok:
					check(t, "the first writer's transaction", kept(all, in))
				case envelope.Conflict:
					if got := shapeOf(before, all, p.beforeEv, events); !sameShape(got, p.cleanShape) {
						t.Errorf("a refused second writer changed the home: %s against %s", got, p.cleanShape)
					}
				default:
					t.Errorf("the second writer is answered %s %+v", r.run.resp.Status, r.run.resp.Error)
				}
			})

			t.Run("a stale expectation, after the first has committed", func(t *testing.T) {
				b, guarded := stale(p)
				if !guarded {
					t.Skip("the writer replaces no record, so no expectation can be stale")
				}
				dir := clone(t, p.before)
				first, err := execute(dir, p.now, p.req, p.data, nil, cstore.Waiting{})
				if err != nil {
					t.Fatal(err)
				}
				if first.resp.Status != p.clean.resp.Status {
					t.Fatalf("the first writer is answered %s", first.resp.Status)
				}
				mid := files(dir)
				r, err := execute(dir, p.now, b, b.bytes(), nil, cstore.Waiting{})
				if err != nil {
					t.Fatal(err)
				}
				check(t, "the second writer", conflictOf(r.resp))
				if d := differences(mid, files(dir)); len(d) != 0 {
					t.Errorf("a conflict changed the home: %v", d)
				}
			})

			t.Run("no expectation, held at the lock", func(t *testing.T) {
				b := p.req.again()
				h := hold(t, p)
				in, err := readIntent(files(h.dir), p.req.id)
				if err != nil || in == nil {
					t.Fatalf("the held writer has no intent: %v", err)
				}
				found, proceed, done := second(h.dir, p, b)
				select {
				case <-found:
				case r := <-done:
					t.Fatalf("the second writer ended while the first held the lock: %+v, %v", r.run, r.err)
				}
				h.finish(t)
				proceed()
				r := <-done
				if r.err != nil {
					t.Fatal(r.err)
				}
				all := files(h.dir)
				check(t, "the first writer's transaction", kept(all, in))
				check(t, "the home", leftovers(all))
				events, err := journalEvents(h.dir)
				if err != nil {
					t.Fatal(err)
				}
				seen := map[string]int{}
				for _, e := range events {
					if seen[e.ID]++; seen[e.ID] > 1 {
						t.Errorf("event %s is in the journal twice", e.ID)
					}
				}
				check(t, "the second writer's receipts and events", agrees(all, r.run.resp, newEvents(p.beforeEv, events)))
			})
		})
	}
}

// corruption makes a record the writer needs malformed in the way a torn write
// or a hand edit would.
var corruptions = []struct {
	name string
	do   func(data []byte) []byte
}{
	{"torn", func(data []byte) []byte { return data[:len(data)/2] }},
	{"breaking its schema", func(data []byte) []byte {
		v, err := canon.Parse(data)
		if err != nil {
			mustf("a record to corrupt does not parse: %v", err)
		}
		obj := v.(map[string]any)
		delete(obj, "created_at")
		out, err := canon.Encode(obj)
		must(err)
		return append(out, '\n')
	}},
}

// A record the operation needs that is malformed stops the mutation with
// record_malformed and nothing written, and the writer goes on once the record
// is as it was.
func TestAMalformedRecordStopsEveryWriterWithNothingWritten(t *testing.T) {
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			p := prepareFor(t, sc)
			if len(p.needs) == 0 {
				t.Fatal("the scenario names no record the operation needs")
			}
			for _, need := range p.needs {
				for _, c := range corruptions {
					t.Run(strings.ReplaceAll(need, "/", "|")+" "+c.name, func(t *testing.T) {
						dir := clone(t, p.before)
						original := files(dir)[need]
						if original == nil {
							t.Fatalf("the home holds no %s", need)
						}
						bad := c.do(original)
						writeFile(dir, need, bad)
						before := files(dir)
						run, err := execute(dir, p.now, p.req, p.data, nil, cstore.Waiting{})
						if err != nil {
							t.Fatal(err)
						}
						code := envelope.CodeRecordMalformed
						if c, ok := sc.stops[need]; ok {
							code = c
						}
						if run.resp.Status == envelope.Ok || run.resp.Error == nil || run.resp.Error.Code != code {
							t.Errorf("the answer is %s %+v, not %s", run.resp.Status, run.resp.Error, code)
						}
						if run.resp.Body != nil || len(run.resp.Receipts) != 0 {
							t.Errorf("a stopped mutation carries a body or receipts")
						}
						if d := differences(poolFiles(before), poolFiles(files(dir))); len(d) != 0 {
							t.Errorf("a stopped mutation wrote %v", d)
						}
						events, err := journalEvents(dir)
						if err != nil {
							t.Fatal(err)
						}
						if n := newEvents(p.beforeEv, events); len(n) != 0 {
							t.Errorf("a stopped mutation appended %d events", len(n))
						}
						// The record is never repaired by guessing: it is as it was.
						if string(files(dir)[need]) != string(bad) {
							t.Errorf("%s was changed", need)
						}
						// Once the record is as it was, the same request is carried out.
						writeFile(dir, need, original)
						again, err := execute(dir, p.now, p.req, p.data, nil, cstore.Waiting{})
						if err != nil {
							t.Fatal(err)
						}
						if again.resp.Status != p.clean.resp.Status {
							t.Errorf("after the record is restored the request is answered %s %+v", again.resp.Status, again.resp.Error)
						}
						if got := shapeOf(files(p.before), files(dir), p.beforeEv, mustEvents(dir)); !sameShape(got, p.cleanShape) {
							t.Errorf("after the record is restored the request wrote %s; clean, %s", got, p.cleanShape)
						}
					})
				}
			}
		})
	}
}
