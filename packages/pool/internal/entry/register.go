package entry

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/packages/pool/internal/read"
)

// Registry is what an executable is built from: the operations built so far
// and the readers of the kinds hive-pool read can answer.
type Registry struct {
	Reads *read.Registry
}

// NewRegistry returns a registry with nothing built.
func NewRegistry() *Registry { return &Registry{Reads: read.NewRegistry()} }

// RegisterOperation replaces the declared-only operation of the command.
func (r *Registry) RegisterOperation(command string, op *cli.Operation) {}

// Executable is the executable the registry describes.
func (r *Registry) Executable() *cli.Executable { return Executable() }
