package specification

import "github.com/kesk-git/hive/packages/pool/internal/entry"

func init() { Register(entry.Default) }

// Register builds the operations define, accept and close of hive-spec and the
// reader of the kind specification into the registry.
func Register(r *entry.Registry) { entry.RegisterSpecification(r, Define, Accept, Close) }
