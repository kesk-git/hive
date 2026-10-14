// Package entry describes the pool executable: the commands it owns, as the
// package's declaration lists them, each answering capabilities and refusing
// every operation that is not built. An operation is built by registering it
// (register.go), and hive-pool read is built once a kind has a reader.
package entry

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
)

// Version is the implementation version of the executable.
const Version = "0.1.0"

// notBuilt is the reason every operation is declared unsupported.
const notBuilt = "the operation is declared and not built yet"

// commands are the command names of the package with the operations its
// declaration lists for each.
var commands = []struct {
	name       string
	operations []string
}{
	{"hive-pool", []string{"add", "read", "update", "transition", "open-generation", "close-generation", "hold", "release", "hold-question", "revise-question", "answer-question", "release-question", "complete-question", "close-question", "import-legacy", "export-legacy"}},
	{"hive-admit", []string{"create", "check", "approve"}},
	{"hive-imperative", []string{"create", "revise", "render", "deliver"}},
	{"hive-spec", []string{"define", "accept", "close"}},
	{"hive-rule", []string{"record", "amend", "expire", "resolve"}},
	{"hive-memory", []string{"render", "check", "propagate", "budget", "miss", "subject", "enqueue", "fold"}},
}

// Executable is the hive-pool executable of the default registry: the
// operations built so far, and every other declared operation refused. Started
// under any of its command names it acts as that command; the contract
// library's harness answers capabilities and --version and refuses a name the
// executable does not own.
func Executable() *cli.Executable { return Default.Executable() }

// unsupported declares an operation the design names and the build has not
// reached: calling it is refused with unsupported_operation.
func unsupported(name string) *cli.Operation {
	return &cli.Operation{OperationDecl: envelope.OperationDecl{
		Name:         name,
		State:        envelope.Unsupported,
		Reason:       notBuilt,
		Mode:         envelope.Unary,
		Versions:     []string{},
		ReceiptKinds: []string{},
		Executes:     []string{},
		Calls:        []string{},
	}}
}
