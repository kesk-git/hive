package rulewrite

import (
	"errors"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/decision"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// Subject is hive-memory subject (spec/pool.md, Subjects): it changes an existing
// subject's complete loads_at, under the expected revision of the subject and a
// standing word authority, with pool.subject.revised. It changes no fact and no
// rule. The authority is checked first, inside the transaction under the store's
// lock and before any record is read, as the budget's writer does.
func Subject(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	id := str(body["subject_id"])
	standing, err := decision.WordStands(c.Store, c.Request.Scope, c.Request.Authority)
	if err != nil {
		return nil, err
	}
	if !standing.Stands() {
		reasons := make([]any, len(standing.Reasons))
		for i, r := range standing.Reasons {
			reasons[i] = r
		}
		return nil, envelope.Common(envelope.CodeAuthorityInsufficient,
			"a subject's loading steps are changed only on the Hive Mind's word, which must stand").With("reasons", reasons)
	}
	if !expects(c, "pool.subject", id) {
		return nil, invalid("hive-memory subject names the expected revision of the subject, pool.subject " + id)
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	l, om, err := poolstore.Load(c.Store, "subject", id)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return nil, envelope.Common(envelope.CodeNotFound, "no subject has that identity")
	case err != nil:
		return nil, poolstore.Failure("the subject", err)
	case om != nil:
		return nil, envelope.Common(envelope.CodeRecordMalformed, "the subject cannot be read").With("record", om.ID)
	}
	s, err := rule.ParseSubject(l.Data)
	if err != nil {
		return nil, envelope.Common(envelope.CodeRecordMalformed, "the subject cannot be read").With("record", id).Because(err)
	}
	if err := checkContained(c, s.Scope); err != nil {
		return nil, err
	}
	loads := texts(body["loads_at"])
	if err := checkLoadsAt(loads, s.Scope); err != nil {
		return nil, err
	}
	stamp, err := c.Stamp()
	if err != nil {
		return nil, internal("the time of the revision cannot be written", err)
	}

	next := copyValue(l.Data).(map[string]any)
	next["loads_at"] = textsAny(loads)
	next["updated_at"] = stamp
	b := &builder{c: c}
	final, ref, err := b.put("subject", id, next, l.Revision)
	if err != nil {
		return nil, err
	}
	in := tree(body["intake"])
	receipt, err := b.receipt(receiptRevised, []envelope.Evidence{{Role: "intake", Ref: envelope.RecordRef{
		Schema: str(in["schema"]), ID: str(in["id"]), HomeID: str(in["home_id"]), RecordRevision: whole(in["record_revision"]), Digest: str(in["digest"]),
	}}})
	if err != nil {
		return nil, err
	}
	if err := b.seal(receipt, ref, envScope(s.Scope), false); err != nil {
		return nil, err
	}
	return b.stage(map[string]any{"subject": final})
}

func textsAny(s []string) []any {
	out := make([]any, len(s))
	for i, e := range s {
		out[i] = e
	}
	return out
}
