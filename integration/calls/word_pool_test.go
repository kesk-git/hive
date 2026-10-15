package calls

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// The real call from hive-word to hive-pool (spec/word.md, What test-word
// proves, and spec/commands.md, A command with no store), proved through the
// two built executables of dist/ and nothing else: hive-word starts hive-pool
// as the external program it is, found by name on the search path, in a home
// the test built. The items a question holds are added through hive-pool, and
// every record the tests read back is read through hive-pool read.

// wpChanged lists what a call wrote that is a record or a receipt of the
// Pool: the files under pool/records/ and pool/receipts/ that differ between
// two snapshots. A recorded outcome under pool/requests/ is the replay's own
// and is judged where a test judges replay.
func wpChanged(before, after map[string]string) []string {
	var out []string
	for name, sum := range after {
		if !strings.HasPrefix(name, "pool/records/") && !strings.HasPrefix(name, "pool/receipts/") {
			continue
		}
		if was, ok := before[name]; !ok || was != sum {
			out = append(out, "wrote "+name)
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok && (strings.HasPrefix(name, "pool/records/") || strings.HasPrefix(name, "pool/receipts/")) {
			out = append(out, "removed "+name)
		}
	}
	slices.Sort(out)
	return out
}

// wpSame reports whether two bodies are the same document.
func wpSame(t testing.TB, a, b map[string]any) bool {
	t.Helper()
	return bytes.Equal(encodeObject(t, a), encodeObject(t, b))
}

// wpReceiptIDs are the identities of the receipts of a response, in order.
func wpReceiptIDs(resp *envelope.Response) []string {
	out := []string{}
	for _, r := range resp.Receipts {
		out = append(out, r.ReceiptID)
	}
	return out
}

// TestWordPoolWriteThroughReadsBack proves that a mutation made through
// hive-word is written into the Pool by the real hive-pool and read back from
// it: a question held, answered and released, each step read from the records
// the Pool keeps, with the receipts the response returned in full and the
// words exactly as they were given.
//
// It catches a word that returns a body the Pool never wrote, one that
// changes a word of the question or the answer on the way, a receipt that is
// not the Pool's, and an answer that releases the hold on acceptance.
func TestWordPoolWriteThroughReadsBack(t *testing.T) {
	f := newWpFixture(t)
	item := f.addItem()

	// hold
	held := f.holdOn(item, wpWords)
	if got, want := wpKinds(held.resp), []string{"pool.hold.placed", "pool.question.held"}; !slices.Equal(got, want) {
		t.Errorf("hold receipts %v, want %v", got, want)
	}
	q := f.read("question", held.question)
	if q["state"] != "open" || q["held_task_id"] != item || q["hold_id"] != held.hold || q["card_id"] != held.card || q["decision_round_id"] != held.round {
		t.Errorf("the question as read back: state %v, item %v, hold %v, card %v, round %v", q["state"], q["held_task_id"], q["hold_id"], q["card_id"], q["decision_round_id"])
	}
	revisions, _ := q["revisions"].([]any)
	if len(revisions) != 1 {
		t.Fatalf("the question has %d revisions", len(revisions))
	}
	first, _ := revisions[0].(map[string]any)
	if first["text"] != wpWords || first["consequence"] != wpWords || first["recommendation"] != "keep" {
		t.Errorf("the words as read back: text %q, consequence %q, recommendation %v", first["text"], first["consequence"], first["recommendation"])
	}
	if first["asked_at"] != "2026-10-11T08:00:00.000Z" {
		t.Errorf("asked_at = %v, want the request's issued_at", first["asked_at"])
	}
	round := f.read("decision-round", held.round)
	if round["state"] != "open" || round["complete"] != true {
		t.Errorf("the round as read back: state %v, complete %v", round["state"], round["complete"])
	}
	if h := f.read("hold", held.hold); h["state"] != "active" || h["kind"] != "word" || h["task_id"] != item {
		t.Errorf("the hold as read back: state %v, kind %v, item %v", h["state"], h["kind"], h["task_id"])
	}
	for _, r := range held.resp.Receipts {
		if _, ok := f.recordedReceipts()[r.ReceiptID]; !ok {
			t.Errorf("receipt %s of the response is no receipt in the Pool", r.ReceiptID)
		}
	}

	// answer: accepted, the hold stays.
	text := "Keep it,\n  but only until the \"spring\" review — 'agreed'."
	answered := f.operatorAnswer(held, f.ids.take(), mergeMap(wpKeep, map[string]any{"text": text})).word(t, f.home)
	aresp := wpOK(t, "answer", answered)
	if aresp.Body["disposition"] != "accepted" {
		t.Fatalf("disposition = %v, want accepted", aresp.Body["disposition"])
	}
	if got, want := wpKinds(aresp), []string{"pool.answer.accepted"}; !slices.Equal(got, want) {
		t.Errorf("answer receipts %v, want %v", got, want)
	}
	q = f.read("question", held.question)
	answers, _ := q["answers"].([]any)
	if q["state"] != "answered" || len(answers) != 1 {
		t.Fatalf("the question as read back: state %v, %d answers", q["state"], len(answers))
	}
	a, _ := answers[0].(map[string]any)
	if a["text"] != text || a["option_id"] != "keep" || a["option_label"] != "Keep" || a["disposition"] != "accepted" {
		t.Errorf("the answer as read back: %v", a)
	}
	src, _ := a["source"].(map[string]any)
	if src["kind"] != "operator-request" || a["recorded_by"].(map[string]any)["kind"] != "operator" {
		t.Errorf("the answer's source %v and caller %v", src, a["recorded_by"])
	}
	if h := f.read("hold", held.hold); h["state"] != "active" {
		t.Errorf("the hold is %v after an answer: acceptance never releases it", h["state"])
	}
	if r := f.read("decision-round", held.round); r["state"] != "closed" {
		t.Errorf("the round is %v with no card open", r["state"])
	}

	// release: the owner's disposition, against the revision it read.
	rev := f.revision(held.question)
	rel := wpCall{operation: "release", n: f.ids.take(), caller: wpSupervisor, expected: wpExpecting(held.question, rev),
		body: map[string]any{"question_id": held.question, "note": "Proceed as answered."}}
	rresp := wpOK(t, "release", rel.word(t, f.home))
	if got, want := wpKinds(rresp), []string{"pool.hold.released", "pool.question.released"}; !slices.Equal(got, want) {
		t.Errorf("release receipts %v, want %v", got, want)
	}
	if q := f.read("question", held.question); q["state"] != "released" {
		t.Errorf("the question is %v after release", q["state"])
	}
	if h := f.read("hold", held.hold); h["state"] != "released" {
		t.Errorf("the hold is %v after release", h["state"])
	}
}

// recordedReceipts are the receipts the home keeps, by identity.
func (f *wpFixture) recordedReceipts() map[string]bool {
	f.t.Helper()
	out := map[string]bool{}
	for _, name := range wpUnder(f.t, f.home, "pool/receipts") {
		base := name[strings.LastIndex(name, "/")+1:]
		out[strings.TrimSuffix(base, ".json")] = true
	}
	return out
}

func mergeMap(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// TestWordPoolEachExitState proves each exit state the Pool can return
// through hive-word, as the table of spec/commands.md fixes it: the Pool's
// exit code and status, its own code, no body, the receipts only where the
// state leaves one, and no record or receipt of the Pool written by a state
// that changes nothing.
//
// Four states are left out, each for a reason of its own. too_large and usage
// cannot come from the Pool through word: word refuses an over-limit request
// first and builds the Pool call itself. failed and unknown come only from a
// storage fault or an interrupted transaction, and no fixture produces them on
// all three platforms without writing the store's internal transaction files;
// their crossing of the command line is already proved by the transport tests
// against the test-only command, as states_test.go states.
//
// It catches a word that turns a refusal into another state or code, that
// carries a body beside an error, that loses the hold of a held answer, and a
// state that writes while it says it wrote nothing.
func TestWordPoolEachExitState(t *testing.T) {
	type want struct {
		exit   int
		status string
		code   string
	}
	// do sets the home up and returns the call that ends in the state, and a
	// check of what the state leaves, or nil.
	cases := []struct {
		name string
		want want
		do   func(f *wpFixture) (wpCall, func(*testing.T, *wpFixture, *envelope.Response))
	}{
		{"ok", want{0, "ok", ""}, func(f *wpFixture) (wpCall, func(*testing.T, *wpFixture, *envelope.Response)) {
			return wpCall{operation: "hold", n: f.ids.take(), caller: wpSupervisor, body: wpHoldBody(f.addItem(), wpWords)}, nil
		}},
		{"invalid", want{3, "invalid", "pool.option_unknown"}, func(f *wpFixture) (wpCall, func(*testing.T, *wpFixture, *envelope.Response)) {
			body := wpHoldBody(f.addItem(), wpWords)
			body["recommendation"] = "elsewhere"
			return wpCall{operation: "hold", n: f.ids.take(), caller: wpSupervisor, body: body}, nil
		}},
		{"refused", want{4, "refused", "pool.held_item_archived"}, func(f *wpFixture) (wpCall, func(*testing.T, *wpFixture, *envelope.Response)) {
			item := f.addItem()
			f.archive(item)
			return wpCall{operation: "hold", n: f.ids.take(), caller: wpSupervisor, body: wpHoldBody(item, wpWords)}, nil
		}},
		{"refused, a disposition with no answer", want{4, "refused", "pool.question_unanswered"}, func(f *wpFixture) (wpCall, func(*testing.T, *wpFixture, *envelope.Response)) {
			h := f.hold()
			return wpCall{operation: "release", n: f.ids.take(), caller: wpSupervisor, expected: wpExpecting(h.question, f.revision(h.question)),
				body: map[string]any{"question_id": h.question, "note": "Proceed."}}, nil
		}},
		{"conflict, a second answer", want{5, "conflict", "pool.answer_exists"}, func(f *wpFixture) (wpCall, func(*testing.T, *wpFixture, *envelope.Response)) {
			h := f.hold()
			wpOK(f.t, "first answer", f.operatorAnswer(h, f.ids.take(), wpKeep).word(f.t, f.home))
			return f.operatorAnswer(h, f.ids.take(), map[string]any{"option_id": "switch", "option_label": "Switch"}), nil
		}},
		{"conflict, a revision the caller did not read", want{5, "conflict", "revision_conflict"}, func(f *wpFixture) (wpCall, func(*testing.T, *wpFixture, *envelope.Response)) {
			h := f.hold()
			wpOK(f.t, "answer", f.operatorAnswer(h, f.ids.take(), wpKeep).word(f.t, f.home))
			return wpCall{operation: "release", n: f.ids.take(), caller: wpSupervisor, expected: wpExpecting(h.question, 1),
				body: map[string]any{"question_id": h.question, "note": "Proceed."}}, nil
		}},
		{"not_found", want{6, "not_found", "not_found"}, func(f *wpFixture) (wpCall, func(*testing.T, *wpFixture, *envelope.Response)) {
			return wpCall{operation: "hold", n: f.ids.take(), caller: wpSupervisor, body: wpHoldBody(ident("task", 777), wpWords)}, nil
		}},
		{"held, an answer to a changed question", want{10, "held", "word.question_changed"}, func(f *wpFixture) (wpCall, func(*testing.T, *wpFixture, *envelope.Response)) {
			h := f.hold()
			stale := f.operatorAnswer(h, f.ids.take(), wpKeep)
			body := map[string]any{"question_id": h.question, "text": "Which vendor, now?", "consequence": wpWords, "options": wpOptions(), "recommendation": "switch"}
			wpOK(f.t, "revise", wpCall{operation: "revise", n: f.ids.take(), caller: wpSupervisor, expected: wpExpecting(h.question, f.revision(h.question)), body: body}.word(f.t, f.home))
			return stale, func(t *testing.T, f *wpFixture, resp *envelope.Response) {
				if resp.Error.Hold == nil || resp.Error.Hold.ID != h.hold {
					t.Errorf("error.hold = %+v, want the question's hold %s", resp.Error.Hold, h.hold)
				}
				if got, want := wpKinds(resp), []string{"pool.answer.unreconciled"}; !slices.Equal(got, want) {
					t.Errorf("receipts %v, want %v", got, want)
				}
				q := f.read("question", h.question)
				answers, _ := q["answers"].([]any)
				if q["state"] != "open" || len(answers) != 1 || answers[0].(map[string]any)["disposition"] != "unreconciled" {
					t.Errorf("the question as read back: state %v, answers %v; want open, one unreconciled", q["state"], answers)
				}
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newWpFixture(t)
			c, check := tc.do(f)
			before := snapshot(t, f.home)
			r := c.word(t, f.home)
			resp := r.must(t)
			if r.result.Code != int64(tc.want.exit) || string(resp.Status) != tc.want.status {
				t.Fatalf("exit code %d, status %q, error %+v; want %d, %s", r.result.Code, resp.Status, resp.Error, tc.want.exit, tc.want.status)
			}
			if resp.RequestID != wpRequest(t, c.with("hive-word")).id {
				t.Errorf("request_id = %q, want the request's", resp.RequestID)
			}
			if resp.Command != "hive-word" || resp.Responder.Package != "word" {
				t.Errorf("answered by %s of package %s, want hive-word of word", resp.Command, resp.Responder.Package)
			}
			if tc.want.status == "ok" {
				if resp.Error != nil || resp.Body == nil || len(resp.Receipts) == 0 {
					t.Errorf("ok carries error %+v, body %v, %d receipts", resp.Error, resp.Body != nil, len(resp.Receipts))
				}
			} else {
				if resp.Error == nil || resp.Error.Code != tc.want.code || resp.Error.Message == "" {
					t.Fatalf("error = %+v, want code %s with a message", resp.Error, tc.want.code)
				}
				if resp.Body != nil {
					t.Errorf("%s carries a body: %v", tc.want.status, resp.Body)
				}
				if tc.want.status != "held" && len(resp.Receipts) != 0 {
					t.Errorf("%s carries receipts %v", tc.want.status, wpKinds(resp))
				}
				if tc.want.status != "held" {
					if changed := wpChanged(before, snapshot(t, f.home)); len(changed) != 0 {
						t.Errorf("%s changed the Pool: %v", tc.want.status, changed)
					}
				}
			}
			if check != nil {
				check(t, f, resp)
			}
		})
	}
}

// with returns the call as a call of the command.
func (c wpCall) with(command string) wpCall {
	c.command = command
	return c
}

// TestWordPoolUnavailableWhenARecordedOutcomeIsMalformed proves the state of
// a recorded outcome the Pool cannot read, which a retry through word meets
// before any identity is minted: unavailable, record_malformed, no body, and
// nothing sent on to be applied. The fixture damages the Pool's own record of
// the outcome, as a partial write by another program would.
//
// It catches a word that replays, or applies again, over an outcome the Pool
// says it cannot read.
func TestWordPoolUnavailableWhenARecordedOutcomeIsMalformed(t *testing.T) {
	f := newWpFixture(t)
	item := f.addItem()
	c := wpCall{operation: "hold", n: f.ids.take(), caller: wpSupervisor, body: wpHoldBody(item, wpWords)}
	wpOK(t, "hold", c.word(t, f.home))

	path := "pool/requests/" + wpRequest(t, c.with("hive-word")).id + ".json"
	if _, ok := snapshot(t, f.home)[path]; !ok {
		t.Fatalf("the Pool recorded no outcome at %s", path)
	}
	wpWrite(t, f.home, path, []byte("{\"schema\":\"hive.request-outcome\",\n"))
	before := snapshot(t, f.home)

	r := c.word(t, f.home)
	wantFailure(t, r, wpRequest(t, c.with("hive-word")).id, 8, "unavailable", "record_malformed")
	// The one diagnostic event of a failure with detail is the exception to
	// "nothing is written" (spec/commands.md, Exit and error states).
	after := snapshot(t, f.home)
	for name := range after {
		if strings.HasPrefix(name, "journal/") {
			delete(after, name)
		}
	}
	for name := range before {
		if strings.HasPrefix(name, "journal/") {
			delete(before, name)
		}
	}
	sameSnapshot(t, "a retry over a malformed outcome", before, after)
}

// wpScenario is a mutation of one operation, made after what it needs is in
// place: the first call, and the call of the same request_id with other words.
type wpScenario struct {
	name string
	// build makes the home ready and returns the call and the same call with
	// other words.
	build func(f *wpFixture) (first, other wpCall)
	// kinds are the kinds of the receipts the first call returns.
	kinds []string
}

func wpScenarios() []wpScenario {
	other := func(c wpCall, key string) wpCall {
		body := mergeMap(c.body, map[string]any{key: fmt.Sprint(c.body[key]) + " — and more"})
		c.body = body
		return c
	}
	return []wpScenario{
		{"hold", func(f *wpFixture) (wpCall, wpCall) {
			c := wpCall{operation: "hold", n: f.ids.take(), caller: wpSupervisor, body: wpHoldBody(f.addItem(), wpWords)}
			return c, other(c, "text")
		}, []string{"pool.hold.placed", "pool.question.held"}},
		{"revise", func(f *wpFixture) (wpCall, wpCall) {
			h := f.hold()
			c := wpCall{operation: "revise", n: f.ids.take(), caller: wpSupervisor, expected: wpExpecting(h.question, f.revision(h.question)),
				body: map[string]any{"question_id": h.question, "text": "Which vendor, now?", "consequence": wpWords, "options": wpOptions(), "recommendation": "keep"}}
			return c, other(c, "text")
		}, []string{"pool.question.revised"}},
		{"answer", func(f *wpFixture) (wpCall, wpCall) {
			h := f.hold()
			c := f.operatorAnswer(h, f.ids.take(), mergeMap(wpKeep, map[string]any{"text": wpWords}))
			return c, other(c, "text")
		}, []string{"pool.answer.accepted"}},
		{"release", func(f *wpFixture) (wpCall, wpCall) {
			h := f.hold()
			wpOK(f.t, "answer", f.operatorAnswer(h, f.ids.take(), wpKeep).word(f.t, f.home))
			c := wpCall{operation: "release", n: f.ids.take(), caller: wpSupervisor, expected: wpExpecting(h.question, f.revision(h.question)),
				body: map[string]any{"question_id": h.question, "note": "Proceed as answered."}}
			return c, other(c, "note")
		}, []string{"pool.hold.released", "pool.question.released"}},
		{"complete", func(f *wpFixture) (wpCall, wpCall) {
			// A question with a decision item of its own gates no work, so it is
			// completed and not released.
			resp := wpOK(f.t, "hold", wpCall{operation: "hold", n: f.ids.take(), caller: wpSupervisor, body: map[string]any{
				"new_item": map[string]any{"title": "A decision"}, "origin": map[string]any{"kind": "supervisor"},
				"text": wpWords, "consequence": wpWords, "options": wpOptions(), "recommendation": "keep",
				"round": map[string]any{"complete": true},
			}}.word(f.t, f.home))
			item, _ := resp.Body["item"].(map[string]any)
			h := wpHeldFrom(f.t, item["task_id"].(string), resp)
			wpOK(f.t, "answer", f.operatorAnswer(h, f.ids.take(), wpKeep).word(f.t, f.home))
			c := wpCall{operation: "complete", n: f.ids.take(), caller: wpSupervisor, expected: wpExpecting(h.question, f.revision(h.question)),
				body: map[string]any{"question_id": h.question, "note": "Nothing is left to do."}}
			return c, other(c, "note")
		}, []string{"pool.hold.released", "pool.question.completed"}},
		{"close", func(f *wpFixture) (wpCall, wpCall) {
			h := f.hold()
			f.archive(h.item)
			c := wpCall{operation: "close", n: f.ids.take(), caller: wpSupervisor, expected: wpExpecting(h.question, f.revision(h.question)),
				body: map[string]any{"question_id": h.question, "reason": "The work was archived.", "evidence": []any{
					map[string]any{"kind": "item-archived", "ref": map[string]any{"schema": "pool.item", "id": h.item}},
				}}}
			return c, other(c, "reason")
		}, []string{"pool.hold.released", "pool.question.mooted"}},
	}
}

// TestWordPoolReplayThroughBoth proves replay through hive-word and
// hive-pool for each of the six mutating operations: the same request
// returns the recorded outcome with replayed true, the same body and the same
// receipts, and writes nothing; and a request_id used before with other words
// is a conflict, replay_mismatch, with nothing written.
//
// It catches a replay that runs the operation again or mints new identities,
// one that answers another body or other receipts than the first, a reuse of
// a request_id with other words that is not refused, and a refusal that
// changes the home.
func TestWordPoolReplayThroughBoth(t *testing.T) {
	for _, sc := range wpScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			f := newWpFixture(t)
			first, other := sc.build(f)

			r1 := first.word(t, f.home)
			resp1 := wpOK(t, sc.name, r1)
			if resp1.Replayed {
				t.Error("a first request is not a replay")
			}
			if got := wpKinds(resp1); !slices.Equal(got, sc.kinds) {
				t.Fatalf("receipts %v, want %v", got, sc.kinds)
			}
			written := snapshot(t, f.home)

			// The same request again, once by standard input with a response file.
			again := first.with("hive-word")
			r2 := call{program: "hive-word", operation: again.operation, home: f.home, req: wpRequest(t, again), stdin: true, responseFile: true}.run(t)
			resp2 := wpOK(t, "replay", r2)
			if !resp2.Replayed {
				t.Error("the same request did not return replayed true")
			}
			if !wpSame(t, resp1.Body, resp2.Body) {
				t.Errorf("the replay's body is not the recorded one")
			}
			if !slices.Equal(wpReceiptIDs(resp1), wpReceiptIDs(resp2)) {
				t.Errorf("replay receipts %v, want the recorded %v", wpReceiptIDs(resp2), wpReceiptIDs(resp1))
			}
			sameSnapshot(t, "a replay", written, snapshot(t, f.home))

			// The same request_id with other words.
			r3 := other.word(t, f.home)
			wantFailure(t, r3, wpRequest(t, other.with("hive-word")).id, 5, "conflict", "replay_mismatch")
			sameSnapshot(t, "a reused request_id", written, snapshot(t, f.home))
		})
	}
}

// TestWordPoolRetryAfterTheRoundChanged proves that an answer sent again
// after the first one changed the round, and after the owner released the
// question, returns the outcome recorded for it and is not checked against the
// records as they now stand: a fresh answer to the released question is
// refused, and the retry of the first is not.
//
// It catches a replay that checks the retry against the present records, as a
// retry of an answer would be refused for the change its own first
// application made.
func TestWordPoolRetryAfterTheRoundChanged(t *testing.T) {
	f := newWpFixture(t)
	h := f.hold()
	answer := f.operatorAnswer(h, f.ids.take(), mergeMap(wpKeep, map[string]any{"text": wpWords}))
	first := wpOK(t, "answer", answer.word(t, f.home))
	if first.Body["disposition"] != "accepted" {
		t.Fatalf("disposition = %v, want accepted", first.Body["disposition"])
	}
	if r := f.read("decision-round", h.round); r["state"] != "closed" {
		t.Fatalf("the round is %v: the first answer should have closed it", r["state"])
	}

	// The retry after the round changed.
	r := answer.word(t, f.home)
	retry := wpOK(t, "retry of the answer", r)
	if !retry.Replayed || !wpSame(t, first.Body, retry.Body) || !slices.Equal(wpReceiptIDs(first), wpReceiptIDs(retry)) {
		t.Errorf("the retry: replayed %v, same body %v, receipts %v; want the recorded outcome %v", retry.Replayed, wpSame(t, first.Body, retry.Body), wpReceiptIDs(retry), wpReceiptIDs(first))
	}

	// The question is released, which changes it again.
	wpOK(t, "release", wpCall{operation: "release", n: f.ids.take(), caller: wpSupervisor, expected: wpExpecting(h.question, f.revision(h.question)),
		body: map[string]any{"question_id": h.question, "note": "Proceed as answered."}}.word(t, f.home))
	settled := snapshot(t, f.home)

	// A new answer is now refused, so the retry's outcome is the recorded one
	// and not a fresh check.
	fresh := f.operatorAnswer(h, f.ids.take(), mergeMap(wpKeep, map[string]any{"text": "Another"}))
	wantFailure(t, fresh.word(t, f.home), wpRequest(t, fresh.with("hive-word")).id, 4, "refused", "pool.question_closed")
	again := answer.word(t, f.home)
	retry = wpOK(t, "retry after the release", again)
	if !retry.Replayed || !wpSame(t, first.Body, retry.Body) || !slices.Equal(wpReceiptIDs(first), wpReceiptIDs(retry)) {
		t.Errorf("the retry after release: replayed %v, receipts %v; want the recorded outcome %v", retry.Replayed, wpReceiptIDs(retry), wpReceiptIDs(first))
	}
	if changed := wpChanged(settled, snapshot(t, f.home)); len(changed) != 0 {
		t.Errorf("a retry changed the Pool: %v", changed)
	}
}

// wpHandoff is the request hive-word sends hive-pool for the word request in
// hand, as a second identical request that had read no recorded outcome would
// send it: the same request_id, correlation and scope, the caller the package
// word with its command hive-word, and as the causation the original request
// and its digest. The test builds it from the request document, and the
// operation's body is the one the caller gives, with identities of its own
// minting where the operation has any.
func wpHandoff(t testing.TB, word request, c wpCall) request {
	t.Helper()
	digest, err := envelope.Digest(word.data)
	if err != nil {
		t.Fatal(err)
	}
	c.command = "hive-pool"
	c.caller = envelope.Caller{Kind: envelope.CallerPackage, Package: "word", Command: "hive-word"}
	c.causation = &envelope.RecordRef{Schema: envelope.RequestSchema, ID: word.id, Digest: digest}
	return wpRequest(t, c)
}

// TestWordPoolTwoIdenticalRequestsOneOutcome proves that two identical
// requests share one outcome and one set of records. Two processes cannot be
// interleaved by control from outside, so the test fixes the two orders in
// which the second request can meet the first (spec/test-targets.md, rule 5)
// and runs each against the real executables:
//
//   - The second read the Pool before the first wrote. The first request runs
//     to its outcome; then the test sends hive-pool, as the package word, the
//     handoff that the second request would send having read no recorded
//     outcome: the same original request and its digest, with identities of
//     its own minting. The Pool answers the first outcome with replayed true,
//     and writes no second question, round, hold, answer or receipt.
//   - The second read after the first wrote. The identical request sent to
//     hive-word again returns the recorded outcome with replayed true.
//
// It runs for a hold, which mints identities, and for an answer.
//
// It catches a Pool that applies a handoff it has an outcome for, one that
// answers replay_mismatch for the identities a retry minted anew, and two
// outcomes for one request.
func TestWordPoolTwoIdenticalRequestsOneOutcome(t *testing.T) {
	// counts are the number of records of each kind and of receipts.
	counts := func(f *wpFixture) map[string]int {
		out := map[string]int{}
		for _, dir := range []string{"question", "decision-round", "hold", "item"} {
			out[dir] = len(wpUnder(f.t, f.home, "pool/records/"+dir))
		}
		out["receipts"] = len(wpUnder(f.t, f.home, "pool/receipts"))
		return out
	}
	scenarios := []struct {
		name string
		// start makes the home ready and returns the call, the operation of
		// hive-pool it hands off to, and the body of that handoff, with the
		// identities of a second request's own minting.
		start func(f *wpFixture) (wpCall, string, map[string]any)
		// same judges the Pool's body against the first outcome's.
		same func(t *testing.T, first, pool map[string]any)
	}{
		{"hold", func(f *wpFixture) (wpCall, string, map[string]any) {
			c := wpCall{operation: "hold", n: f.ids.take(), caller: wpSupervisor, body: wpHoldBody(f.addItem(), wpWords)}
			body := map[string]any{
				"question": map[string]any{
					"question_id": ident(ids.Question, 500), "asked_at": "2026-10-11T08:00:00.000Z",
					"held_task_id": c.body["held_task_id"], "origin": c.body["origin"],
					"text": c.body["text"], "consequence": c.body["consequence"],
					"options": c.body["options"], "recommendation": c.body["recommendation"],
				},
				"round": map[string]any{
					"decision_round_id": ident(ids.DecisionRound, 500), "new": true,
					"origin": c.body["origin"], "complete": true,
				},
				"card_id":      ident(ids.Card, 500),
				"requested_by": map[string]any{"kind": "supervisor", "session_id": wpSupervisor.SessionID, "session_generation": wpSupervisor.SessionGeneration},
			}
			return c, "hold-question", body
		}, func(t *testing.T, first, pool map[string]any) {
			for _, key := range []string{"question", "round", "hold", "item"} {
				if !wpSame(t, first[key].(map[string]any), pool[key].(map[string]any)) {
					t.Errorf("the Pool's %s is not the first outcome's", key)
				}
			}
			if q := pool["question"].(map[string]any)["question_id"]; q == ident(ids.Question, 500) {
				t.Error("the Pool applied the identities of the second request")
			}
		}},
		{"answer", func(f *wpFixture) (wpCall, string, map[string]any) {
			c := f.operatorAnswer(f.hold(), f.ids.take(), mergeMap(wpKeep, map[string]any{"text": wpWords}))
			body := mergeMap(c.body, map[string]any{"requested_by": map[string]any{"kind": "operator"}})
			return c, "answer-question", body
		}, func(t *testing.T, first, pool map[string]any) {
			for _, key := range []string{"disposition", "answer_sequence", "duplicate", "question", "round", "hold"} {
				if !wpSame(t, map[string]any{"v": first[key]}, map[string]any{"v": pool[key]}) {
					t.Errorf("the Pool's %s is not the first outcome's", key)
				}
			}
		}},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			f := newWpFixture(t)
			c, poolOperation, body := sc.start(f)

			// The first request runs to its outcome.
			word := c.with("hive-word")
			first := wpOK(t, "the first request", c.word(t, f.home))
			if first.Replayed {
				t.Fatal("the first request is not a replay")
			}
			written, held := snapshot(t, f.home), counts(f)

			// The second request read the Pool before the first wrote.
			handoff := wpHandoff(t, wpRequest(t, word), wpCall{operation: poolOperation, n: c.n, body: body, expected: c.expected})
			r := call{program: "hive-pool", operation: poolOperation, home: f.home, req: handoff}.run(t)
			pool := wpOK(t, "the second request's handoff", r)
			if !pool.Replayed {
				t.Error("the Pool applied the second request's handoff: replayed is false")
			}
			if !slices.Equal(wpReceiptIDs(pool), wpReceiptIDs(first)) {
				t.Errorf("the handoff's receipts %v, want the first outcome's %v", wpReceiptIDs(pool), wpReceiptIDs(first))
			}
			sc.same(t, first.Body, pool.Body)
			sameSnapshot(t, "the second request's handoff", written, snapshot(t, f.home))
			if got := counts(f); !maps.Equal(got, held) {
				t.Errorf("records and receipts after the handoff %v, want %v: a second set was written", got, held)
			}

			// The second request read after the first wrote.
			again := wpOK(t, "the second request", c.word(t, f.home))
			if !again.Replayed || !wpSame(t, first.Body, again.Body) || !slices.Equal(wpReceiptIDs(again), wpReceiptIDs(first)) {
				t.Errorf("the identical request again: replayed %v, same body %v, receipts %v; want the recorded outcome %v",
					again.Replayed, wpSame(t, first.Body, again.Body), wpReceiptIDs(again), wpReceiptIDs(first))
			}
			sameSnapshot(t, "the identical request again", written, snapshot(t, f.home))
		})
	}
}

// TestWordPoolRecordCrossesWhole proves that a record of 2,097,152 bytes
// crosses the call whole: as the text of a question it goes from the request
// through hive-word to hive-pool, is written, is returned by word in its
// response, read back from the Pool and returned again by the replay, byte
// for byte; and as the free text of an answer it does the same.
//
// It catches a transport between the two executables that cuts a large
// request or response at a pipe or buffer size, a text that is changed on the
// way, and a replay that returns less than was written.
func TestWordPoolRecordCrossesWhole(t *testing.T) {
	f := newWpFixture(t)
	item := f.addItem()
	record := pattern(recordBytes)
	if len(record) != 2097152 {
		t.Fatalf("the record is %d bytes", len(record))
	}

	questionText := func(m map[string]any) any {
		revisions, _ := m["revisions"].([]any)
		if len(revisions) == 0 {
			return nil
		}
		first, _ := revisions[0].(map[string]any)
		return first["text"]
	}

	hold := wpCall{operation: "hold", n: f.ids.take(), caller: wpSupervisor, body: wpHoldBody(item, record)}
	resp := wpOK(t, "hold", hold.word(t, f.home))
	h := wpHeldFrom(t, item, resp)
	if !sameRecord(questionText(resp.Body["question"].(map[string]any)), recordBytes) {
		t.Error("word returned the question's text changed or cut")
	}
	if !sameRecord(f.read("question", h.question)["revisions"].([]any)[0].(map[string]any)["text"], recordBytes) {
		t.Error("the question's text as the Pool holds it is not the record")
	}
	replay := wpOK(t, "replay of the hold", hold.word(t, f.home))
	if !replay.Replayed || !sameRecord(questionText(replay.Body["question"].(map[string]any)), recordBytes) {
		t.Errorf("the replay of the hold (replayed %v) did not return the record whole", replay.Replayed)
	}

	answer := f.operatorAnswer(h, f.ids.take(), mergeMap(wpKeep, map[string]any{"text": record}))
	aresp := wpOK(t, "answer", answer.word(t, f.home))
	answers, _ := aresp.Body["question"].(map[string]any)["answers"].([]any)
	if len(answers) != 1 || !sameRecord(answers[0].(map[string]any)["text"], recordBytes) {
		t.Error("word returned the answer's text changed or cut")
	}
	stored, _ := f.read("question", h.question)["answers"].([]any)
	if len(stored) != 1 || !sameRecord(stored[0].(map[string]any)["text"], recordBytes) {
		t.Error("the answer's text as the Pool holds it is not the record")
	}
	if again := wpOK(t, "replay of the answer", answer.word(t, f.home)); !again.Replayed || !wpSame(t, aresp.Body, again.Body) {
		t.Errorf("the replay of the answer (replayed %v) did not return the record whole", again.Replayed)
	}
}
