package dispatch

import (
	"os"
	"testing"

	"github.com/kesk-git/hive/contract/declare"
)

// The executable is the one the package declares: it passes the harness's
// own check, and agrees with hive-package.json in its commands and operations.
func TestTheExecutableAgreesWithTheDeclaration(t *testing.T) {
	set, problems := declare.Read(os.DirFS("../.."))
	if err := problems.Err(); err != nil {
		t.Fatal(err)
	}
	e, err := Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Check(); err != nil {
		t.Fatal(err)
	}
	if ps := set.Agree(e); len(ps) != 0 {
		t.Error(ps)
	}
}
