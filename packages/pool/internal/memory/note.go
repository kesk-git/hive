package memory

import (
	"fmt"
	"strings"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
)

// The note record (spec/pool.md, Generated knowledge, The maintenance queue and
// the fold): schema, the states it passes through, and the prefix its identity
// carries.
const (
	schemaNote = "pool.note"
	kindNote   = "note"
	notePrefix = "mnote_"

	noteVersion = "1.0"

	noteStatePending = "pending"
	noteStateFolded  = "folded"
	noteStateRouted  = "routed"
)

// Note is a maintenance note: a fact with its subject, or a fact whose subject
// is not known, kept exactly as it was given. Data is the record as it was read
// or built, whole.
type Note struct {
	NoteID         string
	RecordRevision int64
	Scope          rule.Scope
	Text           string
	SubjectID      string
	FactID         string
	Provenance     map[string]any
	Origin         string
	State          string
	Data           map[string]any
}

// ParseNote reads a note record as it is stored. The store has held it to its
// schema; this reads the members the fold acts on and refuses what the schema
// leaves to the reader: a note whose identity is not a note's.
func ParseNote(data map[string]any) (*Note, error) {
	n := &Note{
		NoteID: str(data["note_id"]), RecordRevision: num(data["record_revision"]), Scope: scopeOf(data),
		Text: str(data["text"]), SubjectID: str(data["subject_id"]), FactID: str(data["fact_id"]),
		Provenance: tree(data["provenance"]), Origin: str(data["origin"]), State: str(data["state"]), Data: data,
	}
	if !strings.HasPrefix(n.NoteID, notePrefix) || n.Text == "" || n.Provenance == nil || n.Origin == "" {
		return nil, fmt.Errorf("a note has its identity, text, provenance and origin")
	}
	switch n.State {
	case noteStatePending, noteStateFolded, noteStateRouted:
	default:
		return nil, fmt.Errorf("%q is not a state of a note", n.State)
	}
	return n, nil
}

// mintNoteID gives a note its identity: the prefix of a maintenance note and a
// ULID (spec/pool.md, Identifiers).
func mintNoteID(c *cli.Call) (string, error) { return ids.Mint(c.Clock, ids.Note) }

// newNote is a note record as enqueue writes it: pending, at the scope of the
// request, with the members the caller gave kept as they were given.
func newNote(id, homeID string, scope rule.Scope, given map[string]any, now string) map[string]any {
	rec := map[string]any{
		"schema": schemaNote, "schema_version": noteVersion, "record_revision": int64(1),
		"note_id": id, "text": given["text"], "provenance": copyValue(given["provenance"]), "origin": given["origin"],
		"state": noteStatePending, "created_at": now, "updated_at": now,
	}
	putScope(rec, scope)
	for _, m := range []string{"subject_id", "fact_id"} {
		if v, ok := given[m]; ok {
			rec[m] = v
		}
	}
	return rec
}

// settled is the note after the fold: folded with the fact it became, or routed
// with the item it became, each with the reference to the fold receipt.
func (n *Note) settled(receiptID, homeID, taskID, factID string) map[string]any {
	rec := copyValue(n.Data).(map[string]any)
	disposition := map[string]any{"fold": map[string]any{"schema": schemaReceipt, "id": receiptID, "home_id": homeID}}
	if taskID != "" {
		rec["state"] = noteStateRouted
		disposition["task_id"] = taskID
	} else {
		rec["state"] = noteStateFolded
		rec["fact_id"] = factID
	}
	rec["disposition"] = disposition
	return rec
}

// originatingTask is the task the note's observer names, where it names one.
func (n *Note) originatingTask() string {
	return str(tree(n.Provenance["observer"])["task_id"])
}
