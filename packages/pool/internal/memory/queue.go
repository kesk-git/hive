package memory

import (
	"errors"
	"fmt"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The maintenance queue (spec/pool.md, Generated knowledge): the home's one
// record of the pending notes, in the order they were queued.
const (
	schemaQueue = "pool.maintenance-queue"
	kindQueue   = "maintenance-queue"
	queueID     = "maintenance"
)

// Queue is the maintenance queue as it was read: its record, its revision and
// the digest it was read at.
type Queue struct {
	Revision int64
	Digest   string
	Pending  []string
	Data     map[string]any
}

// loadQueue reads the home's maintenance queue, which must exist and be
// readable, with the digest of the record as it stands. A queue the home does
// not hold is not_found, as a missing budget configuration is.
func loadQueue(c *cli.Call) (*Queue, error) {
	l, om, err := poolstore.Load(c.Store, kindQueue, queueID)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return nil, envelope.Common(envelope.CodeNotFound, "the home holds no maintenance queue")
	case err != nil:
		return nil, poolstore.Failure("the maintenance queue", err)
	case om != nil:
		return nil, malformed("the maintenance queue cannot be read", queueID, nil)
	}
	if l.Data["home_id"] != c.Home.HomeID {
		return nil, malformed("the maintenance queue is not this home's", queueID, nil)
	}
	k, _ := poolstore.Lookup(kindQueue)
	path, err := k.Path(queueID)
	if err != nil {
		return nil, internal("the maintenance queue has no path", err)
	}
	rec, err := c.Store.ReadRecord(path)
	if err != nil {
		return nil, poolstore.Failure("the maintenance queue", err)
	}
	q := &Queue{Revision: l.Revision, Digest: rec.Digest, Data: l.Data}
	seen := map[string]bool{}
	for _, v := range list(l.Data["pending"]) {
		id := str(v)
		if seen[id] {
			return nil, malformed("the maintenance queue names a note twice", queueID, fmt.Errorf("duplicate pending note"))
		}
		seen[id] = true
		q.Pending = append(q.Pending, id)
	}
	return q, nil
}

// with is the queue's record with the pending list given, for the writer to
// stage at the revision it was read at.
func (q *Queue) with(pending []string) map[string]any {
	rec := copyValue(q.Data).(map[string]any)
	l := make([]any, len(pending))
	for i, id := range pending {
		l[i] = id
	}
	rec["pending"] = l
	return rec
}

// expectsQueue says whether the request names the expected revision of the
// maintenance queue.
func expectsQueue(c *cli.Call) bool {
	for _, e := range c.Request.ExpectedRevisions {
		if e.Schema == schemaQueue && e.ID == queueID {
			return true
		}
	}
	return false
}

// homeScope is the scope the queue's receipts are in: the queue is the home's.
func homeScope(c *cli.Call) rule.Scope {
	return rule.Scope{Level: envelope.ScopeHive, HomeID: c.Home.HomeID}
}
