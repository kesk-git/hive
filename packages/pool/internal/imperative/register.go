package imperative

import "github.com/kesk-git/hive/packages/pool/internal/entry"

func init() { Register(entry.Default) }

// Register builds create, revise, render and deliver of hive-imperative and the
// reader of the kind imperative into the registry.
func Register(r *entry.Registry) {
	entry.RegisterImperative(r, Create, Revise, Render, Deliver)
}
