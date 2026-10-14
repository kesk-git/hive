// Command hive-pool is the pool executable. It acts as the command it was
// started under: the contract library's harness answers for it.
package main

import (
	"os"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
	_ "github.com/kesk-git/hive/packages/pool/internal/item"
)

func main() {
	os.Exit(cli.Run(entry.Executable()))
}
