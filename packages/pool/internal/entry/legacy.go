package entry

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
)

// Handler is the handler of an operation.
type Handler = func(*cli.Call) (*cli.Result, error)

// RegisterLegacy builds import-legacy and export-legacy of hive-pool into the
// registry, with the handlers of the package that reads and renders the legacy
// backlog. Both are declared read-only as they stand: import-legacy imports
// with dry_run true, which writes nothing, and refuses dry_run false as an
// operation the build has not reached.
func RegisterLegacy(r *Registry, importLegacy, exportLegacy Handler) {
	for _, op := range []struct {
		name   string
		handle Handler
	}{
		{"import-legacy", importLegacy},
		{"export-legacy", exportLegacy},
	} {
		r.RegisterOperation("hive-pool", &cli.Operation{
			OperationDecl: envelope.OperationDecl{
				Name:           op.name,
				State:          envelope.Supported,
				Mode:           envelope.Unary,
				Versions:       []string{"1.0"},
				RequestSchema:  "urn:hive:schema:ops:hive-pool." + op.name + ".request:v1",
				ResponseSchema: "urn:hive:schema:ops:hive-pool." + op.name + ".response:v1",
				ReceiptKinds:   []string{},
				Executes:       []string{},
				Calls:          []string{},
			},
			Handle: op.handle,
		})
	}
}
