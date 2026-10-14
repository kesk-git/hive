// Package budget keeps the Pool's budget configuration record: the one record
// of a home that holds the byte budget of every knowledge projection and the
// provisioned defaults new entries start from (spec/pool.md, Generated
// knowledge, The budget record). This lane builds the record's reader and the
// writer that applies the Hive Mind's word to it; the operation hive-memory
// budget, its check that current and reachable renderings fit, and the
// renderer are a later lane's, which registers the operation on the writer.
package budget

import (
	"errors"

	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// kind is the kind of record the Pool stores the configuration as, and ID the
// one identity of it a home has.
const (
	kind = "budget"
	ID   = "memory"
)

// Entry is the budget of one projection, in bytes.
type Entry struct {
	ProjectionID string
	MaxBytes     int64
}

// Record is the budget configuration as the Pool holds it, whole, with the
// revision it was read at.
type Record struct {
	Data     map[string]any
	Revision int64
	// Projections are the entries, in the order the record keeps them.
	Projections []Entry
}

// Read reads the home's budget configuration, which must exist and be readable.
// It writes nothing, and every reader of the record, which the rule writes and
// the renderer among them, reads it through here. The error is an
// *envelope.Failure: not_found where the home holds none, record_malformed where
// it cannot be read or is another home's.
func Read(st *cstore.Store, homeID string) (*Record, error) {
	l, om, err := poolstore.Load(st, kind, ID)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return nil, envelope.Common(envelope.CodeNotFound, "the home holds no budget configuration")
	case err != nil:
		return nil, poolstore.Failure("the budget configuration", err)
	case om != nil:
		return nil, envelope.Common(envelope.CodeRecordMalformed, "the budget configuration cannot be read")
	}
	if l.Data["home_id"] != homeID {
		return nil, envelope.Common(envelope.CodeRecordMalformed, "the budget configuration is not this home's")
	}
	r := &Record{Data: l.Data, Revision: l.Revision}
	list, _ := l.Data["projections"].([]any)
	for _, v := range list {
		m, _ := v.(map[string]any)
		id, _ := m["projection_id"].(string)
		max, ok := whole(m["max_bytes"])
		if id == "" || !ok {
			return nil, envelope.Common(envelope.CodeRecordMalformed, "the budget configuration cannot be read")
		}
		r.Projections = append(r.Projections, Entry{ProjectionID: id, MaxBytes: max})
	}
	return r, nil
}

// Max is the budget of the projection, and false where the record holds no
// entry for it.
func (r *Record) Max(projectionID string) (int64, bool) {
	for _, e := range r.Projections {
		if e.ProjectionID == projectionID {
			return e.MaxBytes, true
		}
	}
	return 0, false
}

// whole is the whole number a parsed member holds.
func whole(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), float64(int64(n)) == n
	}
	return 0, false
}
