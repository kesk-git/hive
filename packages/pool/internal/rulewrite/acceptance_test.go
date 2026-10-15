package rulewrite_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// The acceptance test of the rule writers (spec/pool.md, Rules, Subjects, What a
// write refuses and Generated knowledge): hive-rule record, amend and expire,
// and hive-memory subject.

// same says whether two decoded values are one value.
func same(t testing.TB, a, b any) bool {
	t.Helper()
	x, err := canon.Encode(a)
	if err != nil {
		t.Fatal(err)
	}
	y, err := canon.Encode(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(x) == string(y)
}

func withExpiry(body map[string]any, expires string) map[string]any {
	body["expires_at"] = expires
	return body
}

func TestARecordedRulesWordsAndDatesAreNeverChanged(t *testing.T) {
	h, filing := seed(t)
	first := h.recorded(withExpiry(opRule("dispatch.cap", "always", filing, "Dispatch at most four Swarms."), d15), request{})
	if first["status"] != "in-force" || first["expires_at"] != d15 || first["record_revision"] != int64(1) {
		t.Fatalf("recorded %v", first)
	}

	extended := ok(t, h.amend(first, map[string]any{"kind": "extend", "words": "Extended by five days.", "new_expires_at": d20}))["rule"].(map[string]any)
	noted := ok(t, h.amend(extended, map[string]any{"kind": "note", "words": "Still wanted."}))["rule"].(map[string]any)
	h.clk.Set(time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC))
	resp := h.expire(noted, "2026-10-20T01:00:00.000Z")
	expired := ok(t, resp)["rule"].(map[string]any)
	if !slices.Contains(kinds(resp), "pool.rule.expired") || expired["status"] != "expired" {
		t.Fatalf("expired %v, receipts %v", expired, kinds(resp))
	}

	stored := h.stored("rule", first["rule_id"].(string))
	for _, key := range []string{"rule_id", "subject", "words", "origin", "rule_line", "applies", "effective_at", "expires_at", "authority", "created_at", "kind", "tier", "filed_under"} {
		if !same(t, first[key], stored[key]) {
			t.Errorf("%s was %v and is %v", key, first[key], stored[key])
		}
	}
	amendments := stored["amendments"].([]any)
	if len(amendments) != 2 || amendments[0].(map[string]any)["kind"] != "extend" || amendments[1].(map[string]any)["kind"] != "note" {
		t.Errorf("amendments %v", amendments)
	}
	if stored["record_revision"] != int64(4) || stored["expiry"].(map[string]any)["noticed_at"] != "2026-10-20T01:00:00.000Z" {
		t.Errorf("stored %v", stored)
	}
}

func TestARuleLineOverThreeHundredCodePointsIsRefusedAndTheWordsAreNoPartOfTheCap(t *testing.T) {
	h, filing := seed(t)
	h.unchanged(func() {
		resp := refused(t, h.record(opRule("dispatch.cap", "always", filing, strings.Repeat("é", 301)), request{}), envelope.Refused, "pool.rule_line_too_long")
		d := detail(t, resp)
		if d["characters"] != int64(301) || d["max_characters"] != int64(300) {
			t.Errorf("detail %v", d)
		}
		moves(t, resp)
	})
	// 300 code points are 600 bytes and fit; the words are kept whole, however long.
	long := opRule("dispatch.cap", "always", filing, strings.Repeat("é", 300))
	long["words"] = strings.Repeat("A ruling of many words. ", 200)
	if rec := h.recorded(long, request{}); rec["words"] != long["words"] {
		t.Error("the words were not kept whole")
	}
}

func TestASecondRuleOnOneSubjectAtOneScopeIsRefusedUnlessItNamesTheStandingRule(t *testing.T) {
	h, filing := seed(t)
	standing := h.recorded(opRule("dispatch.cap", "always", filing, "Dispatch at most four Swarms."), request{})
	h.unchanged(func() {
		refused(t, h.record(opRule("dispatch.cap", "always", filing, "Dispatch at most six Swarms."), request{}), envelope.Refused, "pool.rule_conflict")
	})
	body := opRule("dispatch.cap", "always", filing, "Dispatch at most six Swarms.")
	body["changes"] = map[string]any{"rule_id": standing["rule_id"], "record_revision": standing["record_revision"], "relation": "supersedes"}
	if rec := h.recorded(body, request{}); rec["changes"] == nil {
		t.Errorf("recorded %v", rec)
	}
	if after := h.stored("rule", standing["rule_id"].(string)); !same(t, after, standing) {
		t.Error("the standing rule was edited")
	}
}

func TestAnExpiryBeforeItsTimeIsRefusedAndASecondCallReturnsTheFirstReceipt(t *testing.T) {
	h, filing := seed(t)
	rec := h.recorded(withExpiry(opRule("dispatch.cap", "always", filing, "Dispatch at most four Swarms."), d15), request{})
	h.unchanged(func() {
		refused(t, h.expire(rec, "2026-10-14T09:00:00.000Z"), envelope.Refused, "pool.rule_not_expired")
	})
	h.clk.Set(time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC))
	first := h.expire(rec, "2026-10-15T01:00:00.000Z")
	ok(t, first)
	var firstReceipt envelope.Receipt
	for _, r := range first.Receipts {
		if r.Kind == "pool.rule.expired" {
			firstReceipt = r
		}
	}
	if firstReceipt.ReceiptID == "" {
		t.Fatalf("receipts %v", kinds(first))
	}
	h.clk.Advance(time.Hour)
	h.unchanged(func() {
		second := h.expire(rec, "2026-10-15T02:00:00.000Z")
		ok(t, second)
		if len(second.Receipts) != 1 || second.Receipts[0].ReceiptID != firstReceipt.ReceiptID {
			t.Errorf("the second call returned %v, not the first receipt %s", second.Receipts, firstReceipt.ReceiptID)
		}
	})
}

// liftReceipt is a receipt of the kind about the subject, as a caller carries it.
func liftReceipt(t testing.TB, kind, subjectID string) map[string]any {
	t.Helper()
	r := envelope.Receipt{
		ReceiptID: mint(t, ids.Receipt), Kind: kind, KindVersion: "1.0", WrittenAt: "2026-10-14T08:00:00.000Z",
		Writer:    envelope.Writer{Package: "synapse", Command: "hive-synapse", Operation: "close", ImplementationVersion: "test"},
		RequestID: mint(t, ids.Request), CorrelationID: mint(t, ids.Correlation),
		Scope:     envelope.Scope{Level: "hive", HomeID: homeID},
		Authority: envelope.Authority{Kind: "none"},
		Subject:   &envelope.RecordRef{Schema: "synapse.round", ID: subjectID, HomeID: homeID, RecordRevision: 3},
		Evidence:  []envelope.Evidence{}, Body: map[string]any{},
	}
	data, err := r.Encode()
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)
}

func TestALiftIsAcceptedOnAReceiptOfTheKindAndSubjectItsConditionNamesAndRefusedOtherwise(t *testing.T) {
	h, filing := seed(t)
	body := opRule("dispatch.cap", "always", filing, "Dispatch at most one Swarm until the round closes.")
	body["lift"] = map[string]any{"receipt_kind": "synapse.round.closed", "subject": map[string]any{"schema": "synapse.round", "id": "round_a"}}
	rec := h.recorded(body, request{})

	for name, receipt := range map[string]map[string]any{
		"another kind":    liftReceipt(t, "synapse.round.opened", "round_a"),
		"another subject": liftReceipt(t, "synapse.round.closed", "round_b"),
	} {
		h.unchanged(func() {
			resp := h.amend(rec, map[string]any{"kind": "lift", "words": "The round closed.", "receipt": receipt})
			if resp.Status != envelope.Refused {
				t.Errorf("%s: status %s, error %+v", name, resp.Status, resp.Error)
			}
		})
	}

	receipt := liftReceipt(t, "synapse.round.closed", "round_a")
	resp := h.amend(rec, map[string]any{"kind": "lift", "words": "The round closed.", "receipt": receipt})
	lifted := ok(t, resp)["rule"].(map[string]any)
	if !slices.Contains(kinds(resp), "pool.rule.lifted") || lifted["status"] != "lifted" {
		t.Fatalf("lifted %v, receipts %v", lifted["status"], kinds(resp))
	}
	a := lifted["amendments"].([]any)[0].(map[string]any)
	if !same(t, a["receipt"], receipt) {
		t.Errorf("the lift records the receipt %v, not the one carried whole", a["receipt"])
	}
}

func TestACreatedSubjectAndItsFactsCommitWithTheRuleAndTheProjectionsOrNoneDo(t *testing.T) {
	h, _ := seed(t)
	subjectID := "subj_" + strings.TrimPrefix(mint(t, ids.Task), "task_")
	provenance := fixture(t, "pool.subject", "class-with-fact")["facts"].([]any)[0].(map[string]any)["provenance"]
	body := opRule("dispatch.cap", "subject", subjectID, "Dispatch at most four Swarms.")
	body["create_subject"] = map[string]any{"subject_id": subjectID, "key": "dispatch.limits", "loads_at": []any{"dispatch", "gate"}}
	body["facts"] = []any{map[string]any{"fact_id": "cap-mechanics", "text": "The cap counts Swarms in flight, not queued.", "provenance": provenance}}

	// Facts the subject's projection cannot hold in its budget of 9,216 bytes:
	// nothing is written.
	big := map[string]any{}
	for k, v := range body {
		big[k] = v
	}
	big["facts"] = []any{map[string]any{"fact_id": "cap-mechanics", "text": strings.Repeat("The cap counts Swarms in flight. ", 300), "provenance": provenance}}
	h.unchanged(func() {
		refused(t, h.record(big, request{}), envelope.Refused, "pool.projection_over_budget")
	})
	if h.stored("subject", subjectID) != nil {
		t.Error("the subject was created though the write was refused")
	}

	rec := h.recorded(body, request{})
	sub := h.stored("subject", subjectID)
	if sub == nil || sub["key"] != "dispatch.limits" || len(sub["facts"].([]any)) != 1 || rec["filed_under"] != subjectID {
		t.Fatalf("subject %v, rule %v", sub, rec)
	}
	if h.stored("rule", rec["rule_id"].(string)) == nil || !h.fresh(hiveScope, "subject", "hive", subjectID) {
		t.Error("the projection of the new subject was not rendered with the rule and the subject")
	}
}

func TestEveryAffectedProjectionIsRenderedInTheSameTransaction(t *testing.T) {
	h, filing := seed(t)
	rec := h.recorded(withExpiry(opRule("dispatch.cap", "always", filing, "Dispatch at most four Swarms."), d20), request{})
	for _, audience := range []string{"hive", "fleet"} {
		if !h.fresh(hiveScope, "always", audience, "") {
			t.Errorf("the always projection of %s was not rendered by the record", audience)
		}
	}
	extended := ok(t, h.amend(rec, map[string]any{"kind": "extend", "words": "Five days more.", "new_expires_at": d25}))["rule"].(map[string]any)
	if !h.fresh(hiveScope, "always", "hive", "") {
		t.Error("the amend left the projection waiting for a render")
	}
	h.clk.Set(time.Date(2026, 10, 25, 1, 0, 0, 0, time.UTC))
	ok(t, h.expire(extended, "2026-10-25T01:00:00.000Z"))
	if !h.fresh(hiveScope, "always", "hive", "") {
		t.Error("the expire left the projection waiting for a render")
	}
}

func TestAWriteThatPutsAProjectionOverItsBudgetAtTheClockOrLaterIsRefusedAndWritesNothing(t *testing.T) {
	line := strings.Repeat("a", 60)
	t.Run("at the clock", func(t *testing.T) {
		h, filing := seed(t)
		h.tighten(hiveScope, "always", "hive", "", 120)
		h.unchanged(func() {
			resp := refused(t, h.record(opRule("dispatch.cap", "always", filing, strings.Repeat("a", 100)), request{}), envelope.Refused, "pool.projection_over_budget")
			d := detail(t, resp)
			if d["projection"] == nil || d["bytes"] == nil || d["max_bytes"] != int64(120) {
				t.Errorf("detail %v", d)
			}
			moves(t, resp)
		})
	})
	t.Run("at a later instant", func(t *testing.T) {
		h, filing := seed(t)
		h.tighten(hiveScope, "always", "hive", "", 150)
		h.recorded(opRule("dispatch.cap", "always", filing, line), request{})
		later := opRule("dispatch.model", "always", filing, line)
		later["effective_at"] = d25
		h.unchanged(func() {
			resp := refused(t, h.record(later, request{}), envelope.Refused, "pool.projection_over_budget")
			if d := detail(t, resp); d["at"] != d25 {
				t.Errorf("detail %v", d)
			}
			moves(t, resp)
		})
	})
	t.Run("by an amend", func(t *testing.T) {
		h, filing := seed(t)
		h.tighten(hiveScope, "always", "hive", "", 150)
		temp := h.recorded(withExpiry(opRule("dispatch.cap", "always", filing, line), d15), request{})
		later := opRule("dispatch.model", "always", filing, line)
		later["effective_at"] = d20
		h.recorded(later, request{})
		h.unchanged(func() {
			refused(t, h.amend(temp, map[string]any{"kind": "extend", "words": "Longer.", "new_expires_at": d25}), envelope.Refused, "pool.projection_over_budget")
		})
	})
}

func TestAmendChecksAnExpectedRevisionOnlyWhereTheRequestSuppliesOne(t *testing.T) {
	h, filing := seed(t)
	rec := h.recorded(opRule("dispatch.cap", "always", filing, "Dispatch at most four Swarms."), request{})
	body := func() map[string]any {
		return map[string]any{"rule_id": rec["rule_id"], "kind": "note", "words": "Still wanted."}
	}
	h.unchanged(func() {
		refused(t, h.send("hive-rule", "amend", body(), request{expected: []rev{{"pool.rule", rec["rule_id"].(string), 7}}}), envelope.Conflict, "revision_conflict")
	})
	got := ok(t, h.send("hive-rule", "amend", body(), request{}))["rule"].(map[string]any)
	if got["record_revision"] != int64(2) || len(got["amendments"].([]any)) != 1 {
		t.Errorf("amended %v", got)
	}
}

func TestSubjectRevisesALoadsAtUnderAnExpectedRevisionAndAStandingWord(t *testing.T) {
	h, filing := seed(t)
	rule := h.recorded(opRule("dispatch.cap", "always", filing, "Dispatch at most four Swarms."), request{})
	intake := map[string]any{"schema": "pool.question", "id": questionID, "home_id": homeID, "record_revision": int64(3)}
	body := func(loads ...any) map[string]any {
		return map[string]any{"subject_id": filing, "loads_at": loads, "intake": intake}
	}
	expect := []rev{{"pool.subject", filing, 1}}
	subject := func(b map[string]any, r request) *envelope.Response { return h.send("hive-memory", "subject", b, r) }

	h.unchanged(func() {
		if resp := subject(body("dispatch", "gate"), request{authority: word(3)}); resp.Status != envelope.Invalid {
			t.Errorf("a request that names no expected revision: status %s, error %+v", resp.Status, resp.Error)
		}
		refused(t, subject(body("dispatch", "gate"), request{expected: expect}), envelope.Refused, "authority_insufficient")
		refused(t, subject(body("dispatch", "gate"), request{authority: word(3), expected: []rev{{"pool.subject", filing, 7}}}), envelope.Conflict, "revision_conflict")
		refused(t, subject(body("dispatch", "dispatch"), request{authority: word(3), expected: expect}), envelope.Refused, "pool.subject_invalid")
		refused(t, subject(body("dispatch", "project"), request{authority: word(3), expected: expect}), envelope.Refused, "pool.subject_invalid")
	})

	before := h.records()
	resp := subject(body("dispatch", "gate"), request{authority: word(3), expected: expect})
	got := ok(t, resp)["subject"].(map[string]any)
	if !same(t, got["loads_at"], []any{"dispatch", "gate"}) || got["record_revision"] != int64(2) || !slices.Contains(kinds(resp), "pool.subject.revised") {
		t.Fatalf("subject %v, receipts %v", got, kinds(resp))
	}
	after := h.records()
	if after["pool/records/rule/"+rule["rule_id"].(string)+".json"] != before["pool/records/rule/"+rule["rule_id"].(string)+".json"] {
		t.Error("the revision changed a rule")
	}
	if !same(t, h.stored("subject", filing)["facts"], []any{}) {
		t.Error("the revision changed the facts")
	}
}

func TestAPostureRuleIsRecordedOnlyFromAStandingWord(t *testing.T) {
	h, _ := seed(t)
	filing := h.subject(projectScope, "posture")
	body := opRule("assimilation.posture", "subject", filing, "Land by pull request, never directly.")
	h.unchanged(func() {
		refused(t, h.record(body, request{scope: projectScope}), envelope.Refused, "authority_insufficient")
	})
	if rec := h.recorded(body, request{scope: projectScope, authority: word(3)}); rec["subject"] != "assimilation.posture" {
		t.Errorf("recorded %v", rec)
	}
}
