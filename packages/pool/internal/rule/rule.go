package rule

import (
	"fmt"

	"github.com/kesk-git/hive/contract/canon"
)

// SchemaRule is the name every rule record carries in its schema member, and
// RuleVersion the version of it that this build reads.
const (
	SchemaRule  = "pool.rule"
	RuleVersion = "2.0"
)

// The statuses of a rule, the kinds, the tiers and the relations a rule's
// changes names (records/pool.rule).
const (
	StatusInForce = "in-force"
	StatusExpired = "expired"
	StatusLifted  = "lifted"
	StatusEnded   = "ended"

	KindOperating = "operating"
	KindProduct   = "product"

	RelationSupersedes = "supersedes"
	RelationQualifies  = "qualifies"

	AmendmentExtend = "extend"
	AmendmentEnd    = "end"
	AmendmentLift   = "lift"
	AmendmentNote   = "note"
)

// Rule is one ruling, with its amendments (records/pool.rule). An operating
// rule carries Tier, FiledUnder and RuleLine; a product rule carries Governs
// and Specifications.
type Rule struct {
	RecordRevision int64
	RuleID         string
	Scope          Scope
	// TaskID and Generation narrow the rule to one task generation.
	TaskID     string
	Generation int64
	// Subject is the rule's conflict key.
	Subject string
	Words   string
	// Origin is where the words came from, kept whole.
	Origin   map[string]any
	RuleLine string
	// Applies are the roles the rule applies to; empty is every role.
	Applies        []string
	Status         string
	EffectiveAt    string
	ExpiresAt      string
	Lift           *Lift
	Changes        *Changes
	Authority      map[string]any
	Intake         map[string]any
	Links          []map[string]any
	Amendments     []Amendment
	Expiry         map[string]any
	CreatedAt      string
	UpdatedAt      string
	Writer         map[string]any
	Unknown        map[string]any
	Kind           string
	Tier           string
	FiledUnder     string
	Governs        map[string]any
	Specifications []map[string]any
}

// Lift is a temporary rule's lift condition: the kind of the receipt and the
// record it must be about.
type Lift struct {
	ReceiptKind string
	Subject     map[string]any
}

// Changes names the standing rule this rule supersedes or qualifies.
type Changes struct {
	RuleID         string
	RecordRevision int64
	Relation       string
}

// Amendment is one attributed revision of a rule's operation.
type Amendment struct {
	Sequence     int64
	Kind         string
	Words        string
	NewExpiresAt string
	// Evidence is the receipt a lift names, by reference, and Receipt the
	// receipt as the request carried it, whole.
	Evidence   map[string]any
	Receipt    map[string]any
	Authority  map[string]any
	RecordedAt string
	RecordedBy map[string]any
	RequestID  string
	ReceiptID  string
}

// DecodeRule decodes the bytes of a rule record.
func DecodeRule(data []byte) (*Rule, error) {
	m, err := decode(data)
	if err != nil {
		return nil, err
	}
	return ParseRule(m)
}

// ParseRule decodes a rule from its tree.
func ParseRule(tree map[string]any) (*Rule, error) {
	o := open(SchemaRule, tree)
	if o.str("schema", true) != SchemaRule {
		o.fail("schema is not %s", SchemaRule)
	}
	if o.str("schema_version", true) != RuleVersion {
		o.fail("schema_version is not %s", RuleVersion)
	}
	r := &Rule{
		RecordRevision: o.num("record_revision", true),
		RuleID:         o.str("rule_id", true),
		Scope:          readScope(o),
		TaskID:         o.str("task_id", false),
		Generation:     o.num("generation", false),
		Subject:        o.str("subject", true),
		Words:          o.str("words", true),
		Origin:         o.tree("origin", true),
		RuleLine:       o.str("rule_line", false),
		Applies:        o.texts("applies", true),
		Status:         o.str("status", true),
		EffectiveAt:    o.str("effective_at", true),
		ExpiresAt:      o.str("expires_at", false),
		Authority:      o.tree("authority", true),
		Intake:         o.tree("intake", false),
		Links:          o.trees("links", false),
		Expiry:         o.tree("expiry", false),
		CreatedAt:      o.str("created_at", true),
		UpdatedAt:      o.str("updated_at", true),
		Writer:         o.tree("writer", true),
		Unknown:        o.tree("unknown", false),
		Kind:           o.str("kind", true),
		Tier:           o.str("tier", false),
		FiledUnder:     o.str("filed_under", false),
		Governs:        o.tree("governs", false),
		Specifications: o.trees("specifications", false),
	}
	if c := o.child("lift"); c != nil {
		r.Lift = &Lift{ReceiptKind: c.str("receipt_kind", true), Subject: c.tree("subject", true)}
		if err := c.done(); err != nil {
			o.fail("%v", err)
		}
	}
	if c := o.child("changes"); c != nil {
		r.Changes = &Changes{RuleID: c.str("rule_id", true), RecordRevision: c.num("record_revision", true), Relation: c.str("relation", true)}
		if err := c.done(); err != nil {
			o.fail("%v", err)
		}
	}
	for i, e := range o.array("amendments", true) {
		c := open(fmt.Sprintf("amendments[%d]", i), e)
		a := Amendment{
			Sequence:     c.num("sequence", true),
			Kind:         c.str("kind", true),
			Words:        c.str("words", true),
			NewExpiresAt: c.str("new_expires_at", false),
			Evidence:     c.tree("evidence", false),
			Receipt:      c.tree("receipt", false),
			Authority:    c.tree("authority", true),
			RecordedAt:   c.str("recorded_at", true),
			RecordedBy:   c.tree("recorded_by", true),
			RequestID:    c.str("request_id", true),
			ReceiptID:    c.str("receipt_id", true),
		}
		if err := c.done(); err != nil {
			o.fail("%v", err)
			break
		}
		r.Amendments = append(r.Amendments, a)
	}
	if err := o.done(); err != nil {
		return nil, err
	}
	return r, nil
}

// Tree is the rule as the document it is stored as.
func (r *Rule) Tree() map[string]any {
	m := map[string]any{
		"schema": SchemaRule, "schema_version": RuleVersion, "record_revision": r.RecordRevision,
		"rule_id": r.RuleID, "subject": r.Subject, "words": r.Words, "origin": copyTree(r.Origin),
		"applies": textsAny(r.Applies), "status": r.Status, "effective_at": r.EffectiveAt,
		"authority": copyTree(r.Authority), "created_at": r.CreatedAt, "updated_at": r.UpdatedAt,
		"writer": copyTree(r.Writer), "kind": r.Kind,
	}
	r.Scope.into(m)
	put(m, "task_id", r.TaskID)
	if r.Generation != 0 {
		m["generation"] = r.Generation
	}
	put(m, "rule_line", r.RuleLine)
	put(m, "expires_at", r.ExpiresAt)
	put(m, "tier", r.Tier)
	put(m, "filed_under", r.FiledUnder)
	if r.Lift != nil {
		m["lift"] = map[string]any{"receipt_kind": r.Lift.ReceiptKind, "subject": copyTree(r.Lift.Subject)}
	}
	if r.Changes != nil {
		m["changes"] = map[string]any{"rule_id": r.Changes.RuleID, "record_revision": r.Changes.RecordRevision, "relation": r.Changes.Relation}
	}
	for key, tree := range map[string]map[string]any{"intake": r.Intake, "expiry": r.Expiry, "unknown": r.Unknown, "governs": r.Governs} {
		if tree != nil {
			m[key] = copyTree(tree)
		}
	}
	if r.Links != nil {
		m["links"] = asAny(r.Links)
	}
	if r.Specifications != nil {
		m["specifications"] = asAny(r.Specifications)
	}
	amendments := make([]any, len(r.Amendments))
	for i, a := range r.Amendments {
		amendments[i] = a.tree()
	}
	m["amendments"] = amendments
	return m
}

func (a Amendment) tree() map[string]any {
	m := map[string]any{
		"sequence": a.Sequence, "kind": a.Kind, "words": a.Words, "authority": copyTree(a.Authority),
		"recorded_at": a.RecordedAt, "recorded_by": copyTree(a.RecordedBy), "request_id": a.RequestID, "receipt_id": a.ReceiptID,
	}
	put(m, "new_expires_at", a.NewExpiresAt)
	if a.Evidence != nil {
		m["evidence"] = copyTree(a.Evidence)
	}
	if a.Receipt != nil {
		m["receipt"] = copyTree(a.Receipt)
	}
	return m
}

// Encode is the rule as the canonical document it is stored as.
func (r *Rule) Encode() ([]byte, error) { return canon.Encode(r.Tree()) }
