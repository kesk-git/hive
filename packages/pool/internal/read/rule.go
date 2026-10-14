package read

import (
	"slices"
	"time"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/store"
)

// The readers of the kinds rule and subject (spec/pool.md, Reading the Pool,
// Rules and Subjects). Each returns whole records as they are stored, with no
// derived facts: a record of either kind stands alone, so a read by identity
// takes no lock and a query takes the lock shared.
//
// The package does not import the rule package that holds the codecs of these
// records, which registers through entry: the members a reader selects on are
// read from the record's tree here.

// RuleMembers are the selector members of a read of kind rule: the identity,
// the project, the states of the rule's status, the expiry time a rule is due
// at, which the Shadow and the UI read, and the limit. A rule is not held to
// one item, which the schema gives task_id for.
var RuleMembers = []string{MemberID, MemberProjectID, MemberStates, MemberDueBefore, MemberLimit}

// SubjectMembers are the selector members of a read of kind subject: the
// identity, the project and the limit. A subject has no state and no date.
var SubjectMembers = []string{MemberID, MemberProjectID, MemberLimit}

// RegisterRuleReaders registers the readers of the kinds rule and subject.
func RegisterRuleReaders(r *Registry) {
	standalone := func(s Selector) bool { return s.ID != "" }
	r.Register(Kind{Name: "rule", Members: RuleMembers, Standalone: standalone, Read: readRules})
	r.Register(Kind{Name: "subject", Members: SubjectMembers, Standalone: standalone, Read: readSubjects})
}

func readRules(c *Context) (*Result, error) {
	sel := c.Selector
	if sel.Has(MemberDueBefore) && !clock.Valid(sel.DueBefore) {
		return nil, envelope.Common(envelope.CodeSchemaViolation, "due_before is not a calendar instant")
	}
	return readKind(c, func(sel Selector, l store.Loaded) (bool, error) {
		switch {
		case sel.Has(MemberProjectID) && l.Data["project_id"] != sel.ProjectID:
			return false, nil
		case sel.Has(MemberStates) && !slices.Contains(sel.States, stringOf(l.Data["status"])):
			return false, nil
		case sel.Has(MemberDueBefore):
			due, _ := clock.Parse(sel.DueBefore)
			at, ok, err := expiryOf(l.Data)
			return ok && !at.After(due), err
		}
		return true, nil
	})
}

func readSubjects(c *Context) (*Result, error) {
	return readKind(c, func(sel Selector, l store.Loaded) (bool, error) {
		return !sel.Has(MemberProjectID) || l.Data["project_id"] == sel.ProjectID, nil
	})
}

// expiryOf is the expiry time the clock reads for a rule: the new expiry of
// its latest extend or end amendment, by sequence, in place of the original,
// which is never changed. A rule with none is due at no instant. A time that
// is not a real instant is an error, never read as none. The rule package's
// reading of the same is the one the resolver uses, and a test holds the two
// to each other.
func expiryOf(data map[string]any) (at time.Time, ok bool, err error) {
	text, _ := data["expires_at"].(string)
	var latest int64
	amendments, _ := data["amendments"].([]any)
	for _, v := range amendments {
		a, _ := v.(map[string]any)
		next, _ := a["new_expires_at"].(string)
		seq, _ := a["sequence"].(int64)
		if next != "" && seq >= latest {
			text, latest = next, seq
		}
	}
	if text == "" {
		return time.Time{}, false, nil
	}
	at, err = clock.Parse(text)
	return at, err == nil, err
}

// OpenShared opens the Pool's store for an operation that only reads, as a
// read of a query does, with the lock shared: the records it sees are those
// of whole transactions, an interrupted one completed first. release gives
// the lock up.
func OpenShared(c *cli.Call) (st *cstore.Store, release func(), err error) {
	st, err = open(c)
	if err != nil {
		return nil, nil, err
	}
	lock, err := st.LockShared()
	if err != nil {
		return nil, nil, store.Failure("the Pool", err)
	}
	return st, func() { lock.Release() }, nil
}
