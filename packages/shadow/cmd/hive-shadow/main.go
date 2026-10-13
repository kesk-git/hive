// Command hive-shadow is the executable of the shadow package. It acts as the
// command name it was started under.
package main

import (
	"os"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/packages/shadow/internal/entry"
)

func main() {
	os.Exit(cli.Run(entry.Executable()))
}
