package rulewrite

import (
	"errors"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The codes of the Pool that the writers return (spec/pool.md, Error codes), the
// receipts they write and the version of every receipt kind.
const (
	CodeRuleConflict   = "pool.rule_conflict"
	CodeLineTooLong    = "pool.rule_line_too_long"
	CodeNotExpired     = "pool.rule_not_expired"
	CodeSubjectInvalid = "pool.subject_invalid"
	CodeFactConflict   = "pool.fact_conflict"

	receiptRecorded = "pool.rule.recorded"
	receiptAmended  = "pool.rule.amended"
	receiptLifted   = "pool.rule.lifted"
	receiptExpired  = "pool.rule.expired"
	receiptRevised  = "pool.subject.revised"
	versionOne      = "1.0"

	// MaxRuleLine is the cap of a rule line, in Unicode code points.
	MaxRuleLine = 300
)

// The three moves a recorder makes when a ruling does not fit, in order.
var moves = []any{"shorten-line-and-file-facts", "narrow-subject", "supersede-rule"}

func refuse(code, message string) *envelope.Failure {
	return envelope.Fail(envelope.Refused, code, message)
}

func invalid(message string) *envelope.Failure {
	return envelope.Common(envelope.CodeSchemaViolation, message)
}

func internal(what string, err error) *envelope.Failure {
	return envelope.Common(envelope.CodeInternal, what).Because(err)
}

func str(v any) string { s, _ := v.(string); return s }

func tree(v any) map[string]any { m, _ := v.(map[string]any); return m }

func list(v any) []any { l, _ := v.([]any); return l }

// copyValue is a deep copy of a decoded JSON value.
func copyValue(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = copyValue(x)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = copyValue(x)
		}
		return out
	}
	return v
}

// ruleScope is the scope of the envelope as a rule or a subject carries it.
func ruleScope(s envelope.Scope) rule.Scope {
	return rule.Scope{Level: s.Level, HomeID: s.HomeID, WorkspaceID: s.WorkspaceID, ProjectID: s.ProjectID}
}

// scopeOfRule is the full scope of the rule: its scope and its task generation.
func scopeOfRule(r *rule.Rule) envelope.Scope {
	s := envScope(r.Scope)
	s.TaskID, s.Generation = r.TaskID, r.Generation
	return s
}

// envScope is the scope of a rule or a subject as the envelope carries it.
func envScope(s rule.Scope) envelope.Scope {
	return envelope.Scope{Level: s.Level, HomeID: s.HomeID, WorkspaceID: s.WorkspaceID, ProjectID: s.ProjectID}
}

// contains says whether the scope outer is the scope inner or contains it.
func contains(outer, inner rule.Scope) bool {
	if outer.HomeID != inner.HomeID {
		return false
	}
	switch outer.Level {
	case envelope.ScopeHive:
		return true
	case envelope.ScopeWorkspace:
		return inner.Level != envelope.ScopeHive && inner.WorkspaceID == outer.WorkspaceID
	case envelope.ScopeProject:
		return inner.Level == envelope.ScopeProject && inner.WorkspaceID == outer.WorkspaceID && inner.ProjectID == outer.ProjectID
	}
	return false
}

// checkContained refuses a record outside the scope of the request.
func checkContained(c *cli.Call, scope rule.Scope) error {
	if !contains(ruleScope(c.Request.Scope), scope) {
		return envelope.Common(envelope.CodeScopeMismatch, "the record is outside the scope of the request")
	}
	return nil
}

// checkRuleContained refuses a rule outside the scope of the request: a request
// narrowed to a task generation reaches the rules of that generation only.
func checkRuleContained(c *cli.Call, r *rule.Rule) error {
	if err := checkContained(c, r.Scope); err != nil {
		return err
	}
	if s := c.Request.Scope; s.TaskID != "" && (r.TaskID != s.TaskID || r.Generation != s.Generation) {
		return envelope.Common(envelope.CodeScopeMismatch, "the rule is outside the task generation of the request")
	}
	return nil
}

// checkLevel refuses a request made at a scope a rule or a subject cannot have.
func checkLevel(c *cli.Call) error {
	switch c.Request.Scope.Level {
	case envelope.ScopeHive, envelope.ScopeWorkspace, envelope.ScopeProject:
		return nil
	}
	return envelope.Common(envelope.CodeScopeMismatch, "a rule and a subject are of the Hive's, a workspace's or a project's scope")
}

// expects says whether the request names the expected revision of the record.
func expects(c *cli.Call, schema, id string) bool {
	for _, e := range c.Request.ExpectedRevisions {
		if e.Schema == schema && e.ID == id {
			return true
		}
	}
	return false
}

// checkExpected compares every expected revision the request supplies with the
// record it names, under the lock the operation holds and before anything is
// computed. A record the Pool does not hold is not at the revision named.
func checkExpected(c *cli.Call) error {
	for _, e := range c.Request.ExpectedRevisions {
		stale := envelope.Common(envelope.CodeRevisionConflict, "the record is not at the revision the request expects")
		kind, ok := strings.CutPrefix(e.Schema, "pool.")
		if _, known := poolstore.Lookup(kind); !ok || !known {
			return stale
		}
		l, om, err := poolstore.Load(c.Store, kind, e.ID)
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

// loadRule reads the rule of the identity, whole, with the revision it was read at.
func loadRule(c *cli.Call, id string) (*poolstore.Loaded, *rule.Rule, error) {
	l, om, err := poolstore.Load(c.Store, "rule", id)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return nil, nil, envelope.Common(envelope.CodeNotFound, "no rule has that identity")
	case err != nil:
		return nil, nil, poolstore.Failure("the rule", err)
	case om != nil:
		return nil, nil, envelope.Common(envelope.CodeRecordMalformed, "the rule cannot be read").With("record", om.ID)
	}
	r, err := rule.ParseRule(l.Data)
	if err != nil {
		return nil, nil, envelope.Common(envelope.CodeRecordMalformed, "the rule cannot be read").With("record", id).Because(err)
	}
	return l, r, nil
}

// builder gathers the transaction of one request.
type builder struct {
	c  *cli.Call
	tx poolstore.Transaction
}

// put adds the record to the transaction, finalized, and returns it with the
// reference a receipt names it by.
func (b *builder) put(kind, id string, body map[string]any, expected int64) (map[string]any, *envelope.RecordRef, error) {
	w := b.c.Writer()
	body["writer"] = map[string]any{"package": w.Package, "command": w.Command, "operation": w.Operation, "implementation_version": w.ImplementationVersion}
	k, ok := poolstore.Lookup(kind)
	if !ok {
		return nil, nil, internal("a record cannot be finalized", nil)
	}
	path, err := k.Path(id)
	if err != nil {
		return nil, nil, internal("a record has no path", err)
	}
	content, err := canon.Encode(body)
	if err != nil {
		return nil, nil, internal("a record cannot be encoded", err)
	}
	rw, digest, err := b.c.Finalize(cstore.RecordWrite{Path: path, Content: content, Expected: expected})
	if err != nil {
		return nil, nil, internal("a record cannot be finalized", err)
	}
	v, err := canon.Parse(rw.Content)
	if err != nil {
		return nil, nil, internal("a record cannot be finalized", err)
	}
	final := v.(map[string]any)
	b.tx.Records = append(b.tx.Records, poolstore.Write{Kind: kind, ID: id, Body: final, Expected: expected})
	return final, &envelope.RecordRef{Schema: k.Schema, ID: id, HomeID: b.c.Home.HomeID, RecordRevision: expected + 1, Digest: digest}, nil
}

// receipt makes the receipt of the kind for this call, with the identity given
// where an earlier step named it in a record.
func (b *builder) receipt(kind string, evidence []envelope.Evidence) (envelope.Receipt, error) {
	r, err := b.c.NewReceipt(kind, versionOne, nil, evidence, nil)
	if err != nil {
		return r, internal("a receipt cannot be made", err)
	}
	return r, nil
}

// seal adds the receipt, about the record, in the scope given; for a
// receipt that has an event it appends the event beside it, as the Pool
// registers them.
func (b *builder) seal(r envelope.Receipt, subject *envelope.RecordRef, scope envelope.Scope, event bool) error {
	r.Subject = subject
	r.Scope = scope
	b.tx.Receipts = append(b.tx.Receipts, r)
	if !event {
		return nil
	}
	eventID, err := ids.Mint(b.c.Clock, ids.Event)
	if err != nil {
		return internal("an event cannot be made", err)
	}
	now, err := b.c.Stamp()
	if err != nil {
		return internal("an event cannot be made", err)
	}
	e := map[string]any{
		"schema": "hive.event", "schema_version": versionOne, "event_id": eventID,
		"kind": r.Kind, "kind_version": versionOne,
		"source":         map[string]any{"package": poolstore.Package, "source_id": poolstore.Package},
		"correlation_id": b.c.Request.CorrelationID,
		"causation":      map[string]any{"schema": "hive.request", "id": b.c.Request.RequestID},
		"occurred_at":    now, "observed_at": now,
		"basis": map[string]any{"kind": "receipt", "ref": map[string]any{"schema": "hive.receipt", "id": r.ReceiptID}},
		"body":  map[string]any{},
	}
	e["scope_level"], e["home_id"] = scope.Level, scope.HomeID
	if scope.WorkspaceID != "" {
		e["workspace_id"] = scope.WorkspaceID
	}
	if scope.ProjectID != "" {
		e["project_id"] = scope.ProjectID
	}
	if scope.TaskID != "" {
		e["task_id"] = scope.TaskID
	}
	if scope.Generation != 0 {
		e["generation"] = scope.Generation
	}
	content, err := canon.Encode(e)
	if err != nil {
		return internal("an event cannot be written", err)
	}
	b.tx.Events = append(b.tx.Events, cstore.Event{ID: eventID, Content: content})
	return nil
}
