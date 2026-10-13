// Command hive-dispatch is the executable of the dispatch package. It acts as the
// command name it was started under.
package main

import (
	"fmt"
	"os"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/packages/dispatch"
)

func main() {
	e, err := dispatch.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(cli.Run(e))
}
