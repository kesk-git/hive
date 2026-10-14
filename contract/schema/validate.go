package schema

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// node is one compiled schema: an object of keywords or a boolean schema.
// Nodes are immutable once Load returns.
type node struct {
	loc string // "<$id>#<JSON pointer>", where this schema sits

	designated bool // sits under a designated open member
	open       bool // a bare object, which admits any member

	boolean   bool // a boolean schema; verdict holds its value
	verdict   bool
	ref       *node
	types     []string
	enum      []any
	hasEnum   bool
	constant  any
	hasConst  bool
	props     []property // sorted by name
	required  []string
	closed    bool // additionalProperties is false
	items     *node
	minItems  *int64
	maxItems  *int64
	minLength *int64
	maxLength *int64
	pattern   *regexp.Regexp
	patternIn string
	minimum   *int64
	maximum   *int64
	allOf     []*node
	anyOf     []*node
	oneOf     []*node
	not       *node
	ifNode    *node
	thenNode  *node
	elseNode  *node
}

type property struct {
	name string
	node *node
}

// Violation is one way a document fails a schema.
type Violation struct {
	Instance string // JSON pointer to the failing value; "" is the whole document
	Schema   string // location of the failing keyword: <$id>#<JSON pointer>
	Message  string // what is wrong; it never repeats the document's text
}

func (v Violation) String() string {
	at := v.Instance
	if at == "" {
		at = "(document)"
	}
	return fmt.Sprintf("%s: %s [%s]", at, v.Message, v.Schema)
}

// maxViolations bounds what one validation reports; Omitted counts the rest.
const maxViolations = 100

// ValidationError is the failure of a document against a schema. It lists the
// violations in a fixed order.
type ValidationError struct {
	Violations []Violation
	Omitted    int // violations found after the first maxViolations
}

func (e *ValidationError) Error() string {
	var sb strings.Builder
	for i, v := range e.Violations {
		if i == 3 {
			fmt.Fprintf(&sb, "\n... and %d more", len(e.Violations)-i+e.Omitted)
			break
		}
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(v.String())
	}
	return sb.String()
}

// sink collects violations; a nil sink only asks whether the document passes.
type sink struct {
	list    []Violation
	omitted int
}

func (s *sink) add(inst, loc, format string, args ...any) {
	if len(s.list) >= maxViolations {
		s.omitted++
		return
	}
	s.list = append(s.list, Violation{inst, loc, fmt.Sprintf(format, args...)})
}

// check reports whether v, a well-formed value, satisfies n. When out is not nil every violation is
// added to it; otherwise the first one ends the check.
func (n *node) check(v any, at string, out *sink) bool {
	if n.boolean {
		if !n.verdict && out != nil {
			out.add(at, n.loc, "no value is allowed here")
		}
		return n.verdict
	}
	ok := true
	// fail records one violation and says whether checking may go on.
	fail := func(kw, format string, args ...any) bool {
		ok = false
		if out == nil {
			return false
		}
		out.add(at, n.loc+"/"+kw, format, args...)
		return true
	}

	if n.ref != nil && !n.ref.check(v, at, out) {
		ok = false
		if out == nil {
			return false
		}
	}
	kind := kindOf(v)
	if n.types != nil && !slices.ContainsFunc(n.types, func(t string) bool { return typeMatches(t, kind) }) {
		if !fail("type", "is %s, want %s", kind, strings.Join(n.types, " or ")) {
			return false
		}
	}
	if n.hasEnum && !slices.ContainsFunc(n.enum, func(e any) bool { return equal(e, v) }) {
		if !fail("enum", "is not one of the %d permitted values", len(n.enum)) {
			return false
		}
	}
	if n.hasConst && !equal(n.constant, v) {
		if !fail("const", "is not the one permitted value") {
			return false
		}
	}

	switch t := v.(type) {
	case string:
		if !n.checkString(t, fail) {
			return false
		}
	case map[string]any:
		for _, name := range n.required {
			if _, there := t[name]; !there {
				if !fail("required", "lacks the required member %q", name) {
					return false
				}
			}
		}
		for _, p := range n.props {
			if m, there := t[p.name]; there && !p.node.check(m, at+"/"+escape(p.name), out) {
				ok = false
				if out == nil {
					return false
				}
			}
		}
		if n.closed {
			for _, name := range slices.Sorted(maps.Keys(t)) {
				if !n.hasProp(name) {
					if !fail("additionalProperties", "has a member the schema does not list") {
						return false
					}
				}
			}
		}
	case []any:
		if n.minItems != nil && int64(len(t)) < *n.minItems {
			if !fail("minItems", "has %d items, want at least %d", len(t), *n.minItems) {
				return false
			}
		}
		if n.maxItems != nil && int64(len(t)) > *n.maxItems {
			if !fail("maxItems", "has %d items, want at most %d", len(t), *n.maxItems) {
				return false
			}
		}
		if n.items != nil {
			for i, e := range t {
				if !n.items.check(e, at+"/"+strconv.Itoa(i), out) {
					ok = false
					if out == nil {
						return false
					}
				}
			}
		}
	}
	if i, isInt := v.(int64); isInt {
		if n.minimum != nil && i < *n.minimum {
			if !fail("minimum", "is below the minimum %d", *n.minimum) {
				return false
			}
		}
		if n.maximum != nil && i > *n.maximum {
			if !fail("maximum", "is above the maximum %d", *n.maximum) {
				return false
			}
		}
	}

	for _, b := range n.allOf {
		if !b.check(v, at, out) {
			ok = false
			if out == nil {
				return false
			}
		}
	}
	if len(n.anyOf) > 0 && !slices.ContainsFunc(n.anyOf, func(b *node) bool { return b.check(v, at, nil) }) {
		if !fail("anyOf", "matches none of the %d alternatives", len(n.anyOf)) {
			return false
		}
	}
	if len(n.oneOf) > 0 {
		matches := 0
		for _, b := range n.oneOf {
			if b.check(v, at, nil) {
				matches++
			}
		}
		if matches != 1 {
			if !fail("oneOf", "matches %d of the %d alternatives, want exactly 1", matches, len(n.oneOf)) {
				return false
			}
		}
	}
	if n.not != nil && n.not.check(v, at, nil) {
		if !fail("not", "matches a schema it must not match") {
			return false
		}
	}
	if n.ifNode != nil {
		if n.ifNode.check(v, at, nil) {
			if n.thenNode != nil && !n.thenNode.check(v, at, out) {
				ok = false
				if out == nil {
					return false
				}
			}
		} else if n.elseNode != nil && !n.elseNode.check(v, at, out) {
			ok = false
			if out == nil {
				return false
			}
		}
	}
	return ok
}

func (n *node) hasProp(name string) bool {
	_, found := slices.BinarySearchFunc(n.props, name, func(p property, name string) int {
		return strings.Compare(p.name, name)
	})
	return found
}

// checkString applies the keywords that look at a string. It returns false
// when checking should stop.
func (n *node) checkString(s string, fail func(kw, format string, args ...any) bool) bool {
	if n.minLength != nil || n.maxLength != nil {
		length := int64(utf8.RuneCountInString(s))
		if n.minLength != nil && length < *n.minLength {
			if !fail("minLength", "has %d characters, want at least %d", length, *n.minLength) {
				return false
			}
		}
		if n.maxLength != nil && length > *n.maxLength {
			if !fail("maxLength", "has %d characters, want at most %d", length, *n.maxLength) {
				return false
			}
		}
	}
	if n.pattern != nil && !n.pattern.MatchString(s) {
		if !fail("pattern", "does not match the pattern %s", n.patternIn) {
			return false
		}
	}
	return true
}

// escape writes a JSON pointer segment.
func escape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// kindOf names the JSON type of v, which Decode produced: "integer" for a
// whole number.
func kindOf(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case int64:
		return "integer"
	}
	return "invalid"
}

func typeMatches(want, kind string) bool {
	return want == kind || (want == "number" && kind == "integer")
}

// equal is JSON equality: strings byte for byte, objects by member, arrays by
// position, whole numbers by value.
func equal(a, b any) bool {
	if x, ok := a.(int64); ok {
		y, ok := b.(int64)
		return ok && x == y
	}
	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case []any:
		y, ok := b.([]any)
		return ok && slices.EqualFunc(x, y, equal)
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, xv := range x {
			if yv, there := y[k]; !there || !equal(xv, yv) {
				return false
			}
		}
		return true
	}
	return false
}
