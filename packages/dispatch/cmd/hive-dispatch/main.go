// Command hive-dispatch is the executable of the dispatch package. It acts as the
// command name it was started under.
package main

import (
	"os"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/packages/dispatch/internal/entry"
)

func main() {
	os.Exit(cli.Run(entry.Executable()))
}
