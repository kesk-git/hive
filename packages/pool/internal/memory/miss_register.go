package memory

import "github.com/kesk-git/hive/packages/pool/internal/entry"

func init() { RegisterMiss(entry.Default) }

// RegisterMiss builds miss of hive-memory and the reader of the kind miss into
// the registry.
func RegisterMiss(r *entry.Registry) {
	entry.RegisterMemoryMiss(r, MissOp, MissReader())
}
