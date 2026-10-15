package entry

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/read"
)

// RegisterMemory builds render, check, propagate and budget of hive-memory into
// the registry, with the handlers of the package that keeps the projections,
// and the reader of the kind projection with its derivation of freshness, which
// declare hive-pool read supported as the first reader does. The package
// registers them into the default registry from its init function, as the
// package that keeps the specifications does.
func RegisterMemory(r *Registry, render, check, propagate, budget Handler, derive read.Derive) {
	for _, op := range []struct {
		name, version, request, response string
		handle                           Handler
		mutates                          bool
		receipts                         []string
	}{
		{"render", "2.0", "render.request:v2", "render.response:v2", render, true, []string{"pool.projection.rendered"}},
		{"check", "2.0", "check.request:v2", "check.response:v2", check, false, []string{}},
		{"propagate", "2.0", "propagate.request:v2", "propagate.response:v2", propagate, true, []string{"pool.projection.received", "pool.projection.acknowledged"}},
		{"budget", "1.0", "budget.request:v1", "budget.response:v1", budget, true, []string{"pool.budget.changed", "pool.projection.rendered"}},
	} {
		r.RegisterOperation("hive-memory", &cli.Operation{
			OperationDecl: envelope.OperationDecl{
				Name:           op.name,
				State:          envelope.Supported,
				Mode:           envelope.Unary,
				Mutates:        op.mutates,
				Versions:       versions(op.version),
				RequestSchema:  "urn:hive:schema:ops:hive-memory." + op.request,
				ResponseSchema: "urn:hive:schema:ops:hive-memory." + op.response,
				ReceiptKinds:   op.receipts,
				Executes:       []string{},
				Calls:          []string{},
			},
			Handle: op.handle,
		})
	}
	read.RegisterProjectionReader(r.Reads, derive)
}

// versions are the operation versions an operation accepts: the version of its
// body schema, and 1.0, which every operation of the Pool accepts and which the
// cross-package call proof sends to each operation it calls. No v1 body of
// render, check or propagate exists, so a request at 1.0 is read against the
// current schema.
func versions(current string) []string {
	if current == "1.0" {
		return []string{"1.0"}
	}
	return []string{current, "1.0"}
}
