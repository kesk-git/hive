package memory

import (
	"errors"
	"time"

	"github.com/kesk-git/hive/packages/pool/internal/rule"
)

// Defaults are the provisioned defaults a new budget entry is initialized from.
type Defaults struct{ HiveAlways, FleetAlways, Subject int64 }

// Budget is the budget configuration the planner measures against.
type Budget struct {
	Revision int64
	Entries  map[string]int64
	Defaults Defaults
}

// Existing is a projection as it stands.
type Existing struct {
	ID        string
	Scope     rule.Scope
	Tier      string
	Audience  string
	SubjectID string
}

// Snapshot is what a write is planned against.
type Snapshot struct {
	Rules       []*rule.Rule
	Subjects    []*rule.Subject
	Projections []Existing
	Budget      Budget
}

// Write is what a write changes.
type Write struct {
	Rules    []*rule.Rule
	Subjects []*rule.Subject
	Budget   map[string]int64
}

// Rendered is one reachable rendering the planner made.
type Rendered struct {
	ProjectionID string
	Bytes, Max   int64
	At           time.Time
}

// Plan renders, before a write commits, every projection it affects.
func (s *Snapshot) Plan(now time.Time, w Write) ([]Rendered, error) {
	return nil, errors.New("memory: the planner is not built")
}
