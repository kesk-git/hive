package budget_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
)

// The acceptance test of the budget record's writer (spec/pool.md, Generated
// knowledge, the budget record). It drives hive-memory budget as the later lane
// registers it on the writer, in process, over a home of its own seeded from the
// contract's record fixtures, and holds the writer to the contract: a change
// accepted on a word that stands with its before and after entries appended to
// `changes`, any other authority and a word that does not stand refused with
// nothing written, a stale expected revision and an unknown projection refused,
// and `defaults` never changed.

// The projections the seeded configuration holds an entry for.
var (
	hive    = "mem_01jc7d8e9f0g1h2j3k4m5n6p71"
	subject = "mem_01jc7d8e9f0g1h2j3k4m5n6p72"
	fleet   = "mem_01jc7d8e9f0g1h2j3k4m5n6p73"
	unknown = "mem_01jc7d8e9f0g1h2j3k4m5n6p7z"
)

func entryOf(id string, max int64) map[string]any {
	return map[string]any{"projection_id": id, "max_bytes": max}
}

// intake is the receipt of the standing word that the request names.
var intake = map[string]any{"schema": "word.intake", "id": "rcpt_01jc7d8e9f0g1h2j3k4m5n6p7v"}

// seed plants the budget configuration, with an entry for three projections and
// one change already recorded, and a question that is the Hive Mind's word at
// the Hive's scope.
func seed(t testing.TB, question string) *home {
	h := newHome(t)
	rec := fixture(t, "pool.budget", "changed")
	rec["home_id"], rec["record_revision"] = homeID, int64(1)
	rec["projections"] = []any{entryOf(hive, 18432), entryOf(subject, 9216), entryOf(fleet, 4608)}
	h.plant("budget", "memory", rec)
	if question != "" {
		h.plant("question", questionID, hiveQuestion(t, question))
	}
	return h
}

// hiveQuestion is a question fixture as the Hive's own: the word of the Hive
// Mind at the Hive's scope, which is the scope of the budget.
func hiveQuestion(t testing.TB, name string) map[string]any {
	q := fixture(t, "pool.question", name)
	q["scope_level"] = "hive"
	delete(q, "workspace_id")
	delete(q, "project_id")
	return q
}

// A change on a word that stands replaces the named entries, appends its before
// and after entries, authority, intake, request and receipt to `changes`, and
// changes nothing else.
func TestABudgetChangeOnAStandingWordIsRecorded(t *testing.T) {
	h := seed(t, "answered-superseding")
	before := h.stored("budget", "memory")
	files := h.records()

	req := request{authority: word(3), expected: []rev{{"pool.budget", "memory", 1}}}
	resp := h.budget(req, []map[string]any{entryOf(subject, 4096), entryOf(hive, 20000)}, intake)
	if resp.Status != envelope.Ok {
		t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
	}
	stored := h.stored("budget", "memory")
	if got, _ := resp.Body["budget"].(map[string]any); !equal(got, stored) {
		t.Errorf("the response is not the record: %v", got)
	}
	if stored["record_revision"] != int64(2) {
		t.Errorf("record_revision %v", stored["record_revision"])
	}
	if want := []any{entryOf(hive, 20000), entryOf(subject, 4096), entryOf(fleet, 4608)}; !equal(stored["projections"], want) {
		t.Errorf("projections %v, want %v", stored["projections"], want)
	}
	if !equal(stored["defaults"], before["defaults"]) {
		t.Errorf("defaults changed: %v", stored["defaults"])
	}

	changes := stored["changes"].([]any)
	if len(changes) != 2 || !equal(changes[0], before["changes"].([]any)[0]) {
		t.Fatalf("changes %v", changes)
	}
	c := changes[1].(map[string]any)
	if c["sequence"] != int64(2) || c["request_id"] != resp.RequestID || c["recorded_at"] == nil {
		t.Errorf("change %v", c)
	}
	if !equal(c["before"], []any{entryOf(subject, 9216), entryOf(hive, 18432)}) || !equal(c["after"], []any{entryOf(subject, 4096), entryOf(hive, 20000)}) {
		t.Errorf("before %v, after %v", c["before"], c["after"])
	}
	if want := word(3); !equal(c["authority"], want) {
		t.Errorf("authority %v, want %v", c["authority"], want)
	}
	if !equal(c["intake"], intake) {
		t.Errorf("intake %v", c["intake"])
	}

	// One receipt, which the change names, about the record at its new revision.
	if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "pool.budget.changed" {
		t.Fatalf("receipts %+v", resp.Receipts)
	}
	r := resp.Receipts[0]
	if c["receipt_id"] != r.ReceiptID || r.Subject == nil || r.Subject.Schema != "pool.budget" || r.Subject.ID != "memory" || r.Subject.RecordRevision != 2 {
		t.Errorf("the change names receipt %v; the receipt is %+v", c["receipt_id"], r)
	}
	var added, changed []string
	for k, v := range h.records() {
		if old, ok := files[k]; !ok {
			added = append(added, k)
		} else if old != v {
			changed = append(changed, k)
		}
	}
	if !slices.Equal(changed, []string{"pool/records/budget/memory.json"}) || len(added) != 1 || !strings.HasPrefix(added[0], "pool/receipts/") {
		t.Errorf("the request changed %v and added %v", changed, added)
	}

	// A second change appends to the first two and reads the first as its before.
	again := h.budget(request{authority: word(3), expected: []rev{{"pool.budget", "memory", 2}}}, []map[string]any{entryOf(hive, 18432)}, intake)
	if again.Status != envelope.Ok {
		t.Fatalf("status %s, error %+v", again.Status, again.Error)
	}
	last := h.stored("budget", "memory")["changes"].([]any)[2].(map[string]any)
	if last["sequence"] != int64(3) || !equal(last["before"], []any{entryOf(hive, 20000)}) || !equal(last["after"], []any{entryOf(hive, 18432)}) {
		t.Errorf("change %v", last)
	}
}

// Any authority that is not of kind word, a policy request, a temporary rule or
// an instruction among them, is authority_insufficient, and nothing is written.
func TestABudgetChangeRefusesEveryOtherAuthority(t *testing.T) {
	h := seed(t, "answered-superseding")
	for name, auth := range map[string]map[string]any{
		"none":                       authority("none", "", "", 0),
		"a policy request":           authority("posture", "pool.rule", "rule_01jc7d8e9f0g1h2j3k4m5n6p7e", 1),
		"a charter":                  authority("charter", "dispatch.charter", "charter_1", 1),
		"a temporary rule":           authority("rule", "pool.rule", "rule_01jc7d8e9f0g1h2j3k4m5n6p7e", 1),
		"an instruction":             authority("instruction", "dispatch.instruction", "instr_1", 1),
		"the question as a non-word": authority("instruction", "pool.question", questionID, 3),
	} {
		t.Run(name, func(t *testing.T) {
			h.unchanged(func() {
				resp := h.budget(request{authority: auth}, []map[string]any{entryOf(hive, 20000)}, intake)
				refused(t, resp, envelope.Refused, envelope.CodeAuthorityInsufficient)
			})
		})
	}
}

// A word that does not stand changes nothing, one superseded since the revision
// it names among them.
func TestABudgetChangeRefusesAWordThatDoesNotStand(t *testing.T) {
	for name, tc := range map[string]struct {
		question string
		auth     map[string]any
		reasons  []string
	}{
		"an open question":                        {"open-with-options", word(1), []string{"unanswered"}},
		"a deferred question":                     {"deferred-by-later", word(2), []string{"deferred"}},
		"a moot question":                         {"moot-question-replaced", word(2), []string{"moot"}},
		"an answer superseded since the revision": {"answered-superseding", word(2), []string{"superseded"}},
		"a question that does not exist":          {"", word(1), []string{"not-found"}},
	} {
		t.Run(name, func(t *testing.T) {
			h := seed(t, tc.question)
			h.unchanged(func() {
				resp := h.budget(request{authority: tc.auth}, []map[string]any{entryOf(hive, 20000)}, intake)
				refused(t, resp, envelope.Refused, envelope.CodeAuthorityInsufficient)
				if got := reasons(resp); !slices.Equal(got, tc.reasons) {
					t.Errorf("reasons %v, want %v", got, tc.reasons)
				}
			})
		})
	}
	t.Run("a question of a project, for the Hive's budget", func(t *testing.T) {
		h := seed(t, "")
		h.plant("question", questionID, fixture(t, "pool.question", "answered-superseding"))
		h.unchanged(func() {
			resp := h.budget(request{authority: word(3)}, []map[string]any{entryOf(hive, 20000)}, intake)
			refused(t, resp, envelope.Refused, envelope.CodeAuthorityInsufficient)
			if got := reasons(resp); !slices.Equal(got, []string{"out-of-scope"}) {
				t.Errorf("reasons %v", got)
			}
		})
	})
}

// A stale expected revision and an unknown projection are refused, and so is a
// request that names a projection twice or a scope narrower than the home's;
// nothing is applied in part.
func TestABudgetChangeChecksItsRevisionAndItsProjections(t *testing.T) {
	h := seed(t, "answered-superseding")
	h.unchanged(func() {
		stale := request{authority: word(3), expected: []rev{{"pool.budget", "memory", 7}}}
		refused(t, h.budget(stale, []map[string]any{entryOf(hive, 20000)}, intake), envelope.Conflict, envelope.CodeRevisionConflict)

		// A projection of no entry, beside one that has: the whole request is refused.
		resp := h.budget(request{authority: word(3)}, []map[string]any{entryOf(hive, 20000), entryOf(unknown, 1000)}, intake)
		refused(t, resp, envelope.NotFound, envelope.CodeNotFound)
		if resp.Error.Detail["projection_id"] != unknown {
			t.Errorf("detail %v", resp.Error.Detail)
		}

		refused(t, h.budget(request{authority: word(3)}, []map[string]any{entryOf(hive, 20000), entryOf(hive, 21000)}, intake), envelope.Invalid, envelope.CodeSchemaViolation)
		refused(t, h.budget(request{authority: word(3), scope: projectScope}, []map[string]any{entryOf(hive, 20000)}, intake), envelope.Refused, envelope.CodeScopeMismatch)
	})
}

// `defaults` is no member of the request, so no request can change it.
func TestNoBudgetRequestChangesTheDefaults(t *testing.T) {
	h := seed(t, "answered-superseding")
	h.unchanged(func() {
		body := map[string]any{
			"projections": []any{entryOf(hive, 20000)}, "intake": intake,
			"defaults": map[string]any{"hive_always": int64(1), "fleet_always": int64(1), "subject": int64(1)},
		}
		refused(t, h.send("hive-memory", "budget", body, request{authority: word(3)}), envelope.Invalid, envelope.CodeSchemaViolation)
	})
	if resp := h.budget(request{authority: word(3)}, []map[string]any{entryOf(fleet, 9000)}, intake); resp.Status != envelope.Ok {
		t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
	}
	d := h.stored("budget", "memory")["defaults"].(map[string]any)
	if d["hive_always"] != int64(18432) || d["fleet_always"] != int64(4608) || d["subject"] != int64(9216) {
		t.Errorf("defaults %v", d)
	}
}

// A home with no budget configuration has none to change.
func TestABudgetChangeNeedsTheConfiguration(t *testing.T) {
	h := newHome(t)
	h.plant("question", questionID, hiveQuestion(t, "answered-superseding"))
	h.unchanged(func() {
		refused(t, h.budget(request{authority: word(3)}, []map[string]any{entryOf(hive, 20000)}, intake), envelope.NotFound, envelope.CodeNotFound)
	})
}
