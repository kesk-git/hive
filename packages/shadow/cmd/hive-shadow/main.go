// Command hive-shadow is the executable of the shadow package. It acts as the
// command name it was started under.
package main

import (
	"fmt"
	"os"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/packages/shadow"
)

func main() {
	e, err := shadow.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(cli.Run(e))
}
