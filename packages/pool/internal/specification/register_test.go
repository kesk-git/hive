package specification_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kesk-git/hive/contract/declare"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
)

// Importing the package builds define, accept and close into the delivered
// executable, which still agrees with the package's declaration, and builds
// the reader of the kind specification.
func TestImportingThePackageBuildsTheOperationsAndTheReader(t *testing.T) {
	if !slices.Contains(entry.Default.Reads.Names(), "specification") {
		t.Errorf("no reader of specification among %v", entry.Default.Reads.Names())
	}
	e := entry.Executable()
	built := 0
	for _, c := range e.Commands {
		if c.Name != "hive-spec" {
			continue
		}
		for _, op := range c.Operations {
			if op.State != envelope.Supported || !op.Mutates {
				t.Errorf("%s: %+v", op.Name, op.OperationDecl)
			}
			built++
		}
	}
	if built != 3 {
		t.Errorf("%d operations of hive-spec", built)
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

// A read of a specification takes the members of the kind and refuses the
// others, and selects by identity and by project.
func TestTheSpecificationReader(t *testing.T) {
	h := newHome(t)
	a := h.defined(criteria(), nil)
	h.defined(criteria(), nil)
	records := func(sel map[string]any) []any {
		return h.ok(h.pool("read", map[string]any{"kind": "specification", "selector": sel}))["records"].([]any)
	}
	if got := records(map[string]any{"id": a}); len(got) != 1 || got[0].(map[string]any)["specification_id"] != a {
		t.Errorf("by identity: %v", got)
	}
	if got := records(map[string]any{}); len(got) != 2 {
		t.Errorf("all: %d", len(got))
	}
	if got := records(map[string]any{"limit": int64(1)}); len(got) != 1 {
		t.Errorf("limit: %d", len(got))
	}
	if got := records(map[string]any{"project_id": "proj_01m4xkf4z52va9yrp2031hchs1"}); len(got) != 0 {
		t.Errorf("another project: %d", len(got))
	}
	for _, sel := range []map[string]any{
		{"task_id": "task_01m4xkf4z52va9yrp2031hchs1"}, {"states": []any{"accepted"}}, {"ready": true}, {"active": true},
		{"due_before": "2026-10-12T11:00:00.000Z"},
	} {
		refused(t, h.pool("read", map[string]any{"kind": "specification", "selector": sel}), envelope.Invalid, envelope.CodeSchemaViolation)
	}
}
