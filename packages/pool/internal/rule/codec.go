// Package rule keeps the Pool's rule and subject records and reads them
// (spec/pool.md, Rules and Subjects): their typed form, decoded from a stored
// record and encoded back to the same document; the reading of a rule's
// expiry by the clock and of its lift by the receipt recorded with it; and
// hive-rule resolve, the read-only computation of the rules effective for a
// scope, a role and an instant. The package registers the operation and the
// readers of the kinds rule and subject from its init function, so that
// importing it builds them into the executable. It builds no recorder: the
// operations record, amend and expire and hive-memory subject write these
// records and are later lanes.
//
// A codec is not the schema. It decodes a document whose members are of the
// kinds the schema names and refuses any it does not name; whether the
// document is a record is the schema's, which the store holds every record to
// when it is written and when it is read.
package rule

import (
	"errors"
	"fmt"
	"slices"

	"github.com/kesk-git/hive/contract/canon"
)

// Scope is the scope members a rule or a subject carries: the level and the
// identities that level names.
type Scope struct {
	Level       string
	HomeID      string
	WorkspaceID string
	ProjectID   string
}

func (s Scope) into(m map[string]any) {
	m["scope_level"], m["home_id"] = s.Level, s.HomeID
	put(m, "workspace_id", s.WorkspaceID)
	put(m, "project_id", s.ProjectID)
}

func readScope(o *object) Scope {
	return Scope{
		Level:       o.str("scope_level", true),
		HomeID:      o.str("home_id", true),
		WorkspaceID: o.str("workspace_id", false),
		ProjectID:   o.str("project_id", false),
	}
}

// put sets the member when the text is not empty: an optional member of the
// schemas is never empty.
func put(m map[string]any, key, text string) {
	if text != "" {
		m[key] = text
	}
}

// object reads the members of a decoded JSON object, in the kinds the schema
// gives them, and remembers the first member that is not as it should be. done
// reports it, and any member that was never asked for.
type object struct {
	where string
	m     map[string]any
	used  map[string]bool
	err   error
}

func open(where string, v any) *object {
	o := &object{where: where, used: map[string]bool{}}
	m, ok := v.(map[string]any)
	if !ok {
		o.err = fmt.Errorf("%s is not an object", where)
		return o
	}
	o.m = m
	return o
}

func (o *object) fail(format string, args ...any) {
	if o.err == nil {
		o.err = fmt.Errorf("%s: %s", o.where, fmt.Sprintf(format, args...))
	}
}

// raw returns the member. A required member that is absent is an error.
func (o *object) raw(key string, required bool) (any, bool) {
	if o.m == nil {
		return nil, false
	}
	o.used[key] = true
	v, ok := o.m[key]
	if !ok && required {
		o.fail("%s is required", key)
	}
	return v, ok
}

func (o *object) str(key string, required bool) string {
	v, ok := o.raw(key, required)
	if !ok {
		return ""
	}
	s, isString := v.(string)
	if !isString {
		o.fail("%s is not text", key)
	}
	return s
}

func (o *object) num(key string, required bool) int64 {
	v, ok := o.raw(key, required)
	if !ok {
		return 0
	}
	n, isNumber := v.(int64)
	if !isNumber {
		o.fail("%s is not a whole number", key)
	}
	return n
}

// tree returns a member that is a document of its own whose members the codec
// keeps whole. nil when absent.
func (o *object) tree(key string, required bool) map[string]any {
	v, ok := o.raw(key, required)
	if !ok {
		return nil
	}
	m, isObject := v.(map[string]any)
	if !isObject {
		o.fail("%s is not an object", key)
		return nil
	}
	return copyTree(m).(map[string]any)
}

// child returns a member that is an object the codec reads in full. nil when
// absent.
func (o *object) child(key string) *object {
	v, ok := o.raw(key, false)
	if !ok {
		return nil
	}
	c := open(o.where+"."+key, v)
	if c.err != nil {
		o.fail("%s is not an object", key)
		return nil
	}
	return c
}

// array returns the elements of an array member. nil when absent, and
// non-nil, if empty, when present.
func (o *object) array(key string, required bool) []any {
	v, ok := o.raw(key, required)
	if !ok {
		return nil
	}
	a, isArray := v.([]any)
	if !isArray {
		o.fail("%s is not an array", key)
		return nil
	}
	return a
}

// texts returns the members of an array of text. nil when absent.
func (o *object) texts(key string, required bool) []string {
	a := o.array(key, required)
	if a == nil {
		return nil
	}
	out := make([]string, 0, len(a))
	for i, e := range a {
		s, ok := e.(string)
		if !ok {
			o.fail("%s[%d] is not text", key, i)
			return nil
		}
		out = append(out, s)
	}
	return out
}

// trees returns an array member of documents the codec keeps whole. nil when
// absent.
func (o *object) trees(key string, required bool) []map[string]any {
	a := o.array(key, required)
	if a == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(a))
	for i, e := range a {
		m, isObject := e.(map[string]any)
		if !isObject {
			o.fail("%s[%d] is not an object", key, i)
			return nil
		}
		out = append(out, copyTree(m).(map[string]any))
	}
	return out
}

// done reports the first fault, or a member the schema does not name.
func (o *object) done() error {
	if o.err != nil {
		return o.err
	}
	var extra []string
	for k := range o.m {
		if !o.used[k] {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		slices.Sort(extra)
		return fmt.Errorf("%s: %s is not a member of the record", o.where, extra[0])
	}
	return nil
}

// copyTree is a deep copy of a decoded JSON value, so that the typed form
// never shares a document with the one it was decoded from.
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

func asAny(ms []map[string]any) []any {
	out := make([]any, len(ms))
	for i, m := range ms {
		out[i] = copyTree(m)
	}
	return out
}

func textsAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// decode parses the bytes of a record into its tree.
func decode(data []byte) (map[string]any, error) {
	v, err := canon.Parse(data)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("a record is a JSON object")
	}
	return m, nil
}
