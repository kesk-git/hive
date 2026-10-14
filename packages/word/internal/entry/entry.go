// Package entry describes the word executable: the commands it owns, as the
// package's declaration lists them, each answering capabilities and refusing
// every operation, since no operation beyond capabilities is built yet.
package entry

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/journal"
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
	{"hive-word", []string{"hold", "revise", "answer", "release", "complete", "close", "verify"}},
}

// Executable is the hive-word executable. Started under any of its command names
// it acts as that command; the contract library's harness answers
// capabilities and --version and refuses a name the executable does not own.
func Executable() *cli.Executable {
	e := &cli.Executable{
		Package:   "word",
		Name:      "hive-word",
		Version:   Version,
		Storeless: true,
		Journal:   journal.Opener("word"),
	}
	for _, c := range commands {
		cmd := &cli.Command{Name: c.name}
		for _, name := range c.operations {
			cmd.Operations = append(cmd.Operations, unsupported(name))
		}
		e.Commands = append(e.Commands, cmd)
	}
	return e
}

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
