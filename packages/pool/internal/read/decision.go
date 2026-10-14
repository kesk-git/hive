package read

import (
	"errors"
	"slices"
	"time"

	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/store"
)

// The readers of the kinds question and decision-round (spec/pool.md, Reading
// the Pool; spec/word.md, A question's life). Each returns whole records as
// they are stored, with no derived facts: a record of either kind stands
// alone, so a read by identity takes no lock and a query takes the lock shared.
//
// The package does not import the decision package that holds the codecs of
// these records, which registers through entry: the members a reader
// selects on are read from the record's tree here.

// QuestionMembers are the selector members of a read of kind question: the
// identity, the item the question holds, the project, the states, the
// revisit date a deferred question is due at and the limit.
var QuestionMembers = []string{MemberID, MemberTaskID, MemberProjectID, MemberStates, MemberDueBefore, MemberLimit}

// DecisionRoundMembers are the selector members of a read of kind
// decision-round: the identity, the project, the states and the limit. A round
// is not held to one item, and has no date a reader is due at.
var DecisionRoundMembers = []string{MemberID, MemberProjectID, MemberStates, MemberLimit}

// RegisterDecisionReaders registers the readers of the kinds question and
// decision-round.
func RegisterDecisionReaders(r *Registry) {
	standalone := func(s Selector) bool { return s.ID != "" }
	r.Register(Kind{Name: "question", Members: QuestionMembers, Standalone: standalone, Read: readQuestions})
	r.Register(Kind{Name: "decision-round", Members: DecisionRoundMembers, Standalone: standalone, Read: readRounds})
}

// keeper decides whether a record of the kind is selected. An error means the
// record cannot be read as its kind and is omitted as malformed.
type keeper func(sel Selector, l store.Loaded) (bool, error)

func readQuestions(c *Context) (*Result, error) {
	sel := c.Selector
	if sel.Has(MemberDueBefore) && !clock.Valid(sel.DueBefore) {
		return nil, envelope.Common(envelope.CodeSchemaViolation, "due_before is not a calendar instant")
	}
	return readKind(c, func(sel Selector, l store.Loaded) (bool, error) {
		switch {
		case sel.Has(MemberTaskID) && l.Data["held_task_id"] != sel.TaskID:
			return false, nil
		case sel.Has(MemberProjectID) && l.Data["project_id"] != sel.ProjectID:
			return false, nil
		case sel.Has(MemberStates) && !slices.Contains(sel.States, stringOf(l.Data["state"])):
			return false, nil
		case sel.Has(MemberDueBefore):
			due, _ := clock.Parse(sel.DueBefore)
			at, ok, err := revisitAt(l.Data)
			return ok && !at.After(due), err
		}
		return true, nil
	})
}

func readRounds(c *Context) (*Result, error) {
	return readKind(c, func(sel Selector, l store.Loaded) (bool, error) {
		switch {
		case sel.Has(MemberProjectID) && l.Data["project_id"] != sel.ProjectID:
			return false, nil
		case sel.Has(MemberStates) && !slices.Contains(sel.States, stringOf(l.Data["state"])):
			return false, nil
		}
		return true, nil
	})
}

// revisitAt is the revisit date of a deferred question: the date of the latest
// Later that was applied, which the record carries only while the question is
// deferred. A question with none is not due at any instant. A date that is not
// a real instant is an error, never read as none.
func revisitAt(data map[string]any) (at time.Time, ok bool, err error) {
	deferral, present := data["deferral"].(map[string]any)
	if !present {
		return time.Time{}, false, nil
	}
	s, _ := deferral["revisit_at"].(string)
	if at, err = clock.Parse(s); err != nil {
		return time.Time{}, false, err
	}
	return at, true, nil
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

// readKind reads the records of the context's kind that the selector selects,
// by identity or all of them: a record that cannot be read, or whose members
// the selector reads are not as the schema gives them, is omitted and named.
func readKind(c *Context, keep keeper) (*Result, error) {
	sel := c.Selector
	res := &Result{}
	add := func(l store.Loaded) {
		ok, err := keep(sel, l)
		switch {
		case err != nil:
			res.Omitted = append(res.Omitted, Omission{ID: l.ID, Reason: store.ReasonMalformed})
		case ok:
			res.Records = append(res.Records, Record{ID: l.ID, Data: l.Data})
		}
	}
	if sel.ID != "" {
		l, om, err := store.Load(c.Store, c.Kind, sel.ID)
		switch {
		case errors.Is(err, store.ErrNotFound):
		case err != nil:
			return nil, err
		case om != nil:
			res.Omitted = append(res.Omitted, *om)
		default:
			add(*l)
		}
		return res, nil
	}
	all, omitted, err := store.LoadAll(c.Store, c.Kind)
	if err != nil {
		return nil, err
	}
	res.Omitted = omitted
	for _, l := range all {
		add(l)
	}
	return res, nil
}
