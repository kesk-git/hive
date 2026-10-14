package decision

import "github.com/kesk-git/hive/packages/pool/internal/entry"

func init() { Register(entry.Default) }

// Register builds the readers of the kinds question, decision-round and
// request-outcome of hive-pool read into the registry.
func Register(r *entry.Registry) { entry.RegisterDecisionReads(r) }
