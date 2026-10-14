package rule

import "github.com/kesk-git/hive/contract/canon"

// SchemaSubject is the name every subject record carries in its schema
// member, and SubjectVersion the version of it that this build reads.
const (
	SchemaSubject  = "pool.subject"
	SubjectVersion = "1.0"
)

// Subject is a memory subject: a loading label at one non-task scope, with the
// steps that load it and the facts filed under it (records/pool.subject).
type Subject struct {
	RecordRevision int64
	SubjectID      string
	Scope          Scope
	Key            string
	LoadsAt        []string
	Facts          []Fact
	CreatedAt      string
	UpdatedAt      string
	Writer         map[string]any
	Unknown        map[string]any
}

// Fact is one fact of a subject: its subject-local identity, its exact text
// and the evidence it rests on, kept whole.
type Fact struct {
	FactID     string
	Text       string
	Provenance map[string]any
}

// DecodeSubject decodes the bytes of a subject record.
func DecodeSubject(data []byte) (*Subject, error) {
	m, err := decode(data)
	if err != nil {
		return nil, err
	}
	return ParseSubject(m)
}

// ParseSubject decodes a subject from its tree.
func ParseSubject(tree map[string]any) (*Subject, error) {
	o := open(SchemaSubject, tree)
	if o.str("schema", true) != SchemaSubject {
		o.fail("schema is not %s", SchemaSubject)
	}
	if o.str("schema_version", true) != SubjectVersion {
		o.fail("schema_version is not %s", SubjectVersion)
	}
	s := &Subject{
		RecordRevision: o.num("record_revision", true),
		SubjectID:      o.str("subject_id", true),
		Scope:          readScope(o),
		Key:            o.str("key", true),
		LoadsAt:        o.texts("loads_at", true),
		CreatedAt:      o.str("created_at", true),
		UpdatedAt:      o.str("updated_at", true),
		Writer:         o.tree("writer", true),
		Unknown:        o.tree("unknown", false),
	}
	for _, e := range o.array("facts", true) {
		c := open("facts", e)
		f := Fact{FactID: c.str("fact_id", true), Text: c.str("text", true), Provenance: c.tree("provenance", true)}
		if err := c.done(); err != nil {
			o.fail("%v", err)
			break
		}
		s.Facts = append(s.Facts, f)
	}
	if err := o.done(); err != nil {
		return nil, err
	}
	return s, nil
}

// Tree is the subject as the document it is stored as.
func (s *Subject) Tree() map[string]any {
	facts := make([]any, len(s.Facts))
	for i, f := range s.Facts {
		facts[i] = map[string]any{"fact_id": f.FactID, "text": f.Text, "provenance": copyTree(f.Provenance)}
	}
	m := map[string]any{
		"schema": SchemaSubject, "schema_version": SubjectVersion, "record_revision": s.RecordRevision,
		"subject_id": s.SubjectID, "key": s.Key, "loads_at": textsAny(s.LoadsAt), "facts": facts,
		"created_at": s.CreatedAt, "updated_at": s.UpdatedAt, "writer": copyTree(s.Writer),
	}
	s.Scope.into(m)
	if s.Unknown != nil {
		m["unknown"] = copyTree(s.Unknown)
	}
	return m
}

// Encode is the subject as the canonical document it is stored as.
func (s *Subject) Encode() ([]byte, error) { return canon.Encode(s.Tree()) }
