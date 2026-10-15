package imperative

import (
	cstore "github.com/kesk-git/hive/contract/store"
)

// Current is the canonical Imperative of an item as another package reads it:
// the record whole, the number of its current revision and the revisions it
// holds. It carries no behaviour of an operation of this package.
type Current struct {
	// Record is the Imperative record whole, as it is stored.
	Record map[string]any
	// Revision is the record's own revision.
	Revision int64
	// Number is the number of the current revision of the Imperative.
	Number int64
}

// LoadCurrent reads the Imperative of the item under the lock the caller holds,
// through the code the operations of this package read it with. An absent
// Imperative is not_found and one that cannot be read is record_malformed.
func LoadCurrent(st *cstore.Store, taskID string) (*Current, error) {
	l, err := loadImperative(st, taskID)
	if err != nil {
		return nil, err
	}
	return &Current{Record: l.Data, Revision: l.Revision, Number: intOf(l.Data["current_revision"])}, nil
}

// Named is the revision of the Imperative with the number given: its class and
// four sections, its digest and who authored it, or nil where the Imperative
// holds no such revision.
func (c *Current) Named(number int64) map[string]any {
	return revisionNamed(c.Record, number)
}
