package imports

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// validTree reads the fixture of a repository that holds two valid records,
// a third-party module with its dependency record, and a NOTICE written by
// hand, into a tree a case can break one way.
func validTree(t *testing.T) fstest.MapFS {
	t.Helper()
	tree := fstest.MapFS{}
	base := filepath.Join("testdata", "valid")
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(base, p)
		tree[filepath.ToSlash(rel)] = &fstest.MapFile{Data: data}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func rules(ps []Problem) map[int]string {
	out := map[int]string{}
	for _, p := range ps {
		out[p.Rule] += p.String() + "\n"
	}
	return out
}

// The checks pass a repository whose records, files, modules and NOTICE agree.
// It catches a check that rejects a sound repository.
func TestCheckPassesAConsistentRepository(t *testing.T) {
	if ps := Check(validTree(t)); len(ps) != 0 {
		t.Errorf("problems: %v", ps)
	}
}

// Each break of a check is reported by that check and by no other: the
// repository is sound but for the one thing a case changes.
func TestCheckCatchesEachBreak(t *testing.T) {
	cases := []struct {
		name   string
		rule   int
		expect string // a part of the message
		also   int    // a check the break cannot avoid breaking too
		break_ func(fstest.MapFS)
	}{
		{"a record with a missing member", 1, "not a valid record", 3, func(m fstest.MapFS) {
			m["imports/example-lib.json"].Data = bytes.Replace(m["imports/example-lib.json"].Data, []byte(`"local_changes": "Renamed the package.",`), nil, 1)
			m["NOTICE"].Data = notice(m, "a-dep")
		}},
		{"a record named for another import", 1, `named for "other"`, 0, func(m fstest.MapFS) {
			m["imports/other.json"] = &fstest.MapFile{Data: m["imports/example-lib.json"].Data}
		}},
		{"a file under imports that is not a record", 1, "not a record", 0, func(m fstest.MapFS) {
			m["imports/notes.txt"] = &fstest.MapFile{Data: []byte("notes\n")}
		}},
		{"a licence text that was changed", 2, "digest", 0, func(m fstest.MapFS) {
			m["packages/spawning-pool/upstream/example-lib/LICENSE"].Data = []byte("changed\n")
		}},
		{"a licence text that is gone", 2, "cannot be read", 0, func(m fstest.MapFS) {
			delete(m, "packages/dispatch/dep-licence.txt")
		}},
		{"a portion that is gone", 2, "portion", 0, func(m fstest.MapFS) {
			delete(m, "packages/spawning-pool/internal/example/copy.go")
		}},
		{"an upstream file no record names", 3, "no record names it", 0, func(m fstest.MapFS) {
			m["packages/pool/upstream/stray/NOTICE"] = &fstest.MapFile{Data: []byte("stray\n")}
		}},
		{"an upstream file no record names, in a testdata directory", 3, "no record names it", 0, func(m fstest.MapFS) {
			m["packages/pool/upstream/stray/testdata/NOTICE"] = &fstest.MapFile{Data: []byte("stray\n")}
		}},
		{"an upstream file no record names, in a dist directory", 3, "no record names it", 0, func(m fstest.MapFS) {
			m["packages/pool/upstream/stray/dist/NOTICE"] = &fstest.MapFile{Data: []byte("stray\n")}
		}},
		{"an upstream file no record names, under a nested .git", 3, "no record names it", 0, func(m fstest.MapFS) {
			m["packages/pool/upstream/stray/.git/NOTICE"] = &fstest.MapFile{Data: []byte("stray\n")}
		}},
		{"a module without a dependency record", 4, "requires example.invalid/other v0.1.0", 0, func(m fstest.MapFS) {
			m["go.mod"].Data = append(m["go.mod"].Data, []byte("\nrequire example.invalid/other v0.1.0\n")...)
		}},
		{"a module pinned at another version", 4, "pins example.invalid/dep at v1.2.4", 0, func(m fstest.MapFS) {
			m["go.mod"].Data = bytes.Replace(m["go.mod"].Data, []byte("v1.2.3"), []byte("v1.2.4"), 1)
		}},
		{"a declaration that names a record that does not exist", 4, `"ghost"`, 0, func(m fstest.MapFS) {
			m["packages/spawning-pool/hive-package.json"].Data = []byte(`{"libraries":[],"imports":["ghost"]}`)
		}},
		{"records without the root licence", 5, "LICENSE", 0, func(m fstest.MapFS) {
			delete(m, "LICENSE")
		}},
		{"a NOTICE that was edited", 6, "differs", 0, func(m fstest.MapFS) {
			m["NOTICE"].Data = bytes.Replace(m["NOTICE"].Data, []byte("2020"), []byte("2021"), 1)
		}},
		{"a NOTICE that is missing", 6, "cannot be read", 0, func(m fstest.MapFS) {
			delete(m, "NOTICE")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tree := validTree(t)
			c.break_(tree)
			got := rules(Check(tree))
			if !strings.Contains(got[c.rule], c.expect) {
				t.Errorf("check %d did not report %q; it reported %q", c.rule, c.expect, got[c.rule])
			}
			for rule, text := range got {
				if rule != c.rule && rule != c.also {
					t.Errorf("check %d also reported: %s", rule, text)
				}
			}
		})
	}
}

// notice regenerates a tree's NOTICE from the records named, so that a case
// that breaks a record breaks only that record.
func notice(m fstest.MapFS, ids ...string) []byte {
	var records []Record
	all, _ := LoadRecords(m)
	for _, r := range all {
		for _, id := range ids {
			if r.ImportID == id {
				records = append(records, r)
			}
		}
	}
	return Notice(records)
}

// The notice is the fixed header and one block for each record in import_id
// order, with each notice verbatim, a multi-line one included. The expected
// text of the fixture is written by hand. It catches a block out of order, a
// notice that is trimmed or reflowed, and a file that does not end in one line
// feed.
func TestNoticeOfTheFixtureRecords(t *testing.T) {
	tree := validTree(t)
	records, problems := LoadRecords(tree)
	if len(problems) != 0 || len(records) != 2 {
		t.Fatalf("%d records, problems %v", len(records), problems)
	}
	if got, want := Notice(records), tree["NOTICE"].Data; !bytes.Equal(got, want) {
		t.Errorf("the notice differs from the hand-written one:\n%s\nwant:\n%s", got, want)
	}
	// The order does not depend on the order the records are given in.
	if got := Notice([]Record{records[1], records[0]}); !bytes.Equal(got, tree["NOTICE"].Data) {
		t.Errorf("the notice follows the order the records came in")
	}
}

// With no record the notice is the header and the one sentence that none
// exists yet.
func TestNoticeWithNoRecord(t *testing.T) {
	got := string(Notice(nil))
	if !strings.HasSuffix(got, "\n\nNo import record exists yet.\n") || strings.Count(got, "\n--------") != 0 {
		t.Errorf("%q", got)
	}
}

// The repository as committed passes all six checks: zero records, the
// licence present and a NOTICE equal to the one the records produce.
func TestRepositoryImports(t *testing.T) {
	root := filepath.Join("..", "..")
	if ps := Check(os.DirFS(root)); len(ps) != 0 {
		for _, p := range ps {
			t.Error(p)
		}
	}
}
