package schema

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/kesk-git/hive/spec"
)

const idPrefix = "urn:hive:schema:"

// ErrUnknownSchema is wrapped by the error for a reference no schema file
// answers.
var ErrUnknownSchema = errors.New("schema: no such schema")

var (
	dirName  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	fileName = regexp.MustCompile(`^([a-z][a-z0-9.-]*)\.v([1-9][0-9]*)\.schema\.json$`)
)

// Registry holds every schema of one directory tree, each held to the profile
// and every reference between them resolved. It is immutable and safe for
// concurrent use.
type Registry struct {
	docs map[string]*document
}

type document struct {
	file  string
	root  *node
	nodes map[string]*node
}

// LoadError lists everything wrong with a tree of schema files, in a fixed
// order.
type LoadError struct {
	Problems []Problem
}

func (e *LoadError) Error() string {
	lines := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		lines[i] = p.String()
	}
	return fmt.Sprintf("schema: %d problems\n%s", len(lines), strings.Join(lines, "\n"))
}

var embedded = sync.OnceValues(func() (*Registry, error) {
	return Load(spec.Schemas, "schemas")
})

// Embedded loads the schema files that spec/embed.go embeds, once.
func Embedded() (*Registry, error) { return embedded() }

// Load reads every file under root of fsys as a schema. A file's place fixes
// its $id (receipts/x.v1.schema.json is urn:hive:schema:receipts:x:v1), and
// Load refuses a file whose $id says otherwise, a keyword outside the
// profile, a closed-object rule broken, a reference that selects nothing and
// a chain of references that returns to where it began without reading any
// more of a document. It reports every such problem at once.
func Load(fsys fs.FS, root string) (*Registry, error) {
	r := &Registry{docs: map[string]*document{}}
	var probs []Problem
	var refs []pendingRef

	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := p
		if root != "." {
			rel = strings.TrimPrefix(p, root+"/")
		}
		id, err := idForPath(rel)
		if err != nil {
			probs = append(probs, Problem{rel, err.Error()})
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		doc, err := Decode(data)
		if err != nil {
			probs = append(probs, Problem{rel, err.Error()})
			return nil
		}
		c := &compiler{id: id, nodes: map[string]*node{}}
		top := c.compileFile(doc)
		probs = append(probs, c.probs...)
		refs = append(refs, c.refs...)
		r.docs[id] = &document{rel, top, c.nodes}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("schema: reading %s: %w", root, err)
	}

	probs = append(probs, r.resolve(refs)...)
	probs = append(probs, openUses(refs)...)
	probs = append(probs, r.cycles()...)
	if len(probs) > 0 {
		slices.SortStableFunc(probs, func(a, b Problem) int {
			return strings.Compare(a.Where+"\x00"+a.Msg, b.Where+"\x00"+b.Msg)
		})
		return nil, &LoadError{probs}
	}
	return r, nil
}

// idForPath derives the $id of the file at rel, a slash path below the root
// of the schema tree.
func idForPath(rel string) (string, error) {
	dir, file := path.Split(rel)
	m := fileName.FindStringSubmatch(file)
	if m == nil {
		return "", errors.New("a schema file is named <name>.v<major>.schema.json, in lowercase")
	}
	var parts []string
	if dir != "" {
		for _, seg := range strings.Split(strings.TrimSuffix(dir, "/"), "/") {
			if !dirName.MatchString(seg) {
				return "", fmt.Errorf("directory %q is not a lowercase name", seg)
			}
			parts = append(parts, seg)
		}
	}
	parts = append(parts, m[1])
	return idPrefix + strings.Join(parts, ":") + ":v" + m[2], nil
}

type refParts struct {
	id      string
	pointer string // canonical JSON pointer; "" selects the whole document
}

// splitRef reads a $ref: an absolute urn:hive:schema identifier, and where it
// selects a part, a JSON pointer in the fragment.
func splitRef(text string) (refParts, error) {
	id, frag, hasFrag := strings.Cut(text, "#")
	if !strings.HasPrefix(id, idPrefix) || len(id) == len(idPrefix) {
		return refParts{}, errors.New("a reference names a schema by its absolute urn:hive:schema identifier; there is no relative reference and no network fetch")
	}
	if !hasFrag {
		return refParts{id: id}, nil
	}
	if !strings.HasPrefix(frag, "/") {
		return refParts{}, errors.New("a fragment is a JSON pointer starting with /")
	}
	frag, err := url.PathUnescape(frag)
	if err != nil || !utf8.ValidString(frag) {
		return refParts{}, errors.New("the fragment is not a valid percent-encoded JSON pointer")
	}
	var canon strings.Builder
	for _, tok := range strings.Split(frag[1:], "/") {
		if strings.Contains(strings.ReplaceAll(strings.ReplaceAll(tok, "~0", ""), "~1", ""), "~") {
			return refParts{}, errors.New("the fragment is not a JSON pointer: ~ must be followed by 0 or 1")
		}
		tok = strings.ReplaceAll(strings.ReplaceAll(tok, "~1", "/"), "~0", "~")
		canon.WriteString("/" + escape(tok))
	}
	return refParts{id, canon.String()}, nil
}

func (r *Registry) resolve(refs []pendingRef) []Problem {
	var probs []Problem
	for _, pr := range refs {
		parts, err := splitRef(pr.text)
		if err != nil {
			continue // reported when the file was read
		}
		doc := r.docs[parts.id]
		if doc == nil {
			probs = append(probs, Problem{pr.from.loc, fmt.Sprintf("$ref %s resolves to no schema file", pr.text)})
			continue
		}
		target := doc.nodes[parts.pointer]
		if target == nil {
			probs = append(probs, Problem{pr.from.loc, fmt.Sprintf("$ref %s selects no schema", pr.text)})
			continue
		}
		pr.from.ref = target
	}
	return probs
}

// openUses finds a reference that reaches an open object from anywhere but a
// designated open member, however the reference is written or chained.
func openUses(refs []pendingRef) []Problem {
	var probs []Problem
	for _, pr := range refs {
		seen := map[*node]bool{}
		t := pr.from.ref
		for t != nil && !t.open && t.ref != nil && !seen[t] {
			seen[t] = true
			t = t.ref
		}
		if t != nil && t.open && !pr.from.designated {
			probs = append(probs, Problem{pr.from.loc, fmt.Sprintf("$ref %s reaches an open object; only %s may be open", pr.text, strings.Join(openMembers, ", "))})
		}
	}
	return probs
}

// edges lists the schemas that apply to the same value as n does; then and
// else apply only beside an if. A cycle among them never reads any more of a
// document and so never ends, so Load refuses it: the guard that validation
// terminates.
func (n *node) edges() []*node {
	var out []*node
	add := func(m ...*node) {
		for _, x := range m {
			if x != nil {
				out = append(out, x)
			}
		}
	}
	add(n.ref, n.not, n.ifNode)
	if n.ifNode != nil {
		add(n.thenNode, n.elseNode)
	}
	add(n.allOf...)
	add(n.anyOf...)
	add(n.oneOf...)
	return out
}

// cycles finds a chain of schemas that applies to one value and returns to
// where it began.
func (r *Registry) cycles() []Problem {
	var all []*node
	for _, id := range slices.Sorted(maps.Keys(r.docs)) {
		doc := r.docs[id]
		for _, ptr := range slices.Sorted(maps.Keys(doc.nodes)) {
			all = append(all, doc.nodes[ptr])
		}
	}
	const (
		walking = 1
		done    = 2
	)
	state := map[*node]int{}
	var trail []*node
	var probs []Problem
	var visit func(n *node)
	visit = func(n *node) {
		state[n] = walking
		trail = append(trail, n)
		for _, next := range n.edges() {
			switch state[next] {
			case 0:
				visit(next)
			case walking:
				i := slices.Index(trail, next)
				chain := make([]string, 0, len(trail)-i+1)
				for _, m := range trail[i:] {
					chain = append(chain, m.loc)
				}
				chain = append(chain, next.loc)
				probs = append(probs, Problem{next.loc, "a chain of references returns to this schema without reading more of the document: " + strings.Join(chain, " -> ")})
			}
		}
		trail = trail[:len(trail)-1]
		state[n] = done
	}
	for _, n := range all {
		if state[n] == 0 {
			visit(n)
		}
	}
	return probs
}

// IDs lists the $id of every schema held, sorted.
func (r *Registry) IDs() []string { return slices.Sorted(maps.Keys(r.docs)) }

// lookup finds the schema a reference names: an $id, or an $id and a pointer.
func (r *Registry) lookup(ref string) (*node, error) {
	parts, err := splitRef(ref)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrUnknownSchema, ref, err)
	}
	if doc := r.docs[parts.id]; doc != nil {
		if n := doc.nodes[parts.pointer]; n != nil {
			return n, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrUnknownSchema, ref)
}

// Validate decodes data as a Hive record (see Decode) and checks it against
// the schema ref names, an $id with or without a JSON pointer. It returns a
// *SyntaxError if the bytes are not a record, a *ValidationError if the
// record does not satisfy the schema, and nil if it does.
func (r *Registry) Validate(ref string, data []byte) error {
	n, err := r.lookup(ref)
	if err != nil {
		return err
	}
	v, err := Decode(data)
	if err != nil {
		return err
	}
	return run(n, v)
}

func run(n *node, v any) error {
	var out sink
	if n.check(v, "", &out) {
		return nil
	}
	return &ValidationError{out.list, out.omitted}
}
