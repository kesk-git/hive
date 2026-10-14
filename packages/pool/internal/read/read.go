// Package read is the foundation of hive-pool read (spec/pool.md, Reading the
// Pool): what every read of every kind does the same way, with the readers of
// the kinds registered in it.
//
// A read is one kind, by identity or by query. It refuses a selector member
// that does not apply to the kind, and a kind with no reader built, before it
// opens anything. A read of one record that stands alone takes no lock; every
// other read takes the Pool's lock shared, which completes an interrupted
// transaction first. The records come back whole, under a projection header
// with the home, the time of the read, the journal cursor at that moment and
// whether the read was complete; a record that cannot be read is omitted and
// named, so that an incomplete read is never an authoritative empty result.
package read

import (
	"fmt"
	"slices"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/journal"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/store"
)

// The schemas of the operation.
const (
	RequestSchema  = "urn:hive:schema:ops:hive-pool.read.request:v1"
	ResponseSchema = "urn:hive:schema:ops:hive-pool.read.response:v1"
)

// Operation is the read operation over the registry: read-only, answering the
// kinds that have a reader.
func (r *Registry) Operation() *cli.Operation {
	return &cli.Operation{
		OperationDecl: envelope.OperationDecl{
			Name:           "read",
			State:          envelope.Supported,
			Mode:           envelope.Unary,
			Versions:       []string{"1.0"},
			RequestSchema:  RequestSchema,
			ResponseSchema: ResponseSchema,
			ReceiptKinds:   []string{},
			Executes:       []string{},
			Calls:          []string{},
		},
		Handle: r.handle,
	}
}

// selector reads the selector of a validated request body.
func selector(body map[string]any) Selector {
	m, _ := body["selector"].(map[string]any)
	var s Selector
	for _, name := range members {
		v, ok := m[name]
		if !ok {
			continue
		}
		s.Present = append(s.Present, name)
		switch name {
		case MemberID:
			s.ID, _ = v.(string)
		case MemberTaskID:
			s.TaskID, _ = v.(string)
		case MemberProjectID:
			s.ProjectID, _ = v.(string)
		case MemberDueBefore:
			s.DueBefore, _ = v.(string)
		case MemberStates:
			for _, e := range v.([]any) {
				s.States = append(s.States, e.(string))
			}
		case MemberReady:
			s.Ready, _ = v.(bool)
		case MemberActive:
			s.Active, _ = v.(bool)
		case MemberLimit:
			n, _ := v.(int64)
			s.Limit = int(n)
		}
	}
	return s
}

// refuseMember is the refusal of a selector member that does not apply to the
// kind: a violation of the operation's own request schema, as the schema's
// description of selector says.
func refuseMember(kind, member string) *envelope.Failure {
	return envelope.Common(envelope.CodeSchemaViolation, fmt.Sprintf("the selector member %s does not apply to a read of kind %s", member, kind))
}

func (r *Registry) handle(c *cli.Call) (*cli.Result, error) {
	kindName, _ := c.Request.Body["kind"].(string)
	sel := selector(c.Request.Body)
	// What does not apply to the kind is known without a reader.
	for _, m := range sel.Present {
		if !applies(kindName, m) {
			return nil, refuseMember(kindName, m)
		}
	}
	kind, ok := r.Lookup(kindName)
	if !ok {
		return nil, envelope.Common(envelope.CodeUnsupportedOperation, fmt.Sprintf("no reader of kind %s is built; a kind with no reader is refused and never answered as empty", kindName))
	}
	for _, m := range sel.Present {
		if !slices.Contains(kind.Members, m) {
			return nil, refuseMember(kindName, m)
		}
	}
	st, err := open(c)
	if err != nil {
		return nil, err
	}
	if kind.Standalone == nil || !kind.Standalone(sel) {
		lock, err := st.LockShared()
		if err != nil {
			return nil, store.Failure("the Pool", err)
		}
		defer lock.Release()
	}
	// The time and the cursor are taken once the records are as whole as they
	// will be seen: after the lock, which has recovered the store.
	readAt, err := clock.Stamp(c.Clock)
	if err != nil {
		return nil, envelope.Common(envelope.CodeInternal, "the time of the read cannot be written").Because(err)
	}
	cursor, err := cursor(c)
	if err != nil {
		return nil, err
	}
	res, err := kind.Read(&Context{Call: c, Store: st, Kind: kindName, Selector: sel})
	if err != nil {
		return nil, store.Failure("a "+kindName+" record", err)
	}
	return assemble(c, kindName, sel, readAt, cursor, res)
}

// open opens the Pool's store in the home, with the journal recovery appends
// to.
func open(c *cli.Call) (*cstore.Store, error) {
	e := c.Executable
	var j cstore.Journal
	if e.Journal != nil {
		var err error
		if j, err = e.Journal(c.Home.Dir); err != nil {
			return nil, envelope.Common(envelope.CodeRecordMalformed, "the home journal cannot be opened").Because(err)
		}
	}
	st, err := cstore.Open(c.Home.Dir, store.Package, cstore.Options{Clock: c.Clock, Journal: j, Waiting: e.Waiting})
	if err != nil {
		return nil, store.Failure("the home", err)
	}
	if e.Fault != nil {
		st.Fault(e.Fault)
	}
	return st, nil
}

// cursor is the last home_sequence in the journal, 0 for an empty one.
func cursor(c *cli.Call) (int64, error) {
	j, err := journal.Open(c.Home.Dir, journal.Options{Package: store.Package, Clock: c.Clock})
	if err != nil {
		return 0, envelope.Common(envelope.CodeRecordMalformed, "the home journal cannot be opened").Because(err)
	}
	head, err := j.Head()
	if err != nil {
		return 0, envelope.Common(envelope.CodeRecordMalformed, "the home journal cannot be read").Because(err)
	}
	return head, nil
}

// reasons are the reasons an omission may give, as the response schema names
// them.
var reasons = []string{store.ReasonLimit, store.ReasonMalformed, store.ReasonUnsupported}

// assemble makes the projection of what the reader found: the limit applied,
// the records and the derived facts in step, and the header.
func assemble(c *cli.Call, kind string, sel Selector, readAt string, cursor int64, res *Result) (*cli.Result, error) {
	if res == nil {
		return nil, envelope.Common(envelope.CodeInternal, "the reader of "+kind+" returned nothing")
	}
	records, omitted := res.Records, slices.Clone(res.Omitted)
	for _, o := range omitted {
		if o.ID == "" || !slices.Contains(reasons, o.Reason) {
			return nil, envelope.Common(envelope.CodeInternal, "the reader of "+kind+" named an omission the response cannot carry")
		}
	}
	if sel.ID != "" && len(records) == 0 && len(omitted) == 0 {
		return nil, envelope.Common(envelope.CodeNotFound, "no record of kind "+kind+" has that identity")
	}
	if sel.Limit > 0 && len(records) > sel.Limit {
		for _, r := range records[sel.Limit:] {
			omitted = append(omitted, Omission{ID: r.ID, Reason: store.ReasonLimit})
		}
		records = records[:sel.Limit]
	}
	out := make([]any, len(records))
	var derived []any
	for i, r := range records {
		if r.Data == nil {
			return nil, envelope.Common(envelope.CodeInternal, "the reader of "+kind+" returned a record with no content")
		}
		out[i] = r.Data
		if (r.Derived != nil) != (records[0].Derived != nil) {
			return nil, envelope.Common(envelope.CodeInternal, "the reader of "+kind+" gave derived facts for some records and not for all")
		}
		if r.Derived != nil {
			derived = append(derived, r.Derived)
		}
	}
	header := map[string]any{
		"home_id":        c.Home.HomeID,
		"read_at":        readAt,
		"journal_cursor": cursor,
		"complete":       len(omitted) == 0,
	}
	if len(omitted) > 0 {
		list := make([]any, len(omitted))
		for i, o := range omitted {
			list[i] = map[string]any{"id": o.ID, "reason": o.Reason}
		}
		header["omitted"] = list
	}
	body := map[string]any{"projection": header, "kind": kind, "records": out}
	if derived != nil {
		body["derived"] = derived
	}
	return &cli.Result{Body: body}, nil
}
