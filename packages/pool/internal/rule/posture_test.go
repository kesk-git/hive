package rule_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
)

// The acceptance test of the Assimilation posture check (spec/pool.md, Rules,
// the paragraph on a project's Assimilation posture). The rule recorder, a later
// lane, calls the check before it records a rule on the subject
// assimilation.posture; the test holds the check to the contract: it accepts a
// word authority that stands, and refuses every other kind of authority and a
// word that does not stand, with authority_insufficient.

const (
	homeID      = "home_01jc7d8e9f0g1h2j3k4m5n6p7a"
	workspaceID = "ws_01jc7d8e9f0g1h2j3k4m5n6p7b"
	projectID   = "proj_01jc7d8e9f0g1h2j3k4m5n6p7c"
	questionID  = "q_01jc7d8e9f0g1h2j3k4m5n6p7q"
)

var scope = envelope.Scope{Level: envelope.ScopeProject, HomeID: homeID, WorkspaceID: workspaceID, ProjectID: projectID}

// pool is the store of a home holding the question fixture, or none.
func pool(t testing.TB, question string) *cstore.Store {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "home")
	// The root is made and secured first, as Hive makes a home, so that every
	// entry made in it is owner-only and owned by the user.
	if err := cstore.MakeDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := cstore.SecureDir(dir); err != nil {
		t.Fatal(err)
	}
	put := func(rel string, rec map[string]any) {
		data, err := canon.Encode(rec)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := cstore.MakeDir(filepath.Dir(path)); err != nil {
			t.Fatal(err)
		}
		f, err := cstore.CreateFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	put("home.json", map[string]any{
		"schema": "hive.home", "schema_version": "1.0", "record_revision": int64(1),
		"home_id": homeID, "role": "hive", "name": "A test home", "parent": map[string]any{"kind": "none"},
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z",
		"writer": map[string]any{"package": "pool", "command": "hive-pool", "operation": "provision", "implementation_version": "test"},
	})
	if question != "" {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contract", "testdata", "schemas", "records_pool.question", "valid", question+".json"))
		if err != nil {
			t.Fatal(err)
		}
		v, err := canon.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		put("pool/records/question/"+questionID+".json", v.(map[string]any))
	}
	st, err := cstore.Open(dir, "pool", cstore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func authority(kind, schema string, revision int64) envelope.Authority {
	if kind == envelope.AuthorityNone {
		return envelope.Authority{Kind: kind}
	}
	return envelope.Authority{Kind: kind, Ref: &envelope.RecordRef{Schema: schema, ID: questionID, HomeID: homeID, RecordRevision: revision}}
}

// reasons are the reasons the refusal names, sorted.
func reasons(t testing.TB, err error) []string {
	t.Helper()
	f := envelope.AsFailure(err)
	if f == nil || f.Status != envelope.Refused || f.Err.Code != envelope.CodeAuthorityInsufficient {
		t.Fatalf("want refused authority_insufficient; got %v", err)
	}
	out := []string{}
	for _, v := range f.Err.Detail["reasons"].([]any) {
		out = append(out, v.(string))
	}
	slices.Sort(out)
	return out
}

// A word that stands is accepted: an answered question at the revision of its
// applied answer, and a released one.
func TestThePostureCheckAcceptsAStandingWord(t *testing.T) {
	for name, tc := range map[string]struct {
		question string
		revision int64
	}{
		"an answered question": {"answered-superseding", 3},
		"a released question":  {"released", 2},
	} {
		t.Run(name, func(t *testing.T) {
			if err := rule.CheckPosture(pool(t, tc.question), scope, authority(envelope.AuthorityWord, "pool.question", tc.revision)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Every other kind of authority is refused, a posture rule among them: the
// posture is the Hive Mind's own sentence, and no rule or instruction speaks
// for it.
func TestThePostureCheckRefusesEveryOtherAuthority(t *testing.T) {
	st := pool(t, "answered-superseding")
	for _, kind := range []string{
		envelope.AuthorityNone, envelope.AuthorityPosture, envelope.AuthorityCharter, envelope.AuthorityRule, envelope.AuthorityInstruction,
	} {
		t.Run(kind, func(t *testing.T) {
			err := rule.CheckPosture(st, scope, authority(kind, "pool.question", 3))
			if got := reasons(t, err); !slices.Equal(got, []string{"not-word"}) {
				t.Errorf("reasons %v", got)
			}
		})
	}
}

// A word that does not stand is refused, with the reason the word's check gives.
func TestThePostureCheckRefusesAWordThatDoesNotStand(t *testing.T) {
	for name, tc := range map[string]struct {
		question string
		revision int64
		scope    envelope.Scope
		reasons  []string
	}{
		"an open question":                        {"open-with-options", 1, scope, []string{"unanswered"}},
		"a deferred question":                     {"deferred-by-later", 2, scope, []string{"deferred"}},
		"a moot question":                         {"moot-question-replaced", 2, scope, []string{"moot"}},
		"an answer superseded since the revision": {"answered-superseding", 2, scope, []string{"superseded"}},
		"a question that does not exist":          {"", 1, scope, []string{"not-found"}},
		"a question of another project": {"answered-superseding", 3, envelope.Scope{
			Level: envelope.ScopeProject, HomeID: homeID, WorkspaceID: workspaceID, ProjectID: "proj_01jc7d8e9f0g1h2j3k4m5n6p7z"}, []string{"out-of-scope"}},
	} {
		t.Run(name, func(t *testing.T) {
			err := rule.CheckPosture(pool(t, tc.question), tc.scope, authority(envelope.AuthorityWord, "pool.question", tc.revision))
			if got := reasons(t, err); !slices.Equal(got, tc.reasons) {
				t.Errorf("reasons %v, want %v", got, tc.reasons)
			}
		})
	}
}
