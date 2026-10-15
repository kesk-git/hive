package rulewrite

import "github.com/kesk-git/hive/packages/pool/internal/entry"

func init() { Register(entry.Default) }

// Register builds record, amend and expire of hive-rule, and subject of
// hive-memory, into the registry.
func Register(r *entry.Registry) {
	entry.RegisterRuleWriters(r, Record, Amend, Expire)
	entry.RegisterMemorySubject(r, Subject)
}
