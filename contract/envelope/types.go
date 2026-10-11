package envelope

import (
	"fmt"
)

// RecordRef is a reference to one durable record. RecordRevision and Digest
// are given when the reference binds a particular state of the record; a
// zero RecordRevision and an empty Digest or HomeID are absent.
type RecordRef struct {
	Schema         string
	ID             string
	HomeID         string
	RecordRevision int64
	Digest         string
}

// Authority is the recorded authority an operation relies on: a kind, and
// for every kind but none a reference to the record. It is a pointer the
// callee verifies, never a grant by itself.
type Authority struct {
	Kind string
	Ref  *RecordRef
}

// The kinds of authority.
const (
	AuthorityNone        = "none"
	AuthorityWord        = "word"
	AuthorityPosture     = "posture"
	AuthorityCharter     = "charter"
	AuthorityRule        = "rule"
	AuthorityInstruction = "instruction"
)

// Caller is the attribution of who issued a request. It authenticates no one
// and grants nothing. The fields beyond Kind are given as the kind requires:
// Package and Command for a package, SessionID and SessionGeneration for a
// supervisor, TaskID and Generation for a worker. An empty string or a zero
// number is absent.
type Caller struct {
	Kind              string
	Package           string
	Command           string
	SessionID         string
	SessionGeneration int64
	TaskID            string
	Generation        int64
}

// The kinds of caller.
const (
	CallerPackage    = "package"
	CallerSupervisor = "supervisor"
	CallerWorker     = "worker"
	CallerOperator   = "operator"
	CallerTest       = "test"
)

// Scope is where a request, receipt or event belongs (spec/identities.md,
// Scope): a declared level and the flat identity fields the level requires.
// An empty string or a zero Generation is absent.
type Scope struct {
	Level       string
	HomeID      string
	WorkspaceID string
	ProjectID   string
	CloneID     string
	TaskID      string
	Generation  int64
}

// The scope levels.
const (
	ScopeHive      = "hive"
	ScopeWorkspace = "workspace"
	ScopeProject   = "project"
)

// Writer is the command that wrote a durable record.
type Writer struct {
	Package               string
	Command               string
	Operation             string
	ImplementationVersion string
}

// Responder is the package, command, implementation version and contract
// version that answered a request.
type Responder struct {
	Package               string
	Command               string
	ImplementationVersion string
	ContractVersion       string
}

// ExpectedRevision is one record a mutation depends on, with the revision
// the caller read.
type ExpectedRevision struct {
	Schema         string
	ID             string
	RecordRevision int64
}

// Evidence is one record a receipt's fact rests on, with its role.
type Evidence struct {
	Role string
	Ref  RecordRef
}

// A field reader over the decoded tree of a document. Every document is
// validated against its schema before it is read, so a failure here is an
// envelope of the wrong shape, which is reported and never guessed at.
type fields struct {
	m  map[string]any
	at string
}

func (f fields) fail(key, want string) error {
	return fmt.Errorf("envelope: %s/%s is not %s", f.at, key, want)
}

func (f fields) has(key string) bool {
	_, ok := f.m[key]
	return ok
}

func (f fields) str(key string) (string, error) {
	s, ok := f.m[key].(string)
	if !ok {
		return "", f.fail(key, "a string")
	}
	return s, nil
}

func (f fields) optStr(key string) (string, error) {
	if !f.has(key) {
		return "", nil
	}
	return f.str(key)
}

func (f fields) nullStr(key string) (string, error) {
	if f.m[key] == nil {
		return "", nil
	}
	return f.str(key)
}

func (f fields) whole(key string) (int64, error) {
	n, ok := f.m[key].(int64)
	if !ok {
		return 0, f.fail(key, "a whole number")
	}
	return n, nil
}

func (f fields) optWhole(key string) (int64, error) {
	if !f.has(key) {
		return 0, nil
	}
	return f.whole(key)
}

func (f fields) boolean(key string) (bool, error) {
	b, ok := f.m[key].(bool)
	if !ok {
		return false, f.fail(key, "a boolean")
	}
	return b, nil
}

func (f fields) object(key string) (fields, error) {
	m, ok := f.m[key].(map[string]any)
	if !ok {
		return fields{}, f.fail(key, "an object")
	}
	return fields{m, f.at + "/" + key}, nil
}

func (f fields) optObject(key string) (fields, bool, error) {
	if !f.has(key) {
		return fields{}, false, nil
	}
	o, err := f.object(key)
	return o, true, err
}

func (f fields) array(key string) ([]any, error) {
	a, ok := f.m[key].([]any)
	if !ok {
		return nil, f.fail(key, "an array")
	}
	return a, nil
}

func (f fields) objects(key string) ([]fields, error) {
	a, err := f.array(key)
	if err != nil {
		return nil, err
	}
	out := make([]fields, 0, len(a))
	for i, e := range a {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, f.fail(fmt.Sprintf("%s/%d", key, i), "an object")
		}
		out = append(out, fields{m, fmt.Sprintf("%s/%s/%d", f.at, key, i)})
	}
	return out, nil
}

func (f fields) strings(key string) ([]string, error) {
	a, err := f.array(key)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(a))
	for i, e := range a {
		s, ok := e.(string)
		if !ok {
			return nil, f.fail(fmt.Sprintf("%s/%d", key, i), "a string")
		}
		out = append(out, s)
	}
	return out, nil
}

func readRecordRef(f fields) (RecordRef, error) {
	var r RecordRef
	var err error
	if r.Schema, err = f.str("schema"); err != nil {
		return r, err
	}
	if r.ID, err = f.str("id"); err != nil {
		return r, err
	}
	if r.HomeID, err = f.optStr("home_id"); err != nil {
		return r, err
	}
	if r.RecordRevision, err = f.optWhole("record_revision"); err != nil {
		return r, err
	}
	if r.Digest, err = f.optStr("digest"); err != nil {
		return r, err
	}
	return r, nil
}

func readOptRecordRef(f fields, key string) (*RecordRef, error) {
	o, ok, err := f.optObject(key)
	if err != nil || !ok {
		return nil, err
	}
	r, err := readRecordRef(o)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func readAuthority(f fields) (Authority, error) {
	var a Authority
	var err error
	if a.Kind, err = f.str("kind"); err != nil {
		return a, err
	}
	if a.Ref, err = readOptRecordRef(f, "ref"); err != nil {
		return a, err
	}
	return a, nil
}

func readCaller(f fields) (Caller, error) {
	var c Caller
	var err error
	if c.Kind, err = f.str("kind"); err != nil {
		return c, err
	}
	if c.Package, err = f.optStr("package"); err != nil {
		return c, err
	}
	if c.Command, err = f.optStr("command"); err != nil {
		return c, err
	}
	if c.SessionID, err = f.optStr("session_id"); err != nil {
		return c, err
	}
	if c.SessionGeneration, err = f.optWhole("session_generation"); err != nil {
		return c, err
	}
	if c.TaskID, err = f.optStr("task_id"); err != nil {
		return c, err
	}
	if c.Generation, err = f.optWhole("generation"); err != nil {
		return c, err
	}
	return c, nil
}

func readScope(f fields) (Scope, error) {
	var s Scope
	var err error
	if s.Level, err = f.str("scope_level"); err != nil {
		return s, err
	}
	if s.HomeID, err = f.str("home_id"); err != nil {
		return s, err
	}
	if s.WorkspaceID, err = f.optStr("workspace_id"); err != nil {
		return s, err
	}
	if s.ProjectID, err = f.optStr("project_id"); err != nil {
		return s, err
	}
	if s.CloneID, err = f.optStr("clone_id"); err != nil {
		return s, err
	}
	if s.TaskID, err = f.optStr("task_id"); err != nil {
		return s, err
	}
	if s.Generation, err = f.optWhole("generation"); err != nil {
		return s, err
	}
	return s, nil
}

func readWriter(f fields) (Writer, error) {
	var w Writer
	var err error
	if w.Package, err = f.str("package"); err != nil {
		return w, err
	}
	if w.Command, err = f.str("command"); err != nil {
		return w, err
	}
	if w.Operation, err = f.str("operation"); err != nil {
		return w, err
	}
	if w.ImplementationVersion, err = f.str("implementation_version"); err != nil {
		return w, err
	}
	return w, nil
}

func readResponder(f fields) (Responder, error) {
	var r Responder
	var err error
	if r.Package, err = f.str("package"); err != nil {
		return r, err
	}
	if r.Command, err = f.str("command"); err != nil {
		return r, err
	}
	if r.ImplementationVersion, err = f.str("implementation_version"); err != nil {
		return r, err
	}
	if r.ContractVersion, err = f.str("contract_version"); err != nil {
		return r, err
	}
	return r, nil
}

// The tree form of each value, as written: an absent field is left out.

func (r RecordRef) tree() map[string]any {
	m := map[string]any{"schema": r.Schema, "id": r.ID}
	put(m, "home_id", r.HomeID)
	putWhole(m, "record_revision", r.RecordRevision)
	put(m, "digest", r.Digest)
	return m
}

func (a Authority) tree() map[string]any {
	m := map[string]any{"kind": a.Kind}
	if a.Ref != nil {
		m["ref"] = a.Ref.tree()
	}
	return m
}

func (c Caller) tree() map[string]any {
	m := map[string]any{"kind": c.Kind}
	put(m, "package", c.Package)
	put(m, "command", c.Command)
	put(m, "session_id", c.SessionID)
	putWhole(m, "session_generation", c.SessionGeneration)
	put(m, "task_id", c.TaskID)
	putWhole(m, "generation", c.Generation)
	return m
}

// into writes the scope's flat fields into m.
func (s Scope) into(m map[string]any) {
	m["scope_level"] = s.Level
	m["home_id"] = s.HomeID
	put(m, "workspace_id", s.WorkspaceID)
	put(m, "project_id", s.ProjectID)
	put(m, "clone_id", s.CloneID)
	put(m, "task_id", s.TaskID)
	putWhole(m, "generation", s.Generation)
}

func (w Writer) tree() map[string]any {
	return map[string]any{
		"package":                w.Package,
		"command":                w.Command,
		"operation":              w.Operation,
		"implementation_version": w.ImplementationVersion,
	}
}

func (r Responder) tree() map[string]any {
	return map[string]any{
		"package":                r.Package,
		"command":                r.Command,
		"implementation_version": r.ImplementationVersion,
		"contract_version":       r.ContractVersion,
	}
}

func put(m map[string]any, key, value string) {
	if value != "" {
		m[key] = value
	}
}

func putWhole(m map[string]any, key string, value int64) {
	if value != 0 {
		m[key] = value
	}
}

// stringsTree writes a list of strings as a JSON array, empty when nil.
func stringsTree(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

// cloneTree copies a JSON tree, so that a document handed out shares nothing
// with the value it was made from.
func cloneTree(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = cloneTree(e)
		}
		return m
	case []any:
		a := make([]any, len(x))
		for i, e := range x {
			a[i] = cloneTree(e)
		}
		return a
	}
	return v
}

// cloneObject is cloneTree for an open member; nil stays nil.
func cloneObject(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	return cloneTree(m).(map[string]any)
}
