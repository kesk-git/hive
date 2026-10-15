package memory

import (
	"fmt"
	"time"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/budget"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// Defaults are the provisioned defaults a new budget entry is initialized from.
type Defaults struct{ HiveAlways, FleetAlways, Subject int64 }

// Budget is the budget configuration the planner measures against.
type Budget struct {
	Revision int64
	Entries  map[string]int64
	Defaults Defaults
}

// Max is the budget of the projection: its entry, or the default a new entry is
// initialized from where it has none.
func (b Budget) Max(id string, k Key) int64 {
	if max, ok := b.Entries[id]; ok {
		return max
	}
	switch {
	case k.Tier == TierSubject:
		return b.Defaults.Subject
	case k.Scope.Level == envelope.ScopeHive:
		return b.Defaults.HiveAlways
	}
	return b.Defaults.FleetAlways
}

// Existing is a projection as it stands.
type Existing struct {
	ID        string
	Scope     rule.Scope
	Tier      string
	Audience  string
	SubjectID string

	// Revision, Data, BudgetRevision and Current are the record as it was read;
	// a projection stood in memory leaves them empty.
	Revision       int64
	Data           map[string]any
	BudgetRevision int64
	Current        Rendering
}

// Key is the identity of the projection.
func (e Existing) Key() Key {
	return Key{Scope: e.Scope, Tier: e.Tier, Audience: e.Audience, SubjectID: e.SubjectID}
}

// Snapshot is what a write is planned against, and what a rendering is made
// from: the rules, the subjects with their facts, the projections as they stand,
// the budget configuration and the workspace and project scopes the home
// registers.
type Snapshot struct {
	Rules       []*rule.Rule
	Subjects    []*rule.Subject
	Projections []Existing
	Budget      Budget
	Scopes      []rule.Scope

	// budgetData is the budget record as it was read.
	budgetData map[string]any
	resolved   map[resKey]*rule.Resolution
}

type resKey struct {
	scope rule.Scope
	role  string
	at    time.Time
}

// Load reads the Pool's rules, subjects, projections and budget configuration
// under the lock the caller holds, and the scopes the home's registry admits. A
// record that cannot be read ends the read: what is in force cannot be known
// over a hole in the records.
func Load(st *cstore.Store, home *cstore.Home) (*Snapshot, error) {
	s, err := loadSources(st, home.HomeID)
	if err != nil {
		return nil, err
	}
	scopes, err := cli.RegisteredScopes(home)
	if err != nil {
		return nil, err
	}
	for _, sc := range scopes {
		s.Scopes = append(s.Scopes, rule.Scope{Level: sc.Level, HomeID: sc.HomeID, WorkspaceID: sc.WorkspaceID, ProjectID: sc.ProjectID})
	}
	projections, omitted, err := poolstore.LoadAll(st, "projection")
	if err != nil {
		return nil, poolstore.Failure("the projections", err)
	}
	if len(omitted) > 0 {
		return nil, malformed("a projection cannot be read", omitted[0].ID, nil)
	}
	for _, l := range projections {
		e, err := ParseProjection(l.Data)
		if err != nil {
			return nil, malformed("a projection cannot be read", l.ID, err)
		}
		s.Projections = append(s.Projections, e)
	}
	return s, nil
}

// loadSources reads what a projection's freshness is computed from: the rules,
// the subjects and the budget configuration, and no projection.
func loadSources(st *cstore.Store, homeID string) (*Snapshot, error) {
	s := &Snapshot{}
	rules, omitted, err := poolstore.LoadAll(st, "rule")
	if err != nil {
		return nil, poolstore.Failure("the rules", err)
	}
	if len(omitted) > 0 {
		return nil, malformed("a rule cannot be read, so the rules in force cannot be known", omitted[0].ID, nil)
	}
	for _, l := range rules {
		r, err := rule.ParseRule(l.Data)
		if err != nil {
			return nil, malformed("a rule cannot be read, so the rules in force cannot be known", l.ID, err)
		}
		s.Rules = append(s.Rules, r)
	}
	subjects, omitted, err := poolstore.LoadAll(st, "subject")
	if err != nil {
		return nil, poolstore.Failure("the subjects", err)
	}
	if len(omitted) > 0 {
		return nil, malformed("a subject cannot be read, so a projection cannot be made", omitted[0].ID, nil)
	}
	for _, l := range subjects {
		sub, err := rule.ParseSubject(l.Data)
		if err != nil {
			return nil, malformed("a subject cannot be read, so a projection cannot be made", l.ID, err)
		}
		s.Subjects = append(s.Subjects, sub)
	}
	rec, err := budget.Read(st, homeID)
	if err != nil {
		return nil, err
	}
	s.Budget = Budget{Revision: rec.Revision, Entries: map[string]int64{}}
	for _, e := range rec.Projections {
		s.Budget.Entries[e.ProjectionID] = e.MaxBytes
	}
	d := tree(rec.Data["defaults"])
	s.Budget.Defaults = Defaults{HiveAlways: num(d["hive_always"]), FleetAlways: num(d["fleet_always"]), Subject: num(d["subject"])}
	s.budgetData = rec.Data
	return s, nil
}

func malformed(what, id string, err error) *envelope.Failure {
	f := envelope.Common(envelope.CodeRecordMalformed, what).With("record", id)
	if err != nil {
		f = f.Because(err)
	}
	return f
}

// ParseProjection reads a projection record as it is stored.
func ParseProjection(data map[string]any) (Existing, error) {
	e := Existing{
		ID: str(data["projection_id"]), Scope: scopeOf(data), Tier: str(data["tier"]), Audience: str(data["audience"]),
		SubjectID: str(data["subject_id"]), Revision: num(data["record_revision"]), Data: data, BudgetRevision: num(data["budget_revision"]),
	}
	cur, err := parseRendering(data["current"])
	if err != nil {
		return e, fmt.Errorf("projection %s: %w", e.ID, err)
	}
	e.Current = cur
	return e, nil
}

// projection finds the projection of the key as it stands.
func (s *Snapshot) projection(k Key) (Existing, bool) {
	for _, e := range s.Projections {
		if e.Key() == k {
			return e, true
		}
	}
	return Existing{}, false
}

func (s *Snapshot) subject(id string) *rule.Subject {
	for _, sub := range s.Subjects {
		if sub.SubjectID == id {
			return sub
		}
	}
	return nil
}

// home is the home the records are of.
func (s *Snapshot) home() string {
	for _, r := range s.Rules {
		return r.Scope.HomeID
	}
	for _, sub := range s.Subjects {
		return sub.Scope.HomeID
	}
	for _, e := range s.Projections {
		return e.Scope.HomeID
	}
	return ""
}

// resolve resolves the rules for the scope, the role and the instant, as
// hive-rule resolve does, and remembers the answer.
func (s *Snapshot) resolve(scope rule.Scope, role string, at time.Time) (*rule.Resolution, error) {
	k := resKey{scope, role, at}
	if res, ok := s.resolved[k]; ok {
		return res, nil
	}
	res, err := rule.Compute(rule.Request{Scope: envelopeScope(scope), Role: role, At: at}, s.Rules)
	if err != nil {
		return nil, fmt.Errorf("a rule cannot be placed in time: %w", err)
	}
	if s.resolved == nil {
		s.resolved = map[resKey]*rule.Resolution{}
	}
	s.resolved[k] = res
	return res, nil
}
