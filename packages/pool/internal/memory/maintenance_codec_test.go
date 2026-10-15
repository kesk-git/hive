package memory

import (
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
)

func subjectWith(facts ...rule.Fact) *rule.Subject {
	return &rule.Subject{SubjectID: "subj_01jc7d8e9f0g1h2j3k4m5n6p7a", RecordRevision: 3, Facts: facts}
}

// A fact is filed once: the same identity with the same text is idempotent and
// with other text is a conflict that replaces nothing.
func TestFilingAFact(t *testing.T) {
	have := rule.Fact{FactID: "one", Text: "First."}
	sub := subjectWith(have)
	for _, c := range []struct {
		name    string
		fact    rule.Fact
		outcome factOutcome
		facts   int
	}{
		{"new identity", rule.Fact{FactID: "two", Text: "Second."}, factAdded, 2},
		{"same identity, same text", rule.Fact{FactID: "one", Text: "First."}, factRepeated, 1},
		{"same identity, other text", rule.Fact{FactID: "one", Text: "Other."}, factConflicts, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			next, outcome := fileFact(sub, c.fact)
			if outcome != c.outcome || len(next.Facts) != c.facts {
				t.Errorf("outcome %v with %d facts", outcome, len(next.Facts))
			}
			if len(sub.Facts) != 1 || sub.Facts[0].FactID != have.FactID || sub.Facts[0].Text != have.Text {
				t.Errorf("the subject that was read changed: %v", sub.Facts)
			}
		})
	}
}

// A missing fact identity is minted from the note's ULID and never from its text.
func TestAMissingFactIdentityIsMintedFromTheNote(t *testing.T) {
	n := &Note{NoteID: "mnote_01jc7d8e9f0g1h2j3k4m5n6p7a", Text: "Words that would make a fine name."}
	if got := factIDOf(n); got != "note-01jc7d8e9f0g1h2j3k4m5n6p7a" {
		t.Errorf("fact identity %q", got)
	}
	n.FactID = "chosen.name"
	if got := factIDOf(n); got != "chosen.name" {
		t.Errorf("fact identity %q", got)
	}
}

func TestANoteIsMintedInTheFormOfItsSchema(t *testing.T) {
	c := &cli.Call{Clock: clock.NewFake(time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC))}
	a, err := mintNoteID(c)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := mintNoteID(c)
	if !strings.HasPrefix(a, "mnote_") || len(a) != len("mnote_")+26 || a == b {
		t.Errorf("identities %q and %q", a, b)
	}
}

func TestParseNoteRefusesWhatIsNotANote(t *testing.T) {
	ok := map[string]any{
		"note_id": "mnote_01jc7d8e9f0g1h2j3k4m5n6p7a", "record_revision": int64(1), "text": "T", "origin": "stow",
		"state": "pending", "provenance": map[string]any{"observer": map[string]any{"kind": "worker", "task_id": "task_01jc7d8e9f0g1h2j3k4m5n6p7a"}},
	}
	n, err := ParseNote(ok)
	if err != nil || n.originatingTask() != "task_01jc7d8e9f0g1h2j3k4m5n6p7a" || n.Scope.Level != "" {
		t.Fatalf("%v %+v", err, n)
	}
	for name, mutate := range map[string]func(map[string]any){
		"another identity": func(m map[string]any) { m["note_id"] = "rule_01jc7d8e9f0g1h2j3k4m5n6p7a" },
		"no text":          func(m map[string]any) { m["text"] = "" },
		"no provenance":    func(m map[string]any) { delete(m, "provenance") },
		"a state of none":  func(m map[string]any) { m["state"] = "lost" },
	} {
		t.Run(name, func(t *testing.T) {
			m := copyValue(ok).(map[string]any)
			mutate(m)
			if _, err := ParseNote(m); err == nil {
				t.Error("read")
			}
		})
	}
}

// Only the planner's refusal of a projection of the note's own subject routes
// the note; any other refusal fails the fold.
func TestOnlyTheSubjectsOwnBudgetRoutesANote(t *testing.T) {
	over := func(projection string) error {
		return refuse(CodeOverBudget, "over").With("projection", projection)
	}
	const subject = "subj_01jc7d8e9f0g1h2j3k4m5n6p7a"
	for name, c := range map[string]struct {
		err  error
		want bool
	}{
		"its own projection":    {over("hive subject class-0 " + subject), true},
		"another subject's":     {over("hive subject class-0 subj_01jc7d8e9f0g1h2j3k4m5n6p7b"), false},
		"an always projection":  {over("hive always hive"), false},
		"another refusal":       {refuse(CodeRuleConflict, "conflict").With("projection", "x "+subject), false},
		"not a failure at all":  {errTest, false},
		"a failure of a status": {envelope.Common(envelope.CodeInternal, "x"), false},
	} {
		if got := overBudgetOf(c.err, subject); got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

type testError string

func (e testError) Error() string { return string(e) }

const errTest = testError("not a failure")
