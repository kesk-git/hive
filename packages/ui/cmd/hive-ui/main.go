// Command hive-ui is the executable of the ui package. It acts as the
// command name it was started under.
package main

import (
	"os"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/packages/ui/internal/entry"
)

func main() {
	os.Exit(cli.Run(entry.Executable()))
}
