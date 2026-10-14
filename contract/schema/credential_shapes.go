package schema

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// The fixed list of credential shapes of the design's Secrets section, stated
// in spec/records.md (Encoding). A shape is a string that is unmistakable by
// construction: a vendor's token prefix with its exact length and alphabet,
// matched as a whole token (boundary T), or the opening armour line of a
// private key, matched as a whole line (boundary L). The list is a floor. It
// changes only as a contract change, and a change to it changes no schema.
//
// Matching is case-sensitive ASCII on the string value after ordinary JSON
// parsing, within one string and never across fields. It decodes nothing
// further, redacts and rewrites nothing, and makes no validity or network
// check.

// tokenShape is a shape of boundary T: prefix, then a body of exactly one of
// the lengths from the alphabet.
type tokenShape struct {
	name    string
	prefix  string
	lengths []int
	// underscore says whether the body admits "_" beside letters and digits.
	underscore bool
}

// lineShape is a shape of boundary L: the whole marker on a line of its own.
type lineShape struct {
	name   string
	marker string
}

var tokenShapes = []tokenShape{
	{"github.pat-classic", "ghp_", []int{36}, false},
	{"github.oauth-access", "gho_", []int{36}, false},
	{"github.app-user-access", "ghu_", []int{36}, false},
	{"github.app-installation-opaque", "ghs_", []int{36, 255}, false},
	{"github.app-refresh-scanner40", "ghr_", []int{36}, false},
	{"github.pat-fine-grained", "github_pat_", []int{82}, true},
}

var lineShapes = []lineShape{
	{"private-key.pkcs8", "-----BEGIN PRIVATE KEY-----"},
	{"private-key.pkcs8-encrypted", "-----BEGIN ENCRYPTED PRIVATE KEY-----"},
	{"private-key.rsa", "-----BEGIN RSA PRIVATE KEY-----"},
	{"private-key.dsa", "-----BEGIN DSA PRIVATE KEY-----"},
	{"private-key.ec", "-----BEGIN EC PRIVATE KEY-----"},
	{"private-key.sm2", "-----BEGIN SM2 PRIVATE KEY-----"},
	{"private-key.openssh", "-----BEGIN OPENSSH PRIVATE KEY-----"},
	{"private-key.openpgp", "-----BEGIN PGP PRIVATE KEY BLOCK-----"},
	{"private-key.pgp2-secret", "-----BEGIN PGP SECRET KEY BLOCK-----"},
}

// ShapeNames lists the names of the fifteen shapes, in the order the contract
// states them.
func ShapeNames() []string {
	names := make([]string, 0, len(tokenShapes)+len(lineShapes))
	for _, s := range tokenShapes {
		names = append(names, s.name)
	}
	for _, s := range lineShapes {
		names = append(names, s.name)
	}
	return names
}

// ShapeError is the refusal of a body that holds a string of a listed shape:
// it names the field and the shape, never the text. Field is the JSON pointer,
// within the document checked, of the nearest declared field that contains the
// match, with array positions kept where they lie between declared fields; ""
// is the root of the document.
type ShapeError struct {
	Field string
	Shape string
}

func (e *ShapeError) Error() string {
	return fmt.Sprintf("schema: a string of the shape %s is held at %s", e.Shape, e.Where())
}

// Where names the field for a sentence: its pointer, or the root of the
// document.
func (e *ShapeError) Where() string {
	if e.Field == "" {
		return "the document root"
	}
	return e.Field
}

// WhereIn is Where for a document that is called what, a body: its pointer, or
// the root of the what.
func (e *ShapeError) WhereIn(what string) string {
	if e.Field == "" {
		return "the root of the " + what
	}
	return e.Field
}

// Under returns the same refusal for a document that sits at the pointer
// prefix of a larger one.
func (e *ShapeError) Under(prefix string) *ShapeError {
	return &ShapeError{Field: prefix + e.Field, Shape: e.Shape}
}

// shapeOf names the listed shape that the string s is of, or "".
func shapeOf(s string) string {
	if len(s) < 4 {
		return ""
	}
	if strings.Contains(s, "-----BEGIN ") {
		if name := lineShapeOf(s); name != "" {
			return name
		}
	}
	return tokenShapeOf(s)
}

func isTokenByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-'
}

// tokenShapeOf finds, among the maximal runs of [A-Za-z0-9_.-] that s holds, a
// run that is a candidate whole: a run is preceded by the start of s or a byte
// outside the set and followed by the end of s or one, so a candidate is
// never a slice of a longer run.
func tokenShapeOf(s string) string {
	for i := 0; i < len(s); {
		if !isTokenByte(s[i]) {
			i++
			continue
		}
		j := i
		for j < len(s) && isTokenByte(s[j]) {
			j++
		}
		if name := tokenRunShape(s[i:j]); name != "" {
			return name
		}
		i = j
	}
	return ""
}

// tokenRunShape reports the shape that the whole run is.
func tokenRunShape(run string) string {
	for _, sh := range tokenShapes {
		body, ok := strings.CutPrefix(run, sh.prefix)
		if !ok || !slices.Contains(sh.lengths, len(body)) {
			continue
		}
		valid := true
		for i := 0; i < len(body); i++ {
			c := body[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' && sh.underscore) {
				valid = false
				break
			}
		}
		if valid {
			return sh.name
		}
	}
	return ""
}

// lineShapeOf finds a line of s that is the whole opening marker of a private
// key. A line runs from the start of s or just after a line feed to the end of
// s, a line feed, or a carriage return followed by a line feed; nothing else,
// indentation, trailing space and a lone carriage return included, belongs to
// no marker.
func lineShapeOf(s string) string {
	for start := 0; start <= len(s); {
		end := strings.IndexByte(s[start:], '\n')
		var line string
		if end < 0 {
			line = s[start:]
			end = len(s)
		} else {
			end += start
			line = strings.TrimSuffix(s[start:end], "\r")
		}
		for _, sh := range lineShapes {
			if line == sh.marker {
				return sh.name
			}
		}
		start = end + 1
	}
	return ""
}

// HoldsShape reports whether the string s is, or holds as a whole token run or a
// whole line, a string of a listed shape. A caller that is about to cut a string,
// a diagnostic clipping an error chain, asks it of the whole string first.
func HoldsShape(s string) bool { return shapeOf(s) != "" }

// CheckShapes looks through a decoded document, as Decode yields it, for a
// string of a listed shape in a value or in a member name, and returns a
// *ShapeError for the first found, in the order of the sorted member names. No
// schema is known, so a match is named at the root.
func CheckShapes(v any) error {
	if m := findShape(nil, v, "", ""); m != nil {
		return m
	}
	return nil
}

// CheckShapes is the no-value check of the owner's validation, applied to the
// decoded document v as one the schema ref names: a match in a value or in a
// member name is a *ShapeError that names the nearest declared field that
// contains it, as the schema ref and what it refers to declare fields, or the
// root. A reference that names no schema is an error wrapping ErrUnknownSchema.
func (r *Registry) CheckShapes(ref string, v any) error {
	n, err := r.lookup(ref)
	if err != nil {
		return err
	}
	if m := findShape(closure(n), v, "", ""); m != nil {
		return m
	}
	return nil
}

// ValidateOwned is Validate with the no-value check first: the document is
// decoded as a Hive record (see Decode), refused whole with a *ShapeError if
// any string of it, value or member name, is of a listed shape, and only then
// checked against the schema ref names. A document that holds a shape is
// refused for it whatever else is wrong with it, so nothing that follows reads
// or reports it further.
func (r *Registry) ValidateOwned(ref string, data []byte) error {
	n, err := r.lookup(ref)
	if err != nil {
		return err
	}
	v, err := Decode(data)
	if err != nil {
		return err
	}
	if m := findShape(closure(n), v, "", ""); m != nil {
		return m
	}
	return run(n, v)
}

// closure lists the schemas that apply to the same value as n does, n itself
// first, whichever branch of a combinator they sit in: the places where a
// field of the value may be declared.
func closure(n *node) []*node {
	var out []*node
	seen := map[*node]bool{}
	var add func(m *node)
	add = func(m *node) {
		if m == nil || seen[m] {
			return
		}
		seen[m] = true
		out = append(out, m)
		add(m.ref)
		for _, b := range m.allOf {
			add(b)
		}
		for _, b := range m.anyOf {
			add(b)
		}
		for _, b := range m.oneOf {
			add(b)
		}
		add(m.ifNode)
		add(m.thenNode)
		add(m.elseNode)
	}
	add(n)
	return out
}

// declared returns the schemas that apply to the member name of a value that
// nodes apply to, and whether any of them declares it.
func declared(nodes []*node, name string) ([]*node, bool) {
	var members []*node
	found := false
	for _, n := range nodes {
		if i, there := slices.BinarySearchFunc(n.props, name, func(p property, name string) int {
			return strings.Compare(p.name, name)
		}); there {
			found = true
			members = append(members, closure(n.props[i].node)...)
		}
	}
	return members, found
}

// findShape walks v, to which nodes apply, at the pointer at, whose nearest
// declared field is field.
func findShape(nodes []*node, v any, at, field string) *ShapeError {
	switch t := v.(type) {
	case string:
		if name := shapeOf(t); name != "" {
			return &ShapeError{Field: field, Shape: name}
		}
	case map[string]any:
		for _, name := range slices.Sorted(maps.Keys(t)) {
			if shape := shapeOf(name); shape != "" {
				return &ShapeError{Field: field, Shape: shape}
			}
			members, isDeclared := declared(nodes, name)
			inner := field
			if isDeclared {
				inner = at + "/" + escape(name)
			}
			if m := findShape(members, t[name], at+"/"+escape(name), inner); m != nil {
				return m
			}
		}
	case []any:
		var items []*node
		for _, n := range nodes {
			if n.items != nil {
				items = append(items, closure(n.items)...)
			}
		}
		for i, e := range t {
			if m := findShape(items, e, at+"/"+strconv.Itoa(i), field); m != nil {
				return m
			}
		}
	}
	return nil
}
