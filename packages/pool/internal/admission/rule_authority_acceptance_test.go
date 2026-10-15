package admission_test

import (
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// A standing rule clears a sensitive boundary before launch only when it is the
// Hive Mind's own sentence recorded as a rule: its subject names that one
// boundary, it is in force when the work is checked, and its scope contains the
// admission's.

var boundaries = []string{"spend", "deletion", "security", "publication"}

// ruleOn plants a rule of the Hive's scope on the subject, in force and
// applied by a word that stands, after the change given, and returns the
// authority that names it.
func (h *home) ruleOn(t *testing.T, subject string, change func(rule map[string]any)) map[string]any {
	t.Helper()
	rule := fixture(t, "pool.rule_v2", "minimal")
	rule["record_revision"], rule["home_id"], rule["subject"] = int64(1), homeID, subject
	rule["authority"] = word(3)
	if change != nil {
		change(rule)
	}
	h.plant("rule", rule["rule_id"].(string), rule)
	return authority("rule", "pool.rule", rule["rule_id"].(string), 1)
}

// hiveQuestion makes the answered question the Hive's, so that a rule of any
// scope of the home can rest on it.
func (h *home) hiveQuestion(t *testing.T) {
	t.Helper()
	q := fixture(t, "pool.question", "answered-superseding")
	q["scope_level"] = "hive"
	delete(q, "workspace_id")
	delete(q, "project_id")
	h.plant("question", questionID, q)
}

// heldBecause checks the spend boundary is held on authority_required, with a
// finding that gives the clause for the rule it did not accept.
func heldBecause(t *testing.T, h *home, id, clause string) {
	t.Helper()
	check := wants(t, h, id, h.check(id, map[string]any{}), heldOn("spend"))
	if message := check["findings"].([]any)[0].(map[string]any)["message"].(string); !strings.Contains(message, clause) {
		t.Errorf("the finding %q does not say %q", message, clause)
	}
}

func heldOn(boundaries ...string) outcome {
	findings := []string{}
	for range boundaries {
		findings = append(findings, "pool.authority_required")
	}
	return outcome{status: envelope.Held, code: "held", result: "held", findings: findings, reason: "authority_required"}
}

func TestARuleOnOneBoundaryClearsThatBoundaryOnly(t *testing.T) {
	h := newHome(t)
	h.work(sensitive(boundaries...))
	h.hiveQuestion(t)
	id := h.admission(request{authority: h.ruleOn(t, "admission.spend", nil)})
	check := wants(t, h, id, h.check(id, map[string]any{}), heldOn("deletion", "security", "publication"))
	for i, b := range boundaries[1:] {
		message := check["findings"].([]any)[i].(map[string]any)["message"].(string)
		if !strings.Contains(message, b) {
			t.Errorf("finding %d names %q, not the %s boundary", i, message, b)
		}
	}
}

func TestARuleOnEachBoundaryAdmitsWorkThatTouchesIt(t *testing.T) {
	for _, b := range boundaries {
		t.Run(b, func(t *testing.T) {
			h := newHome(t)
			h.work(sensitive(b))
			h.hiveQuestion(t)
			id := h.admission(request{authority: h.ruleOn(t, "admission."+b, nil)})
			wants(t, h, id, h.check(id, map[string]any{}), outcome{status: envelope.Ok, result: "admitted", findings: []string{}})
		})
	}
}

func TestARuleNarrowedToThisTaskClears(t *testing.T) {
	h := newHome(t)
	h.work(sensitive("spend"))
	h.hiveQuestion(t)
	id := h.admission(request{authority: h.ruleOn(t, "admission.spend", func(r map[string]any) { r["task_id"] = taskID })})
	wants(t, h, id, h.check(id, map[string]any{}), outcome{status: envelope.Ok, result: "admitted", findings: []string{}})
}

func TestARuleClearsNothingThatDoesNotMeetTheConditions(t *testing.T) {
	lifted := func(rule map[string]any) {
		subject := map[string]any{"schema": "synapse.round", "id": "round_a"}
		receipt := envelope.Receipt{
			ReceiptID: mint(t, ids.Receipt), Kind: "synapse.round.closed", KindVersion: "1.0", WrittenAt: "2026-10-14T08:00:00.000Z",
			Writer:    envelope.Writer{Package: "synapse", Command: "hive-synapse", Operation: "close", ImplementationVersion: "test"},
			RequestID: mint(t, ids.Request), CorrelationID: mint(t, ids.Correlation),
			Scope:     envelope.Scope{Level: "hive", HomeID: homeID},
			Authority: envelope.Authority{Kind: "none"},
			Subject:   &envelope.RecordRef{Schema: "synapse.round", ID: "round_a", HomeID: homeID, RecordRevision: 3},
			Evidence:  []envelope.Evidence{}, Body: map[string]any{},
		}
		data, err := receipt.Encode()
		if err != nil {
			t.Fatal(err)
		}
		whole, err := canon.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		rule["lift"] = map[string]any{"receipt_kind": "synapse.round.closed", "subject": subject}
		rule["amendments"] = []any{map[string]any{
			"sequence": int64(1), "kind": "lift", "words": "The round closed.", "authority": map[string]any{"kind": "none"},
			"evidence":    map[string]any{"schema": "pool.receipt", "id": receipt.ReceiptID, "home_id": homeID, "record_revision": int64(1)},
			"receipt":     whole,
			"recorded_at": "2026-10-14T08:30:00.000Z", "recorded_by": map[string]any{"kind": "test"},
			"request_id": mint(t, ids.Request), "receipt_id": mint(t, ids.Receipt),
		}}
	}
	for name, c := range map[string]struct {
		subject string
		change  func(rule map[string]any)
		why     string
	}{
		"another subject":   {"example.subject", nil, "subject is not admission.spend"},
		"another boundary":  {"admission.deletion", nil, "subject is not admission.spend"},
		"not yet in effect": {"admission.spend", func(r map[string]any) { r["effective_at"] = "2026-10-15T09:00:00.000Z" }, "not in force"},
		"expired":           {"admission.spend", func(r map[string]any) { r["expires_at"] = "2026-10-14T08:00:00.000Z" }, "not in force"},
		"ended":             {"admission.spend", func(r map[string]any) { r["status"] = "ended" }, "not in force"},
		"lifted":            {"admission.spend", lifted, "not in force"},
		"a scope that is another": {"admission.spend", func(r map[string]any) {
			r["scope_level"], r["workspace_id"], r["project_id"] = "project", workspaceID, "proj_01jc7d8e9f0g1h2j3k4m5n6p7d"
		}, "scope does not contain"},
		"another task": {"admission.spend", func(r map[string]any) { r["task_id"] = "task_01jc7d8e9f0g1h2j3k4m5n6p7g" }, "another task"},
		"a generation":                 {"admission.spend", func(r map[string]any) { r["task_id"], r["generation"] = taskID, int64(1) }, "generation"},
		"an authority that is no word": {"admission.spend", func(r map[string]any) { r["authority"] = map[string]any{"kind": "none"} }, "not a word"},
		"a word that no longer stands": {"admission.spend", func(r map[string]any) { r["authority"] = word(1) }, "no longer stands"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHome(t)
			h.work(sensitive("spend"))
			h.hiveQuestion(t)
			id := h.admission(request{authority: h.ruleOn(t, c.subject, c.change)})
			heldBecause(t, h, id, c.why)
		})
	}
	t.Run("a reference naming no readable rule", func(t *testing.T) {
		h := newHome(t)
		h.work(sensitive("spend"))
		h.hiveQuestion(t)
		id := h.admission(request{authority: authority("rule", "pool.rule", "rule_01jc7d8e9f0g1h2j3k4m5n6p7e", 1)})
		heldBecause(t, h, id, "names no rule")
	})
	t.Run("a reference at a revision beyond the rule's", func(t *testing.T) {
		h := newHome(t)
		h.work(sensitive("spend"))
		h.hiveQuestion(t)
		ref := h.ruleOn(t, "admission.spend", nil)
		ref["ref"].(map[string]any)["record_revision"] = int64(2)
		id := h.admission(request{authority: ref})
		heldBecause(t, h, id, "has not reached")
	})
}
