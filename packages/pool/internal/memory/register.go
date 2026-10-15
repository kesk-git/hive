package memory

import "github.com/kesk-git/hive/packages/pool/internal/entry"

func init() { Register(entry.Default) }

// Register builds render, check, propagate and budget of hive-memory and the
// reader of the kind projection into the registry.
func Register(r *entry.Registry) {
	entry.RegisterMemory(r, RenderOp, CheckOp, PropagateOp, BudgetOp, Derive)
}
