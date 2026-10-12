package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/store"
)

// effectRequest is a request of the effect operation in the mode given.
func effectRequest(t testing.TB, n int, mode string) []byte {
	t.Helper()
	return request(t, "effect", n, func(m map[string]any) { m["body"].(map[string]any)["home"].(map[string]any)["name"] = mode })
}

// outcomeOf reports whether the request has a recorded outcome in the home.
func outcomeOf(home string, n int) bool {
	_, err := os.Stat(filepath.Join(home, filepath.FromSlash(store.OutcomePath("dispatch", id(ids.Request, n)))))
	return err == nil
}

// receiptsOnDisk lists the receipt identifiers the home holds.
func receiptsOnDisk(t testing.TB, home string) []string {
	t.Helper()
	var out []string
	for rel := range tree(t, home) {
		if strings.HasPrefix(rel, "dispatch/receipts/") {
			out = append(out, strings.TrimSuffix(strings.TrimPrefix(rel, "dispatch/receipts/"), ".json"))
		}
	}
	return out
}

// An operation commits its intent, its record and its receipt, durably,
// before its effect: the intent is on disk when the effect runs, and the
// answer returns the receipts of the intent with the operation's own.
func TestIntentCommittedBeforeTheEffect(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	resp := h.invoke(t, home, "effect", effectRequest(t, 1, "ok"))
	expect(t, resp, envelope.Ok, "")
	if len(resp.Receipts) != 2 || resp.Receipts[0].Kind != "dispatch.thing.intended" || resp.Receipts[1].Kind != "dispatch.thing.done" {
		t.Fatalf("receipts %+v", resp.Receipts)
	}
	if h.calls["effected"] != 1 || !outcomeOf(home, 1) {
		t.Errorf("effect %d, outcome %v", h.calls["effected"], outcomeOf(home, 1))
	}
	for _, rc := range resp.Receipts {
		written, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(store.ReceiptPath("dispatch", rc.ReceiptID))))
		if err != nil {
			t.Fatal(err)
		}
		if enc, _ := rc.Encode(); string(written) != string(append(enc, '\n')) && string(written) != string(enc) {
			t.Errorf("the receipt on disk is not the one returned")
		}
	}
	// The record's revision rose with the intent and again with the completion.
	if resp.Body["revision"] != int64(1) {
		t.Errorf("body %v", resp.Body)
	}
	rec, err := openStore(t, home).ReadRecord(store.RecordPath("dispatch", "thing", "intended"))
	if err != nil || rec.Revision != 2 {
		t.Errorf("record %+v, %v", rec, err)
	}
	// A replay returns the recorded answer, with every receipt, and does not
	// run the operation or the effect again.
	again := h.invoke(t, home, "effect", effectRequest(t, 1, "ok"))
	expect(t, again, envelope.Ok, "")
	if !again.Replayed || len(again.Receipts) != 2 || h.calls["effected"] != 1 {
		t.Errorf("replay %+v, effect %d", again, h.calls["effected"])
	}
	if left := tree(t, home); left["dispatch/tx/"+id(ids.Request, 1)+".json"] != "" {
		t.Error("the intent remains")
	}
}

// An unknown outcome after a committed intent returns exactly the receipts
// of that intent, which are on disk, and records no outcome: the request
// stays open, and sending it again runs the operation again.
func TestUnknownOutcomeReturnsTheCommittedReceipts(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	resp := h.invoke(t, home, "effect", effectRequest(t, 2, "unknown"))
	expect(t, resp, envelope.Unknown, "outcome_unknown")
	if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "dispatch.thing.intended" || resp.Receipts[0].RequestID != id(ids.Request, 2) || resp.Replayed || resp.Error.Retryable {
		t.Fatalf("%+v", resp)
	}
	if on := receiptsOnDisk(t, home); len(on) != 1 || on[0] != resp.Receipts[0].ReceiptID {
		t.Errorf("receipts on disk %v", on)
	}
	if outcomeOf(home, 2) {
		t.Error("an outcome was recorded for an unknown outcome")
	}
	rec, err := openStore(t, home).ReadRecord(store.RecordPath("dispatch", "thing", "intended"))
	if err != nil || rec.Revision != 1 {
		t.Errorf("the intent record %+v, %v", rec, err)
	}
	if h.calls["effected"] != 1 {
		t.Errorf("effect %d", h.calls["effected"])
	}
	// Sent again, the operation runs again; it commits its intent anew, which
	// the record the first one left refuses: nothing is replayed.
	again := h.invoke(t, home, "effect", effectRequest(t, 2, "unknown"))
	expect(t, again, envelope.Conflict, "already_exists")
	if again.Replayed || len(again.Receipts) != 0 || h.calls["effected"] != 1 {
		t.Errorf("%+v, effect %d", again, h.calls["effected"])
	}
	if outcomeOf(home, 2) {
		t.Error("an outcome was recorded for a refused retry")
	}
}

// A refusal after an intent returns the intent's receipts with its own,
// records the outcome and is replayed whole.
func TestRefusalAfterAnIntent(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	bare := h.invoke(t, home, "effect", effectRequest(t, 3, "refused"))
	expect(t, bare, envelope.Refused, "authority_missing")
	if len(bare.Receipts) != 1 || bare.Receipts[0].Kind != "dispatch.thing.intended" || !outcomeOf(home, 3) {
		t.Fatalf("%+v, outcome %v", bare, outcomeOf(home, 3))
	}
	replayed := h.invoke(t, home, "effect", effectRequest(t, 3, "refused"))
	if !replayed.Replayed || len(replayed.Receipts) != 1 || replayed.Receipts[0].ReceiptID != bare.Receipts[0].ReceiptID || h.calls["effected"] != 1 {
		t.Errorf("replay %+v, effect %d", replayed, h.calls["effected"])
	}
	// With a receipt of its own: both are returned, both are on disk.
	h2 := newHarness(t)
	home2 := newHome(t)
	own := h2.invoke(t, home2, "effect", effectRequest(t, 4, "refused with a receipt"))
	expect(t, own, envelope.Refused, "authority_missing")
	if len(own.Receipts) != 2 || own.Receipts[0].Kind != "dispatch.thing.intended" || own.Receipts[1].Kind != "dispatch.thing.refused" || len(receiptsOnDisk(t, home2)) != 2 || !outcomeOf(home2, 4) {
		t.Errorf("%+v", own)
	}
}

// Once an intent is committed, a status that promises nothing happened is
// answered as unknown, with the intent's receipts, and nothing is recorded.
func TestStatusAfterAnIntentIsNeverNothingHappened(t *testing.T) {
	for _, mode := range []string{"unavailable", "plain error", "huge", "wrong revision"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			home := newHome(t)
			resp := h.invoke(t, home, "effect", effectRequest(t, 5, mode))
			expect(t, resp, envelope.Unknown, "outcome_unknown")
			if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "dispatch.thing.intended" || resp.Error.Retryable || resp.Replayed {
				t.Fatalf("%+v", resp)
			}
			if outcomeOf(home, 5) || len(receiptsOnDisk(t, home)) != 1 {
				t.Errorf("outcome %v, receipts %v", outcomeOf(home, 5), receiptsOnDisk(t, home))
			}
		})
	}
	// A handler that attaches receipts of its own to an unknown outcome is at
	// fault; the intent's are the only ones returned.
	h := newHarness(t)
	home := newHome(t)
	resp := h.invoke(t, home, "effect", effectRequest(t, 6, "unknown with a receipt"))
	expect(t, resp, envelope.Unknown, "outcome_unknown")
	if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "dispatch.thing.intended" {
		t.Errorf("%+v", resp)
	}
}

// An intent with no receipts is still committed: the effect after it may have
// happened, so an ordinary error is answered as unknown, with no receipts.
func TestRecordOnlyIntentIsStillCommitted(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	resp := h.invoke(t, home, "effect", effectRequest(t, 7, "record only"))
	expect(t, resp, envelope.Unknown, "outcome_unknown")
	if len(resp.Receipts) != 0 || resp.Error.Retryable || h.calls["effected"] != 1 {
		t.Fatalf("%+v, effected %d", resp, h.calls["effected"])
	}
	if outcomeOf(home, 7) {
		t.Error("an outcome was recorded for an unknown result")
	}
}

// An intent the store refuses writes nothing and runs no effect: the
// operation's failure says why, as the store's refusals do.
func TestIntentRefusedBeforeTheEffect(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	before := tree(t, home)
	resp := h.invoke(t, home, "effect", effectRequest(t, 7, "conflict"))
	expect(t, resp, envelope.Conflict, "revision_conflict")
	if len(resp.Receipts) != 0 || h.calls["effected"] != 0 || outcomeOf(home, 7) {
		t.Errorf("%+v, effect %d", resp, h.calls["effected"])
	}
	after := tree(t, home)
	delete(after, "locks/dispatch.lock")
	delete(before, "locks/dispatch.lock")
	same(t, before, after)
}

// An intent whose transaction is interrupted is durable and not complete:
// the answer is unknown, no effect ran, and the next lock completes it.
func TestIntentInterrupted(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	crashed := ""
	h.exe.Fault = func(name string) error {
		if crashed == "" && strings.HasPrefix(name, "create dispatch/receipts/") {
			crashed = name
			return errors.New("test: the process crashed here")
		}
		return nil
	}
	expectUnknown(t, h.invoke(t, home, "effect", effectRequest(t, 8, "ok")))
	if crashed == "" || h.calls["effect"] != 1 || h.calls["effected"] != 0 || outcomeOf(home, 8) {
		t.Fatalf("crashed at %q, handler runs %d, effects %d, outcome %v", crashed, h.calls["effect"], h.calls["effected"], outcomeOf(home, 8))
	}
	h.exe.Fault = nil
	// The next command to lock the store completes the intent: the record and
	// the receipt are there, and the effect has still not run.
	again := h.invoke(t, home, "effect", effectRequest(t, 8, "ok"))
	expect(t, again, envelope.Conflict, "already_exists")
	if len(receiptsOnDisk(t, home)) != 1 || h.calls["effect"] != 2 || h.calls["effected"] != 0 || outcomeOf(home, 8) {
		t.Errorf("receipts %v, handler runs %d, effects %d", receiptsOnDisk(t, home), h.calls["effect"], h.calls["effected"])
	}
}

// A read-only operation commits no intent.
func TestReadOnlyCommitsNoIntent(t *testing.T) {
	if _, err := (&Call{}).CommitIntent(Intent{Events: []store.Event{{}}}); err == nil {
		t.Error("a call with no store committed an intent")
	}
}

// openStore opens the dispatch store of the home for a read.
func openStore(t testing.TB, home string) *store.Store {
	t.Helper()
	st, err := store.Open(home, "dispatch", store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// An intent whose receipts the unknown answer could not carry within the
// response limit is refused before anything is written, so that the receipts
// committed are always the ones returned, and the effect does not run.
func TestIntentWhoseReceiptsCannotBeReturnedIsRefused(t *testing.T) {
	h := newHarness(t)
	home := newHome(t)
	h.exe.command("hive-fleet").Limits.MaxResponseBytes = 4096
	before := tree(t, home)
	resp := h.invoke(t, home, "effect", effectRequest(t, 8, "large receipt"))
	expect(t, resp, envelope.TooLarge, "record_too_large")
	if len(resp.Receipts) != 0 || h.calls["effected"] != 0 || outcomeOf(home, 8) {
		t.Errorf("%+v, effect %d", resp, h.calls["effected"])
	}
	after := tree(t, home)
	delete(after, "locks/dispatch.lock")
	delete(before, "locks/dispatch.lock")
	same(t, before, after)
}
