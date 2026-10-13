// Command hive-spawning-pool is the spawning-pool executable. It acts as the command it was
// started under: the contract library's harness answers for it.
package main

import (
	"os"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/packages/spawning-pool/internal/entry"
)

func main() {
	os.Exit(cli.Run(entry.Executable()))
}
