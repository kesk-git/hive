package memory

import "github.com/kesk-git/hive/packages/pool/internal/entry"

func init() { RegisterMaintenance(entry.Default) }

// RegisterMaintenance builds enqueue and fold of hive-memory, and the readers of
// the kinds note and maintenance-queue, into the registry.
func RegisterMaintenance(r *entry.Registry) {
	entry.RegisterMemoryMaintenance(r, EnqueueOp, FoldOp)
}
