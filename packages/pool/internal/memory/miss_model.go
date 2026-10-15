package memory

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The miss record (spec/pool.md, Generated knowledge, Misses): its schema, the
// kind it is stored as, the prefix its identity carries, and the receipts that
// assert its writes.
const (
	schemaMiss = "pool.miss"
	kindMiss   = "miss"
	missPrefix = "miss_"

	missVersion = "1.0"

	receiptMissed      = "pool.rule.missed"
	receiptMissRevised = "pool.miss.revised"
)

// StepOfPoolRefusals is the step the Pool names for a refusal of its own that it
// reports as a miss. The refusals reported are those of hive-rule record and
// amend, which write a rule record when a stow binds one, so they are met at the
// stow step.
const StepOfPoolRefusals = "stow"

// recordRef is a reference to one record as a provenance carries it: its schema
// and identity, and the home it is of, which is the reporting home where omitted.
type recordRef struct{ Schema, ID, Home string }

func refOf(v any, home string) recordRef {
	m := tree(v)
	r := recordRef{Schema: str(m["schema"]), ID: str(m["id"]), Home: str(m["home_id"])}
	if r.Home == "" {
		r.Home = home
	}
	return r
}

// key is the identity of an incident: the originating home, the schema and the
// record. A revision or a digest binds evidence and makes no other incident.
func (r recordRef) key() string { return r.Home + "|" + r.Schema + "|" + r.ID }

// Miss is a miss as it is stored: against the filing subject of the rule it
// cites, for the acting session or task generation, at the step, observed at an
// instant, with the provenance of the incident it reports.
type Miss struct {
	ID           string
	Revision     int64
	Scope        rule.Scope
	SubjectID    string
	RuleID       string
	RuleRevision int64
	Actor        map[string]any
	Step         string
	ObservedAt   string
	Independence string
	Incident     recordRef
	DerivedFrom  []recordRef
	Data         map[string]any
}

// ParseMiss reads a miss record as it is stored. The store has held it to its
// schema; this reads the members the count acts on.
func ParseMiss(data map[string]any) (*Miss, error) {
	m := &Miss{
		ID: str(data["miss_id"]), Revision: num(data["record_revision"]), Scope: scopeOf(data), SubjectID: str(data["subject_id"]),
		RuleID: str(tree(data["rule"])["rule_id"]), RuleRevision: num(tree(data["rule"])["record_revision"]),
		Actor: tree(data["actor"]), Step: str(data["step"]), ObservedAt: str(data["observed_at"]), Data: data,
	}
	p := tree(data["provenance"])
	m.Independence = str(p["independence"])
	m.Incident = refOf(p["incident"], m.Scope.HomeID)
	for _, d := range list(p["derived_from"]) {
		m.DerivedFrom = append(m.DerivedFrom, refOf(d, m.Scope.HomeID))
	}
	if !strings.HasPrefix(m.ID, missPrefix) || m.SubjectID == "" || m.RuleID == "" || m.Actor == nil || p == nil || m.Incident.ID == "" {
		return nil, fmt.Errorf("a miss has its identity, subject, rule, actor and provenance")
	}
	return m, nil
}

// loadMisses reads every miss of the Pool. A miss that cannot be read ends the
// read: what a subject has been counted over cannot be known over a hole in the
// evidence.
func loadMisses(st *cstore.Store) ([]*Miss, error) {
	loaded, omitted, err := poolstore.LoadAll(st, kindMiss)
	if err != nil {
		return nil, poolstore.Failure("the misses", err)
	}
	if len(omitted) > 0 {
		return nil, malformed("a miss cannot be read, so the misses counted against a subject cannot be known", omitted[0].ID, nil)
	}
	out := make([]*Miss, 0, len(loaded))
	for _, l := range loaded {
		m, err := ParseMiss(l.Data)
		if err != nil {
			return nil, malformed("a miss cannot be read, so the misses counted against a subject cannot be known", l.ID, err)
		}
		out = append(out, m)
	}
	return out, nil
}

// mintMissID gives a miss its identity: the prefix of a miss and a ULID
// (spec/pool.md, Identifiers).
func mintMissID(c *cli.Call) (string, error) { return ids.Mint(c.Clock, ids.Miss) }

// citedRule is a rule a miss is filed for, read as it stands: its revision and
// the subject it is filed under.
type citedRule struct {
	ID       string
	Revision int64
	Subject  *rule.Subject
}

// readCited binds the cited revision to the rule's filing subject. Earlier
// revisions remain eligible so a later rule update cannot erase an earlier
// miss. A rule the Pool does not hold or a revision beyond its recorded revision
// is not found; a product rule has no filing subject to count against and is
// refused.
func readCited(st *cstore.Store, ruleID string, revision int64) (*citedRule, error) {
	l, om, err := poolstore.Load(st, "rule", ruleID)
	switch {
	case isNotFound(err):
		return nil, envelope.Common(envelope.CodeNotFound, "no rule has that identity")
	case err != nil:
		return nil, poolstore.Failure("the rule", err)
	case om != nil:
		return nil, malformed("the rule cannot be read", om.ID, nil)
	}
	r, err := rule.ParseRule(l.Data)
	if err != nil {
		return nil, malformed("the rule cannot be read", ruleID, err)
	}
	if revision < 1 || revision > r.RecordRevision {
		return nil, envelope.Common(envelope.CodeNotFound, "the rule has no such revision")
	}
	if r.Kind != rule.KindOperating || r.FiledUnder == "" {
		return nil, refuse(CodeSubjectInvalid, "the rule has no filing subject, so no miss counts against one")
	}
	s, om, err := poolstore.Load(st, "subject", r.FiledUnder)
	switch {
	case isNotFound(err):
		return nil, malformed("the subject the rule is filed under is not a subject of the Pool", r.FiledUnder, nil)
	case err != nil:
		return nil, poolstore.Failure("the subject", err)
	case om != nil:
		return nil, malformed("the subject cannot be read", om.ID, nil)
	}
	sub, err := rule.ParseSubject(s.Data)
	if err != nil {
		return nil, malformed("the subject cannot be read", r.FiledUnder, err)
	}
	return &citedRule{ID: ruleID, Revision: revision, Subject: sub}, nil
}

func isNotFound(err error) bool { return errors.Is(err, poolstore.ErrNotFound) }
