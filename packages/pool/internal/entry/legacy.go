package entry

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
)

// Handler is the handler of an operation.
type Handler = func(*cli.Call) (*cli.Result, error)

// RegisterLegacy builds import-legacy and export-legacy of hive-pool into the
// registry, with the handlers of the package that reads and renders the legacy
// backlog. import-legacy mutates: with dry_run false it writes the migration,
// the items and the holds in one transaction, and with dry_run true it writes
// nothing of the Pool's, only the outcome the harness records for every
// request of a mutating operation. export-legacy writes nothing and is
// read-only.
func RegisterLegacy(r *Registry, importLegacy, exportLegacy Handler) {
	for _, op := range []struct {
		name     string
		handle   Handler
		mutates  bool
		receipts []string
	}{
		{"import-legacy", importLegacy, true, []string{"pool.legacy.imported", "pool.item.added", "pool.hold.placed"}},
		{"export-legacy", exportLegacy, false, []string{}},
	} {
		r.RegisterOperation("hive-pool", &cli.Operation{
			OperationDecl: envelope.OperationDecl{
				Name:           op.name,
				State:          envelope.Supported,
				Mode:           envelope.Unary,
				Mutates:        op.mutates,
				Versions:       []string{"1.0"},
				RequestSchema:  "urn:hive:schema:ops:hive-pool." + op.name + ".request:v1",
				ResponseSchema: "urn:hive:schema:ops:hive-pool." + op.name + ".response:v1",
				ReceiptKinds:   op.receipts,
				Executes:       []string{},
				Calls:          []string{},
			},
			Handle: op.handle,
		})
	}
}
