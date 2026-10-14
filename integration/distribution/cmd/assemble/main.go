// Command assemble writes the artefact of spec/distribution.md from the seven
// executables `make build` left in dist/: dist/hive-<version>-<os>-<arch>/.
// It builds nothing and runs on all three platforms the same way. Run it from
// the repository root through `make assemble`, which puts dist/ first on the
// search path so that the executables are found where contract/exec looks.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kesk-git/hive/integration/distribution/artefact"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	dir, err := artefact.Assemble(artefact.Options{
		Root:    root,
		Dist:    filepath.Join(root, "dist"),
		Inspect: artefact.Process{},
	})
	if err != nil {
		fail(err)
	}
	fmt.Println("assemble: wrote", dir)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "assemble:", err)
	os.Exit(1)
}
