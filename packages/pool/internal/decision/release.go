package decision

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
)

// The operation release-question of hive-pool, the fourth of the six the Mind's
// word persists through (spec/word.md, release and complete). It records the
// disposition released on the applied answer of a question and releases the
// question's word hold in the same transaction, and accepts only a handoff from
// hive-word. The package registers it from its init function, as it does the
// others.

func init() { RegisterRelease(entry.Default) }

// RegisterRelease builds the operation release-question of hive-pool into the
// registry.
func RegisterRelease(r *entry.Registry) {
	r.RegisterOperation("hive-pool", operation("release-question", releaseQuestion, "pool.question.released", receiptHoldReleased))
}

// releaseQuestion frees the work the question gates, as the answer directs. A
// question whose held item, as it stands in the transaction, is a decision item
// with no live work beneath it gates no work and is completed instead.
func releaseQuestion(c *cli.Call) (*cli.Result, error) {
	return dispose(c, how{state: StateReleased, receipt: "pool.question.released", refuse: func(w work) error {
		if w.decisionItem && !w.beneath {
			return refuse(codeDecisionItem, "a question whose held item is a decision item with no live work beneath it releases no work, and is completed instead")
		}
		return nil
	}})
}
