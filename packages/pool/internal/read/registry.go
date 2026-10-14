package read

import (
	"github.com/kesk-git/hive/contract/cli"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/store"
)

// The members of a read selector.
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

// Selector is the selector of a read request.
type Selector struct {
	ID, TaskID, ProjectID, DueBefore string
	States                           []string
	Ready, Active                    bool
	Limit                            int
	Present                          []string
}

// Record is one record of a result, whole, with the derived facts beside it.
type Record struct {
	ID      string
	Data    map[string]any
	Derived map[string]any
}

// Omission names a record the read could not return.
type Omission = store.Omission

// Result is what a kind's reader found.
type Result struct {
	Records []Record
	Omitted []Omission
}

// Context is what a kind's reader is given.
type Context struct {
	Call     *cli.Call
	Store    *cstore.Store
	Kind     string
	Selector Selector
}

// Kind is a kind with a built reader.
type Kind struct {
	Name       string
	Members    []string
	Standalone func(Selector) bool
	Read       func(*Context) (*Result, error)
}

// Registry holds the kinds with a built reader.
type Registry struct{}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Register adds a kind.
func (r *Registry) Register(k Kind) {}

// Len is the number of kinds registered.
func (r *Registry) Len() int { return 0 }
