package rule

import "github.com/kesk-git/hive/packages/pool/internal/entry"

func init() { Register(entry.Default) }

// Register builds the operation resolve of hive-rule and the readers of the
// kinds rule and subject into the registry.
func Register(r *entry.Registry) { entry.RegisterRuleResolve(r, Resolve) }
