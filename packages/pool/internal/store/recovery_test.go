package store_test

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
)

// prepareFor seeds the scenario for a test, failing it when the home cannot
// be set up or the request does not do what the scenario's writer is for.
func prepareFor(t *testing.T, sc *scenario) *prepared {
	t.Helper()
	p, err := prepare(sc)
	if err != nil {
		t.Fatal(err)
	}
	if p.clean.resp.Status == envelope.Failed || p.clean.resp.Status == envelope.Unknown || p.clean.resp.Status == envelope.Invalid {
		t.Fatalf("the request of %s is %s: %+v", sc.name, p.clean.resp.Status, p.clean.resp.Error)
	}
	return p
}

var idShape = regexp.MustCompile(`_[0-9a-z]{26}`)

// label is a step with the identities it names taken out, so that the name of
// a case is the same from run to run.
func label(i int, step string) string {
	return fmt.Sprintf("%02d %s", i, strings.ReplaceAll(idShape.ReplaceAllString(step, "_<id>"), "/", "|"))
}

// leftovers are the files of an interrupted write that a complete one leaves
// behind: an intent in tx/ and the temporary files of tmp/.
func leftovers(all map[string][]byte) []string {
	var bad []string
	for path := range all {
		if strings.HasPrefix(path, "pool/tmp/") || strings.HasPrefix(path, "pool/tx/") {
			bad = append(bad, "left behind: "+path)
		}
	}
	slices.Sort(bad)
	return bad
}

// sameResponse reports how two responses to one request differ, ignoring the
// time they were completed and whether one is a replay.
func sameResponse(a, b *envelope.Response) []string {
	var bad []string
	if a.Status != b.Status {
		bad = append(bad, fmt.Sprintf("status %s against %s", a.Status, b.Status))
	}
	if !reflect.DeepEqual(a.Body, b.Body) {
		bad = append(bad, fmt.Sprintf("body %v against %v", a.Body, b.Body))
	}
	if !reflect.DeepEqual(a.Error, b.Error) {
		bad = append(bad, fmt.Sprintf("error %+v against %+v", a.Error, b.Error))
	}
	ra, rb := receiptIDs(a), receiptIDs(b)
	if !slices.Equal(ra, rb) {
		bad = append(bad, fmt.Sprintf("receipts %v against %v", ra, rb))
	}
	return bad
}

func receiptIDs(r *envelope.Response) []string {
	out := []string{}
	for _, x := range r.Receipts {
		out = append(out, x.ReceiptID)
	}
	return out
}

// settled checks a home after the request has been recovered or sent again:
// the transaction is whole and was written once, the request has the outcome
// the intent said, the response is that outcome, a replay, and the receipts
// and events agree with it. before are the files of the home as the request
// found it; crashed are those the crash left, from which the intent is read;
// durable says whether the crash left the transaction the store's.
func settled(t *testing.T, p *prepared, before, crashed map[string][]byte, dir string, resp *envelope.Response, durable bool) {
	t.Helper()
	id := p.req.id
	all := files(dir)
	events, err := journalEvents(dir)
	if err != nil {
		t.Fatal(err)
	}
	check(t, "the home after recovery", leftovers(all))
	if durable {
		in, err := readIntent(crashed, id)
		if err != nil {
			t.Fatal(err)
		}
		if in != nil {
			check(t, "the transaction of the intent", completeOnce(all, events, in))
		}
		rec, err := recorded(all, id)
		if err != nil {
			t.Fatalf("no outcome is recorded after recovery: %v", err)
		}
		if !resp.Replayed {
			t.Errorf("the request sent again after a durable crash is answered afresh, not as the replay of its recorded outcome")
		}
		check(t, "the response against the recorded outcome", sameResponse(resp, rec))
	} else {
		if resp.Replayed {
			t.Errorf("a request of which nothing was durable is answered as a replay")
		}
		check(t, "the response against a clean run", sameResponse(&envelope.Response{Status: resp.Status}, &envelope.Response{Status: p.clean.resp.Status}))
	}
	got := shapeOf(before, all, p.beforeEv, events)
	if !sameShape(got, p.cleanShape) {
		t.Errorf("the transaction wrote %s; written clean, it writes %s", got, p.cleanShape)
	}
	check(t, "the receipts and events", agrees(all, resp, newEvents(p.beforeEv, events)))
	// A request sent once more is the replay of the recorded outcome and writes
	// nothing, to the Pool or to the journal.
	again, err := execute(dir, p.now, p.req, p.data, nil, cstore.Waiting{})
	if err != nil {
		t.Fatal(err)
	}
	if !again.resp.Replayed {
		t.Errorf("the request sent once more is not a replay")
	}
	check(t, "the replay against the recovered response", sameResponse(again.resp, resp))
	if d := differences(all, files(dir)); len(d) != 0 {
		t.Errorf("a replay changed the home: %v", d)
	}
}

// crashStep stops the writer's request at the n-th step the store names and
// holds the recovery to the contract when the request is sent again. At the
// first step that leaves the transaction durable, at the append of the events
// and at the last step, the other commands that take the lock are held to it
// too: the next lock of the store and a read of the request's outcome. It says
// whether the transaction was durable, and what it has seen of the steps
// before.
func crashStep(t *testing.T, p *prepared, n int, others bool, seen *bool) {
	t.Helper()
	before := files(p.before)
	dir := clone(t, p.before)
	crashed, err := crashAt(dir, p.now, p.req, p.data, n)
	if err != nil {
		t.Fatal(err)
	}
	if len(crashed.steps) <= n {
		t.Fatalf("the request named %d steps, and the crash was set at step %d of %d of a clean run", len(crashed.steps), n, len(p.clean.steps))
	}
	left := files(dir)
	events, err := journalEvents(dir)
	if err != nil {
		t.Fatal(err)
	}
	dur := durable(left, p.req.id)
	if dur {
		if crashed.resp.Status != envelope.Unknown || crashed.resp.Error == nil || crashed.resp.Error.Code != envelope.CodeOutcomeUnknown {
			t.Errorf("a crash after the transaction became durable is answered %s %+v, not outcome_unknown", crashed.resp.Status, crashed.resp.Error)
		}
		others = others || !*seen
		*seen = true
	} else {
		if crashed.resp.Status == envelope.Ok || crashed.resp.Status == envelope.Unknown {
			t.Errorf("a crash before the transaction became durable is answered %s", crashed.resp.Status)
		}
		if d := differences(poolFiles(before), poolFiles(left)); len(d) != 0 {
			t.Errorf("a crash before the intent was durable left %v", d)
		}
		if len(newEvents(p.beforeEv, events)) != 0 {
			t.Errorf("a crash before the intent was durable left events")
		}
	}
	finals := map[string]map[string][]byte{}
	if others {
		// These copies are made before the request is sent again over the crash.
		t.Run("the next lock, then the same request", func(t *testing.T) {
			d := clone(t, dir)
			if err := recoverByLock(d, p.now); err != nil {
				t.Fatalf("the next lock cannot complete the transaction: %v", err)
			}
			if in, err := readIntent(left, p.req.id); err != nil {
				t.Fatal(err)
			} else if in != nil && dur {
				check(t, "the transaction after the next lock", completeOnce(files(d), mustEvents(d), in))
			}
			run, err := execute(d, p.now, p.req, p.data, nil, cstore.Waiting{})
			if err != nil {
				t.Fatal(err)
			}
			settled(t, p, before, left, d, run.resp, dur)
			finals["lock"] = poolFiles(files(d))
		})
		if dur {
			t.Run("a read of the request's outcome", func(t *testing.T) {
				d := clone(t, dir)
				read := &req{command: "hive-pool", op: "read", body: map[string]any{"kind": "request-outcome", "selector": map[string]any{"id": p.req.id}}}
				got, err := execute(d, p.now, read, read.bytes(), nil, cstore.Waiting{})
				if err != nil {
					t.Fatal(err)
				}
				if got.resp.Status != envelope.Ok {
					t.Fatalf("a read of the outcome of an interrupted transaction is %s %+v; it recovers the transaction before it answers", got.resp.Status, got.resp.Error)
				}
				if in, err := readIntent(left, p.req.id); err != nil {
					t.Fatal(err)
				} else if in != nil {
					check(t, "the transaction after the read", completeOnce(files(d), mustEvents(d), in))
				}
				run, err := execute(d, p.now, p.req, p.data, nil, cstore.Waiting{})
				if err != nil {
					t.Fatal(err)
				}
				settled(t, p, before, left, d, run.resp, dur)
				finals["read"] = poolFiles(files(d))
			})
		}
	}
	t.Run("the same request again", func(t *testing.T) {
		run, err := execute(dir, p.now, p.req, p.data, nil, cstore.Waiting{})
		if err != nil {
			t.Fatal(err)
		}
		settled(t, p, before, left, dir, run.resp, dur)
		finals["again"] = poolFiles(files(dir))
	})
	if dur {
		for _, other := range []string{"lock", "read"} {
			if finals[other] != nil {
				if d := differences(finals["again"], finals[other]); len(d) != 0 {
					t.Errorf("the home depends on who completes the transaction (%s): %v", other, d)
				}
			}
		}
	}
}

// Every multi-record transaction is rolled forward from its intent exactly
// once, from a crash at each step the store names: the writes of the intent,
// of each record, receipt and the outcome, the append of the events and the
// removal of the intent.
func TestEveryDurableStepOfEveryWriterRollsForwardExactlyOnce(t *testing.T) {
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			p := prepareFor(t, sc)
			seen := false
			for i, step := range p.clean.steps {
				others := strings.HasPrefix(step, "events ") || i == len(p.clean.steps)-1
				t.Run(label(i, step), func(t *testing.T) { crashStep(t, p, i, others, &seen) })
			}
		})
	}
}

// firstWrite is the step of a clean run at which the first file of the
// transaction after its intent is renamed into place.
func firstWrite(steps []string) int {
	for i, s := range steps {
		if strings.HasPrefix(s, "rename pool/") && !strings.HasPrefix(s, "rename pool/tx/") {
			return i
		}
	}
	return -1
}

// A recovery is itself interrupted at each step it names, and the next one
// completes the transaction all the same, once.
func TestEveryStepOfARecoveryRollsForwardExactlyOnce(t *testing.T) {
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			p := prepareFor(t, sc)
			n := firstWrite(p.clean.steps)
			if n < 0 {
				t.Fatal("the request writes no file after its intent")
			}
			before := files(p.before)
			dir := clone(t, p.before)
			if _, err := crashAt(dir, p.now, p.req, p.data, n); err != nil {
				t.Fatal(err)
			}
			left := files(dir)
			if !durable(left, p.req.id) {
				t.Fatalf("the transaction is not durable at step %d", n)
			}
			probe, err := execute(clone(t, dir), p.now, p.req, p.data, nil, cstore.Waiting{})
			if err != nil {
				t.Fatal(err)
			}
			for j, step := range probe.steps {
				t.Run(label(j, step), func(t *testing.T) {
					d := clone(t, dir)
					second, err := crashAt(d, p.now, p.req, p.data, j)
					if err != nil {
						t.Fatal(err)
					}
					if second.resp.Status != envelope.Unknown {
						t.Errorf("a recovery stopped at %q is answered %s %+v, not unknown", step, second.resp.Status, second.resp.Error)
					}
					run, err := execute(d, p.now, p.req, p.data, nil, cstore.Waiting{})
					if err != nil {
						t.Fatal(err)
					}
					settled(t, p, before, left, d, run.resp, true)
				})
			}
		})
	}
}

// A request sent again returns the outcome recorded for it, with the receipts
// and the events it wrote and nothing more.
func TestAReplayReturnsTheRecordedOutcomeOfEveryWriter(t *testing.T) {
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			p := prepareFor(t, sc)
			dir := clone(t, p.before)
			first, err := execute(dir, p.now, p.req, p.data, nil, cstore.Waiting{})
			if err != nil {
				t.Fatal(err)
			}
			if first.resp.Replayed {
				t.Fatal("the first answer is a replay")
			}
			all := files(dir)
			events, err := journalEvents(dir)
			if err != nil {
				t.Fatal(err)
			}
			rec, err := recorded(all, p.req.id)
			if err != nil {
				t.Fatalf("no outcome is recorded: %v", err)
			}
			check(t, "the first response against the recorded outcome", sameResponse(first.resp, rec))
			check(t, "the first response", agrees(all, first.resp, newEvents(p.beforeEv, events)))
			if got := shapeOf(files(p.before), all, p.beforeEv, events); !sameShape(got, p.cleanShape) {
				t.Errorf("a second run wrote %s, the first %s", got, p.cleanShape)
			}
			// The receipts written are those the response names, and no others.
			named := map[string]bool{}
			for _, id := range receiptIDs(first.resp) {
				named[id] = true
			}
			for path := range all {
				if rest, ok := strings.CutPrefix(path, "pool/receipts/"); ok {
					if _, existed := files(p.before)[path]; !existed && !named[strings.TrimSuffix(rest, ".json")] {
						t.Errorf("the receipt %s is written and the response does not name it", rest)
					}
				}
			}
			for i := 0; i < 2; i++ {
				again, err := execute(dir, p.now, p.req, p.data, nil, cstore.Waiting{})
				if err != nil {
					t.Fatal(err)
				}
				if !again.resp.Replayed {
					t.Errorf("replay %d is not marked as one", i+1)
				}
				check(t, fmt.Sprintf("replay %d against the first response", i+1), sameResponse(again.resp, first.resp))
				if len(again.steps) != 0 && slices.ContainsFunc(again.steps, func(s string) bool {
					return strings.HasPrefix(s, "create ") || strings.HasPrefix(s, "rename ") || strings.HasPrefix(s, "events ")
				}) {
					t.Errorf("a replay wrote: %v", again.steps)
				}
				if d := differences(all, files(dir)); len(d) != 0 {
					t.Errorf("replay %d changed the home: %v", i+1, d)
				}
				evs, err := journalEvents(dir)
				if err != nil {
					t.Fatal(err)
				}
				if len(evs) != len(events) {
					t.Errorf("replay %d appended %d events", i+1, len(evs)-len(events))
				}
			}
		})
	}
}

// Every operation the registry builds that mutates is a writer of this proof:
// the scenarios drive each of them, so that no operation that writes to pool/
// is left to a case that names only some.
func TestEveryMutatingOperationOfTheRegistryIsDriven(t *testing.T) {
	driven := map[string]bool{}
	for _, sc := range scenarios {
		driven[sc.command+" "+sc.op] = true
	}
	registered := map[string]bool{}
	for _, c := range entry.Executable().Commands {
		for _, op := range c.Operations {
			if op.State == envelope.Supported && op.Mutates {
				registered[c.Name+" "+op.Name] = true
			}
		}
	}
	for key := range registered {
		if !driven[key] {
			t.Errorf("%s mutates and no scenario drives it", key)
		}
	}
	for key := range driven {
		if !registered[key] {
			t.Errorf("%s is driven and is not a mutating operation the registry builds", key)
		}
	}
	if len(registered) == 0 {
		t.Fatal("the registry builds no mutating operation")
	}
}

// The checks the cases make are not vacuous: a transaction a crash left at the
// append of its events, and not recovered, fails the check of a complete one on
// the intent that is still there, the records and receipts it has not written
// and the event that is not in the journal.
func TestTheChecksReportATransactionLeftUnrecovered(t *testing.T) {
	var sc *scenario
	for _, s := range scenarios {
		if s.name == "hive-pool hold" {
			sc = s
		}
	}
	p := prepareFor(t, sc)
	at := slices.IndexFunc(p.clean.steps, func(s string) bool { return strings.HasPrefix(s, "events ") })
	if at < 0 {
		t.Fatal("the request appends no events")
	}
	dir := clone(t, p.before)
	if _, err := crashAt(dir, p.now, p.req, p.data, at); err != nil {
		t.Fatal(err)
	}
	left := files(dir)
	in, err := readIntent(left, p.req.id)
	if err != nil || in == nil {
		t.Fatalf("the crash left no intent: %v", err)
	}
	got := strings.Join(completeOnce(left, mustEvents(dir), in), "\n")
	for _, want := range []string{"the intent is still there", "absent: pool/requests/", "is in the journal 0 times"} {
		if !strings.Contains(got, want) {
			t.Errorf("the check of a complete transaction does not report %q of one left unrecovered:\n%s", want, got)
		}
	}
	if shape := shapeOf(files(p.before), left, p.beforeEv, mustEvents(dir)); sameShape(shape, p.cleanShape) {
		t.Errorf("an unrecovered transaction has the shape of a complete one: %s", shape)
	}
}
