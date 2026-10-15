package memory

import (
	"errors"

	"github.com/kesk-git/hive/packages/pool/internal/read"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// MissMembers are the selector members of a read of kind miss. The request
// schema gives the kind no selector member of its own, so it declares what the
// schema gives every kind: the identity, which selects one record, and the
// limit. A miss is read by its identity or listed in the order of the identities.
var MissMembers = []string{read.MemberID, read.MemberLimit}

// MissReader is the reader of the kind miss (spec/pool.md, Reading the Pool): it
// returns whole records as they are stored, with no derived facts, as the readers
// of the kinds note and maintenance-queue do. A miss stands alone, so a read by
// identity takes no lock and a list takes the lock shared.
func MissReader() read.Kind {
	return read.Kind{
		Name:       kindMiss,
		Members:    MissMembers,
		Standalone: func(s read.Selector) bool { return s.ID != "" },
		Read:       readMisses,
	}
}

// readMisses returns the misses the selector selects; a record that cannot be
// read is named and omitted.
func readMisses(c *read.Context) (*read.Result, error) {
	res := &read.Result{}
	if id := c.Selector.ID; id != "" {
		l, om, err := poolstore.Load(c.Store, kindMiss, id)
		switch {
		case errors.Is(err, poolstore.ErrNotFound):
		case err != nil:
			return nil, err
		case om != nil:
			res.Omitted = append(res.Omitted, *om)
		default:
			res.Records = append(res.Records, read.Record{ID: l.ID, Data: l.Data})
		}
		return res, nil
	}
	all, omitted, err := poolstore.LoadAll(c.Store, kindMiss)
	if err != nil {
		return nil, err
	}
	res.Omitted = omitted
	for _, l := range all {
		res.Records = append(res.Records, read.Record{ID: l.ID, Data: l.Data})
	}
	return res, nil
}
