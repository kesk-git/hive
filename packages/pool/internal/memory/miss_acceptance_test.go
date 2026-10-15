package memory_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	_ "github.com/kesk-git/hive/packages/pool/internal/rulewrite"
)

// The acceptance test of independent misses and delivery evidence (spec/pool.md,
// Generated knowledge, Misses and Independent evidence provenance). It drives
// hive-memory miss and check through the executable, and the Pool's own
// refusals through hive-rule record and amend, over a home seeded as the other
// tests of this package seed one.

const (
	seenBefore = "2026-10-14T08:00:00.000Z"
	seenAt     = "2026-10-14T09:00:00.000Z"
	seenAfter  = "2026-10-14T10:00:00.000Z"

	missedKind  = "pool.rule.missed"
	revisedKind = "pool.miss.revised"
)

// sendAs is send with the caller given: a supervisor or a worker names the
// session or the task generation that acts.
func (h *home) sendAs(caller map[string]any, command, operation string, body map[string]any, r request) *envelope.Response {
	h.t.Helper()
	if r.scope == nil {
		r.scope = hiveScope
	}
	if r.authority == nil {
		r.authority = map[string]any{"kind": "none"}
	}
	if r.version == "" {
		r.version = versions[operation]
	}
	req := map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": command, "operation": operation, "operation_version": r.version,
		"request_id": mint(h.t, ids.Request), "correlation_id": mint(h.t, ids.Correlation),
		"issued_at": "2026-10-14T09:00:00.000Z", "caller": caller,
		"authority": r.authority, "body": body,
	}
	for k, v := range r.scope {
		req[k] = v
	}
	if len(r.expected) > 0 {
		list := []any{}
		for _, e := range r.expected {
			list = append(list, map[string]any{"schema": e.schema, "id": e.id, "record_revision": e.revision})
		}
		req["expected_revisions"] = list
	}
	data, err := canon.Encode(req)
	if err != nil {
		h.t.Fatal(err)
	}
	var cmd *cli.Command
	for _, c := range h.exe.Commands {
		if c.Name == command {
			cmd = c
		}
	}
	out, err := h.exe.Handle(cli.Invocation{Command: cmd, Operation: operation, Home: h.dir, Request: append(data, '\n')})
	if err != nil {
		h.t.Fatal(err)
	}
	resp, err := envelope.DecodeResponse(out.Encoded)
	if err != nil {
		h.t.Fatal(err)
	}
	return resp
}

// sessionActor is a supervisor session as the caller of a request and as the
// actor a miss names.
func sessionActor(t testing.TB) (caller, actor map[string]any) {
	id := mint(t, ids.Session)
	return map[string]any{"kind": "supervisor", "session_id": id, "session_generation": int64(1)},
		map[string]any{"kind": "session", "session_id": id, "session_generation": int64(1)}
}

// workerActor is a task generation as the caller of a request and as the actor a
// miss names.
func workerActor(t testing.TB) (caller, actor map[string]any) {
	id := mint(t, ids.Task)
	return map[string]any{"kind": "worker", "task_id": id, "generation": int64(1)},
		map[string]any{"kind": "generation", "task_id": id, "generation": int64(1)}
}

func recordRef(schema, id string) map[string]any {
	return map[string]any{"schema": schema, "id": id, "home_id": homeID}
}

// provenance is a provenance whose incident is the record named.
func accountOf(incident, independence string, derived ...map[string]any) map[string]any {
	from := []any{}
	for _, d := range derived {
		from = append(from, d)
	}
	return map[string]any{
		"incident": recordRef("pool.item", incident), "observer": map[string]any{"kind": "test"},
		"independence": independence, "derived_from": from, "evidence": []any{recordRef("pool.item", incident)},
	}
}

// missWorld is a home with one subject and one rule of the subject's tier filed
// under it.
type missWorld struct {
	*home
	subject, rule string
}

func newMissWorld(t testing.TB) *missWorld {
	h, subject := seed(t)
	return &missWorld{home: h, subject: subject, rule: h.rule("subject", subject, hiveScope, nil)}
}

func (w *missWorld) body(rule string, revision int64, actor, prov map[string]any, observed string) map[string]any {
	return map[string]any{
		"rule": map[string]any{"rule_id": rule, "record_revision": revision}, "actor": actor,
		"step": "dispatch", "provenance": prov, "observed_at": observed,
	}
}

// file sends hive-memory miss for the rule at the revision.
func (w *missWorld) file(rule string, revision int64, actor, prov map[string]any, observed string) *envelope.Response {
	w.t.Helper()
	return w.send("hive-memory", "miss", w.body(rule, revision, actor, prov, observed), request{version: "1.0"})
}

// missed files a miss that must be recorded and returns its record.
func (w *missWorld) missed(actor, prov map[string]any) map[string]any {
	w.t.Helper()
	resp := w.file(w.rule, 1, actor, prov, seenAt)
	body := w.ok(resp)
	if body["recorded"] != true || body["miss"] == nil {
		w.t.Fatalf("the miss was not recorded: %v", body)
	}
	if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != missedKind {
		w.t.Fatalf("receipts %v, want one %s", resp.Receipts, missedKind)
	}
	return body["miss"].(map[string]any)
}

// received records the actor's receipt of the subject's projection at the time.
func (w *missWorld) received(actor map[string]any, at string) {
	w.t.Helper()
	p, _ := w.rendered(hiveScope, "subject", "class-3", w.subject)
	cur := current(p)
	w.ok(w.send("hive-memory", "propagate", map[string]any{
		"projection_id": p["projection_id"], "consumer": actor, "step": "received",
		"rendering": cur["rendering"], "digest": cur["digest"], "at": at,
	}, request{}))
}

// delivered stands an Imperative of the task whose delivery to the generation at
// the time names the rules given.
func (w *missWorld) delivered(actor map[string]any, at string, rules ...map[string]any) {
	w.t.Helper()
	rec := fixture(w.t, "pool.imperative", "memory-inventory")
	rec["task_id"], rec["home_id"] = actor["task_id"], homeID
	d := rec["deliveries"].([]any)[0].(map[string]any)
	d["generation"], d["delivered_at"] = actor["generation"], at
	named := []any{}
	for _, r := range rules {
		named = append(named, r)
	}
	d["rules"] = named
	w.plant("imperative", actor["task_id"].(string), rec)
}

func ruleAt(id string, revision int64) map[string]any {
	return map[string]any{"rule_id": id, "record_revision": revision}
}

// counted is the report of check about the subject.
func (w *missWorld) counted() map[string]any {
	w.t.Helper()
	for _, v := range w.check("")["subjects"].([]any) {
		if m := v.(map[string]any); m["subject_id"] == w.subject {
			return m
		}
	}
	return nil
}

// stored misses, by identity.
func (h *home) misses() map[string]map[string]any {
	h.t.Helper()
	out := map[string]map[string]any{}
	dir := filepath.Join(h.dir, "pool", "records", "miss")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".json")
		out[id] = h.stored("miss", id)
	}
	return out
}

func kindsOf(resp *envelope.Response) []string {
	var out []string
	for _, r := range resp.Receipts {
		out = append(out, r.Kind)
	}
	slices.Sort(out)
	return out
}

func TestNoMissWhenTheActorReceivedAProjectionCarryingTheRuleAtTheRevisionCited(t *testing.T) {
	for name, actor := range map[string]func(testing.TB) (map[string]any, map[string]any){"a session": sessionActor, "a task generation": workerActor} {
		t.Run(name, func(t *testing.T) {
			w := newMissWorld(t)
			_, who := actor(t)
			w.received(who, seenBefore)
			w.unchanged(func() {
				resp := w.file(w.rule, 1, who, accountOf("task_a", "independent"), seenAt)
				body := w.ok(resp)
				if body["recorded"] != false || body["miss"] != nil || len(resp.Receipts) != 0 {
					t.Errorf("body %v, receipts %v; want recorded false, no miss, no receipt", body, resp.Receipts)
				}
			})
			// The instant of the receipt is no later than the incident: one at the
			// same instant is received.
			w.received(who, seenAt)
			if body := w.ok(w.file(w.rule, 1, who, accountOf("task_a", "independent"), seenAt)); body["recorded"] != false {
				t.Errorf("a receipt at the instant of the incident did not prevent the miss: %v", body)
			}
		})
	}
}

func TestNoMissWhenAnImperativeDeliveryNamesTheRuleAtTheRevisionCited(t *testing.T) {
	w := newMissWorld(t)
	_, actor := workerActor(t)
	w.delivered(actor, seenBefore, ruleAt(w.rule, 1))
	w.unchanged(func() {
		resp := w.file(w.rule, 1, actor, accountOf("task_a", "independent"), seenAt)
		body := w.ok(resp)
		if body["recorded"] != false || body["miss"] != nil || len(resp.Receipts) != 0 {
			t.Errorf("body %v, receipts %v; want recorded false, no miss, no receipt", body, resp.Receipts)
		}
	})
}

func TestAMissIsRecordedWhenOnlyAnotherRevisionWasReceivedOrTheDeliveryCameLater(t *testing.T) {
	t.Run("a projection of another revision", func(t *testing.T) {
		w := newMissWorld(t)
		_, actor := workerActor(t)
		w.received(actor, seenBefore)
		w.amend(w.rule, R{"rule_line": "A line of the second revision."})
		resp := w.file(w.rule, 2, actor, accountOf("task_a", "independent"), seenAt)
		body := w.ok(resp)
		if body["recorded"] != true || !slices.Equal(kindsOf(resp), []string{missedKind}) {
			t.Fatalf("body %v, receipts %v", body, kindsOf(resp))
		}
		if rule := body["miss"].(map[string]any)["rule"].(map[string]any); rule["record_revision"] != int64(2) {
			t.Errorf("the miss names %v, want revision 2", rule)
		}
	})
	t.Run("an Imperative delivery of another revision", func(t *testing.T) {
		w := newMissWorld(t)
		_, actor := workerActor(t)
		w.delivered(actor, seenBefore, ruleAt(w.rule, 1))
		w.amend(w.rule, R{"rule_line": "A line of the second revision."})
		if body := w.ok(w.file(w.rule, 2, actor, accountOf("task_a", "independent"), seenAt)); body["recorded"] != true {
			t.Errorf("body %v", body)
		}
	})
	t.Run("a projection received later than the incident", func(t *testing.T) {
		w := newMissWorld(t)
		_, actor := sessionActor(t)
		w.received(actor, seenAfter)
		if body := w.ok(w.file(w.rule, 1, actor, accountOf("task_a", "independent"), seenAt)); body["recorded"] != true {
			t.Errorf("body %v", body)
		}
	})
	t.Run("an Imperative delivered later than the incident", func(t *testing.T) {
		w := newMissWorld(t)
		_, actor := workerActor(t)
		w.delivered(actor, seenAfter, ruleAt(w.rule, 1))
		if body := w.ok(w.file(w.rule, 1, actor, accountOf("task_a", "independent"), seenAt)); body["recorded"] != true {
			t.Errorf("body %v", body)
		}
	})
	t.Run("a delivery to another generation", func(t *testing.T) {
		w := newMissWorld(t)
		_, actor := workerActor(t)
		w.delivered(actor, seenBefore, ruleAt(w.rule, 1))
		other := map[string]any{"kind": "generation", "task_id": actor["task_id"], "generation": int64(2)}
		if body := w.ok(w.file(w.rule, 1, other, accountOf("task_a", "independent"), seenAt)); body["recorded"] != true {
			t.Errorf("body %v", body)
		}
	})
	t.Run("a receipt by another consumer", func(t *testing.T) {
		w := newMissWorld(t)
		_, actor := sessionActor(t)
		_, other := sessionActor(t)
		w.received(other, seenBefore)
		if body := w.ok(w.file(w.rule, 1, actor, accountOf("task_a", "independent"), seenAt)); body["recorded"] != true {
			t.Errorf("body %v", body)
		}
	})
}

func TestAMissCountsAgainstTheFilingSubjectAndNotTheRulesConflictKey(t *testing.T) {
	w := newMissWorld(t)
	_, actor := workerActor(t)
	miss := w.missed(actor, accountOf("task_a", "independent"))
	if miss["subject_id"] != w.subject {
		t.Errorf("the miss is against %v, want the filing subject %s", miss["subject_id"], w.subject)
	}
	conflictKey := w.stored("rule", w.rule)["subject"]
	row := w.counted()
	if row == nil || row["independent_misses"] != int64(1) || row["decision_due"] != false {
		t.Fatalf("subject row %v", row)
	}
	for _, v := range w.check("")["subjects"].([]any) {
		if v.(map[string]any)["subject_id"] == conflictKey {
			t.Errorf("the check counts a miss against the conflict key %v", conflictKey)
		}
	}
	// The record carries what the request gave and what the Pool derives.
	stored := w.stored("miss", miss["miss_id"].(string))
	if stored["schema"] != "pool.miss" || stored["record_revision"] != int64(1) || stored["step"] != "dispatch" ||
		stored["observed_at"] != seenAt || stored["subject_id"] != w.subject || !equal(stored["actor"], actor) ||
		!equal(stored["provenance_history"], []any{}) || !strings.HasPrefix(miss["miss_id"].(string), "miss_") {
		t.Errorf("stored %v", stored)
	}
}

func TestOneIncidentEchoedByAWorkerItsSupervisorAndASynchronizedReportCountsOnce(t *testing.T) {
	w := newMissWorld(t)
	_, workerWho := workerActor(t)
	_, supervisorWho := sessionActor(t)
	_, reportWho := sessionActor(t)
	first := w.missed(workerWho, accountOf("incident_a", "independent"))
	second := w.missed(supervisorWho, accountOf("incident_a", "derived", recordRef("pool.miss", first["miss_id"].(string))))
	third := w.missed(reportWho, accountOf("incident_a", "derived",
		recordRef("pool.miss", first["miss_id"].(string)), recordRef("pool.miss", second["miss_id"].(string))))
	row := w.counted()
	if row["independent_misses"] != int64(1) || row["decision_due"] != false {
		t.Fatalf("subject row %v, want one incident and no decision due", row)
	}
	got := []string{}
	for _, v := range row["misses"].([]any) {
		got = append(got, v.(string))
	}
	want := []string{first["miss_id"].(string), second["miss_id"].(string), third["miss_id"].(string)}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("contributing misses %v, want %v: copies stay readable and none is dropped", got, want)
	}
	// Two rules missed in one incident are one incident.
	other := w.rule2()
	resp := w.file(other, 1, workerWho, accountOf("incident_a", "independent"), seenAt)
	if w.ok(resp)["recorded"] != true || w.counted()["independent_misses"] != int64(1) {
		t.Errorf("a second rule missed in the same incident changed the count: %v", w.counted())
	}
}

// rule2 is a second rule of the subject, under another conflict key.
func (w *missWorld) rule2() string {
	return w.rule2Of(w.subject)
}

func (w *missWorld) rule2Of(subject string) string {
	return w.home.rule("subject", subject, hiveScope, R{"subject": "other.key"})
}

func TestASecondIndependentIncidentReachesDecisionDue(t *testing.T) {
	w := newMissWorld(t)
	_, a := workerActor(t)
	_, b := workerActor(t)
	first := w.missed(a, accountOf("incident_a", "independent"))
	if row := w.counted(); row["decision_due"] != false || row["independent_misses"] != int64(1) {
		t.Fatalf("after one incident: %v", row)
	}
	second := w.missed(b, accountOf("incident_b", "independent"))
	row := w.counted()
	if row["independent_misses"] != int64(2) || row["decision_due"] != true || row["question_id"] != nil {
		t.Fatalf("after two incidents: %v", row)
	}
	got := []string{}
	for _, v := range row["misses"].([]any) {
		got = append(got, v.(string))
	}
	slices.Sort(got)
	want := []string{first["miss_id"].(string), second["miss_id"].(string)}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("contributing misses %v, want %v", got, want)
	}
}

func TestNoDecisionIsDueWhenAnEarlierQuestionNamesTheSubject(t *testing.T) {
	w := newMissWorld(t)
	_, a := workerActor(t)
	_, b := workerActor(t)
	w.missed(a, accountOf("incident_a", "independent"))
	w.missed(b, accountOf("incident_b", "independent"))
	q := fixture(t, "pool.question", "answered-superseding")
	q["scope_level"] = "hive"
	delete(q, "workspace_id")
	delete(q, "project_id")
	q["question_id"] = "q_" + strings.TrimPrefix(mint(t, ids.Task), "task_")
	q["origin"] = map[string]any{"kind": "rule", "ref": map[string]any{"schema": "pool.subject", "id": w.subject, "home_id": homeID}}
	w.plant("question", q["question_id"].(string), q)
	row := w.counted()
	if row["independent_misses"] != int64(2) || row["decision_due"] != false || row["question_id"] != q["question_id"] {
		t.Errorf("subject row %v, want the count kept, nothing due and the earlier question named", row)
	}
	// A question about another subject does not stop it.
	w2 := newMissWorld(t)
	w2.missed(a, accountOf("incident_a", "independent"))
	w2.missed(b, accountOf("incident_b", "independent"))
	q["question_id"] = "q_" + strings.TrimPrefix(mint(t, ids.Task), "task_")
	q["origin"] = map[string]any{"kind": "rule", "ref": map[string]any{"schema": "pool.subject", "id": "subj_" + strings.TrimPrefix(mint(t, ids.Task), "task_"), "home_id": homeID}}
	w2.plant("question", q["question_id"].(string), q)
	if row := w2.counted(); row["decision_due"] != true {
		t.Errorf("a question about another subject stopped the decision: %v", row)
	}
}

func TestACorrectionRecomputesTheCountAndKeepsTheFormerProvenance(t *testing.T) {
	w := newMissWorld(t)
	_, a := workerActor(t)
	_, b := sessionActor(t)
	first := w.missed(a, accountOf("incident_a", "independent"))
	second := w.missed(b, accountOf("incident_b", "independent"))
	if row := w.counted(); row["decision_due"] != true {
		t.Fatalf("two independent incidents: %v", row)
	}
	id := second["miss_id"].(string)
	corrected := accountOf("incident_b", "derived", recordRef("pool.miss", first["miss_id"].(string)))
	correct := func(expected int64, prov map[string]any) *envelope.Response {
		body := w.body(w.rule, 1, b, prov, seenAt)
		body["miss_id"] = id
		return w.send("hive-memory", "miss", body, request{version: "1.0", expected: []rev{{"pool.miss", id, expected}}})
	}
	// A stale expectation changes nothing.
	w.unchanged(func() {
		if resp := correct(7, corrected); resp.Status != envelope.Conflict || resp.Error == nil || resp.Error.Code != envelope.CodeRevisionConflict {
			t.Errorf("status %s, error %+v; want revision_conflict", resp.Status, resp.Error)
		}
	})
	resp := correct(1, corrected)
	body := w.ok(resp)
	if body["recorded"] != true || !slices.Equal(kindsOf(resp), []string{revisedKind}) {
		t.Fatalf("body %v, receipts %v", body, kindsOf(resp))
	}
	revised := w.stored("miss", id)
	history := revised["provenance_history"].([]any)
	if revised["record_revision"] != int64(2) || len(history) != 1 || !equal(history[0].(map[string]any)["provenance"], second["provenance"]) ||
		!equal(revised["provenance"], corrected) {
		t.Errorf("revised %v", revised)
	}
	// Rule, actor, step and observation time are what they were.
	for _, member := range []string{"rule", "actor", "step", "observed_at", "subject_id", "created_at", "miss_id"} {
		if !equal(revised[member], second[member]) {
			t.Errorf("%s changed from %v to %v", member, second[member], revised[member])
		}
	}
	// The count is recomputed from the records and no evidence is deleted: the
	// echo is still a contributing miss, no longer a second incident.
	row := w.counted()
	if row["independent_misses"] != int64(1) || row["decision_due"] != false || len(row["misses"].([]any)) != 2 {
		t.Errorf("after the correction: %v", row)
	}
	if len(w.misses()) != 2 {
		t.Errorf("the Pool holds %d misses, want both", len(w.misses()))
	}
	// A correction that restores independence is a second correction and a
	// second history entry, and the count follows it back.
	back := correct(2, accountOf("incident_b", "independent"))
	w.ok(back)
	if row := w.counted(); row["independent_misses"] != int64(2) || row["decision_due"] != true ||
		len(w.stored("miss", id)["provenance_history"].([]any)) != 2 {
		t.Errorf("after the second correction: %v", row)
	}
}

func TestACorrectionChangesNothingButTheProvenance(t *testing.T) {
	w := newMissWorld(t)
	_, actor := workerActor(t)
	miss := w.missed(actor, accountOf("incident_a", "independent"))
	id := miss["miss_id"].(string)
	_, other := workerActor(t)
	otherRule := w.rule2()
	for name, edit := range map[string]func(map[string]any){
		"the rule":          func(b map[string]any) { b["rule"] = ruleAt(otherRule, 1) },
		"the rule revision": func(b map[string]any) { b["rule"] = ruleAt(w.rule, 2) },
		"the actor":         func(b map[string]any) { b["actor"] = other },
		"the step":          func(b map[string]any) { b["step"] = "gate" },
		"the observation":   func(b map[string]any) { b["observed_at"] = seenAfter },
	} {
		t.Run(name, func(t *testing.T) {
			body := w.body(w.rule, 1, actor, accountOf("incident_a", "derived", recordRef("pool.miss", id)), seenAt)
			body["miss_id"] = id
			edit(body)
			w.unchanged(func() {
				resp := w.send("hive-memory", "miss", body, request{version: "1.0", expected: []rev{{"pool.miss", id, 1}}})
				if resp.Status == envelope.Ok || resp.Error == nil {
					t.Errorf("a correction of %s was accepted", name)
				}
				if len(resp.Receipts) != 0 {
					t.Errorf("a refused correction wrote receipts %v", resp.Receipts)
				}
			})
		})
	}
	t.Run("a correction names the revision it expects", func(t *testing.T) {
		body := w.body(w.rule, 1, actor, accountOf("incident_a", "independent"), seenAt)
		body["miss_id"] = id
		w.unchanged(func() {
			if resp := w.send("hive-memory", "miss", body, request{version: "1.0"}); resp.Status != envelope.Invalid {
				t.Errorf("status %s, error %+v; want invalid", resp.Status, resp.Error)
			}
		})
	})
	t.Run("a correction of no miss is not found", func(t *testing.T) {
		missing := "miss_" + strings.TrimPrefix(mint(t, ids.Task), "task_")
		body := w.body(w.rule, 1, actor, accountOf("incident_a", "independent"), seenAt)
		body["miss_id"] = missing
		resp := w.send("hive-memory", "miss", body, request{version: "1.0", expected: []rev{{"pool.miss", missing, 1}}})
		if resp.Error == nil || resp.Error.Code != envelope.CodeRevisionConflict && resp.Error.Code != envelope.CodeNotFound {
			t.Errorf("status %s, error %+v", resp.Status, resp.Error)
		}
	})
}

func TestADanglingOrCyclicDerivationContributesNoCount(t *testing.T) {
	w := newMissWorld(t)
	_, a := workerActor(t)
	_, b := workerActor(t)
	_, c := workerActor(t)
	absent := "miss_" + strings.TrimPrefix(mint(t, ids.Task), "task_")
	dangling := w.missed(a, accountOf("incident_a", "derived", recordRef("pool.miss", absent)))
	_ = dangling
	if row := w.counted(); row["independent_misses"] != int64(0) || row["decision_due"] != false || len(row["misses"].([]any)) != 0 {
		t.Errorf("a derivation from a miss the Pool does not hold counted: %v", row)
	}
	// A derivation from an incident no independent account holds is dangling too.
	w.missed(b, accountOf("incident_b", "derived", recordRef("pool.item", "incident_unreported")))
	if row := w.counted(); row["independent_misses"] != int64(0) {
		t.Errorf("a derivation from an unreported incident counted: %v", row)
	}
	// Two accounts that each derive from the other form a cycle.
	root := w.missed(c, accountOf("incident_c", "independent"))
	if row := w.counted(); row["independent_misses"] != int64(1) {
		t.Fatalf("the independent root alone: %v", row)
	}
	x := w.missed(a, accountOf("incident_c", "derived", recordRef("pool.miss", root["miss_id"].(string))))
	y := w.missed(b, accountOf("incident_c", "derived", recordRef("pool.miss", x["miss_id"].(string))))
	correct := func(miss map[string]any, actor map[string]any, prov map[string]any) {
		id := miss["miss_id"].(string)
		body := w.body(w.rule, 1, actor, prov, seenAt)
		body["miss_id"] = id
		w.ok(w.send("hive-memory", "miss", body, request{version: "1.0", expected: []rev{{"pool.miss", id, miss["record_revision"].(int64)}}}))
	}
	correct(x, a, accountOf("incident_c", "derived", recordRef("pool.miss", y["miss_id"].(string))))
	row := w.counted()
	// x and y are a cycle and count nothing; the root still counts.
	if row["independent_misses"] != int64(1) || len(row["misses"].([]any)) != 1 || row["misses"].([]any)[0] != root["miss_id"] {
		t.Errorf("a cycle contributed: %v", row)
	}
	// Reconciled, the derivation counts again: the account derives from the root.
	correct(map[string]any{"miss_id": x["miss_id"], "record_revision": int64(2)}, a, accountOf("incident_c", "derived", recordRef("pool.miss", root["miss_id"].(string))))
	if row := w.counted(); row["independent_misses"] != int64(1) || len(row["misses"].([]any)) != 3 {
		t.Errorf("after the reconciliation: %v", row)
	}
}

func TestUnknownIndependenceIsNeverCountedAsIndependent(t *testing.T) {
	w := newMissWorld(t)
	_, a := workerActor(t)
	_, b := workerActor(t)
	_, c := workerActor(t)
	unknown1 := w.missed(a, accountOf("incident_a", "unknown"))
	w.missed(b, accountOf("incident_b", "unknown"))
	row := w.counted()
	if row["independent_misses"] != int64(0) || row["decision_due"] != false || len(row["misses"].([]any)) != 0 {
		t.Errorf("unknown independence counted: %v", row)
	}
	// An account derived from an unknown root contributes nothing either.
	w.missed(c, accountOf("incident_a", "derived", recordRef("pool.miss", unknown1["miss_id"].(string))))
	if row := w.counted(); row["independent_misses"] != int64(0) {
		t.Errorf("a derivation from an unknown root counted: %v", row)
	}
	// One independent incident beside the unknown ones is one, and no decision.
	w.missed(a, accountOf("incident_d", "independent"))
	if row := w.counted(); row["independent_misses"] != int64(1) || row["decision_due"] != false {
		t.Errorf("one independent incident: %v", row)
	}
}

func TestIncidentIdentityIsHomeSchemaAndRecordNotRevisionOrDigest(t *testing.T) {
	w := newMissWorld(t)
	_, a := workerActor(t)
	_, b := workerActor(t)
	_, c := workerActor(t)
	at := func(revision int64, digest string) map[string]any {
		p := accountOf("incident_a", "independent")
		inc := p["incident"].(map[string]any)
		inc["record_revision"], inc["digest"] = revision, "sha256:"+strings.Repeat(digest, 64)
		return p
	}
	w.missed(a, at(1, "1"))
	w.missed(b, at(2, "2"))
	if row := w.counted(); row["independent_misses"] != int64(1) {
		t.Errorf("a revision and a digest made another incident: %v", row)
	}
	// An incident of another home is another incident, and one with no home is the
	// reporting home's.
	other := accountOf("incident_a", "independent")
	other["incident"].(map[string]any)["home_id"] = mint(t, ids.Home)
	w.missed(c, other)
	if row := w.counted(); row["independent_misses"] != int64(2) {
		t.Errorf("an incident of another home: %v", row)
	}
	_, d := workerActor(t)
	bare := accountOf("incident_a", "independent")
	delete(bare["incident"].(map[string]any), "home_id")
	w.missed(d, bare)
	if row := w.counted(); row["independent_misses"] != int64(2) {
		t.Errorf("an incident with no home is the reporting home's: %v", row)
	}
}

func TestMissAndCheckPlaceNoHoldAndWriteNoQuestion(t *testing.T) {
	w := newMissWorld(t)
	_, a := workerActor(t)
	_, b := workerActor(t)
	w.missed(a, accountOf("incident_a", "independent"))
	w.missed(b, accountOf("incident_b", "independent"))
	w.unchanged(func() {
		if row := w.counted(); row["decision_due"] != true {
			t.Fatalf("subject row %v", row)
		}
	})
	for _, kind := range []string{"hold", "question", "item", "decision-round"} {
		entries, err := os.ReadDir(filepath.Join(w.dir, "pool", "records", kind))
		if err == nil {
			for _, e := range entries {
				if kind == "question" && strings.HasPrefix(e.Name(), "q_01jc7d8e9f0g1h2j3k4m5n6p7q") {
					continue
				}
				t.Errorf("the Pool wrote %s/%s for a second miss", kind, e.Name())
			}
		}
	}
}

func TestAMissRefusesWhatItCannotRecord(t *testing.T) {
	w := newMissWorld(t)
	_, actor := workerActor(t)
	absent := mint(t, ids.Rule)
	product := w.product()
	for _, c := range []struct {
		name, code string
		body       map[string]any
	}{
		{"a rule the Pool does not hold", envelope.CodeNotFound, w.body(absent, 1, actor, accountOf("task_a", "independent"), seenAt)},
		{"a revision the rule has not reached", envelope.CodeNotFound, w.body(w.rule, 9, actor, accountOf("task_a", "independent"), seenAt)},
		{"a product rule, which has no filing subject", "pool.subject_invalid", w.body(product, 1, actor, accountOf("task_a", "independent"), seenAt)},
	} {
		t.Run(c.name, func(t *testing.T) {
			w.unchanged(func() {
				resp := w.send("hive-memory", "miss", c.body, request{version: "1.0"})
				if resp.Status == envelope.Ok || resp.Error == nil || resp.Error.Code != c.code || len(resp.Receipts) != 0 {
					t.Errorf("status %s, error %+v, receipts %v; want %s", resp.Status, resp.Error, resp.Receipts, c.code)
				}
			})
		})
	}
}

// product plants a product rule.
func (w *missWorld) product() string {
	id := mint(w.t, ids.Rule)
	_, rec := ruleRecord(w.t, "subject", w.subject, hiveScope, R{"rule_id": id, "kind": "product", "governs": map[string]any{"stage": int64(2)}, "specifications": []any{}})
	for _, member := range []string{"tier", "filed_under", "rule_line"} {
		delete(rec, member)
	}
	w.plant("rule", id, rec)
	return id
}

// The Pool's own refusals that cite a rule, each through the writer that makes
// it: the Pool reports the refusal as a miss when the request names an acting
// session or task generation that did not receive the rule.

func opBody(subject, filedUnder, line string) map[string]any {
	return map[string]any{
		"subject": subject, "words": "The Hive Mind's words, kept whole.", "origin": map[string]any{"kind": "ruling"},
		"applies": []any{}, "kind": "operating", "tier": "subject", "filed_under": filedUnder, "rule_line": line,
	}
}

func productBody() map[string]any {
	return map[string]any{
		"subject": "stage.ruling", "words": "The ruling.", "origin": map[string]any{"kind": "ruling"},
		"applies": []any{}, "kind": "product", "governs": map[string]any{"stage": int64(2)},
	}
}

func (h *home) recordAs(caller map[string]any, body map[string]any) *envelope.Response {
	h.t.Helper()
	return h.sendAs(caller, "hive-rule", "record", body, request{version: "2.0"})
}

// reportedMiss checks that the response is the refusal it was, now carrying the
// receipts of the misses, and returns the miss of the rule for the actor.
func (w *missWorld) reportedMiss(resp *envelope.Response, code string, actor map[string]any, rule string, revision int64) map[string]any {
	w.t.Helper()
	if resp.Status != envelope.Refused || resp.Error == nil || resp.Error.Code != code || resp.Body != nil {
		w.t.Fatalf("status %s, error %+v; want refused %s", resp.Status, resp.Error, code)
	}
	for _, k := range kindsOf(resp) {
		if k != missedKind {
			w.t.Fatalf("receipts %v, want only receipts of a miss", kindsOf(resp))
		}
	}
	var found map[string]any
	for _, m := range w.misses() {
		if m["rule"].(map[string]any)["rule_id"] == rule {
			found = m
		}
	}
	if found == nil {
		w.t.Fatalf("no miss of %s among %v", rule, w.misses())
	}
	prov := found["provenance"].(map[string]any)
	incident := prov["incident"].(map[string]any)
	if found["subject_id"] != w.subject || found["rule"].(map[string]any)["record_revision"] != revision || !equal(found["actor"], actor) ||
		found["step"] != "dispatch" || prov["independence"] != "independent" || len(prov["derived_from"].([]any)) != 0 ||
		incident["schema"] != "hive.request" || incident["home_id"] != homeID || found["observed_at"] != seenAt {
		w.t.Errorf("the miss is %v", found)
	}
	return found
}

// thePoolReports runs a refusal as four callers: one that names no actor, one
// that names an actor who was delivered every rule the refusal cites, and a
// session and a task generation that were not. Only the last two leave misses.
func (w *missWorld) thePoolReports(t *testing.T, code string, cited []string, refuse func(caller map[string]any) *envelope.Response) {
	t.Helper()
	w.unchanged(func() {
		resp := refuse(map[string]any{"kind": "test"})
		if resp.Status != envelope.Refused || resp.Error == nil || resp.Error.Code != code || len(resp.Receipts) != 0 {
			t.Fatalf("a caller naming no actor: status %s, error %+v, receipts %v", resp.Status, resp.Error, resp.Receipts)
		}
	})
	informed, informedActor := workerActor(t)
	named := []map[string]any{}
	for _, id := range cited {
		named = append(named, ruleAt(id, 1))
	}
	w.delivered(informedActor, seenBefore, named...)
	w.unchanged(func() {
		resp := refuse(informed)
		if resp.Status != envelope.Refused || resp.Error == nil || resp.Error.Code != code || len(resp.Receipts) != 0 {
			t.Fatalf("an actor who was delivered the rules: status %s, error %+v, receipts %v", resp.Status, resp.Error, resp.Receipts)
		}
	})
	caller, actor := sessionActor(t)
	resp := refuse(caller)
	for _, id := range cited {
		w.reportedMiss(resp, code, actor, id, 1)
	}
	if len(resp.Receipts) != len(cited) {
		t.Errorf("receipts %v, want one for each of the %d rules the refusal cites", kindsOf(resp), len(cited))
	}
	// The refusal's own answer is what it was: the same code and the rules it names.
	if d := resp.Error.Detail; d == nil || d["rules"] == nil {
		t.Errorf("the refusal lost its detail: %+v", resp.Error)
	}
	// A task generation is reported as well.
	workerCaller, workerWho := workerActor(t)
	again := refuse(workerCaller)
	if again.Status != envelope.Refused || len(again.Receipts) != len(cited) {
		t.Errorf("a worker's refusal: status %s, receipts %v", again.Status, kindsOf(again))
	}
	reported := 0
	for _, m := range w.misses() {
		if equal(m["actor"], workerWho) {
			reported++
		}
	}
	if reported != len(cited) {
		t.Errorf("the worker's generation has %d misses, want %d", reported, len(cited))
	}
}

func TestRecordRefusedForAConflictReportsTheStandingRuleAsMissed(t *testing.T) {
	// hive-rule record, pool.rule_conflict from the resolver's reading of the rules after the write.
	w := newMissWorld(t)
	standing := w.home.rule("subject", w.subject, hiveScope, R{"subject": "dispatch.cap"})
	w.thePoolReports(t, "pool.rule_conflict", []string{standing}, func(caller map[string]any) *envelope.Response {
		return w.recordAs(caller, opBody("dispatch.cap", w.subject, "Dispatch at most six Swarms."))
	})
}

func TestAmendRefusedForAConflictReportsTheRulesNamedAsMissed(t *testing.T) {
	// hive-rule amend, pool.rule_conflict from the resolver's reading at a later instant.
	w := newMissWorld(t)
	temp := opBody("dispatch.cap", w.subject, "Dispatch at most four Swarms.")
	temp["expires_at"] = d15
	tempRule := w.ok(w.recordAs(map[string]any{"kind": "test"}, temp))["rule"].(map[string]any)
	next := opBody("dispatch.cap", w.subject, "Dispatch at most six Swarms.")
	next["effective_at"] = d20
	nextRule := w.ok(w.recordAs(map[string]any{"kind": "test"}, next))["rule"].(map[string]any)
	extend := func(caller map[string]any) *envelope.Response {
		return w.sendAs(caller, "hive-rule", "amend", map[string]any{
			"rule_id": tempRule["rule_id"], "kind": "extend", "words": "Longer.", "new_expires_at": d25,
		}, request{version: "1.0", expected: []rev{{"pool.rule", tempRule["rule_id"].(string), tempRule["record_revision"].(int64)}}})
	}
	w.thePoolReports(t, "pool.rule_conflict", []string{tempRule["rule_id"].(string), nextRule["rule_id"].(string)}, extend)
	// The refusal names two rules of one subject in one incident: two misses, one incident.
	got := map[string]bool{}
	for _, m := range w.misses() {
		got[m["rule"].(map[string]any)["rule_id"].(string)] = true
	}
	if !got[tempRule["rule_id"].(string)] || !got[nextRule["rule_id"].(string)] {
		t.Errorf("misses of %v, want both rules the refusal names", got)
	}
	// The same actor met one refusal: one incident, one count.
	if row := w.counted(); row == nil || row["decision_due"] != false {
		t.Errorf("subject row %v", row)
	}
}

func TestAProductConflictRefusalNamesRulesThatHaveNoFilingSubjectAndLeavesNoMiss(t *testing.T) {
	// hive-rule record and amend, pool.rule_conflict between product rules.
	w := newMissWorld(t)
	caller, _ := sessionActor(t)
	w.recordAs(map[string]any{"kind": "test"}, productBody())
	w.unchanged(func() {
		resp := w.recordAs(caller, productBody())
		if resp.Status != envelope.Refused || resp.Error == nil || resp.Error.Code != "pool.rule_conflict" || len(resp.Receipts) != 0 {
			t.Errorf("record: status %s, error %+v, receipts %v", resp.Status, resp.Error, resp.Receipts)
		}
	})
	temp := productBody()
	temp["subject"], temp["expires_at"] = "stage.other", d15
	tempRule := w.ok(w.recordAs(map[string]any{"kind": "test"}, temp))["rule"].(map[string]any)
	later := productBody()
	later["subject"], later["effective_at"] = "stage.other", d20
	w.ok(w.recordAs(map[string]any{"kind": "test"}, later))
	w.unchanged(func() {
		resp := w.sendAs(caller, "hive-rule", "amend", map[string]any{
			"rule_id": tempRule["rule_id"], "kind": "extend", "words": "Longer.", "new_expires_at": d25,
		}, request{version: "1.0", expected: []rev{{"pool.rule", tempRule["rule_id"].(string), tempRule["record_revision"].(int64)}}})
		if resp.Status != envelope.Refused || resp.Error == nil || resp.Error.Code != "pool.rule_conflict" || len(resp.Receipts) != 0 {
			t.Errorf("amend: status %s, error %+v, receipts %v", resp.Status, resp.Error, resp.Receipts)
		}
	})
}

func TestARecordNamingARuleOfAnotherConflictKeyIsRefusedAsInvalidAndLeavesNoMiss(t *testing.T) {
	// hive-rule record, schema_violation from changes naming a rule of another
	// conflict key: a refusal with the status invalid, which leaves no write.
	w := newMissWorld(t)
	standing := w.stored("rule", w.rule)
	caller, _ := sessionActor(t)
	body := opBody("another.key", w.subject, "A line.")
	body["changes"] = map[string]any{"rule_id": w.rule, "record_revision": standing["record_revision"], "relation": "supersedes"}
	w.unchanged(func() {
		resp := w.recordAs(caller, body)
		if resp.Status != envelope.Invalid || resp.Error == nil || resp.Error.Code != envelope.CodeSchemaViolation || len(resp.Receipts) != 0 {
			t.Errorf("status %s, error %+v, receipts %v", resp.Status, resp.Error, resp.Receipts)
		}
	})
}
