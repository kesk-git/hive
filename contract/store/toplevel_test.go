package store

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
)

// homeContent is the content of the home record of testdata, without what the
// store sets.
func homeContent(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/home/home.json")
	if err != nil {
		t.Fatal(err)
	}
	obj := parsed(t, data)
	delete(obj, "record_revision")
	delete(obj, "updated_at")
	out, err := canon.Encode(obj)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// provision is the transaction that creates a home: home.json and the
// registry's record, together.
func provision(t *testing.T) Tx {
	return Tx{
		RequestID: req9,
		Records: []RecordWrite{
			{HomeFilePath, homeContent(t), 0},
			{ws1, []byte(`{"slug":"alpha"}`), 0},
		},
		Receipts: []Receipt{{rcpt1, []byte(`{"receipt_id":"` + rcpt1 + `","kind":"dispatch.home.created"}`)}},
		Outcome:  []byte(`{"status":"ok"}`),
	}
}

// A dispatch transaction that names home.json writes the home's own record,
// at the top of the home, beside the records of its own directory and in the
// same transaction.
func TestDispatchWritesHomeJSONInATransaction(t *testing.T) {
	home := newHome(t)
	s, _, _ := open(t, home, "dispatch")
	w := lock(t, s)
	defer w.Release()
	got, err := w.Commit(provision(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.Records[0].Path != HomeFilePath || got.Records[0].Revision != 1 {
		t.Errorf("Committed = %+v", got.Records)
	}
	if _, err := os.Stat(filepath.Join(home, "dispatch", homeFile)); err == nil {
		t.Error("home.json was written below dispatch/")
	}
	h, err := CheckHome(home)
	if err != nil {
		t.Fatal(err)
	}
	if h.HomeID != "home_01arz3ndektsv4rrffq69g5fav" {
		t.Errorf("the home is identified as %q", h.HomeID)
	}
	rec, err := s.ReadRecord(HomeFilePath)
	if err != nil || rec.Revision != 1 || rec.Digest != got.Records[0].Digest {
		t.Errorf("ReadRecord = %+v, %v", rec, err)
	}
	if _, err := s.ReadRecord(ws1); err != nil {
		t.Error(err)
	}

	// It is replaced like any record, at its revision.
	if _, err := w.Replace(req1, HomeFilePath, homeContent(t), 2); !errors.Is(err, ErrRevisionConflict) {
		t.Errorf("Replace at the wrong revision: %v", err)
	}
	if got, err := w.Replace(req1, HomeFilePath, homeContent(t), 1); err != nil || got.Revision != 2 {
		t.Errorf("Replace = %+v, %v", got, err)
	}
	if _, err := CheckHome(home); err != nil {
		t.Error(err)
	}
}

// A ui transaction writes under reviews/, at the top of the home.
func TestUIWritesReviewsInATransaction(t *testing.T) {
	home := newHome(t)
	s, _, _ := open(t, home, "ui")
	w := lock(t, s)
	defer w.Release()
	review := ReviewPath("task-1/1/review-1/review.json")
	if _, err := w.Commit(Tx{
		RequestID: req9,
		Records:   []RecordWrite{{review, []byte(`{"n":1}`), 0}, {RecordPath("state", "x"), []byte(`{}`), 0}},
		Outcome:   []byte(`{"status":"ok"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "reviews", "task-1", "1", "review-1", "review.json")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(home, "ui", "reviews")); err == nil {
		t.Error("the review was written below ui/")
	}
	if _, err := CheckHome(home); err != nil {
		t.Error(err)
	}
	if rec, err := s.ReadRecord(review); err != nil || rec.Revision != 1 {
		t.Errorf("ReadRecord = %+v, %v", rec, err)
	}
}

// A package writes its own directory and the one top-level path the table
// gives it, and nothing else of the home. A path that is not the package's own
// top-level path is a path of its own directory, and a path that leaves it is
// refused.
func TestTopLevelPathsAreOwned(t *testing.T) {
	home := newHome(t)
	stores := map[string]*Store{}
	for _, pkg := range storePackages {
		stores[pkg], _, _ = open(t, home, pkg)
		lock(t, stores[pkg]).Release()
	}
	before := tree(t, home)

	// The names of another package's top-level path land below the writer's
	// own directory, and the top of the home is untouched.
	for _, tc := range []struct{ pkg, path string }{
		{"pool", HomeFilePath},
		{"pool", ReviewPath("a/review.json")},
		{"spawning-pool", HomeFilePath},
		{"shadow", ReviewPath("a/review.json")},
		{"synapse", HomeFilePath},
		{"dispatch", ReviewPath("a/review.json")},
		{"ui", "home.json/x.json"},
	} {
		t.Run(tc.pkg+" "+tc.path, func(t *testing.T) {
			s := stores[tc.pkg]
			w := lock(t, s)
			defer w.Release()
			if _, err := w.Replace(req1, tc.path, []byte(`{}`), 0); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(home, tc.pkg, filepath.FromSlash(tc.path))); err != nil {
				t.Errorf("not written below %s/: %v", tc.pkg, err)
			}
			for _, top := range []string{homeFile, "reviews"} {
				if _, err := os.Lstat(filepath.Join(home, top)); err == nil {
					t.Errorf("%s was written at the top of the home", top)
				}
			}
		})
	}
	for _, pkg := range storePackages {
		os.RemoveAll(filepath.Join(home, pkg))
	}
	for _, pkg := range storePackages {
		stores[pkg], _, _ = open(t, home, pkg)
		lock(t, stores[pkg]).Release()
	}
	sameTree(t, "after the writes below the packages", tree(t, home), before)

	for _, tc := range []struct{ pkg, path string }{
		{"dispatch", "../journal/x.json"},
		{"dispatch", "../home.json"},
		{"dispatch", "../../home.json"},
		{"dispatch", "../"},
		{"dispatch", "home.json/../../home.json"},
		{"ui", "../reviews/a/review.json"},
		{"ui", "reviews/"},
		{"ui", "reviews/../home.json"},
		{"ui", "reviews/a/../../home.json"},
		{"ui", "reviews/a//review.json"},
		{"ui", "reviews/a/Review.json"},
		{"ui", "reviews/a/review"},
		{"ui", "reviews/a/x.json.tmp"},
		{"ui", "/reviews/a/review.json"},
	} {
		t.Run("refused "+tc.pkg+" "+tc.path, func(t *testing.T) {
			s := stores[tc.pkg]
			w := lock(t, s)
			defer w.Release()
			if _, err := w.Replace(req1, tc.path, []byte(`{}`), 0); err == nil {
				t.Error("Replace wrote the path")
			}
			if _, err := w.Commit(Tx{RequestID: req2, Records: []RecordWrite{{tc.path, []byte(`{}`), 0}}, Outcome: []byte(`{}`)}); err == nil {
				t.Error("Commit wrote the path")
			}
			if _, err := s.ReadRecord(tc.path); err == nil || errors.Is(err, os.ErrNotExist) {
				t.Errorf("ReadRecord = %v", err)
			}
			if _, err := s.Read(tc.path); err == nil || errors.Is(err, os.ErrNotExist) {
				t.Errorf("Read = %v", err)
			}
		})
	}
	sameTree(t, "after the refused writes", tree(t, home), before)

	// An intent that names a path its package does not own is not rolled
	// forward: it stops the store, and nothing outside the store is written.
	d := stores["dispatch"]
	w := lock(t, d)
	data, err := d.encodeIntent(plan(t, d, provision(t)))
	w.Release()
	if err != nil {
		t.Fatal(err)
	}
	pool := stores["pool"]
	write(t, pool, IntentPath(req9), []byte(strings.Replace(string(data), `"package":"dispatch"`, `"package":"pool"`, 1)))
	if _, err := pool.Lock(); !errors.Is(err, ErrRecordMalformed) {
		t.Fatalf("Lock = %v; want ErrRecordMalformed", err)
	}
	if _, err := os.Stat(filepath.Join(home, homeFile)); err == nil {
		t.Error("the intent wrote home.json for a package that does not own it")
	}
}

// A transaction that writes top-level paths is interrupted at each of its
// steps. Recovery by the next lock completes it, or finds nothing to
// complete, and removes the temporary files a crash left at the top of the
// home.
func TestTopLevelTransactionsRecover(t *testing.T) {
	cases := []struct {
		pkg   string
		tx    func(t *testing.T) Tx
		paths []string
	}{
		{"dispatch", provision, []string{HomeFilePath}},
		{"ui", func(t *testing.T) Tx {
			return Tx{
				RequestID: req9,
				Records: []RecordWrite{
					{ReviewPath("task-1/1/review-1/review.json"), []byte(`{"n":1}`), 0},
					{ReviewPath("task-1/1/review-1/final.json"), []byte(`{"n":2}`), 0},
				},
				Outcome: []byte(`{"status":"ok"}`),
			}
		}, []string{ReviewPath("task-1/1/review-1/review.json"), ReviewPath("task-1/1/review-1/final.json")}},
	}
	for _, tc := range cases {
		t.Run(tc.pkg, func(t *testing.T) {
			fresh := func() (*Store, string) {
				home := newHome(t)
				s, _, _ := open(t, home, tc.pkg)
				lock(t, s).Release()
				return s, home
			}
			s, home := fresh()
			untouched := tree(t, home)
			w := lock(t, s)
			steps := trace(t, s, func() {
				if _, err := w.Commit(tc.tx(t)); err != nil {
					t.Fatal(err)
				}
			})
			w.Release()
			done := tree(t, home)
			durable := slices.Index(steps, "syncdir "+IntentPath(req9))
			for i, step := range steps {
				t.Run(step, func(t *testing.T) {
					s, home := fresh()
					w := lock(t, s)
					hit := faultAt(s, step, 1)
					_, err := w.Commit(tc.tx(t))
					w.Release()
					if !*hit || !errors.Is(err, errCrash) {
						t.Fatalf("the interruption did not happen: hit %v, err %v", *hit, err)
					}
					s.fault = nil
					for _, p := range tc.paths {
						if _, err := s.ReadRecord(p); err != nil && !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("a reader after the interruption: %v", err)
						}
					}
					lock(t, s).Release()
					want := done
					if i < durable {
						want = untouched
					}
					sameTree(t, "after recovery", tree(t, home), want)
					if left := temps(t, home); len(left) != 0 {
						t.Errorf("temporary files after recovery: %v", left)
					}
					h, err := CheckHome(home)
					if err != nil || len(h.Unknown) != 0 {
						t.Errorf("CheckHome = %+v, %v", h, err)
					}
				})
			}
		})
	}
}

// A writer empties tmp/, the one directory the store makes temporary files
// in, and removes nothing else: whatever its name, no file at the top of the
// home, in the package's own directory or below a review directory is taken
// for a temporary file.
func TestClearingTmpTouchesNothingElse(t *testing.T) {
	name := func(file, req string) string { return file + "." + req + tempSuffix }
	populate := func(t *testing.T, home string, left, kept []string) {
		t.Helper()
		for _, rel := range append(slices.Clone(left), kept...) {
			full := filepath.Join(home, filepath.FromSlash(rel))
			os.MkdirAll(filepath.Dir(full), dirMode)
			if err := os.WriteFile(full, []byte("x"), fileMode); err != nil {
				t.Fatal(err)
			}
		}
	}
	check := func(t *testing.T, home string, left, kept []string) {
		t.Helper()
		for _, rel := range left {
			if _, err := os.Lstat(filepath.Join(home, filepath.FromSlash(rel))); err == nil {
				t.Errorf("%s was left", rel)
			}
		}
		for _, rel := range kept {
			if _, err := os.Lstat(filepath.Join(home, filepath.FromSlash(rel))); err != nil {
				t.Errorf("%s was removed: %v", rel, err)
			}
		}
	}
	t.Run("dispatch", func(t *testing.T) {
		home := newHome(t)
		s, _, _ := open(t, home, "dispatch")
		left := []string{"dispatch/tmp/" + req1 + tempSuffix, "dispatch/tmp/" + req2 + tempSuffix, "dispatch/tmp/sub/x"}
		kept := []string{
			"home.json.backup.tmp", "home.json.tmp", name(homeFile, req1), "reviews/a/" + name("review.json", req1),
			"dispatch/" + name(task1, req2), "dispatch/records/item/capture.tmp", "pool/tmp/" + req1 + tempSuffix,
		}
		populate(t, home, left, kept)
		lock(t, s).Release()
		check(t, home, left, kept)
	})
	t.Run("ui", func(t *testing.T) {
		home := newHome(t)
		s, _, _ := open(t, home, "ui")
		left := []string{"ui/tmp/" + req1 + tempSuffix}
		kept := []string{
			"reviews/a/1/b/attachments/0001/capture.json." + req1 + tempSuffix, "reviews/a/1/b/" + name("review.json", req1),
			"reviews/a/1/b/round-0001.feedback.json.tmp", "reviews/a/1/b/revision-0001.html",
			"reviews/a/1/b/assets/0001/x.json.backup.tmp", "home.json." + req1 + ".tmp", "ui/" + name(task1, req2),
		}
		populate(t, home, left, kept)
		lock(t, s).Release()
		check(t, home, left, kept)
	})
}

// A write that goes to the top of the home or below reviews/ makes its
// temporary file in the package's own tmp/ and moves it into place from there;
// a crash leaves nothing at the top of the home.
func TestTopLevelWritesUseThePackageTmp(t *testing.T) {
	for _, tc := range []struct {
		pkg, rel string
		content  func(t *testing.T) []byte
	}{
		{"dispatch", HomeFilePath, homeContent},
		{"ui", ReviewPath("task-1/1/review-1/review.json"), func(*testing.T) []byte { return []byte(`{"n":1}`) }},
	} {
		t.Run(tc.pkg, func(t *testing.T) {
			home := newHome(t)
			s, _, _ := open(t, home, tc.pkg)
			before := tree(t, home)
			w := lock(t, s)
			hit := faultAt(s, "rename "+tc.rel, 1)
			if _, err := w.Replace(req1, tc.rel, tc.content(t), 0); !errors.Is(err, errCrash) || !*hit {
				t.Fatalf("Replace = %v", err)
			}
			w.Release()
			s.fault = nil
			left := temps(t, home)
			if want := []string{tc.pkg + "/tmp/" + req1 + tempSuffix}; !slices.Equal(left, want) {
				t.Fatalf("temporary files = %v, want %v", left, want)
			}
			delete(before, "locks/"+tc.pkg+".lock")
			lock(t, s).Release()
			if left := temps(t, home); len(left) != 0 {
				t.Errorf("temporary files after the lock: %v", left)
			}
			h, err := CheckHome(home)
			if err != nil || len(h.Unknown) != 0 {
				t.Errorf("CheckHome = %+v, %v", h, err)
			}
		})
	}
}
