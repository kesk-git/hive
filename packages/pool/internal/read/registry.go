package read

import (
	"fmt"
	"slices"
	"sync"

	"github.com/kesk-git/hive/contract/cli"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/store"
)

// The members of a read selector, as the request schema names them.
const (
	MemberID        = "id"
	MemberTaskID    = "task_id"
	MemberProjectID = "project_id"
	MemberStates    = "states"
	MemberReady     = "ready"
	MemberActive    = "active"
	MemberDueBefore = "due_before"
	MemberLimit     = "limit"
)

// members are the selector members in the order of the request schema.
var members = []string{MemberID, MemberTaskID, MemberProjectID, MemberStates, MemberReady, MemberActive, MemberDueBefore, MemberLimit}

// RequestOutcome is the kind that reads the Pool's own replay record.
const RequestOutcome = "request-outcome"

// Selector is the selector of a read request.
type Selector struct {
	ID, TaskID, ProjectID, DueBefore string
	States                           []string
	Ready, Active                    bool
	Limit                            int
	// Present lists the members the request gave, in schema order.
	Present []string
}

// Has reports whether the request gave the member.
func (s Selector) Has(member string) bool { return slices.Contains(s.Present, member) }

// Record is one record of a result: whole, as it is stored, with the derived
// facts that come beside it, never stored, where the kind has them.
type Record struct {
	ID      string
	Data    map[string]any
	Derived map[string]any
}

// Omission names a record the read could not return, and why.
type Omission = store.Omission

// Result is what a kind's reader found. Records are in the order the reader
// wants them returned; an Omission is a record of the kind that could not be
// read and is therefore not among them.
type Result struct {
	Records []Record
	Omitted []Omission
}

// Context is what a kind's reader is given. Store is open and, for a read that
// takes the lock shared, the lock is held and the store has recovered any
// interrupted transaction: the records the reader sees are those of whole
// transactions.
type Context struct {
	Call     *cli.Call
	Store    *cstore.Store
	Kind     string
	Selector Selector
}

// Kind is a kind with a built reader.
type Kind struct {
	// Name is the kind, one that a read can name.
	Name string
	// Members are the selector members that apply to the kind. A request that
	// gives another is refused by the read before the reader runs.
	Members []string
	// Standalone says whether the read of the selector is of one record that
	// stands alone, which is read without a lock. A nil Standalone is never:
	// the read takes the lock shared, which is the safe answer for a list and
	// for a record whose derived facts read other records.
	Standalone func(Selector) bool
	// Read finds the records. It returns a *envelope.Failure to end the read
	// with another status; a record it cannot read it names in Omitted. It
	// returns every record the selector selects: the read applies the limit.
	Read func(*Context) (*Result, error)
}

// Registry holds the kinds that have a reader. A kind not in it is not
// offered: a read of it is refused, never answered as empty.
type Registry struct {
	mu    sync.RWMutex
	kinds map[string]Kind
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{kinds: map[string]Kind{}} }

// Default is the registry of the delivered executable. A package that builds a
// kind's reader registers it here from an init function.
var Default = NewRegistry()

// Register adds a kind to the default registry.
func Register(k Kind) { Default.Register(k) }

// Register adds a kind. It panics on a registration that cannot be right, as
// a mistake of the program and not of a request: a kind a read cannot name,
// one registered twice, one with no reader, and a member that is not one of
// the schema's or that cannot apply to the kind.
func (r *Registry) Register(k Kind) {
	if !validKind(k.Name) {
		panic(fmt.Sprintf("read: %q is not a kind a read can name", k.Name))
	}
	if k.Read == nil {
		panic(fmt.Sprintf("read: the kind %s has no reader", k.Name))
	}
	for _, m := range k.Members {
		if !slices.Contains(members, m) {
			panic(fmt.Sprintf("read: %q is not a selector member", m))
		}
		if !applies(k.Name, m) {
			panic(fmt.Sprintf("read: the member %s cannot apply to the kind %s", m, k.Name))
		}
	}
	k.Members = slices.Clone(k.Members)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.kinds[k.Name]; ok {
		panic(fmt.Sprintf("read: the kind %s is registered twice", k.Name))
	}
	r.kinds[k.Name] = k
}

// Lookup finds the kind, if a reader of it is built.
func (r *Registry) Lookup(name string) (Kind, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.kinds[name]
	return k, ok
}

// Len is the number of kinds that have a reader.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.kinds)
}

// Names are the kinds that have a reader, in order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.kinds))
	for n := range r.kinds {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// validKind reports whether a read can name the kind: the kinds the Pool
// stores, and the replay record.
func validKind(name string) bool {
	if name == RequestOutcome {
		return true
	}
	_, ok := store.Lookup(name)
	return ok
}

// applies reports whether the member can apply to the kind at all, by what
// the request schema and spec/pool.md say outright: the replay record is
// selected by its request_id alone; ready is of items, active of holds; and a
// due time is of holds, rules and questions. What they leave unsaid, which
// kinds take task_id, project_id, states and limit, the kind's own
// registration declares.
func applies(kind, member string) bool {
	if kind == RequestOutcome {
		return member == MemberID
	}
	switch member {
	case MemberReady:
		return kind == "item"
	case MemberActive:
		return kind == "hold"
	case MemberDueBefore:
		return kind == "hold" || kind == "rule" || kind == "question"
	}
	return true
}
