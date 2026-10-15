package admission

import (
	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/decision"
)

// The authority admission accepts for a sensitive boundary (spec/pool.md,
// Admission): an approval on the record, or the request's own authority
// verified by the Pool. The kinds it can verify at stage 2 are word and rule;
// a posture, charter or instruction reference is insufficient until stage 4
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

// authorityStanding says what the record gives a sensitive boundary. A word
// authority is verified against the Pool's own question record by the rule of
// spec/word.md, The word stands, at the scope of the admission. A rule
// authority is not verified here: the contract names the kind as verifiable
// and states no rule for it, so it gives nothing until it does.
func authorityStanding(st *cstore.Store, rec *Record) (standing, error) {
	if rec.Approved() {
		return given, nil
	}
	auth := authorityOf(tree(tree(rec.Data["request"])["authority"]))
	switch auth.Kind {
	case envelope.AuthorityWord:
		verdict, err := decision.WordStands(st, rec.Scope(), auth)
		if err != nil {
			return notGiven, err
		}
		if verdict.Stands() {
			return given, nil
		}
	case envelope.AuthorityPosture, envelope.AuthorityCharter, envelope.AuthorityInstruction:
		return insufficient, nil
	}
	return notGiven, nil
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
