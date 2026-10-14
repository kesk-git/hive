// Package read is the foundation of hive-pool read: the projection, the
// locking and the refusals, with the readers of each kind registered in it.
package read

import "github.com/kesk-git/hive/contract/cli"

// Operation is the read operation of the registry.
func (r *Registry) Operation() *cli.Operation { return nil }
