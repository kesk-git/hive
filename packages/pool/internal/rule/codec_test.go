package rule_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The codecs of the rule and subject records (spec/pool.md, Rules and
// Subjects): a record decodes to its typed form and encodes back to the same
// document, for every valid record of the contract's fixtures, and what is not
// one of the schema's records does not decode.

func fixtures(t testing.TB, dir string) map[string][]byte {
	t.Helper()
	dir = filepath.Join("..", "..", "..", "..", "contract", "testdata", "schemas", dir, "valid")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("fixtures of %s: %v", dir, err)
	}
	out := map[string][]byte{}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = data
	}
	return out
}

func canonical(t testing.TB, v any) string {
	t.Helper()
	out, err := canon.Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// Every valid rule round trips, as a tree and as a document, and what is
// encoded is still a rule of its schema, stored under its identity.
func TestARuleRoundTrips(t *testing.T) {
	kind, _ := poolstore.Lookup("rule")
	check := func(t *testing.T, data []byte) {
		tree, err := canon.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		r, err := rule.DecodeRule(data)
		if err != nil {
			t.Fatal(err)
		}
		if canonical(t, r.Tree()) != canonical(t, tree) {
			t.Errorf("the tree changed:\n got %s\nwant %s", canonical(t, r.Tree()), canonical(t, tree))
		}
		encoded, err := r.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if err := kind.Check(encoded, r.RuleID, false); err != nil {
			t.Errorf("what was encoded is not a rule: %v", err)
		}
	}
	for name, data := range fixtures(t, "records_pool.rule_v2") {
		t.Run(name, func(t *testing.T) { check(t, data) })
	}
	t.Run("amended", func(t *testing.T) {
		h := newHome(t)
		id := h.rule("dispatch.model", "project", R{
			"expires_at": d20, "lift": liftCondition(), "task_id": "task_01000000000000000000000000", "generation": int64(2),
			"changes":    map[string]any{"rule_id": "rule_01000000000000000000000000", "record_revision": int64(3), "relation": "qualifies"},
			"amendments": []any{lift(1, receipt(liftKind, liftSubject)), amendment(2, "extend", map[string]any{"new_expires_at": d30})},
			"unknown":    map[string]any{"note": "kept"},
		})
		data, err := os.ReadFile(filepath.Join(h.dir, "pool", "records", "rule", id+".json"))
		if err != nil {
			t.Fatal(err)
		}
		check(t, data)
	})
}

// Every valid subject round trips the same way.
func TestASubjectRoundTrips(t *testing.T) {
	kind, _ := poolstore.Lookup("subject")
	for name, data := range fixtures(t, "records_pool.subject") {
		t.Run(name, func(t *testing.T) {
			tree, err := canon.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			s, err := rule.DecodeSubject(data)
			if err != nil {
				t.Fatal(err)
			}
			if canonical(t, s.Tree()) != canonical(t, tree) {
				t.Errorf("the tree changed:\n got %s\nwant %s", canonical(t, s.Tree()), canonical(t, tree))
			}
			encoded, err := s.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if err := kind.Check(encoded, s.SubjectID, false); err != nil {
				t.Errorf("what was encoded is not a subject: %v", err)
			}
		})
	}
}

// A document that is not one of the schema's records does not decode: another
// schema or version, a member the schema does not name, a member of the wrong
// kind, a required member absent.
func TestWhatIsNotARecordDoesNotDecode(t *testing.T) {
	ruleData := fixtures(t, "records_pool.rule_v2")["minimal.json"]
	subjectData := fixtures(t, "records_pool.subject")["minimal.json"]
	for name, mutate := range map[string]func(map[string]any){
		"schema":       func(m map[string]any) { m["schema"] = "pool.item" },
		"version":      func(m map[string]any) { m["schema_version"] = "3.0" },
		"extra member": func(m map[string]any) { m["surprise"] = true },
		"wrong kind":   func(m map[string]any) { m["words"] = int64(3) },
		"absent":       func(m map[string]any) { delete(m, "status") },
		"amendments":   func(m map[string]any) { m["amendments"] = []any{"text"} },
		"changes extra": func(m map[string]any) {
			m["changes"] = map[string]any{"rule_id": "x", "record_revision": int64(1), "relation": "qualifies", "more": 1}
		},
	} {
		t.Run("rule "+name, func(t *testing.T) {
			tree, _ := canon.Parse(ruleData)
			mutate(tree.(map[string]any))
			if _, err := rule.ParseRule(tree.(map[string]any)); err == nil {
				t.Error("decoded")
			}
		})
	}
	for name, mutate := range map[string]func(map[string]any){
		"schema":       func(m map[string]any) { m["schema"] = "pool.rule" },
		"extra member": func(m map[string]any) { m["surprise"] = true },
		"loads_at":     func(m map[string]any) { m["loads_at"] = "dispatch" },
		"fact":         func(m map[string]any) { m["facts"] = []any{map[string]any{"fact_id": "a"}} },
	} {
		t.Run("subject "+name, func(t *testing.T) {
			tree, _ := canon.Parse(subjectData)
			mutate(tree.(map[string]any))
			if _, err := rule.ParseSubject(tree.(map[string]any)); err == nil {
				t.Error("decoded")
			}
		})
	}
}
