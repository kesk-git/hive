package budget

import (
	"errors"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/decision"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The writer of the budget record. The record is changed only by a request
// whose authority is of kind word and stands, verified by the rule of
// spec/word.md, The word stands, against the Pool's own question record; any
// other authority, a policy request, a temporary rule or an instruction among
// them, is authority_insufficient. A change appends its before and after
// entries, authority, intake, request and receipt to the record's changes,
// having checked each projection identity and the record's expected revision,
// and never changes defaults. The operation that calls it is a later lane's, and
// adds to it the check that current and reachable renderings fit.

const (
	versionOne  = "1.0"
	receiptKind = "pool.budget.changed"
)

// Change is a budget change that passed every check the writer makes and is not
// yet written.
type Change struct {
	c   *cli.Call
	rec *Record
	// Before are the entries the change replaces and After those that replace
	// them, in the order the request named the projections.
	Before, After []Entry
	intake        map[string]any
}

// Apply is the whole of a budget change, for the operation that calls the
// writer: it prepares the change from the request and stages it.
func Apply(c *cli.Call) (*cli.Result, error) {
	ch, err := Prepare(c)
	if err != nil {
		return nil, err
	}
	return ch.Stage()
}

// Prepare checks the request of hive-memory budget in the order the contract
// states and writes nothing: the authority first, inside the transaction under
// the store's lock and before any record is read, so that a refused request
// reveals nothing and the answer it relies on cannot be superseded between the
// check and the write; then the expected revision of every record the request
// names; then the configuration, the scope and each projection identity.
func Prepare(c *cli.Call) (*Change, error) {
	standing, err := decision.WordStands(c.Store, c.Request.Scope, c.Request.Authority)
	if err != nil {
		return nil, err
	}
	if !standing.Stands() {
		reasons := make([]any, len(standing.Reasons))
		for i, r := range standing.Reasons {
			reasons[i] = r
		}
		return nil, envelope.Common(envelope.CodeAuthorityInsufficient,
			"the budget is changed only on the Hive Mind's word, which must stand").With("reasons", reasons)
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	rec, err := Read(c.Store, c.Home.HomeID)
	if err != nil {
		return nil, err
	}
	if err := checkScope(c); err != nil {
		return nil, err
	}

	ch := &Change{c: c, rec: rec}
	ch.intake, _ = copyTree(c.Request.Body["intake"]).(map[string]any)
	list, _ := c.Request.Body["projections"].([]any)
	seen := map[string]bool{}
	for _, v := range list {
		m, _ := v.(map[string]any)
		id, _ := m["projection_id"].(string)
		max, _ := whole(m["max_bytes"])
		if seen[id] {
			return nil, envelope.Common(envelope.CodeSchemaViolation, "the request names a projection more than once").With("projection_id", id)
		}
		seen[id] = true
		was, ok := rec.Max(id)
		if !ok {
			return nil, envelope.Common(envelope.CodeNotFound, "the budget configuration holds no entry for the projection").With("projection_id", id)
		}
		ch.Before = append(ch.Before, Entry{ProjectionID: id, MaxBytes: was})
		ch.After = append(ch.After, Entry{ProjectionID: id, MaxBytes: max})
	}
	return ch, nil
}

// Stage makes the changed record and its receipt and stages them: the named
// entries replaced, the change appended to changes, defaults as they were. The
// record is written at the revision it was read at, so a record changed since
// is a conflict and nothing is written.
func (ch *Change) Stage() (*cli.Result, error) {
	c, rec := ch.c, ch.rec
	now, err := c.Stamp()
	if err != nil {
		return nil, internal("a change cannot be given a time", err)
	}
	// The change names its receipt, which names the record: the receipt's
	// identity is made first and its subject once the record is final.
	receipt, err := c.NewReceipt(receiptKind, versionOne, nil, nil, nil)
	if err != nil {
		return nil, internal("a receipt cannot be made", err)
	}

	body := copyTree(rec.Data).(map[string]any)
	after := map[string]int64{}
	for _, e := range ch.After {
		after[e.ProjectionID] = e.MaxBytes
	}
	entries := make([]any, 0, len(rec.Projections))
	for _, e := range rec.Projections {
		if max, ok := after[e.ProjectionID]; ok {
			e.MaxBytes = max
		}
		entries = append(entries, entryTree(e))
	}
	body["projections"] = entries
	changes, _ := body["changes"].([]any)
	body["changes"] = append(changes, map[string]any{
		"sequence": int64(len(changes)) + 1, "authority": authorityTree(c.Request.Authority), "intake": ch.intake,
		"before": entryTrees(ch.Before), "after": entryTrees(ch.After),
		"recorded_at": now, "request_id": c.Request.RequestID, "receipt_id": receipt.ReceiptID,
	})
	w := c.Writer()
	body["writer"] = map[string]any{"package": w.Package, "command": w.Command, "operation": w.Operation, "implementation_version": w.ImplementationVersion}

	k, _ := poolstore.Lookup(kind)
	path, err := k.Path(ID)
	if err != nil {
		return nil, internal("the budget configuration has no path", err)
	}
	content, err := canon.Encode(body)
	if err != nil {
		return nil, internal("the budget configuration cannot be encoded", err)
	}
	rw, digest, err := c.Finalize(cstore.RecordWrite{Path: path, Content: content, Expected: rec.Revision})
	if err != nil {
		return nil, internal("the budget configuration cannot be finalized", err)
	}
	v, err := canon.Parse(rw.Content)
	if err != nil {
		return nil, internal("the budget configuration cannot be finalized", err)
	}
	final := v.(map[string]any)

	receipt.Subject = &envelope.RecordRef{Schema: "pool.budget", ID: ID, HomeID: c.Home.HomeID, RecordRevision: rec.Revision + 1, Digest: digest}
	receipt.Scope = envelope.Scope{Level: envelope.ScopeHive, HomeID: c.Home.HomeID}

	staged, err := poolstore.Stage(c, poolstore.Transaction{
		Records:  []poolstore.Write{{Kind: kind, ID: ID, Body: final, Expected: rec.Revision}},
		Receipts: []envelope.Receipt{receipt},
	})
	if err != nil {
		return nil, err
	}
	return staged.Result(map[string]any{"budget": final}), nil
}

func internal(what string, err error) *envelope.Failure {
	return envelope.Common(envelope.CodeInternal, what).Because(err)
}

func entryTree(e Entry) map[string]any {
	return map[string]any{"projection_id": e.ProjectionID, "max_bytes": e.MaxBytes}
}

func entryTrees(entries []Entry) []any {
	out := make([]any, len(entries))
	for i, e := range entries {
		out[i] = entryTree(e)
	}
	return out
}

// checkScope refuses a request made at a scope narrower than the home's: the
// configuration is the home's one record, so only a request at the Hive's scope
// contains it.
func checkScope(c *cli.Call) error {
	if c.Request.Scope.Level != envelope.ScopeHive || c.Request.Scope.HomeID != c.Home.HomeID {
		return envelope.Common(envelope.CodeScopeMismatch, "the budget configuration is the home's, outside the scope of the request")
	}
	return nil
}

// checkExpected compares every expected revision the request supplies with the
// record it names, whether or not the operation writes that record. A record
// the Pool does not hold is not at the revision named.
func checkExpected(c *cli.Call) error {
	for _, e := range c.Request.ExpectedRevisions {
		stale := envelope.Common(envelope.CodeRevisionConflict, "the record is not at the revision the request expects")
		name, ok := strings.CutPrefix(e.Schema, "pool.")
		if _, known := poolstore.Lookup(name); !ok || !known {
			return stale
		}
		l, om, err := poolstore.Load(c.Store, name, e.ID)
		switch {
		case errors.Is(err, poolstore.ErrNotFound):
			return stale
		case err != nil:
			return poolstore.Failure("a record the request expects", err)
		case om != nil:
			return envelope.Common(envelope.CodeRecordMalformed, "a record the request expects cannot be read").With("record", om.ID)
		case l.Revision != e.RecordRevision:
			return stale
		}
	}
	return nil
}

// authorityTree is the authority as a record holds it.
func authorityTree(a envelope.Authority) map[string]any {
	out := map[string]any{"kind": a.Kind}
	if r := a.Ref; r != nil {
		ref := map[string]any{"schema": r.Schema, "id": r.ID}
		if r.HomeID != "" {
			ref["home_id"] = r.HomeID
		}
		if r.RecordRevision != 0 {
			ref["record_revision"] = r.RecordRevision
		}
		if r.Digest != "" {
			ref["digest"] = r.Digest
		}
		out["ref"] = ref
	}
	return out
}

// copyTree is a deep copy of a parsed record, so that the record as read is
// not changed by the body made from it.
func copyTree(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = copyTree(x)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = copyTree(x)
		}
		return out
	}
	return v
}
