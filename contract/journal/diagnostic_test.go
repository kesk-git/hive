package journal

import (
	"testing"

	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/store"
)

// diagnosticEvent is the n-th diagnostic event of pkg with the body given.
func diagnosticEvent(pkg string, n int, body map[string]any) map[string]any {
	m := eventEnvelope(pkg, n)
	m["kind"] = pkg + "." + DiagnosticFact
	m["basis"] = map[string]any{"kind": "observation", "ref": map[string]any{"schema": "hive.request", "id": id(ids.Request, n)}}
	m["body"] = body
	return m
}

// The diagnostic kind is the one kind common to every package: each of the
// seven writes its own instance, and all of them resolve the one shared body
// schema, which refuses what it does not list.
func TestDiagnosticKindResolvesTheSharedSchema(t *testing.T) {
	for n, pkg := range []string{"pool", "spawning-pool", "dispatch", "shadow", "synapse", "word", "ui"} {
		home := newHome(t, false)
		j, _ := open(t, home, pkg, 0)
		body := map[string]any{"command": "hive-" + pkg, "status": "failed", "code": "internal", "request_id": id(ids.Request, n+1), "detail": []any{"open a record: file does not exist"}}
		append_(t, j, encoded(t, diagnosticEvent(pkg, n+1, body)))
		got := readAll(t, j, 0)
		if len(got) != 1 || got[0].Kind != pkg+".diagnostic.recorded" {
			t.Errorf("%s: read %v", pkg, got)
		}

		body["value"] = "x"
		if err := j.Append([]store.Event{encoded(t, diagnosticEvent(pkg, n+2, body))}); err == nil {
			t.Errorf("%s: a body with a member the schema does not list was appended", pkg)
		}
	}
}

// A kind that only looks like the diagnostic one has no schema of its own to
// borrow: its body is empty, as the body of any kind that registers none.
func TestOtherFactsDoNotBorrowTheDiagnosticSchema(t *testing.T) {
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 0)
	body := map[string]any{"command": "hive-pool", "status": "failed", "code": "internal"}
	e := diagnosticEvent("pool", 1, body)
	e["kind"] = "pool.thing.recorded"
	if err := j.Append([]store.Event{encoded(t, e)}); err == nil {
		t.Error("a kind that registers no body schema was appended with a body")
	}
	e = diagnosticEvent("pool", 2, body)
	e["kind"] = "pool.other.diagnostic.recorded"
	if err := j.Append([]store.Event{encoded(t, e)}); err == nil {
		t.Error("a longer kind borrowed the diagnostic schema")
	}
}

// The journal says truthfully that its own validation applies the no-value
// check; Opener opens it for the package named.
func TestJournalSaysItAppliesTheNoValueCheck(t *testing.T) {
	home := newHome(t, false)
	j, err := Opener("pool")(home)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := j.(interface{ ChecksValues() bool })
	if !ok || !c.ChecksValues() {
		t.Errorf("the journal does not say it applies the check: %v", ok)
	}
	if jj, _ := j.(*Journal); jj == nil || jj.Package() != "pool" {
		t.Errorf("journal %v", j)
	}
	if _, err := Opener("nothing")(home); err == nil {
		t.Error("a package of no name was opened")
	}
}
