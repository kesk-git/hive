package memory

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
)

const (
	receiptQueued = "pool.note.queued"
	receiptFolded = "pool.memory.folded"
)

// EnqueueOp is hive-memory enqueue (spec/pool.md, Generated knowledge, The
// maintenance queue and the fold): it appends the report or stow notes the
// request carries to the maintenance queue, under the queue's expected
// revision, keeping each note's exact text, its optional subject identity and
// local fact identity, its origin and its provenance, and writes
// pool.note.queued for each. A note is at the scope of the request. It decides
// nothing about a note: whether its subject is known is the fold's to find.
//
// A request replayed under its identity returns its recorded outcome, so a
// replay never appends a note again.
func EnqueueOp(c *cli.Call) (*cli.Result, error) {
	scope := ruleScope(c)
	switch scope.Level {
	case envelope.ScopeHive, envelope.ScopeWorkspace, envelope.ScopeProject:
	default:
		return nil, envelope.Common(envelope.CodeScopeMismatch, "a note is of the Hive's, a workspace's or a project's scope")
	}
	if !expectsQueue(c) {
		return nil, invalid("hive-memory enqueue names the expected revision of the maintenance queue, " + schemaQueue + " " + queueID)
	}
	q, err := loadQueue(c)
	if err != nil {
		return nil, err
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	stamp, err := c.Stamp()
	if err != nil {
		return nil, internal("the time of the notes cannot be written", err)
	}

	b := &builder{c: c}
	pending := append([]string(nil), q.Pending...)
	var written []any
	for _, given := range list(c.Request.Body["notes"]) {
		id, err := mintNoteID(c)
		if err != nil {
			return nil, internal("a note cannot be given an identity", err)
		}
		final, ref, err := b.put(kindNote, id, newNote(id, c.Home.HomeID, scope, tree(given), stamp), 0)
		if err != nil {
			return nil, err
		}
		if err := b.receipt("", receiptQueued, ref, scope); err != nil {
			return nil, err
		}
		pending = append(pending, id)
		written = append(written, final)
	}
	queue, _, err := b.put(kindQueue, queueID, q.with(pending), q.Revision)
	if err != nil {
		return nil, err
	}
	return b.stage(map[string]any{"notes": written, "queue": queue})
}

// ruleScope is the scope of the request as a note, a subject and a projection
// carry it: the task and the clone a request may be narrowed to are no part of
// it.
func ruleScope(c *cli.Call) rule.Scope {
	s := c.Request.Scope
	return rule.Scope{Level: s.Level, HomeID: s.HomeID, WorkspaceID: s.WorkspaceID, ProjectID: s.ProjectID}
}
