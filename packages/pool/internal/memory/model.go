package memory

import (
	"time"

	"fmt"
	"github.com/kesk-git/hive/contract/clock"
	"strconv"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
)

// The tiers of a projection, the version of the renderer that writes the text
// and the sources of a rendering name, and the schemas of the records a
// rendering reads.
const (
	TierAlways  = "always"
	TierSubject = "subject"

	RendererVersion = "memory-renderer-1"

	schemaRule    = "pool.rule"
	schemaSubject = "pool.subject"
	schemaBudget  = "pool.budget"
	budgetID      = "memory"
	ownerPool     = "pool"
)

// The audiences of a subject projection: the rule roles, which an always
// projection narrows to the Hive and the Fleet.
var roles = []string{
	"hive", "fleet", "class-0", "class-1", "class-2", "class-3", "class-4", "class-5", "class-6", "class-7",
	"reviewer", "fixer", "validation",
}

// Key is what identifies a projection: its full scope, tier, audience and, for a
// subject projection, its subject (spec/pool.md, Generated knowledge).
type Key struct {
	Scope     rule.Scope
	Tier      string
	Audience  string
	SubjectID string
}

func (k Key) String() string {
	s := k.Scope.Level
	if k.Scope.WorkspaceID != "" {
		s += "/" + k.Scope.WorkspaceID
	}
	if k.Scope.ProjectID != "" {
		s += "/" + k.Scope.ProjectID
	}
	s += " " + k.Tier + " " + k.Audience
	if k.SubjectID != "" {
		s += " " + k.SubjectID
	}
	return s
}

// Valid says whether a projection of the key can exist. An always projection is
// the Hive's, of audience hive or fleet at the Hive's scope, or a Fleet's own
// at its workspace's scope; a subject projection is of any audience at a scope
// the subject's own contains. The contract states no other always projection
// and no budget for one, so none is rendered.
func (k Key) Valid() error {
	switch k.Scope.Level {
	case envelope.ScopeHive, envelope.ScopeWorkspace, envelope.ScopeProject:
	default:
		return fmt.Errorf("a projection is of the Hive's, a workspace's or a project's scope, not %q", k.Scope.Level)
	}
	switch k.Tier {
	case TierAlways:
		if k.SubjectID != "" {
			return fmt.Errorf("an always projection has no subject")
		}
		switch {
		case k.Scope.Level == envelope.ScopeHive && (k.Audience == "hive" || k.Audience == "fleet"):
		case k.Scope.Level == envelope.ScopeWorkspace && k.Audience == "fleet":
		default:
			return fmt.Errorf("an always projection is the Hive's, of audience hive or fleet, or a Fleet's own at its workspace's scope")
		}
	case TierSubject:
		if k.SubjectID == "" {
			return fmt.Errorf("a subject projection names its subject")
		}
		if !has(roles, k.Audience) {
			return fmt.Errorf("%q is not an audience", k.Audience)
		}
	default:
		return fmt.Errorf("%q is not a tier", k.Tier)
	}
	return nil
}

func has(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// within says whether the scope inner is the scope outer or lies beneath it.
func within(outer, inner rule.Scope) bool {
	if outer.HomeID != inner.HomeID && outer.HomeID != "" && inner.HomeID != "" {
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

// envelopeScope is the scope as a resolution is asked for.
func envelopeScope(s rule.Scope) envelope.Scope {
	return envelope.Scope{Level: s.Level, HomeID: s.HomeID, WorkspaceID: s.WorkspaceID, ProjectID: s.ProjectID}
}

// scopeOf is the scope a record carries.
func scopeOf(rec map[string]any) rule.Scope {
	return rule.Scope{
		Level: str(rec["scope_level"]), HomeID: str(rec["home_id"]),
		WorkspaceID: str(rec["workspace_id"]), ProjectID: str(rec["project_id"]),
	}
}

// putScope writes the scope's members into a record.
func putScope(m map[string]any, s rule.Scope) {
	m["scope_level"], m["home_id"] = s.Level, s.HomeID
	if s.WorkspaceID != "" {
		m["workspace_id"] = s.WorkspaceID
	}
	if s.ProjectID != "" {
		m["project_id"] = s.ProjectID
	}
}

func str(v any) string { s, _ := v.(string); return s }

func num(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}

func list(v any) []any { l, _ := v.([]any); return l }

func tree(v any) map[string]any { m, _ := v.(map[string]any); return m }

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

// Source is one record a rendering read, at the revision it read.
type Source struct {
	Schema, ID string
	Revision   int64
}

func (s Source) key() string { return s.Schema + "/" + s.ID + "@" + strconv.FormatInt(s.Revision, 10) }

// ref is the record reference the schema names a source by.
func (s Source) ref(homeID string) map[string]any {
	return map[string]any{"schema": s.Schema, "id": s.ID, "home_id": homeID, "record_revision": s.Revision}
}

// tree is the source as a rendering holds it.
func (s Source) tree(homeID string) map[string]any {
	return map[string]any{"ref": s.ref(homeID), "owner": ownerPool}
}

func sourceOf(v any) Source {
	ref := tree(tree(v)["ref"])
	return Source{Schema: str(ref["schema"]), ID: str(ref["id"]), Revision: num(ref["record_revision"])}
}

// RuleRef is one line of a rendering's rule inventory.
type RuleRef struct {
	RuleID   string
	Revision int64
}

// Rendering is one rendering of a projection: what was read, and what it made.
type Rendering struct {
	Number         int64
	RenderedAt     string
	RendererVer    string
	RulesDigest    string
	Sources        []Source
	Text           string
	Digest         string
	Bytes          int64
	Rules          []RuleRef
	At             string
	Conflicts      []rule.Conflict
	budgetRevision int64
}

// tree is the rendering as the record holds it.
func (r *Rendering) tree(homeID string) map[string]any {
	sources := make([]any, len(r.Sources))
	for i, s := range r.Sources {
		sources[i] = s.tree(homeID)
	}
	rules := make([]any, len(r.Rules))
	for i, e := range r.Rules {
		rules[i] = map[string]any{"rule_id": e.RuleID, "record_revision": e.Revision}
	}
	return map[string]any{
		"rendering": r.Number, "rendered_at": r.RenderedAt, "renderer_version": r.RendererVer,
		"rules_digest": r.RulesDigest, "sources": sources, "text": r.Text, "digest": r.Digest,
		"bytes": r.Bytes, "rules": rules,
	}
}

func parseRendering(v any) (Rendering, error) {
	m := tree(v)
	if m == nil {
		return Rendering{}, fmt.Errorf("a rendering is an object")
	}
	r := Rendering{
		Number: num(m["rendering"]), RenderedAt: str(m["rendered_at"]), RendererVer: str(m["renderer_version"]),
		RulesDigest: str(m["rules_digest"]), Text: str(m["text"]), Digest: str(m["digest"]), Bytes: num(m["bytes"]),
	}
	for _, s := range list(m["sources"]) {
		r.Sources = append(r.Sources, sourceOf(s))
	}
	for _, e := range list(m["rules"]) {
		e := tree(e)
		r.Rules = append(r.Rules, RuleRef{RuleID: str(e["rule_id"]), Revision: num(e["record_revision"])})
	}
	if r.Number < 1 || r.Digest == "" {
		return Rendering{}, fmt.Errorf("a rendering is numbered and has the digest of its text")
	}
	return r, nil
}

func parseInstant(s string) (time.Time, error) { return clock.Parse(s) }
