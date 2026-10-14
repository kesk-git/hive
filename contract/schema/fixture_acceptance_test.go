package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// The acceptance scenarios of the fixture rules: what the walker must require
// of the committed schemas, and what a schema's invalid examples may be.

// fixtureTree writes files, each a slash path below the returned root, and
// returns the root.
func fixtureTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// mentions reports whether some problem contains every one of the words.
func mentions(problems []string, words ...string) bool {
	for _, p := range problems {
		ok := true
		for _, w := range words {
			ok = ok && strings.Contains(p, w)
		}
		if ok {
			return true
		}
	}
	return false
}

const (
	commonID  = "urn:hive:schema:common:v1"
	requestID = "urn:hive:schema:request:v1"
)

func TestFixtureAcceptanceRequiresEveryNonDeferredSchema(t *testing.T) {
	r, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	root := fixtureTree(t, map[string]string{
		"common/valid/open_object.ok.json":      "{}\n",
		"common/invalid/open_object.array.json": "[]\n",
	})
	got := checkFixtureTree(r, root, []string{commonID, requestID}, nil)
	if !mentions(got, "request", "no accepted example") || !mentions(got, "request", "no refused example") {
		t.Errorf("a required schema with no fixture directory was not refused for both proofs; got %q", got)
	}
	if mentions(got, "common") {
		t.Errorf("a schema with correct fixtures was reported: %q", got)
	}
}

func TestFixtureAcceptanceRunsAnInvalidTable(t *testing.T) {
	r, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	root := fixtureTree(t, map[string]string{
		"common/valid/open_object.ok.json": "{}\n",
		"common/invalid.jsonl":             `{"name":"open_object.array","base":"open_object.ok.json","edits":[{"op":"replace","path":"","value":[]}]}` + "\n",
	})
	if got := checkFixtureTree(r, root, []string{commonID}, nil); len(got) > 0 {
		t.Errorf("a table row refusing its base was reported: %q", got)
	}
}

func TestFixtureAcceptanceTightensAStageDeferral(t *testing.T) {
	const id = "urn:hive:schema:later:v1"
	r, err := Load(fstest.MapFS{
		"later.v1.schema.json":  file(id, `"type":"object","properties":{"x":{"type":"integer"}},"required":["x"],"additionalProperties":false`),
		"common.v1.schema.json": file(commonID, `"$defs":{"open_object":{"type":"object"}}`),
	}, ".")
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{commonID, id}
	deferred := []fixtureDeferral{{ID: id, Stage: 2}}
	common := map[string]string{
		"common/valid/open_object.ok.json":      "{}\n",
		"common/invalid/open_object.array.json": "[]\n",
	}
	later := map[string]string{
		"later/valid/ok.json":              `{"x":1}` + "\n",
		"later/invalid/missing-x.json":     "{}\n",
		"common/valid/open_object.ok.json": "{}\n",
	}
	with := func(base map[string]string, more map[string]string) string {
		files := map[string]string{}
		for k, v := range base {
			files[k] = v
		}
		for k, v := range more {
			files[k] = v
		}
		return fixtureTree(t, files)
	}

	if got := checkFixtureTree(r, with(common, nil), ids, deferred); len(got) > 0 {
		t.Errorf("a deferred schema without fixtures was reported: %q", got)
	}
	if got := checkFixtureTree(r, with(common, nil), ids, nil); !mentions(got, "later", "no accepted example") {
		t.Errorf("a schema whose deferral was removed, with no fixtures, was not refused: %q", got)
	}
	if got := checkFixtureTree(r, with(common, later), ids, deferred); !mentions(got, "later", "deferred") {
		t.Errorf("a deferral kept beside complete fixtures was not refused as stale: %q", got)
	}
	if got := checkFixtureTree(r, with(common, later), ids, nil); len(got) > 0 {
		t.Errorf("complete fixtures with the deferral removed were reported: %q", got)
	}
}
