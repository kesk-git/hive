package entry

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/read"
)

// RegisterSpecification builds define, accept and close of hive-spec into the
// registry, with the handlers of the package that keeps the specification
// records, and the reader of the kind specification, which declares
// hive-pool read supported as the first reader does. The package registers
// them into the default registry from its init function, as the package that
// keeps the admission records registers approve.
func RegisterSpecification(r *Registry, define, accept, closeSpecification Handler) {
	for _, op := range []struct {
		name     string
		versions []string
		handle   Handler
		receipts []string
	}{
		// define is declared at the contract's version for the complete body,
		// 1.2, and accepts the two before it: 1.1 added product bindings and
		// 1.2 touched subjects, both optional members of the one body.
		{"define", []string{"1.2", "1.1", "1.0"}, define, []string{"pool.specification.defined", "pool.specification.invalidated", "pool.hold.placed"}},
		{"accept", []string{"1.0"}, accept, []string{"pool.specification.accepted", "pool.hold.released"}},
		{"close", []string{"1.0"}, closeSpecification, []string{"pool.specification.closed"}},
	} {
		r.RegisterOperation("hive-spec", &cli.Operation{
			OperationDecl: envelope.OperationDecl{
				Name:           op.name,
				State:          envelope.Supported,
				Mode:           envelope.Unary,
				Mutates:        true,
				Versions:       op.versions,
				RequestSchema:  "urn:hive:schema:ops:hive-spec." + op.name + ".request:v1",
				ResponseSchema: "urn:hive:schema:ops:hive-spec." + op.name + ".response:v1",
				ReceiptKinds:   op.receipts,
				Executes:       []string{},
				Calls:          []string{},
			},
			Handle: op.handle,
		})
	}
	read.RegisterSpecificationReader(r.Reads)
}
