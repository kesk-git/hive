// Package entry describes the executable of the ui package, hive-ui, for the
// command harness of spec/commands.md: the commands it owns and the
// operations of each. No operation is built yet: the harness answers
// capabilities and refuses the rest. The package's tests hold this description
// to hive-package.json.
package entry

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/journal"
)

// Version is the implementation version the executable reports.
const Version = "0.1.0"

// Executable describes hive-ui once: its one command, which declares no
// operation yet.
func Executable() *cli.Executable {
	return &cli.Executable{
		Package:   "ui",
		Name:      "hive-ui",
		Version:   Version,
		Storeless: false,
		Journal:   journal.Opener("ui"),
		Commands:  []*cli.Command{{Name: "hive-ui"}},
	}
}
