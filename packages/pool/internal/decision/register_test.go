package decision

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kesk-git/hive/contract/declare"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
	"github.com/kesk-git/hive/packages/pool/internal/read"
)

// Importing the package builds the readers of the three kinds into the
// delivered executable, which then declares hive-pool read supported and still
// agrees with the package's declaration.
func TestImportingThePackageBuildsTheReaders(t *testing.T) {
	got := entry.Default.Reads.Names()
	for _, kind := range []string{"question", "decision-round", read.RequestOutcome} {
		if !slices.Contains(got, kind) {
			t.Errorf("no reader of %s among %v", kind, got)
		}
	}
	e := entry.Executable()
	var op *envelope.OperationDecl
	for _, c := range e.Commands {
		for _, o := range c.Operations {
			if c.Name == "hive-pool" && o.Name == "read" {
				op = &o.OperationDecl
			}
		}
	}
	if op == nil || op.State != envelope.Supported || op.Mutates {
		t.Fatalf("read: %+v", op)
	}
	if err := e.Check(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "hive-package.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := declare.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join("..", "..", "..", "word", "hive-package.json"))
	if err != nil {
		t.Fatal(err)
	}
	word, err := declare.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if ps := (&declare.Set{Packages: []*declare.Package{p, word}}).Agree(e); len(ps) != 0 {
		t.Fatal(ps)
	}
}
