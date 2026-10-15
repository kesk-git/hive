package entry

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/read"
)

// RegisterImperative builds create, revise, render and deliver of
// hive-imperative into the registry, with the handlers of the package that keeps
// the Imperatives, and the reader of the kind imperative, which declares
// hive-pool read supported as the first reader does. The package registers them
// into the default registry from its init function, as the package that keeps
// the specifications does.
//
// Each operation is declared at the versions of the body it serves in full
// (spec/pool.md, Contract versions). render and deliver serve the additive
// bodies of 1.1, the inventories and the instant of the rendering, and are
// declared at 1.1 only. create and revise have no member that 1.1 added, so
// their bodies are unchanged and each is declared at 1.0 only. render
// writes nothing and returns no receipt; deliver writes the delivery, and the
// receipts of each projection it records received beside it.
func RegisterImperative(r *Registry, create, revise, render, deliver Handler) {
	for _, op := range []struct {
		name     string
		versions []string
		handle   Handler
		mutates  bool
		receipts []string
	}{
		{"create", []string{"1.0"}, create, true, []string{"pool.imperative.revised"}},
		{"revise", []string{"1.0"}, revise, true, []string{"pool.imperative.revised"}},
		{"render", []string{"1.1"}, render, false, []string{}},
		{"deliver", []string{"1.1"}, deliver, true, []string{"pool.imperative.delivered", "pool.projection.received"}},
	} {
		r.RegisterOperation("hive-imperative", &cli.Operation{
			OperationDecl: envelope.OperationDecl{
				Name:           op.name,
				State:          envelope.Supported,
				Mode:           envelope.Unary,
				Mutates:        op.mutates,
				Versions:       op.versions,
				RequestSchema:  "urn:hive:schema:ops:hive-imperative." + op.name + ".request:v1",
				ResponseSchema: "urn:hive:schema:ops:hive-imperative." + op.name + ".response:v1",
				ReceiptKinds:   op.receipts,
				Executes:       []string{},
				Calls:          []string{},
			},
			Handle: op.handle,
		})
	}
	read.RegisterImperativeReader(r.Reads)
}
