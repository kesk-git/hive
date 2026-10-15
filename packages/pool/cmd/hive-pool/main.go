// Command hive-pool is the pool executable. It acts as the command it was
// started under: the contract library's harness answers for it.
package main

import (
	"os"

	"github.com/kesk-git/hive/contract/cli"
	_ "github.com/kesk-git/hive/packages/pool/internal/admission"
	_ "github.com/kesk-git/hive/packages/pool/internal/decision"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
	_ "github.com/kesk-git/hive/packages/pool/internal/item"
	_ "github.com/kesk-git/hive/packages/pool/internal/legacy"
	_ "github.com/kesk-git/hive/packages/pool/internal/memory"
	_ "github.com/kesk-git/hive/packages/pool/internal/rule"
	_ "github.com/kesk-git/hive/packages/pool/internal/rulewrite"
	_ "github.com/kesk-git/hive/packages/pool/internal/specification"
)

func main() {
	os.Exit(cli.Run(entry.Executable()))
}
