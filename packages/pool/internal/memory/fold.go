package memory

import (
	"errors"
	"strings"
	"time"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// FoldOp is hive-memory fold (spec/pool.md, Generated knowledge, The
// maintenance queue and the fold). In one transaction, under the Pool's lock,
// it drains exactly the whole pending list it reads, in queue order: each note
// becomes a fact of its subject, and the projections of each subject it changed
// are rendered again under their budgets; a note with no subject, one whose
// fact identity stands with other text and one that would take its subject over
// its budget becomes a queued Prey of the supervisor that owns the home, and
// never a line of an always projection. Each note is marked folded or routed
// with the fold receipt in the same transaction that removes it from the queue,
// and the receipt accounts for every note once. A note or a source that cannot
// be read fails the whole transaction, with the queue and its notes intact.
//
// It calls the renderer and Rerender and does not rewrite them: the budget
// each filing is measured against is the planner's, with the notes before it in
// the queue already filed.
func FoldOp(c *cli.Call) (*cli.Result, error) {
	if !expectsQueue(c) {
		return nil, invalid("hive-memory fold names the expected revision of the maintenance queue, " + schemaQueue + " " + queueID)
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	q, err := loadQueue(c)
	if err != nil {
		return nil, err
	}
	receipt, err := c.NewReceipt(receiptFolded, versionOne, nil, nil, nil)
	if err != nil {
		return nil, internal("a receipt cannot be made", err)
	}
	b := &builder{c: c}
	if len(q.Pending) == 0 {
		// Nothing to drain: the receipt of the request records that, and the
		// queue is not written, so its revision is not raised.
		body := foldBody(q.Revision, q.Revision, nil, nil, nil)
		receipt.Body = body
		receipt.Subject = queueRef(c, q.Revision, q.Digest)
		receipt.Scope = envelopeOf(homeScope(c))
		b.tx.Receipts = append(b.tx.Receipts, receipt)
		return b.stage(body)
	}

	notes, err := loadPending(c, q)
	if err != nil {
		return nil, err
	}
	snap, err := Load(c.Store, c.Home)
	if err != nil {
		return nil, err
	}
	now := c.Clock.Now()
	w := newWorking()
	placed := make([]placement, len(notes))
	for i, n := range notes {
		if placed[i], err = w.place(snap, now, n); err != nil {
			return nil, err
		}
	}

	// The subjects the fold changed, each at one new revision.
	for _, sub := range w.changed() {
		if _, _, err := b.put("subject", sub.SubjectID, sub.Tree(), sub.RecordRevision-1); err != nil {
			return nil, err
		}
	}
	if changed := w.changed(); len(changed) > 0 {
		tx, err := snap.Rerender(c, Write{Subjects: changed}, b.tx)
		if err != nil {
			return nil, err
		}
		b.tx = tx
	}

	var facts, items []any
	for i, n := range notes {
		p := placed[i]
		if p.reason == "" {
			facts = append(facts, map[string]any{"note_id": n.NoteID, "subject_id": n.SubjectID, "fact_id": p.factID})
			if _, _, err := b.put(kindNote, n.NoteID, n.settled(receipt.ReceiptID, c.Home.HomeID, "", p.factID), n.RecordRevision); err != nil {
				return nil, err
			}
			continue
		}
		taskID, err := b.routeItem(n, p.reason)
		if err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"note_id": n.NoteID, "task_id": taskID, "reason": p.reason})
		if _, _, err := b.put(kindNote, n.NoteID, n.settled(receipt.ReceiptID, c.Home.HomeID, taskID, ""), n.RecordRevision); err != nil {
			return nil, err
		}
	}

	_, ref, err := b.put(kindQueue, queueID, q.with(nil), q.Revision)
	if err != nil {
		return nil, err
	}
	body := foldBody(q.Revision, q.Revision+1, q.Pending, facts, items)
	receipt.Body, receipt.Subject, receipt.Scope = body, ref, envelopeOf(homeScope(c))
	b.tx.Receipts = append(b.tx.Receipts, receipt)
	return b.stage(body)
}

// foldBody is the body of pool.memory.folded and of the response, which is the
// same document.
func foldBody(before, after int64, notes []string, facts, items []any) map[string]any {
	drained := make([]any, len(notes))
	for i, id := range notes {
		drained[i] = id
	}
	if facts == nil {
		facts = []any{}
	}
	if items == nil {
		items = []any{}
	}
	return map[string]any{
		"queue_revision_before": before, "queue_revision_after": after,
		"notes": drained, "facts": facts, "items": items, "remaining": int64(0),
	}
}

// loadPending reads the notes the queue names, in its order. A note that is
// not there, cannot be read or is no longer pending fails the fold whole.
func loadPending(c *cli.Call, q *Queue) ([]*Note, error) {
	notes := make([]*Note, len(q.Pending))
	for i, id := range q.Pending {
		l, om, err := poolstore.Load(c.Store, kindNote, id)
		switch {
		case errors.Is(err, poolstore.ErrNotFound):
			return nil, malformed("the maintenance queue names a note that does not exist", id, nil)
		case err != nil:
			return nil, poolstore.Failure("a note", err)
		case om != nil:
			return nil, malformed("a pending note cannot be read", id, nil)
		}
		n, err := ParseNote(l.Data)
		switch {
		case err != nil:
			return nil, malformed("a pending note cannot be read", id, err)
		case n.NoteID != id || n.State != noteStatePending || n.Data["home_id"] != c.Home.HomeID:
			return nil, malformed("a note the maintenance queue holds is not a pending note of this home", id, nil)
		}
		notes[i] = n
	}
	return notes, nil
}

// placement is what became of one note: the fact identity it was filed under,
// or the reason it became an item.
type placement struct {
	factID string
	reason string
}

// working is the subjects as the fold leaves them, one copy of each it
// changes, at the one revision it will be written at.
type working struct {
	subjects map[string]*rule.Subject
	order    []string
}

func newWorking() *working { return &working{subjects: map[string]*rule.Subject{}} }

// subject is the subject as the fold stands: the copy it changed, or the
// record as it was read.
func (w *working) subject(snap *Snapshot, id string) *rule.Subject {
	if s, ok := w.subjects[id]; ok {
		return s
	}
	return snap.subject(id)
}

// changed are the subjects the fold has changed, in the order they were first
// changed.
func (w *working) changed() []*rule.Subject {
	out := make([]*rule.Subject, len(w.order))
	for i, id := range w.order {
		out[i] = w.subjects[id]
	}
	return out
}

// with is the subjects changed so far, with the one given in place of its own.
func (w *working) with(sub *rule.Subject) []*rule.Subject {
	out := w.changed()
	for i, s := range out {
		if s.SubjectID == sub.SubjectID {
			out[i] = sub
			return out
		}
	}
	return append(out, sub)
}

// place decides what becomes of the note and records it: a fact of its subject,
// within the subject's budget, or the reason it is routed.
func (w *working) place(snap *Snapshot, now time.Time, n *Note) (placement, error) {
	if n.SubjectID == "" {
		return placement{reason: reasonSubjectUnknown}, nil
	}
	sub := w.subject(snap, n.SubjectID)
	if sub == nil {
		return placement{reason: reasonSubjectUnknown}, nil
	}
	fact := factOf(n)
	next, outcome := fileFact(sub, fact)
	switch outcome {
	case factConflicts:
		return placement{reason: reasonFactConflict}, nil
	case factRepeated:
		return placement{factID: fact.FactID}, nil
	}
	if _, changed := w.subjects[sub.SubjectID]; !changed {
		next.RecordRevision = sub.RecordRevision + 1
	}
	if _, err := snap.Plan(now, Write{Subjects: w.with(next)}); err != nil {
		if overBudgetOf(err, sub.SubjectID) {
			return placement{reason: reasonOverBudget}, nil
		}
		return placement{}, err
	}
	if _, changed := w.subjects[sub.SubjectID]; !changed {
		w.order = append(w.order, sub.SubjectID)
	}
	w.subjects[sub.SubjectID] = next
	return placement{factID: fact.FactID}, nil
}

// overBudgetOf says whether the error is the planner's refusal of a projection
// of the subject. A projection of another subject that is over its budget is
// no reason to route this note: it fails the fold, as any source it cannot
// make sense of does.
func overBudgetOf(err error, subjectID string) bool {
	f := envelope.AsFailure(err)
	if f == nil || f.Err.Code != CodeOverBudget {
		return false
	}
	projection, _ := f.Err.Detail["projection"].(string)
	return strings.HasSuffix(projection, " "+subjectID)
}
