package admission

import (
	"time"

	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/decision"
)

// The authority admission accepts for a sensitive boundary (spec/pool.md,
// Admission): an approval on the record, or the request's own authority
// verified by the Pool. The kinds it can verify at stage 2 are word and rule
// (the rule route is in rule_route.go); a posture, charter or instruction reference is insufficient until stage 4
// adds the route by which admission accepts each.

// The standing of the authority a sensitive boundary rests on.
type standing int

const (
	// notGiven: neither an approval nor an authority that verifies.
	notGiven standing = iota
	// given: an approval is on the record, or the request's authority verified.
	given
	// insufficient: the request's authority is of a kind admission does not
	// yet accept for a sensitive boundary.
	insufficient
)

// grant is what the record gives the sensitive boundaries: one standing for all
// of them, from an approval on the record or a word that stands, and the rule
// the request's authority names, which clears the one boundary its subject
// names.
type grant struct {
	standing standing
	rule     *ruleClaim
}

// of says what the grant gives one boundary at the instant, and, where a rule
// the request named did not clear it, why in one clause.
func (g *grant) of(boundary string, at time.Time) (standing, string) {
	if g.standing == given {
		return given, ""
	}
	if g.rule == nil {
		return g.standing, ""
	}
	why := g.rule.clears(boundary, at)
	if why == "" {
		return given, ""
	}
	return g.standing, why
}

// authorityStanding says what the record gives the sensitive boundaries. A word
// authority, whether the approval on the record or the request's own, is
// verified against the Pool's own question record by the rule of spec/word.md,
// The word stands, at the scope of the admission, so an approval never
// outlives a superseding answer. A rule authority is read as it now stands and
// clears a boundary by the rule route, which names the instant it is asked at.
func authorityStanding(st *cstore.Store, rec *Record) (*grant, error) {
	g := &grant{standing: notGiven}
	if rec.Approved() {
		approval := authorityOf(tree(tree(rec.Data["approval"])["authority"]))
		verdict, err := decision.WordStands(st, rec.Scope(), approval)
		if err != nil {
			return nil, err
		}
		if verdict.Stands() {
			g.standing = given
			return g, nil
		}
	}
	auth := authorityOf(tree(tree(rec.Data["request"])["authority"]))
	switch auth.Kind {
	case envelope.AuthorityWord:
		verdict, err := decision.WordStands(st, rec.Scope(), auth)
		if err != nil {
			return nil, err
		}
		if verdict.Stands() {
			g.standing = given
		}
	case envelope.AuthorityRule:
		claim, err := readRule(st, rec, auth)
		if err != nil {
			return nil, err
		}
		g.rule = claim
	case envelope.AuthorityPosture, envelope.AuthorityCharter, envelope.AuthorityInstruction:
		g.standing = insufficient
	}
	return g, nil
}

// authorityOf reads the authority a record holds.
func authorityOf(m map[string]any) envelope.Authority {
	a := envelope.Authority{Kind: str(m["kind"])}
	if ref := tree(m["ref"]); ref != nil {
		a.Ref = &envelope.RecordRef{
			Schema: str(ref["schema"]), ID: str(ref["id"]), HomeID: str(ref["home_id"]),
			RecordRevision: integer(ref["record_revision"]), Digest: str(ref["digest"]),
		}
	}
	return a
}
