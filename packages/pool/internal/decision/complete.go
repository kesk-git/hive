package decision

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
)

// The operation complete-question of hive-pool, the fifth of the six the Mind's
// word persists through (spec/word.md, release and complete). It records the
// disposition completed on the applied answer of a question: the question is
// closed and releases no work, and its word hold is released in the same
// transaction only where nothing is left for it to gate. It accepts only a
// handoff from hive-word. The package registers it from its init function, as it
// does the others.

func init() { RegisterComplete(entry.Default) }

// RegisterComplete builds the operation complete-question of hive-pool into the
// registry.
func RegisterComplete(r *entry.Registry) {
	r.RegisterOperation("hive-pool", operation("complete-question", completeQuestion, "pool.question.completed", receiptHoldReleased))
}

// completeQuestion closes the question on its applied answer where the held
// item and every item beneath it are each archived or a decision item, as they
// stand in the transaction. A question whose held item or an item beneath it is
// live work is refused: the owner releases it, or archives the work the answer
// stopped and completes it then.
func completeQuestion(c *cli.Call) (*cli.Result, error) {
	return dispose(c, how{state: StateCompleted, receipt: "pool.question.completed", refuse: func(w work) error {
		if w.held || w.beneath {
			return refuse(codeHoldsWork, "the held item or an item beneath it is live work, which the owner releases or archives first")
		}
		return nil
	}})
}
