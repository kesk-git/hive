// Package entry describes the executable of the dispatch package, hive-dispatch, for the
// command harness of spec/commands.md: the commands it owns and the
// operations of each. One operation is built, hive-fleet provision, which
// the registry package implements; every other declared operation is
// unsupported, and the harness answers capabilities and refuses the rest. The
// package's tests hold this description to hive-package.json.
package entry

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/journal"
	"github.com/kesk-git/hive/packages/dispatch/internal/registry"
)

// Version is the implementation version the executable reports.
const Version = "0.1.0"

// notBuilt is the reason every declared operation gives until it is built.
const notBuilt = "The operation is named by the design and is not built yet."

// commands are the commands the executable owns, each with its operations.
var commands = []struct {
	name       string
	operations []string
}{
	{"hive-dispatch", nil},
	{"hive-fleet", []string{"provision", "route", "recover"}},
	{"hive-schedule", []string{"file", "due", "hold", "retire"}},
	{"hive-session-start", nil},
	{"hive-policy", []string{"resolve"}},
	{"hive-context", []string{"observe", "checkpoint"}},
	{"hive-park", nil},
	{"hive-resume", nil},
	{"hive-send", nil},
	{"hive-control", []string{"stop"}},
	{"hive-stow", nil},
	{"hive-recover", nil},
	{"hive-sync", nil},
	{"hive-retire", nil},
}

// Executable describes hive-dispatch once: the commands it owns, each with its
// operations, every one of them unsupported but hive-fleet provision.
func Executable() *cli.Executable {
	e := &cli.Executable{
		Package:   "dispatch",
		Name:      "hive-dispatch",
		Version:   Version,
		Storeless: false,
		Journal:   journal.Opener("dispatch"),
	}
	for _, c := range commands {
		cmd := &cli.Command{Name: c.name}
		for _, name := range c.operations {
			if c.name == registry.Command && name == registry.Operation {
				cmd.Operations = append(cmd.Operations, registry.Provision())
				continue
			}
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
	return e
}
