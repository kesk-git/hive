// Package rule holds what the Pool applies to its rule records beyond what
// the rule recorder, a later lane, writes. It builds no recorder.
package rule

import (
	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/decision"
)

// CheckPosture is the check the rule recorder calls before it records a rule on
// the subject assimilation.posture. A project's Assimilation posture is
// accepted only from the Hive Mind's own sentence, through an authority of kind
// word that stands, by the rule of spec/word.md, The word stands, applied to
// the question the authority names for the scope the rule is recorded at. Every
// other kind of authority, a posture rule among them, and a word that does not
// stand are refused with authority_insufficient, naming the reasons; the check
// reads the question and writes nothing. The recorder calls it inside the
// transaction that records the rule, under the store's lock, so that the answer
// it relies on cannot be superseded between the check and the write.
func CheckPosture(st *cstore.Store, scope envelope.Scope, auth envelope.Authority) error {
	standing, err := decision.WordStands(st, scope, auth)
	if err != nil {
		return err
	}
	if standing.Stands() {
		return nil
	}
	reasons := make([]any, len(standing.Reasons))
	for i, r := range standing.Reasons {
		reasons[i] = r
	}
	return envelope.Common(envelope.CodeAuthorityInsufficient,
		"a project's Assimilation posture is accepted only from the Hive Mind's word, which must stand").With("reasons", reasons)
}
