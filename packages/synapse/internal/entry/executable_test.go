package entry

import (
	"os"
	"testing"

	"github.com/kesk-git/hive/contract/declare"
)

// The executable is the one the package declares: it passes the harness's
// own check, and agrees with hive-package.json in its commands and operations.
func TestTheExecutableAgreesWithTheDeclaration(t *testing.T) {
	set := ownSet(t)
	e := Executable()
	if err := e.Check(); err != nil {
		t.Fatal(err)
	}
	if ps := set.Agree(e); len(ps) != 0 {
		t.Error(ps)
	}
}

// ownSet is the set that holds this package's declaration alone, read from
// hive-package.json two levels above this directory.
func ownSet(t testing.TB) *declare.Set {
	t.Helper()
	data, err := os.ReadFile("../../" + declare.FileName)
	if err != nil {
		t.Fatal(err)
	}
	p, err := declare.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return &declare.Set{Packages: []*declare.Package{p}}
}
