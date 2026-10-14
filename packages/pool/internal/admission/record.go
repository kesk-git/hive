// Package admission keeps the Pool's admission records: the one record of each
// request to dispatch an item, with the checks made against it and, once
// recorded, the Hive Mind's approval of a ruling a check found missing
// (spec/pool.md, Admission). This lane builds the record's view and the
// operation approve; create and check are a later lane's.
package admission

import (
	"errors"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// kind is the kind of record the Pool stores admissions as.
const kind = "admission"

// Record is an admission record as the Pool holds it, whole, with the revision
// it was read at.
type Record struct {
	Data     map[string]any
	Revision int64
}

// Load reads the admission record of the identity, which must exist and be
// readable.
func Load(c *cli.Call, id string) (*Record, error) {
	l, om, err := poolstore.Load(c.Store, kind, id)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return nil, envelope.Common(envelope.CodeNotFound, "no admission record has that identity")
	case err != nil:
		return nil, poolstore.Failure("the admission record", err)
	case om != nil:
		return nil, envelope.Common(envelope.CodeRecordMalformed, "the admission record cannot be read")
	}
	return &Record{Data: l.Data, Revision: l.Revision}, nil
}

// TaskID is the item the admission concerns.
func (r *Record) TaskID() string {
	id, _ := r.Data["task_id"].(string)
	return id
}

// Scope is the scope the record carries, with its clone where it has one.
func (r *Record) Scope() envelope.Scope {
	s := envelope.Scope{}
	s.Level, _ = r.Data["scope_level"].(string)
	s.HomeID, _ = r.Data["home_id"].(string)
	s.WorkspaceID, _ = r.Data["workspace_id"].(string)
	s.ProjectID, _ = r.Data["project_id"].(string)
	s.CloneID, _ = r.Data["clone_id"].(string)
	return s
}

// Approved reports whether the record carries an approval.
func (r *Record) Approved() bool {
	_, ok := r.Data["approval"]
	return ok
}
