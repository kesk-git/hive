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
				Versions:       []string{op.version},
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

// RegisterMemorySubject builds subject of hive-memory into the registry, with the
// handler of the package that writes the subject records. It is a new memory
// operation, so it is declared at 1.0.
func RegisterMemorySubject(r *Registry, subject Handler) {
	r.RegisterOperation("hive-memory", &cli.Operation{
		OperationDecl: envelope.OperationDecl{
			Name:           "subject",
			State:          envelope.Supported,
			Mode:           envelope.Unary,
			Mutates:        true,
			Versions:       []string{"1.0"},
			RequestSchema:  "urn:hive:schema:ops:hive-memory.subject.request:v1",
			ResponseSchema: "urn:hive:schema:ops:hive-memory.subject.response:v1",
			ReceiptKinds:   []string{"pool.subject.revised"},
			Executes:       []string{},
			Calls:          []string{},
		},
		Handle: subject,
	})
}

// RegisterMemoryMiss builds miss of hive-memory into the registry, with the
// handler of the package that records the misses. It is a new memory operation,
// so it is declared at 1.0. A miss writes pool.rule.missed, and a correction of
// its provenance pool.miss.revised.
func RegisterMemoryMiss(r *Registry, miss Handler) {
	r.RegisterOperation("hive-memory", &cli.Operation{
		OperationDecl: envelope.OperationDecl{
			Name:           "miss",
			State:          envelope.Supported,
			Mode:           envelope.Unary,
			Mutates:        true,
			Versions:       []string{"1.0"},
			RequestSchema:  "urn:hive:schema:ops:hive-memory.miss.request:v1",
			ResponseSchema: "urn:hive:schema:ops:hive-memory.miss.response:v1",
			ReceiptKinds:   []string{"pool.rule.missed", "pool.miss.revised"},
			Executes:       []string{},
			Calls:          []string{},
		},
		Handle: miss,
	})
}

// RegisterMemoryMaintenance builds enqueue and fold of hive-memory into the
// registry, with the handlers of the package that keeps the maintenance queue,
// and the readers of the kinds note and maintenance-queue. Both are new memory
// operations, so both are declared at 1.0. enqueue writes the notes it queues
// and a receipt for each; the fold also renders again the projections of the
// subjects it files facts under and adds the items it routes notes to, so it
// declares the receipts of those beside its own.
func RegisterMemoryMaintenance(r *Registry, enqueue, fold Handler) {
	for _, op := range []struct {
		name     string
		handle   Handler
		receipts []string
	}{
		{"enqueue", enqueue, []string{"pool.note.queued"}},
		{"fold", fold, []string{"pool.memory.folded", "pool.projection.rendered", "pool.item.added"}},
	} {
		r.RegisterOperation("hive-memory", &cli.Operation{
			OperationDecl: envelope.OperationDecl{
				Name:           op.name,
				State:          envelope.Supported,
				Mode:           envelope.Unary,
				Mutates:        true,
				Versions:       []string{"1.0"},
				RequestSchema:  "urn:hive:schema:ops:hive-memory." + op.name + ".request:v1",
				ResponseSchema: "urn:hive:schema:ops:hive-memory." + op.name + ".response:v1",
				ReceiptKinds:   op.receipts,
				Executes:       []string{},
				Calls:          []string{},
			},
			Handle: op.handle,
		})
	}
	read.RegisterMaintenanceReaders(r.Reads)
}
