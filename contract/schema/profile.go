package schema

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// metaSchema is the dialect every schema file names.
const metaSchema = "https://json-schema.org/draft/2020-12/schema"

// openObjectID is the schema file that holds the shared declaration of a
// designated open member, at /$defs/open_object.
const openObjectID = "urn:hive:schema:common:v1"

// openMembers are the only members of a record that may be left open, as
// spec/records.md fixes them: body in an envelope, detail in an error and
// unknown in a record.
var openMembers = []string{"body", "detail", "unknown"}

// keywords is the profile of spec/records.md, "Schema notation", in full.
var keywords = []string{
	"$schema", "$id", "$ref", "$defs", "title", "description", "type", "enum",
	"const", "properties", "required", "additionalProperties", "items",
	"minItems", "maxItems", "minLength", "maxLength", "pattern", "minimum",
	"maximum", "allOf", "anyOf", "oneOf", "not", "if", "then", "else",
}

// Keywords lists every keyword of the profile, sorted. The boolean schemas
// true and false are the profile's only other form.
func Keywords() []string { return slices.Sorted(slices.Values(keywords)) }

var jsonTypes = []string{"array", "boolean", "integer", "null", "number", "object", "string"}

// Problem is one way a schema file falls outside the profile, or a reference
// that cannot be resolved.
type Problem struct {
	Where string // <$id>#<JSON pointer>, or a file path where no $id can be named
	Msg   string
}

func (p Problem) String() string { return p.Where + ": " + p.Msg }

// pendingRef is a $ref waiting for every file to be read.
type pendingRef struct {
	from *node
	text string
}

// compiler turns one decoded schema file into nodes, holding it to the
// profile as it goes.
type compiler struct {
	id    string           // the $id the file must carry, derived from its path
	nodes map[string]*node // every schema position of the file, by JSON pointer
	refs  []pendingRef
	probs []Problem
}

func (c *compiler) problem(ptr, format string, args ...any) {
	c.probs = append(c.probs, Problem{c.id + "#" + ptr, fmt.Sprintf(format, args...)})
}

// compileFile compiles a decoded file whose $id must be id.
func (c *compiler) compileFile(doc any) *node {
	root, ok := doc.(map[string]any)
	if !ok {
		if _, isBool := doc.(bool); isBool {
			c.problem("", "a schema file is an object that names its dialect and its $id")
		} else {
			c.problem("", "a schema file is a JSON object")
		}
		return nil
	}
	if s, ok := root["$schema"].(string); !ok || s != metaSchema {
		c.problem("", "$schema must be %q", metaSchema)
	}
	if s, ok := root["$id"].(string); !ok || s != c.id {
		c.problem("", "$id must be %q, which the file's path fixes", c.id)
	}
	return c.schema(doc, "", "", true)
}

// schema compiles the schema at ptr. member is the name of the property it
// sits under, "" otherwise.
func (c *compiler) schema(raw any, ptr, member string, root bool) *node {
	n := &node{loc: c.id + "#" + ptr, designated: slices.Contains(openMembers, member)}
	c.nodes[ptr] = n
	switch t := raw.(type) {
	case bool:
		n.boolean, n.verdict = true, t
		return n
	case map[string]any:
		c.keywords(n, t, ptr, member, root)
		return n
	}
	c.problem(ptr, "a schema is an object or a boolean")
	return n
}

func (c *compiler) keywords(n *node, obj map[string]any, ptr, member string, root bool) {
	for _, kw := range slices.Sorted(maps.Keys(obj)) {
		v := obj[kw]
		at := ptr + "/" + escape(kw)
		if !slices.Contains(keywords, kw) {
			c.problem(at, "keyword %q is not in the profile", kw)
			continue
		}
		switch kw {
		case "$schema", "$id":
			if !root {
				c.problem(at, "%s belongs at the root of a schema file only", kw)
			}
		case "title", "description":
			if _, ok := v.(string); !ok {
				c.problem(at, "%s is a string", kw)
			}
		case "$ref":
			c.reference(n, v, at)
		case "$defs":
			c.schemaMap(v, at, false, func(string, *node) {})
		case "type":
			n.types = c.typeKeyword(v, at)
		case "enum":
			c.enumKeyword(n, v, at)
		case "const":
			n.constant, n.hasConst = v, true
		case "properties":
			c.schemaMap(v, at, true, func(name string, sub *node) {
				n.props = append(n.props, property{name, sub})
			})
			slices.SortFunc(n.props, func(a, b property) int { return strings.Compare(a.name, b.name) })
		case "required":
			n.required = c.requiredKeyword(v, at)
		case "additionalProperties":
			closed, ok := v.(bool)
			if !ok {
				c.problem(at, "additionalProperties is only ever a boolean")
			}
			n.closed = ok && !closed
		case "items":
			n.items = c.schema(v, at, "", false)
		case "minItems":
			n.minItems = c.count(v, at)
		case "maxItems":
			n.maxItems = c.count(v, at)
		case "minLength":
			n.minLength = c.count(v, at)
		case "maxLength":
			n.maxLength = c.count(v, at)
		case "minimum":
			n.minimum = c.whole(v, at)
		case "maximum":
			n.maximum = c.whole(v, at)
		case "pattern":
			src, ok := v.(string)
			if !ok {
				c.problem(at, "pattern is a string")
				break
			}
			re, err := compilePattern(src)
			if err != nil {
				c.problem(at, "pattern %s: %v", src, err)
				break
			}
			n.pattern, n.patternIn = re, src
		case "allOf":
			n.allOf = c.schemaList(v, at)
		case "anyOf":
			n.anyOf = c.schemaList(v, at)
		case "oneOf":
			n.oneOf = c.schemaList(v, at)
		case "not":
			n.not = c.schema(v, at, "", false)
		case "if":
			n.ifNode = c.schema(v, at, "", false)
		case "then":
			n.thenNode = c.schema(v, at, "", false)
		case "else":
			n.elseNode = c.schema(v, at, "", false)
		}
	}
	c.relations(n, obj, ptr, member)
}

// relations holds the rule on which objects are open.
func (c *compiler) relations(n *node, obj map[string]any, ptr, member string) {
	if !slices.Contains(n.types, "object") {
		return
	}
	// Every object is closed, but for the designated open members, which are
	// declared as a bare object.
	bare := true
	for kw := range obj {
		if !slices.Contains([]string{"type", "title", "description", "$schema", "$id"}, kw) {
			bare = false
		}
	}
	switch {
	case bare && !c.isOpenSite(member, ptr):
		c.problem(ptr, "an object that lists no properties is open; only %s may be open", strings.Join(openMembers, ", "))
	case bare:
		n.open = true
	case !bare && !n.closed:
		c.problem(ptr, "an object schema lists its properties and sets additionalProperties to false")
	}
}

// isOpenSite says whether a bare object may sit here: under a designated open
// member, or as the one shared definition of it in the common schema file.
func (c *compiler) isOpenSite(member, ptr string) bool {
	return slices.Contains(openMembers, member) || (c.id == openObjectID && ptr == "/$defs/open_object")
}

// schemaMap compiles an object whose values are schemas ($defs, properties).
func (c *compiler) schemaMap(v any, at string, members bool, each func(name string, sub *node)) {
	m, ok := v.(map[string]any)
	if !ok {
		c.problem(at, "this is an object of schemas")
		return
	}
	for _, name := range slices.Sorted(maps.Keys(m)) {
		member := ""
		if members {
			member = name
		}
		each(name, c.schema(m[name], at+"/"+escape(name), member, false))
	}
}

func (c *compiler) schemaList(v any, at string) []*node {
	list, ok := v.([]any)
	if !ok || len(list) == 0 {
		c.problem(at, "this is a non-empty array of schemas")
		return nil
	}
	out := make([]*node, len(list))
	for i, item := range list {
		out[i] = c.schema(item, fmt.Sprintf("%s/%d", at, i), "", false)
	}
	return out
}

func (c *compiler) typeKeyword(v any, at string) []string {
	var names []string
	switch t := v.(type) {
	case string:
		names = []string{t}
	case []any:
		for _, item := range t {
			s, ok := item.(string)
			if !ok {
				c.problem(at, "type names are strings")
				return nil
			}
			names = append(names, s)
		}
	default:
		c.problem(at, "type is a type name or an array of them")
		return nil
	}
	if len(names) == 0 {
		c.problem(at, "type lists no name: no value can pass")
		return nil
	}
	for i, name := range names {
		if !slices.Contains(jsonTypes, name) {
			c.problem(at, "%q is not a JSON type", name)
			return nil
		}
		if slices.Contains(names[:i], name) {
			c.problem(at, "type names %q twice", name)
			return nil
		}
	}
	return names
}

func (c *compiler) enumKeyword(n *node, v any, at string) {
	list, ok := v.([]any)
	if !ok || len(list) == 0 {
		c.problem(at, "enum is a non-empty array")
		return
	}
	for i := range list {
		for j := range i {
			if equal(list[i], list[j]) {
				c.problem(at, "enum lists the same value twice")
				return
			}
		}
	}
	n.enum, n.hasEnum = list, true
}

func (c *compiler) requiredKeyword(v any, at string) []string {
	list, ok := v.([]any)
	if !ok {
		c.problem(at, "required is an array of member names")
		return nil
	}
	var names []string
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			c.problem(at, "required lists member names, which are strings")
			return nil
		}
		if slices.Contains(names, s) {
			c.problem(at, "required lists %q twice", s)
			return nil
		}
		names = append(names, s)
	}
	return names
}

// count reads a keyword whose value is a whole number of at least zero.
func (c *compiler) count(v any, at string) *int64 {
	n, ok := v.(int64)
	if !ok || n < 0 {
		c.problem(at, "this is a whole number, zero or more")
		return nil
	}
	return &n
}

func (c *compiler) whole(v any, at string) *int64 {
	n, ok := v.(int64)
	if !ok {
		c.problem(at, "this is a whole number")
		return nil
	}
	return &n
}

// reference records a $ref for resolution once every file is read, after the
// checks that need only the text.
func (c *compiler) reference(n *node, v any, at string) {
	text, ok := v.(string)
	if !ok {
		c.problem(at, "$ref is a string")
		return
	}
	if _, err := splitRef(text); err != nil {
		c.problem(at, "$ref %s: %v", text, err)
		return
	}
	c.refs = append(c.refs, pendingRef{n, text})
}
