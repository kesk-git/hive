package decision

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The codecs of the question and decision-round records (spec/word.md, A
// question's life): a record decodes to its typed form and encodes back to the
// same document, for every valid record of the contract's fixtures, and what
// is not one of the schema's records does not decode.

func fixtures(t testing.TB, kind string) map[string][]byte {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "..", "contract", "testdata", "schemas", "records_pool."+kind, "valid")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("fixtures of %s: %v", kind, err)
	}
	out := map[string][]byte{}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = data
	}
	return out
}

func canonical(t testing.TB, v any) string {
	t.Helper()
	out, err := canon.Encode(map[string]any{"v": v})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// Every valid question round trips, as a tree and as a document, and what is
// encoded is still a record of its schema, stored under its identity.
func TestAQuestionRoundTrips(t *testing.T) {
	kind, _ := poolstore.Lookup("question")
	for name, data := range fixtures(t, "question") {
		t.Run(name, func(t *testing.T) {
			tree, err := canon.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			q, err := ParseQuestion(tree.(map[string]any))
			if err != nil {
				t.Fatal(err)
			}
			if canonical(t, q.Tree()) != canonical(t, tree) {
				t.Errorf("the tree changed:\n got %s\nwant %s", canonical(t, q.Tree()), canonical(t, tree))
			}
			encoded, err := q.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if err := kind.Check(encoded, q.QuestionID, false); err != nil {
				t.Errorf("what was encoded is not a question: %v", err)
			}
			again, err := DecodeQuestion(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if canonical(t, again.Tree()) != canonical(t, tree) {
				t.Error("the document changed")
			}
		})
	}
}

// The typed form says what a reader of the record needs: the state, the
// answers with the one now applied, the revisit date of a deferral, and how a
// released or moot question was settled.
func TestAQuestionReadsAsItsStates(t *testing.T) {
	read := func(name string) *Question {
		t.Helper()
		q, err := DecodeQuestion(fixtures(t, "question")[name])
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	open := read("open-with-options.json")
	if open.State != "open" || open.CurrentRevision != 1 || len(open.Revisions) != 1 || open.Revisions[0].Recommendation == "" ||
		len(open.Revisions[0].Options) < 2 || len(open.Answers) != 0 || open.Applied != nil || open.Deferral != nil || open.Disposition != nil || open.Closure != nil {
		t.Errorf("an open question: %+v", open)
	}
	if free := read("open-free-text-decision-item.json"); free.HeldItem != "decision-item" || len(free.Revisions[0].Options) != 0 || free.Revisions[0].Recommendation != "" {
		t.Errorf("a free-text question: %+v", free)
	}
	deferred := read("deferred-by-later.json")
	if deferred.State != "deferred" || deferred.Deferral == nil || deferred.Deferral.RevisitAt != "2026-10-20T09:00:00.000Z" || deferred.Deferral.AnswerSequence != 1 ||
		deferred.Answers[0].Later == nil || deferred.Answers[0].Later.RevisitAt != deferred.Deferral.RevisitAt || deferred.Answers[0].Disposition != "deferred" {
		t.Errorf("a deferred question: %+v", deferred)
	}
	released := read("released.json")
	if released.State != "released" || released.Applied == nil || released.Applied.AnswerSequence != 1 || released.Disposition == nil || released.Disposition.Kind != "released" ||
		released.Answers[0].OptionID == "" || released.Answers[0].OptionLabel == "" || released.Answers[0].Text != "" {
		t.Errorf("a released question: %+v", released)
	}
	if moot := read("moot-question-replaced.json"); moot.State != "moot" || moot.Closure == nil || len(moot.Closure.Evidence) == 0 || moot.Applied != nil {
		t.Errorf("a moot question: %+v", moot)
	}
	superseding := read("answered-superseding.json")
	if superseding.State != "answered" || superseding.Applied == nil || !slices.ContainsFunc(superseding.Answers, func(a Answer) bool { return a.Supersedes != 0 }) {
		t.Errorf("an answered question: %+v", superseding)
	}
	if late := read("unreconciled-late-answer.json"); !slices.ContainsFunc(late.Answers, func(a Answer) bool { return a.Disposition == "unreconciled" }) || late.Applied != nil {
		t.Errorf("a question with a late answer: %+v", late)
	}
}

// Every valid decision round round trips, and its cards read as the schema
// gives them.
func TestADecisionRoundRoundTrips(t *testing.T) {
	kind, _ := poolstore.Lookup("decision-round")
	for name, data := range fixtures(t, "decision-round") {
		t.Run(name, func(t *testing.T) {
			tree, err := canon.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			r, err := ParseRound(tree.(map[string]any))
			if err != nil {
				t.Fatal(err)
			}
			if canonical(t, r.Tree()) != canonical(t, tree) {
				t.Errorf("the tree changed:\n got %s\nwant %s", canonical(t, r.Tree()), canonical(t, tree))
			}
			encoded, err := r.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if err := kind.Check(encoded, r.DecisionRoundID, false); err != nil {
				t.Errorf("what was encoded is not a decision round: %v", err)
			}
			again, err := DecodeRound(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if canonical(t, again.Tree()) != canonical(t, tree) {
				t.Error("the document changed")
			}
		})
	}
}

func TestADecisionRoundReadsAsItsStates(t *testing.T) {
	read := func(name string) *Round {
		t.Helper()
		r, err := DecodeRound(fixtures(t, "decision-round")[name])
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	open := read("open-two-cards.json")
	if open.State != "open" || len(open.Cards) != 2 || open.Cards[0].State != "open" || !open.Complete || open.ClosedAt != "" || len(open.Attachments) != 1 {
		t.Errorf("an open round: %+v", open)
	}
	closed := read("closed-answered-and-moot.json")
	states := []string{}
	for _, c := range closed.Cards {
		states = append(states, c.State)
		if (c.State == "answered") != (c.AnswerSequence != 0) {
			t.Errorf("a card %s with answer %d", c.State, c.AnswerSequence)
		}
	}
	if closed.State != "closed" || closed.ClosedAt == "" || !slices.Contains(states, "answered") || !slices.Contains(states, "moot") {
		t.Errorf("a closed round: %+v", closed)
	}
	if incomplete := read("incomplete-missing-attachment.json"); incomplete.Complete || incomplete.IncompleteReason == "" {
		t.Errorf("an incomplete round: %+v", incomplete)
	}
}

// What is not a record of the schema does not decode: a member it does not
// name, a member of the wrong kind, and a member it requires that is absent.
func TestWhatIsNotARecordDoesNotDecode(t *testing.T) {
	question := func() map[string]any {
		v, _ := canon.Parse(fixtures(t, "question")["open-with-options.json"])
		return v.(map[string]any)
	}
	round := func() map[string]any {
		v, _ := canon.Parse(fixtures(t, "decision-round")["open-two-cards.json"])
		return v.(map[string]any)
	}
	for name, damage := range map[string]func(map[string]any){
		"an unnamed member":      func(m map[string]any) { m["extra"] = "x" },
		"a number for a string":  func(m map[string]any) { m["state"] = int64(1) },
		"a string for a number":  func(m map[string]any) { m["record_revision"] = "1" },
		"a missing member":       func(m map[string]any) { delete(m, "writer") },
		"a missing identity":     func(m map[string]any) { delete(m, "home_id") },
		"an array for an object": func(m map[string]any) { m["origin"] = []any{} },
	} {
		q := question()
		damage(q)
		if _, err := ParseQuestion(q); err == nil {
			t.Errorf("a question with %s decoded", name)
		}
		r := round()
		damage(r)
		if _, err := ParseRound(r); err == nil {
			t.Errorf("a decision round with %s decoded", name)
		}
	}
	nested := question()
	nested["revisions"].([]any)[0].(map[string]any)["options"].([]any)[0].(map[string]any)["colour"] = "red"
	if _, err := ParseQuestion(nested); err == nil {
		t.Error("a question with an option member it does not name decoded")
	}
	if _, err := DecodeQuestion([]byte("not json")); err == nil {
		t.Error("a document that is not JSON decoded")
	}
	if _, err := DecodeRound([]byte("[]")); err == nil {
		t.Error("a document that is not an object decoded")
	}
}

// A released question's disposition keeps the evidence it carries whole, and
// the record still round trips.
func TestADispositionKeepsItsEvidence(t *testing.T) {
	kind, _ := poolstore.Lookup("question")
	v, _ := canon.Parse(fixtures(t, "question")["released.json"])
	tree := v.(map[string]any)
	tree["disposition"].(map[string]any)["evidence"] = []any{
		map[string]any{"schema": "pool.question", "id": "q_01jc7d8e9f0g1h2j3k4m5n6p7z"},
	}
	q, err := ParseQuestion(tree)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Disposition.Evidence) != 1 || q.Disposition.Evidence[0]["id"] != "q_01jc7d8e9f0g1h2j3k4m5n6p7z" {
		t.Errorf("the evidence: %+v", q.Disposition)
	}
	if canonical(t, q.Tree()) != canonical(t, tree) {
		t.Errorf("the tree changed:\n got %s\nwant %s", canonical(t, q.Tree()), canonical(t, tree))
	}
	encoded, err := q.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := kind.Check(encoded, q.QuestionID, false); err != nil {
		t.Errorf("what was encoded is not a question: %v", err)
	}
}
