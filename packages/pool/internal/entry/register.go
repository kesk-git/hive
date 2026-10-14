package entry

import (
	"fmt"
	"slices"
	"sync"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/journal"
	"github.com/kesk-git/hive/packages/pool/internal/read"
)

// Registry is what an executable is built from: the operations that are built,
// and the readers of the kinds hive-pool read can answer. The lanes that build
// an operation or a kind's reader register it, and nothing else of the
// executable changes.
type Registry struct {
	// Reads are the kinds that have a reader. hive-pool read is declared
	// supported once there is one, and refused as unsupported until then.
	Reads *read.Registry

	mu  sync.Mutex
	ops map[string]*cli.Operation
}

// NewRegistry returns a registry with nothing built.
func NewRegistry() *Registry {
	return &Registry{Reads: read.NewRegistry(), ops: map[string]*cli.Operation{}}
}

// Default is the registry of the delivered executable. Its readers are those
// of read.Default.
var Default = &Registry{Reads: read.Default, ops: map[string]*cli.Operation{}}

// RegisterOperation builds an operation of the default registry.
func RegisterOperation(command string, op *cli.Operation) { Default.RegisterOperation(command, op) }

// RegisterOperation builds the operation of the command in place of its
// declared-only stub. It panics on a registration that cannot be right, as a
// mistake of the program: an operation the command does not declare, one built
// twice, one not supported, and read, which the readers of the kinds build.
func (r *Registry) RegisterOperation(command string, op *cli.Operation) {
	if !declared(command, op.Name) {
		panic(fmt.Sprintf("entry: %s does not declare the operation %s", command, op.Name))
	}
	if op.State != envelope.Supported || op.Handle == nil {
		panic(fmt.Sprintf("entry: %s %s is registered to be built and is not supported with a handler", command, op.Name))
	}
	if command == "hive-pool" && op.Name == "read" {
		panic("entry: hive-pool read is built from the readers registered for the kinds, not registered itself")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := command + " " + op.Name
	if _, ok := r.ops[key]; ok {
		panic(fmt.Sprintf("entry: %s is built twice", key))
	}
	r.ops[key] = op
}

// declared reports whether the command declares the operation.
func declared(command, operation string) bool {
	for _, c := range commands {
		if c.name == command {
			return slices.Contains(c.operations, operation)
		}
	}
	return false
}

// Executable is the hive-pool executable of the registry.
func (r *Registry) Executable() *cli.Executable {
	e := &cli.Executable{
		Package:   "pool",
		Name:      "hive-pool",
		Version:   Version,
		Storeless: false,
		Handoffs:  []string{"word"},
		Journal:   journal.Opener("pool"),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range commands {
		cmd := &cli.Command{Name: c.name}
		for _, name := range c.operations {
			switch op, built := r.ops[c.name+" "+name]; {
			case built:
				cmd.Operations = append(cmd.Operations, op)
			case c.name == "hive-pool" && name == "read" && r.Reads.Len() > 0:
				cmd.Operations = append(cmd.Operations, r.Reads.Operation())
			default:
				cmd.Operations = append(cmd.Operations, unsupported(name))
			}
		}
		e.Commands = append(e.Commands, cmd)
	}
	return e
}
