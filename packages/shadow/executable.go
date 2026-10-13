// Package shadow describes the executable of the shadow package, hive-shadow, for the
// command harness of spec/commands.md. The commands and operations come from
// the package's own declaration, hive-package.json, which is embedded, so
// that the executable cannot list a command or an operation the declaration
// does not. No operation is built yet: each declared operation is
// unsupported, and the harness answers capabilities and refuses the rest.
package shadow

import (
	_ "embed"
	"fmt"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/declare"
	"github.com/kesk-git/hive/contract/envelope"
)

// Version is the implementation version the executable reports.
const Version = "0.1.0"

// notBuilt is the reason every declared operation gives while none is built.
const notBuilt = "The operation is named by the design and is not built yet."

//go:embed hive-package.json
var declaration []byte

// Executable describes hive-shadow once: the commands its declaration lists,
// each with the operations it lists, every one of them unsupported.
func Executable() (*cli.Executable, error) {
	p, err := declare.Parse(declaration)
	if err != nil {
		return nil, fmt.Errorf("shadow: the package declaration: %w", err)
	}
	e := &cli.Executable{
		Package:   p.Name,
		Name:      p.Executable,
		Version:   Version,
		Storeless: len(p.HomePaths) == 0,
	}
	for _, c := range p.Commands {
		cmd := &cli.Command{Name: c.Name}
		for _, name := range c.Operations {
			cmd.Operations = append(cmd.Operations, &cli.Operation{OperationDecl: envelope.OperationDecl{
				Name:         name,
				State:        envelope.Unsupported,
				Reason:       notBuilt,
				Mode:         envelope.Unary,
				Versions:     []string{},
				ReceiptKinds: []string{},
				Executes:     []string{},
				Calls:        []string{},
			}})
		}
		e.Commands = append(e.Commands, cmd)
	}
	return e, nil
}
