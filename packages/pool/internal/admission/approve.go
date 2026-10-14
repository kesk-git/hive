package admission

import (
	"errors"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/decision"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The operation approve of hive-admit (spec/pool.md, Admission). It records a
// required ruling on an admission record: the envelope's authority must be of
// kind word and stand, which the Pool verifies against its own question record
// by the rule of spec/word.md, The word stands. It records the approval and
// nothing else, with the receipt pool.admission.approved; the caller runs check
// again, which reads it. The package registers it from its init function, as the
// package of the decision operations does its own.

func init() { entry.RegisterApprove(entry.Default, Approve) }

const (
	versionOne  = "1.0"
	receiptKind = "pool.admission.approved"
)

// Approve records the approval of the admission the request names. The word is
// verified first, inside the transaction under the store's lock, and before any
// admission is read, so that a refused request reveals nothing of the Pool and
// the answer it relies on cannot be superseded between the check and the write.
func Approve(c *cli.Call) (*cli.Result, error) {
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
			"an approval is recorded only on the Hive Mind's word, which must stand").With("reasons", reasons)
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	id, _ := c.Request.Body["admission_id"].(string)
	rec, err := Load(c, id)
	if err != nil {
		return nil, err
	}
	if err := checkScope(c, rec.Scope()); err != nil {
		return nil, err
	}
	if rec.Approved() {
		return nil, envelope.Common(envelope.CodeAlreadyExists, "the admission record already carries an approval")
	}
	now, err := c.Stamp()
	if err != nil {
		return nil, envelope.Common(envelope.CodeInternal, "an approval cannot be given a time").Because(err)
	}

	body := copyTree(rec.Data).(map[string]any)
	body["approval"] = map[string]any{
		"authority": authorityTree(c.Request.Authority), "recorded_at": now, "request_id": c.Request.RequestID,
	}
	w := c.Writer()
	body["writer"] = map[string]any{"package": w.Package, "command": w.Command, "operation": w.Operation, "implementation_version": w.ImplementationVersion}

	path, err := cstorePath(id)
	if err != nil {
		return nil, err
	}
	content, err := canon.Encode(body)
	if err != nil {
		return nil, envelope.Common(envelope.CodeInternal, "the admission record cannot be encoded").Because(err)
	}
	rw, digest, err := c.Finalize(cstore.RecordWrite{Path: path, Content: content, Expected: rec.Revision})
	if err != nil {
		return nil, envelope.Common(envelope.CodeInternal, "the admission record cannot be finalized").Because(err)
	}
	v, err := canon.Parse(rw.Content)
	if err != nil {
		return nil, envelope.Common(envelope.CodeInternal, "the admission record cannot be finalized").Because(err)
	}
	final := v.(map[string]any)

	subject := &envelope.RecordRef{Schema: "pool.admission", ID: id, HomeID: c.Home.HomeID, RecordRevision: rec.Revision + 1, Digest: digest}
	receipt, err := c.NewReceipt(receiptKind, versionOne, subject, nil, nil)
	if err != nil {
		return nil, envelope.Common(envelope.CodeInternal, "a receipt cannot be made").Because(err)
	}
	receipt.Scope = rec.Scope()
	receipt.Scope.TaskID = rec.TaskID()

	staged, err := poolstore.Stage(c, poolstore.Transaction{
		Records:  []poolstore.Write{{Kind: kind, ID: id, Body: final, Expected: rec.Revision}},
		Receipts: []envelope.Receipt{receipt},
	})
	if err != nil {
		return nil, err
	}
	return staged.Result(map[string]any{"admission": final}), nil
}

// cstorePath is the path of the admission record of the identity.
func cstorePath(id string) (string, error) {
	k, ok := poolstore.Lookup(kind)
	if !ok {
		return "", envelope.Common(envelope.CodeInternal, "admission is not a kind of record")
	}
	path, err := k.Path(id)
	if err != nil {
		return "", envelope.Common(envelope.CodeInternal, "the admission record has no path").Because(err)
	}
	return path, nil
}

// checkScope refuses a request whose scope does not contain the scope of the
// record it acts on, before anything is written: a hive-scope request contains
// every record of its home, a workspace-scope request its workspace and that
// workspace's projects, a project-scope request only that project.
func checkScope(c *cli.Call, have envelope.Scope) error {
	want := c.Request.Scope
	in := have.HomeID == want.HomeID
	switch want.Level {
	case envelope.ScopeWorkspace:
		in = in && have.WorkspaceID == want.WorkspaceID
	case envelope.ScopeProject:
		in = in && have.Level == envelope.ScopeProject && have.WorkspaceID == want.WorkspaceID && have.ProjectID == want.ProjectID
	}
	if !in {
		return envelope.Common(envelope.CodeScopeMismatch, "the record is outside the scope of the request")
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
