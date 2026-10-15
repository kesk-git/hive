package item

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
	"github.com/kesk-git/hive/packages/pool/internal/read"
)

func init() { Register(entry.Default) }

// Register builds the operations add, update, transition, hold and release of
// hive-pool and the readers of the kinds item and hold into the registry.
func Register(r *entry.Registry) {
	for _, op := range []*cli.Operation{
		operation("add", add, "pool.item.added", "pool.hold.placed"),
		operation("update", update, "pool.hold.placed", "pool.hold.released"),
		operation("transition", transition, "pool.item.transitioned"),
		operation("open-generation", openGeneration, "pool.generation.opened"),
		operation("close-generation", closeGeneration, "pool.generation.closed"),
		operation("hold", hold, "pool.hold.placed"),
		operation("release", release, "pool.hold.released"),
	} {
		r.RegisterOperation("hive-pool", op)
	}
	read.RegisterItemReaders(r.Reads)
}

func operation(name string, handle func(*cli.Call) (*cli.Result, error), receipts ...string) *cli.Operation {
	return &cli.Operation{
		OperationDecl: envelope.OperationDecl{
			Name:           name,
			State:          envelope.Supported,
			Mode:           envelope.Unary,
			Mutates:        true,
			Versions:       []string{versionOne},
			RequestSchema:  "urn:hive:schema:ops:hive-pool." + name + ".request:v1",
			ResponseSchema: "urn:hive:schema:ops:hive-pool." + name + ".response:v1",
			ReceiptKinds:   receipts,
			Executes:       []string{},
			Calls:          []string{},
		},
		Handle: handle,
	}
}
