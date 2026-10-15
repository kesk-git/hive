package memory_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// The acceptance test of generated knowledge (spec/pool.md, Generated
// knowledge). It drives hive-memory render, check, propagate and budget and the
// read of a projection in process, over a home seeded from the contract's record
// fixtures. The rule and subject writers are a later lane's: rules, subjects and
// facts are stood in the Pool as those writers would leave them.

var intake = map[string]any{"schema": "word.intake", "id": "rcpt_01jc7d8e9f0g1h2j3k4m5n6p7v"}

func ruleRefs(t testing.TB, refs []any) []string {
	t.Helper()
	out := []string{}
	for _, v := range refs {
		out = append(out, v.(map[string]any)["id"].(string))
	}
	return out
}

func sourceIDs(c map[string]any, schema string) []string {
	out := []string{}
	for _, v := range c["sources"].([]any) {
		ref := v.(map[string]any)["ref"].(map[string]any)
		if ref["schema"] == schema {
			out = append(out, ref["id"].(string))
		}
	}
	return out
}

func inventory(c map[string]any) []string {
	out := []string{}
	for _, v := range c["rules"].([]any) {
		out = append(out, v.(map[string]any)["rule_id"].(string))
	}
	return out
}

func has(list []string, want ...string) bool {
	for _, w := range want {
		found := false
		for _, g := range list {
			found = found || g == w
		}
		if !found {
			return false
		}
	}
	return true
}

// resolved is the digest of the resolved rules of the role at the scope, which
// is what a rendering records as its rules digest.
func (h *home) resolved(scope map[string]any, role string) string {
	h.t.Helper()
	body := h.ok(h.send("hive-rule", "resolve", map[string]any{"role": role}, request{scope: scope}))
	d, _ := body["digest"].(string)
	return d
}

// A rendering records its sources at their revisions, the renderer version, the
// resolved-rule digest and the digest of its text, and writes its receipt and
// event; an equal rendering writes nothing; a changed source revision writes a
// new rendering although the text is the same.
func TestARenderingRecordsWhatItReadAndAnEqualOneWritesNothing(t *testing.T) {
	h, filing := seed(t)
	a1 := h.rule("always", filing, hiveScope, nil)
	a2 := h.rule("always", filing, hiveScope, R{"applies": []any{"hive"}})
	other := h.subject(hiveScope, "class.3", "A fact of the subject.")
	sub := h.rule("subject", other, hiveScope, nil)
	product := fixture(t, "pool.rule_v2", "product-target")
	product["home_id"], product["record_revision"], product["rule_id"] = homeID, int64(1), mint(t, ids.Rule)
	h.plant("rule", product["rule_id"].(string), product)

	resp := h.render(hiveScope, "always", "hive", "", "")
	body := h.ok(resp)
	p, unchanged := body["projection"].(map[string]any), body["unchanged"].(bool)
	c := current(p)
	if unchanged || c["rendering"] != int64(1) || p["record_revision"] != int64(1) {
		t.Fatalf("unchanged %v, rendering %v, revision %v", unchanged, c["rendering"], p["record_revision"])
	}
	text := c["text"].(string)
	if c["renderer_version"] == "" || c["digest"] != canon.FileDigest([]byte(text)) || c["bytes"] != int64(len(text)) {
		t.Errorf("renderer %v, digest %v, bytes %v", c["renderer_version"], c["digest"], c["bytes"])
	}
	if want := h.resolved(hiveScope, "hive"); want == "" || c["rules_digest"] != want {
		t.Errorf("rules digest %v, resolved %q", c["rules_digest"], want)
	}
	budget := h.budgetRecord()
	if p["budget_revision"] != budget["record_revision"] || maxOf(budget, p["projection_id"].(string)) != 18432 {
		t.Errorf("budget revision %v of %v; entry %d", p["budget_revision"], budget["record_revision"], maxOf(budget, p["projection_id"].(string)))
	}
	// Every effective rule, and the budget at its revision, are sources; the
	// rules the text renders are the inventory.
	if !has(sourceIDs(c, "pool.rule"), a1, a2, sub) || len(sourceIDs(c, "pool.budget")) != 1 || has(sourceIDs(c, "pool.rule"), product["rule_id"].(string)) {
		t.Errorf("sources %v", c["sources"])
	}
	if got := inventory(c); !has(got, a1, a2) || len(got) != 2 {
		t.Errorf("inventory %v", got)
	}
	if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "pool.projection.rendered" || resp.Receipts[0].Subject == nil || resp.Receipts[0].Subject.ID != p["projection_id"] {
		t.Fatalf("receipts %+v", resp.Receipts)
	}

	// An equal rendering writes nothing.
	h.unchanged(func() {
		again := h.render(hiveScope, "always", "hive", "", "")
		got := h.ok(again)
		if got["unchanged"] != true || len(again.Receipts) != 0 || !equal(got["projection"], p) {
			t.Errorf("an equal rendering: %v, receipts %v", got["unchanged"], again.Receipts)
		}
	})

	// A rule that is no line of the projection changes revision: the text is the
	// same and the sources are not, so there is a second rendering, and the first
	// is kept in history.
	h.amend(sub, nil)
	p2, unchanged := h.rendered(hiveScope, "always", "hive", "")
	c2 := current(p2)
	if unchanged || c2["rendering"] != int64(2) || c2["text"] != text || c2["digest"] != c["digest"] {
		t.Fatalf("unchanged %v, rendering %v, same text %v", unchanged, c2["rendering"], c2["text"] == text)
	}
	if hist := p2["history"].([]any); len(hist) != 1 || !equal(hist[0], c) {
		t.Errorf("history %v", hist)
	}
}

// An always projection holds only the always lines of its role at its exact
// scope and no facts; a subject projection holds the subject lines filed under
// its subject and that subject's facts.
func TestTheTwoTiersHoldWhatTheContractGivesThem(t *testing.T) {
	h, filing := seed(t)
	always := h.rule("always", filing, hiveScope, nil)
	wsAlways := h.rule("always", h.subject(workspaceScope, "filing"), workspaceScope, nil)
	s := h.subject(hiveScope, "class.3", "The build uses make.")
	t2 := h.subject(hiveScope, "class.4", "A fact of another subject.")
	mine := h.rule("subject", s, hiveScope, nil)
	narrow := h.rule("subject", s, hiveScope, R{"applies": []any{"class-5"}})
	elsewhere := h.rule("subject", t2, hiveScope, nil)

	ap, _ := h.rendered(hiveScope, "always", "hive", "")
	ac := current(ap)
	text := ac["text"].(string)
	if got := inventory(ac); len(got) != 1 || got[0] != always || strings.Contains(text, "make") || strings.Contains(text, "another subject") {
		t.Errorf("always inventory %v, text %q", got, text)
	}
	if strings.Contains(text, wsAlways) || strings.Contains(text, mine) {
		t.Errorf("always text %q", text)
	}
	// The workspace's own always projection holds its own line, never the Hive's.
	wp, _ := h.rendered(workspaceScope, "always", "fleet", "")
	if got := inventory(current(wp)); len(got) != 1 || got[0] != wsAlways {
		t.Errorf("workspace always inventory %v", got)
	}

	sp, _ := h.rendered(hiveScope, "subject", "class-3", s)
	sc := current(sp)
	st := sc["text"].(string)
	if sp["subject_id"] != s || !strings.Contains(st, "The build uses make.") || !strings.Contains(st, mine) {
		t.Errorf("subject text %q", st)
	}
	if got := inventory(sc); len(got) != 1 || got[0] != mine {
		t.Errorf("subject inventory %v (not %s, %s, %s)", got, narrow, elsewhere, always)
	}
	if strings.Contains(st, always) || strings.Contains(st, "another subject") || !has(sourceIDs(sc, "pool.subject"), s) {
		t.Errorf("subject text %q, sources %v", st, sc["sources"])
	}
}

// Freshness is computed at the clock: a rule that becomes effective or expires
// by the clock makes a projection stale with nothing written, and a changed
// fact or budget revision does with the rules digest unchanged, in check and in
// the read of the projection alike.
func TestFreshnessIsComputedAtTheClock(t *testing.T) {
	h, filing := seed(t)
	expiring := h.rule("always", filing, hiveScope, R{"expires_at": d15})
	later := h.rule("always", filing, hiveScope, R{"effective_at": d15})
	p, _ := h.rendered(hiveScope, "always", "hive", "")
	id := p["projection_id"].(string)

	fresh := func(want string, stale ...string) {
		t.Helper()
		report := checked(t, h.check(""), id)
		read := h.freshness(id)
		for name, m := range map[string]map[string]any{"check": report, "read": read} {
			if m["state"] != want {
				t.Errorf("%s: state %v, want %s", name, m["state"], want)
			}
			if got := ruleRefs(t, m["stale_sources"].([]any)); len(got) != len(stale) || !has(got, stale...) {
				t.Errorf("%s: stale sources %v, want %v", name, got, stale)
			}
		}
	}
	fresh("fresh")

	h.clk.Set(at(d15).Add(time.Hour))
	h.unchanged(func() { fresh("stale", expiring, later) })

	// Rendered again at the clock it is fresh; a fact changed beside an unchanged
	// rules digest makes it stale.
	h.clk.Set(start)
	s := h.subject(hiveScope, "class.3", "One fact.")
	h.rule("subject", s, hiveScope, nil)
	sp, _ := h.rendered(hiveScope, "subject", "class-3", s)
	sid, digest := sp["projection_id"].(string), current(sp)["rules_digest"]
	h.setFacts(s, "One fact.", "A second fact.")
	for name, m := range map[string]map[string]any{"check": checked(t, h.check(""), sid), "read": h.freshness(sid)} {
		refs := m["stale_sources"].([]any)
		if m["state"] != "stale" || len(refs) != 1 || refs[0].(map[string]any)["schema"] != "pool.subject" || refs[0].(map[string]any)["id"] != s {
			t.Errorf("%s: %v", name, m)
		}
	}
	if now := h.resolved(hiveScope, "class-3"); digest != now {
		t.Errorf("the rules digest changed: %v, %v", digest, now)
	}
	sp, _ = h.rendered(hiveScope, "subject", "class-3", s)
	if m := h.freshness(sid); m["state"] != "fresh" || !strings.Contains(current(sp)["text"].(string), "A second fact.") {
		t.Errorf("after a render: %v", m)
	}

	// The budget revision changed beside an unchanged rules digest.
	b := h.budgetRecord()
	b["record_revision"] = b["record_revision"].(int64) + 1
	h.plant("budget", "memory", b)
	for name, m := range map[string]map[string]any{"check": checked(t, h.check(""), sid), "read": h.freshness(sid)} {
		refs := m["stale_sources"].([]any)
		if m["state"] != "stale" || len(refs) != 1 || refs[0].(map[string]any)["schema"] != "pool.budget" {
			t.Errorf("%s: %v", name, m)
		}
	}
	if now := h.resolved(hiveScope, "class-3"); digest != now {
		t.Errorf("the rules digest changed: %v, %v", digest, now)
	}
}

// check reports each projection's size and budget standing, the lines two
// projections of one scope and audience share, never an always line in the Hive
// and Fleet audiences, and a consumer whose digest is not the current one.
func TestCheckReportsSizeDuplicatesAndDrift(t *testing.T) {
	h, filing := seed(t)
	line := h.rule("always", filing, hiveScope, nil)
	hp, _ := h.rendered(hiveScope, "always", "hive", "")
	fp, _ := h.rendered(hiveScope, "always", "fleet", "")
	hid, fid := hp["projection_id"].(string), fp["projection_id"].(string)
	report := h.check("")
	for _, id := range []string{hid, fid} {
		m := checked(t, report, id)
		if m["state"] != "fresh" || m["max_bytes"] != int64(18432) || m["over_budget"] != false || len(m["duplicates"].([]any)) != 0 || len(m["drift"].([]any)) != 0 {
			t.Errorf("%s: %v", id, m)
		}
	}
	if m := checked(t, report, hid); m["bytes"] != current(hp)["bytes"] || !has(inventory(current(hp)), line) {
		t.Errorf("bytes %v", m["bytes"])
	}
	if report["budget_revision"] != h.budgetRecord()["record_revision"] || report["checked_at"] == nil || report["subjects"] == nil {
		t.Errorf("report %v", report)
	}
	if got := h.check(hid)["projections"].([]any); len(got) != 1 {
		t.Errorf("one projection checked: %v", got)
	}

	// A projection over the budget its record gives it is reported, not repaired.
	b := h.budgetRecord()
	b["record_revision"] = b["record_revision"].(int64) + 1
	for _, v := range b["projections"].([]any) {
		if e := v.(map[string]any); e["projection_id"] == hid {
			e["max_bytes"] = int64(10)
		}
	}
	h.plant("budget", "memory", b)
	h.unchanged(func() {
		if m := checked(t, h.check(""), hid); m["over_budget"] != true || m["max_bytes"] != int64(10) {
			t.Errorf("over budget: %v", m)
		}
	})

	// The budget is raised again by the Hive Mind's word, as a change would.
	b["record_revision"] = b["record_revision"].(int64) + 1
	for _, v := range b["projections"].([]any) {
		v.(map[string]any)["max_bytes"] = int64(18432)
	}
	h.plant("budget", "memory", b)

	// Two projections of one scope and audience that carry one rule.
	shared := mint(t, ids.Rule)
	for i, tier := range []string{"always", "subject"} {
		rec := fixture(t, "pool.projection_v2", "minimal")
		rec["projection_id"], rec["home_id"], rec["record_revision"] = "mem_01jc7d8e9f0g1h2j3k4m5n6p7"+string(rune('d'+i)), homeID, int64(1)
		rec["scope_level"], rec["workspace_id"] = "workspace", workspaceID
		rec["audience"], rec["tier"], rec["budget_revision"] = "fleet", tier, int64(1)
		if tier == "subject" {
			rec["subject_id"] = "subj_01jc7d8e9f0g1h2j3k4m5n6p7s"
		}
		c := rec["current"].(map[string]any)
		c["rendering"], c["bytes"] = int64(1), int64(100)
		c["rules"] = []any{map[string]any{"rule_id": shared, "record_revision": int64(1)}}
		h.plant("projection", rec["projection_id"].(string), rec)
	}
	m := checked(t, h.check(""), "mem_01jc7d8e9f0g1h2j3k4m5n6p7d")
	if d := m["duplicates"].([]any); len(d) != 1 || d[0].(map[string]any)["rule_id"] != shared || d[0].(map[string]any)["also_in"] != "mem_01jc7d8e9f0g1h2j3k4m5n6p7e" {
		t.Errorf("duplicates %v", m["duplicates"])
	}

	// A consumer that received an earlier digest is drift, and not once it has
	// received the current one.
	consumer := map[string]any{"kind": "session", "session_id": mint(t, ids.Session), "session_generation": int64(1)}
	received := func(p map[string]any) {
		t.Helper()
		c := current(p)
		h.ok(h.send("hive-memory", "propagate", map[string]any{"projection_id": hid, "consumer": consumer, "step": "received",
			"rendering": c["rendering"], "digest": c["digest"], "at": "2026-10-14T09:00:00.000Z"}, request{}))
	}
	received(hp)
	h.amend(line, R{"rule_line": "A changed line."})
	np, _ := h.rendered(hiveScope, "always", "hive", "")
	drift := checked(t, h.check(""), hid)["drift"].([]any)
	if len(drift) != 1 || drift[0].(map[string]any)["received"].(map[string]any)["digest"] != current(hp)["digest"] {
		t.Fatalf("drift %v", drift)
	}
	received(np)
	if drift := checked(t, h.check(""), hid)["drift"].([]any); len(drift) != 0 {
		t.Errorf("drift %v", drift)
	}
}

// propagate records a receipt and an acknowledgment, each with its receipt; a
// step already recorded is returned unchanged; every step is retained.
func TestPropagateRecordsEachStepOnceAndKeepsHistory(t *testing.T) {
	h, filing := seed(t)
	line := h.rule("always", filing, hiveScope, nil)
	p, _ := h.rendered(hiveScope, "always", "hive", "")
	id, c := p["projection_id"].(string), current(p)
	consumer := map[string]any{"kind": "generation", "task_id": mint(t, ids.Task), "generation": int64(1)}
	step := func(name string, c map[string]any) *envelope.Response {
		return h.send("hive-memory", "propagate", map[string]any{"projection_id": id, "consumer": consumer, "step": name,
			"rendering": c["rendering"], "digest": c["digest"], "at": "2026-10-14T09:00:00.000Z"}, request{})
	}

	resp := step("received", c)
	body := h.ok(resp)
	got := body["consumer"].(map[string]any)["received"].(map[string]any)
	if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "pool.projection.received" || got["receipt_id"] != resp.Receipts[0].ReceiptID || got["digest"] != c["digest"] {
		t.Fatalf("received: %v, receipts %+v", got, resp.Receipts)
	}
	h.unchanged(func() {
		again := step("received", c)
		if h.ok(again); len(again.Receipts) != 0 || !equal(again.Body["consumer"], body["consumer"]) {
			t.Errorf("a repeated step: receipts %v, %v", again.Receipts, again.Body["consumer"])
		}
	})

	resp = step("acknowledged", c)
	ack := h.ok(resp)["consumer"].(map[string]any)
	if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "pool.projection.acknowledged" || ack["acknowledged"].(map[string]any)["receipt_id"] != resp.Receipts[0].ReceiptID {
		t.Fatalf("acknowledged: %v, receipts %+v", ack, resp.Receipts)
	}

	// The consumer advances to a newer rendering and the history keeps both steps
	// of the first.
	h.amend(line, R{"rule_line": "A changed line."})
	np, _ := h.rendered(hiveScope, "always", "hive", "")
	step("received", current(np))
	stored := h.stored("projection", id)
	if hist := stored["receipt_history"].([]any); len(hist) != 3 {
		t.Errorf("receipt history %v", hist)
	}
	if cons := stored["consumers"].([]any); len(cons) != 1 || cons[0].(map[string]any)["received"].(map[string]any)["rendering"] != int64(2) {
		t.Errorf("consumers %v", cons)
	}

	// A digest that is not the one of the rendering named is no step.
	bad := map[string]any{"rendering": c["rendering"], "digest": "sha256:" + strings.Repeat("0", 64)}
	refused(t, step("received", bad), envelope.Refused, "pool.delivery_inventory")
}

// hive-memory budget changes the budget on a standing word when the current and
// reachable renderings fit, re-rendering the projections in the same
// transaction; a decrease that cannot fit is refused and nothing is written;
// a request that names no expected revision is invalid.
func TestBudgetChangesAreFitCheckedAndReRenderedAtomically(t *testing.T) {
	h, filing := seed(t)
	h.rule("always", filing, hiveScope, nil)
	s := h.subject(hiveScope, "class.3", "One fact.")
	h.rule("subject", s, hiveScope, nil)
	later := h.rule("subject", s, hiveScope, R{"effective_at": d20})
	ap, _ := h.rendered(hiveScope, "always", "hive", "")
	sp, _ := h.rendered(hiveScope, "subject", "class-3", s)
	aid, sid := ap["projection_id"].(string), sp["projection_id"].(string)
	now := current(sp)["bytes"].(int64)
	expect := func() []rev { return []rev{{"pool.budget", "memory", h.budgetRecord()["record_revision"].(int64)}} }
	change := func(max int64, expected []rev) *envelope.Response {
		return h.send("hive-memory", "budget", map[string]any{"projections": []any{entryOf(sid, max)}, "intake": intake},
			request{authority: word(3), expected: expected})
	}

	// Without an expected revision of the budget record the request is invalid.
	h.unchanged(func() {
		refused(t, change(20000, nil), envelope.Invalid, "schema_violation")
	})
	// A decrease below the current rendering, and one that fits now but not at
	// the instant a rule takes effect, are refused with nothing written.
	h.unchanged(func() {
		refused(t, change(10, expect()), envelope.Refused, "pool.projection_over_budget")
		resp := change(now+5, expect())
		refused(t, resp, envelope.Refused, "pool.projection_over_budget")
		if resp.Error.Detail["moves"] == nil {
			t.Errorf("detail %v (later rule %s)", resp.Error.Detail, later)
		}
	})

	before := h.budgetRecord()
	renderings := map[string]int64{}
	for _, id := range []string{aid, sid} {
		renderings[id] = current(h.stored("projection", id))["rendering"].(int64)
	}
	resp := change(now+4000, expect())
	body := h.ok(resp)
	stored := h.budgetRecord()
	if !equal(body["budget"], stored) || stored["record_revision"] != before["record_revision"].(int64)+1 || maxOf(stored, sid) != now+4000 || maxOf(stored, aid) != 18432 {
		t.Fatalf("budget %v", stored)
	}
	if len(stored["changes"].([]any)) != len(before["changes"].([]any))+1 {
		t.Errorf("changes %v", stored["changes"])
	}
	kinds := map[string]int{}
	for _, r := range resp.Receipts {
		kinds[r.Kind]++
	}
	if kinds["pool.budget.changed"] != 1 || kinds["pool.projection.rendered"] != 2 || len(resp.Receipts) != 3 {
		t.Errorf("receipts %v", kinds)
	}
	for _, id := range []string{aid, sid} {
		p := h.stored("projection", id)
		if p["budget_revision"] != stored["record_revision"] || current(p)["rendering"] != renderings[id]+1 {
			t.Errorf("%s: budget revision %v, rendering %v", id, p["budget_revision"], current(p)["rendering"])
		}
		if m := h.freshness(id); m["state"] != "fresh" {
			t.Errorf("%s: %v", id, m)
		}
	}
}

// A projection's freshness is derived from the rules, subjects and budget it
// reads, so a projection that cannot be read beside it neither fails its read
// nor hides the omission of the one that cannot: it is named, and the valid one
// is returned with its freshness.
func TestAReadOfAProjectionSurvivesAMalformedOneBesideIt(t *testing.T) {
	h, _ := seed(t)
	p, _ := h.rendered(hiveScope, "always", "hive", "")
	id := p["projection_id"].(string)
	bad := mint(t, ids.Projection)
	if err := os.WriteFile(filepath.Join(h.dir, "pool", "records", "projection", bad+".json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if m := h.freshness(id); m["state"] != "fresh" {
		t.Errorf("by identity: %v", m)
	}
	body := h.ok(h.send("hive-pool", "read", map[string]any{"kind": "projection", "selector": map[string]any{}}, request{}))
	derived := body["derived"].([]any)
	omitted, _ := body["projection"].(map[string]any)["omitted"].([]any)
	if len(derived) != 1 || derived[0].(map[string]any)["projection_id"] != id || len(omitted) != 1 || omitted[0].(map[string]any)["id"] != bad {
		t.Errorf("a query: derived %v, omitted %v (body %v)", derived, omitted, body)
	}
}

// stale renders a projection, changes its rule and renders it again, and
// returns its identity, its first rendering, and the revision of it a caller
// read before the second rendering and the revision after.
func stale(h *home, filing string) (id string, first map[string]any, before, after []rev) {
	line := h.rule("always", filing, hiveScope, nil)
	p, _ := h.rendered(hiveScope, "always", "hive", "")
	id, first = p["projection_id"].(string), current(p)
	h.amend(line, R{"rule_line": "A changed line."})
	np, _ := h.rendered(hiveScope, "always", "hive", "")
	return id, first, []rev{{"pool.projection", id, p["record_revision"].(int64)}}, []rev{{"pool.projection", id, np["record_revision"].(int64)}}
}

// render compares every revision the request expects before it returns
// unchanged or stages a write: a mismatch is revision_conflict and nothing is
// written.
func TestRenderChecksTheRevisionsTheRequestExpects(t *testing.T) {
	h, filing := seed(t)
	_, _, before, after := stale(h, filing)
	render := func(expected []rev) *envelope.Response {
		return h.send("hive-memory", "render", map[string]any{"tier": "always", "audience": "hive"}, request{expected: expected})
	}
	h.unchanged(func() { refused(t, render(before), envelope.Conflict, "revision_conflict") })
	if body := h.ok(render(after)); body["unchanged"] != true {
		t.Errorf("at the revision read: %v", body)
	}
}

// propagate compares every revision the request expects before it returns
// unchanged or stages a write: a mismatch is revision_conflict and nothing is
// written.
func TestPropagateChecksTheRevisionsTheRequestExpects(t *testing.T) {
	h, filing := seed(t)
	id, first, before, after := stale(h, filing)
	propagate := func(expected []rev) *envelope.Response {
		return h.send("hive-memory", "propagate", map[string]any{
			"projection_id": id, "step": "received", "rendering": first["rendering"], "digest": first["digest"], "at": "2026-10-14T09:00:00.000Z",
			"consumer": map[string]any{"kind": "generation", "task_id": mint(t, ids.Task), "generation": int64(1)},
		}, request{expected: expected})
	}
	h.unchanged(func() { refused(t, propagate(before), envelope.Conflict, "revision_conflict") })
	if resp := propagate(after); len(h.ok(resp)) == 0 || len(resp.Receipts) != 1 {
		t.Errorf("at the revision read: receipts %v", resp.Receipts)
	}
}
