package admission_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
)

// The acceptance test of hive-admit approve (spec/pool.md, Admission, the
// approve bullet). It drives the executable the default registry describes, in
// process, over a home of its own seeded from the contract's record fixtures,
// and holds the operation to the contract: an approval recorded on a word
// authority that stands, and nothing written for an authority of any other
// kind or a word that does not stand.

// seed plants the admission record fixture and the question fixture the word
// authority names.
func seed(t testing.TB, admission, question string) *home {
	h := newHome(t)
	h.plant("admission", admissionID, fixture(t, "pool.admission", admission))
	if question != "" {
		h.plant("question", questionID, fixture(t, "pool.question", question))
	}
	return h
}

// An approval is recorded on a word that stands, with the receipt, and the
// record changes in nothing else.
func TestApproveRecordsTheApprovalOnAStandingWord(t *testing.T) {
	for name, tc := range map[string]struct {
		question string
		revision int64
	}{
		"an answered question at the revision of its applied answer": {"answered-superseding", 3},
		"a released question": {"released", 2},
	} {
		t.Run(name, func(t *testing.T) {
			h := seed(t, "created-no-checks", tc.question)
			before := h.stored("admission", admissionID)
			files := h.records()

			resp := h.approve(request{authority: word(tc.revision)}, admissionID)
			if resp.Status != envelope.Ok {
				t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
			}
			stored := h.stored("admission", admissionID)
			if got, _ := resp.Body["admission"].(map[string]any); !equalTree(got, stored) {
				t.Errorf("the response is not the record: %v", got)
			}

			if stored["record_revision"] != int64(2) {
				t.Errorf("record_revision %v", stored["record_revision"])
			}
			approval, _ := stored["approval"].(map[string]any)
			if approval == nil || approval["request_id"] != resp.RequestID || approval["recorded_at"] == nil {
				t.Fatalf("approval %v", approval)
			}
			auth, _ := approval["authority"].(map[string]any)
			ref, _ := auth["ref"].(map[string]any)
			if auth["kind"] != "word" || ref["schema"] != "pool.question" || ref["id"] != questionID || ref["record_revision"] != int64(tc.revision) {
				t.Errorf("the approval's authority %v", auth)
			}
			// Nothing else of the record changes but what a write sets.
			for k, v := range before {
				switch k {
				case "record_revision", "updated_at", "writer":
				default:
					if !equalTree(v, stored[k]) {
						t.Errorf("%s changed: %v became %v", k, v, stored[k])
					}
				}
			}
			for k := range stored {
				if _, ok := before[k]; !ok && k != "approval" {
					t.Errorf("%s was added", k)
				}
			}
			if stored["state"] != "created" || len(stored["checks"].([]any)) != 0 {
				t.Errorf("the approval ran a check: state %v, checks %v", stored["state"], stored["checks"])
			}

			// One receipt, about the record at its new revision; no other file.
			if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "pool.admission.approved" {
				t.Fatalf("receipts %+v", resp.Receipts)
			}
			s := resp.Receipts[0].Subject
			if s == nil || s.Schema != "pool.admission" || s.ID != admissionID || s.RecordRevision != 2 {
				t.Errorf("the receipt's subject %+v", s)
			}
			if r := resp.Receipts[0]; r.Authority.Kind != "word" || r.Scope.TaskID != before["task_id"] {
				t.Errorf("the receipt carries authority %+v and scope %+v", r.Authority, r.Scope)
			}
			var added, changed []string
			for k, v := range h.records() {
				if old, ok := files[k]; !ok {
					added = append(added, k)
				} else if old != v {
					changed = append(changed, k)
				}
			}
			if !slices.Equal(changed, []string{"pool/records/admission/" + admissionID + ".json"}) || len(added) != 1 || !strings.HasPrefix(added[0], "pool/receipts/") {
				t.Errorf("the request changed %v and added %v", changed, added)
			}
		})
	}
}

func equalTree(a, b any) bool {
	switch a := a.(type) {
	case map[string]any:
		b, ok := b.(map[string]any)
		return ok && len(a) == len(b) && !slices.ContainsFunc(slices.Collect(maps.Keys(a)), func(k string) bool { return !equalTree(a[k], b[k]) })
	case []any:
		b, ok := b.([]any)
		return ok && slices.EqualFunc(a, b, equalTree)
	}
	return a == b
}

// Any other kind of authority is authority_insufficient, and nothing is
// written, whatever record it names.
func TestApproveRefusesEveryOtherKindOfAuthority(t *testing.T) {
	h := seed(t, "created-no-checks", "answered-superseding")
	for name, auth := range map[string]map[string]any{
		"none":        authority("none", "", "", 0),
		"posture":     authority("posture", "pool.rule", "rule_01jc7d8e9f0g1h2j3k4m5n6p7e", 1),
		"charter":     authority("charter", "dispatch.charter", "charter_1", 1),
		"rule":        authority("rule", "pool.rule", "rule_01jc7d8e9f0g1h2j3k4m5n6p7e", 1),
		"instruction": authority("instruction", "dispatch.instruction", "instr_1", 1),
		// Naming the question that stands does not make a word of it.
		"a question named under another kind": authority("rule", "pool.question", questionID, 3),
	} {
		t.Run(name, func(t *testing.T) {
			h.unchanged(func() {
				refused(t, h.approve(request{authority: auth}, admissionID), envelope.Refused, envelope.CodeAuthorityInsufficient)
			})
		})
	}
	// The word that stands is still accepted after the refusals.
	if resp := h.approve(request{authority: word(3)}, admissionID); resp.Status != envelope.Ok {
		t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
	}
}

// A word authority that does not stand is authority_insufficient, naming why,
// and nothing is written.
func TestApproveRefusesAWordThatDoesNotStand(t *testing.T) {
	other := authority("word", "pool.question", questionID, 1)
	other["ref"].(map[string]any)["home_id"] = "home_01jc7d8e9f0g1h2j3k4m5n6p7z"
	wrongSchema := authority("word", "pool.item", questionID, 1)
	for name, tc := range map[string]struct {
		question string
		auth     map[string]any
		scope    map[string]any
		reasons  []string
	}{
		"an open question":                        {"open-with-options", word(1), nil, []string{"unanswered"}},
		"a deferred question":                     {"deferred-by-later", word(2), nil, []string{"deferred"}},
		"a moot question":                         {"moot-question-replaced", word(2), nil, []string{"moot"}},
		"an answer superseded since the revision": {"answered-superseding", word(2), nil, []string{"superseded"}},
		"an answer first applied after it":        {"answered-superseding", word(1), nil, []string{"unanswered"}},
		"a revision the question has not reached": {"answered-superseding", word(9), nil, []string{"malformed"}},
		"a question outside the request's scope":  {"answered-superseding", word(3), hiveScope, []string{"out-of-scope"}},
		"a question that does not exist":          {"", word(1), nil, []string{"not-found"}},
		"a question of another home":              {"answered-superseding", other, nil, []string{"not-found"}},
		"a reference to another schema":           {"answered-superseding", wrongSchema, nil, []string{"malformed"}},
	} {
		t.Run(name, func(t *testing.T) {
			h := seed(t, "created-no-checks", tc.question)
			h.unchanged(func() {
				resp := h.approve(request{authority: tc.auth, scope: tc.scope}, admissionID)
				refused(t, resp, envelope.Refused, envelope.CodeAuthorityInsufficient)
				if got := reasons(resp); !slices.Equal(got, tc.reasons) {
					t.Errorf("reasons %v, want %v", got, tc.reasons)
				}
			})
			if _, ok := h.stored("admission", admissionID)["approval"]; ok {
				t.Errorf("the admission record carries an approval")
			}
		})
	}
}

// The approval is of an admission record that exists, at the revision the
// request expects.
func TestApproveNamesAnExistingAdmissionAtItsRevision(t *testing.T) {
	h := seed(t, "created-no-checks", "answered-superseding")
	h.unchanged(func() {
		refused(t, h.approve(request{authority: word(3)}, "adm_01jc7d8e9f0g1h2j3k4m5n6p7z"), envelope.NotFound, envelope.CodeNotFound)
		stale := request{authority: word(3), expected: []rev{{"pool.admission", admissionID, 7}}}
		refused(t, h.approve(stale, admissionID), envelope.Conflict, envelope.CodeRevisionConflict)
	})
	current := request{authority: word(3), expected: []rev{{"pool.admission", admissionID, 1}}}
	if resp := h.approve(current, admissionID); resp.Status != envelope.Ok {
		t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
	}
}
