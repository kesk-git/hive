package decision_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/decision"
)

// The acceptance test of the Pool's check of an authority reference of kind
// word (spec/word.md, The word stands): the reference's schema and record
// revision, a question of this home standing on an applied answer, the applied
// answer recorded at a revision no greater than the reference's, and the
// question's scope containing the envelope's, a failure giving the reason the
// contract names.

// stands asks the check of the home's Pool, in the scope given, whether the
// word stands on the question at the record revision.
func (h *home) stands(scope envelope.Scope, question string, revision int64) decision.Standing {
	h.t.Helper()
	return h.standing(scope, envelope.Authority{Kind: envelope.AuthorityWord, Ref: &envelope.RecordRef{
		Schema: "pool.question", ID: question, HomeID: h.homeID, RecordRevision: revision,
	}})
}

func (h *home) standing(scope envelope.Scope, auth envelope.Authority) decision.Standing {
	h.t.Helper()
	st, err := cstore.Open(h.dir, "pool", cstore.Options{})
	if err != nil {
		h.t.Fatal(err)
	}
	got, err := decision.WordStands(st, scope, auth)
	if err != nil {
		h.t.Fatal(err)
	}
	return got
}

func (h *home) hiveScope() envelope.Scope {
	return envelope.Scope{Level: envelope.ScopeHive, HomeID: h.homeID}
}

// is checks the reasons a verdict gives, and that it stands exactly when it
// gives none.
func is(t testing.TB, got decision.Standing, reasons ...string) {
	t.Helper()
	if !slices.Equal(got.Reasons, reasons) || got.Stands() != (len(reasons) == 0) {
		t.Errorf("the word stands %v with reasons %v, want %v", got.Stands(), got.Reasons, reasons)
	}
}

// The word stands only on the answer applied at the revision the reference
// names: an answer first applied after it is unanswered, one that superseded
// it since is superseded, and a disposition does not change which answer
// stands.
func TestTheWordStandsOnlyOnTheAnswerAppliedAtTheRevisionNamed(t *testing.T) {
	h := newHome(t)
	scope := h.hiveScope()
	r := h.fresh()

	is(t, h.stands(scope, r.question, 1), "unanswered")

	h.ok(h.operator(r.question, keep)) // record revision 2
	is(t, h.stands(scope, r.question, 2))
	is(t, h.stands(scope, r.question, 1), "unanswered")
	// A reference to a revision the question has not reached is not one.
	is(t, h.stands(scope, r.question, 3), "malformed")

	h.ok(h.operator(r.question, map[string]any{"text": "Instead.", "supersedes": int64(1)})) // revision 3
	is(t, h.stands(scope, r.question, 3))
	is(t, h.stands(scope, r.question, 2), "superseded")
	is(t, h.stands(scope, r.question, 1), "unanswered")

	h.ok(h.release(r.question)) // revision 4: released, the answer still the one applied
	is(t, h.stands(scope, r.question, 4))
	is(t, h.stands(scope, r.question, 3))
	is(t, h.stands(scope, r.question, 2), "superseded")

	t.Run("completed", func(t *testing.T) {
		item := h.item()
		done := h.answeredOn(item)
		h.archive(item)
		h.ok(h.complete(done.question))
		is(t, h.stands(scope, done.question, 3))
		is(t, h.stands(scope, done.question, 1), "unanswered")
	})
}

// A question that is open, deferred or moot never stands, whatever revision
// the reference names, and a hold released by a close is the moot question's.
func TestTheWordDoesNotStandOnAnOpenADeferredOrAMootQuestion(t *testing.T) {
	h := newHome(t)
	scope := h.hiveScope()

	open := h.fresh()
	is(t, h.stands(scope, open.question, 1), "unanswered")

	deferred := h.fresh()
	h.ok(h.operator(deferred.question, laterAt(later)))
	is(t, h.stands(scope, deferred.question, 2), "deferred")
	is(t, h.stands(scope, deferred.question, 1), "deferred")

	moot := h.fresh()
	h.archive(moot.item)
	h.ok(h.closeOn(moot.question, archivedEvidence(moot.item)))
	is(t, h.stands(scope, moot.question, 2), "moot")
	is(t, h.stands(scope, moot.question, 1), "moot")
	if h.stored("hold", moot.hold)["state"] != "released" {
		t.Fatalf("the close left its hold")
	}
}

// The question's scope contains the envelope's: a Hive question stands for any
// scope of its home, a workspace question for that workspace and its projects,
// a project question for that project.
func TestTheWordStandsOnlyWithinTheQuestionsScope(t *testing.T) {
	h := newHome(t)
	ws1, ws2 := h.workspace(), h.workspace()
	p1, p2, other := h.project(ws1), h.project(ws1), h.project(ws2)
	hive := envelope.Scope{Level: envelope.ScopeHive, HomeID: h.homeID}
	workspace := func(id string) envelope.Scope {
		return envelope.Scope{Level: envelope.ScopeWorkspace, HomeID: h.homeID, WorkspaceID: id}
	}
	project := func(w, p string) envelope.Scope {
		return envelope.Scope{Level: envelope.ScopeProject, HomeID: h.homeID, WorkspaceID: w, ProjectID: p}
	}
	answeredIn := func(at *home) string {
		r := at.answeredOn(at.item())
		return r.question
	}

	hiveQuestion := answeredIn(h)
	for name, scope := range map[string]envelope.Scope{
		"hive": hive, "a workspace": workspace(ws1), "a project": project(ws1, p1),
	} {
		t.Run("a Hive question for "+name, func(t *testing.T) { is(t, h.stands(scope, hiveQuestion, 2)) })
	}

	workspaceQuestion := answeredIn(h.inWorkspace(ws1))
	t.Run("a workspace question for its workspace", func(t *testing.T) { is(t, h.stands(workspace(ws1), workspaceQuestion, 2)) })
	t.Run("a workspace question for its project", func(t *testing.T) { is(t, h.stands(project(ws1, p1), workspaceQuestion, 2)) })
	t.Run("a workspace question for the Hive", func(t *testing.T) { is(t, h.stands(hive, workspaceQuestion, 2), "out-of-scope") })
	t.Run("a workspace question for another workspace", func(t *testing.T) {
		is(t, h.stands(workspace(ws2), workspaceQuestion, 2), "out-of-scope")
		is(t, h.stands(project(ws2, other), workspaceQuestion, 2), "out-of-scope")
	})

	projectQuestion := answeredIn(h.inProject(ws1, p1))
	t.Run("a project question for its project", func(t *testing.T) { is(t, h.stands(project(ws1, p1), projectQuestion, 2)) })
	t.Run("a project question for another project, its workspace or the Hive", func(t *testing.T) {
		is(t, h.stands(project(ws1, p2), projectQuestion, 2), "out-of-scope")
		is(t, h.stands(workspace(ws1), projectQuestion, 2), "out-of-scope")
		is(t, h.stands(hive, projectQuestion, 2), "out-of-scope")
	})

	t.Run("a scope reason beside the state's", func(t *testing.T) {
		unanswered := h.inProject(ws1, p1).fresh().question
		is(t, h.stands(project(ws1, p2), unanswered, 1), "unanswered", "out-of-scope")
	})
}

// A reference that names no question of this home, or is not a record
// reference of a question at a revision, or is not of kind word, never stands.
func TestTheWordDoesNotStandOnAnUnknownOrAMalformedReference(t *testing.T) {
	h := newHome(t)
	scope := h.hiveScope()
	r := h.answeredOn(h.item())
	ref := func(f func(*envelope.RecordRef)) envelope.Authority {
		v := &envelope.RecordRef{Schema: "pool.question", ID: r.question, HomeID: h.homeID, RecordRevision: 2}
		f(v)
		return envelope.Authority{Kind: envelope.AuthorityWord, Ref: v}
	}

	for name, tc := range map[string]struct {
		auth    envelope.Authority
		reasons []string
	}{
		"a question that does not exist": {ref(func(v *envelope.RecordRef) { v.ID = mint(t, ids.Question) }), []string{"not-found"}},
		"a question of another home":     {ref(func(v *envelope.RecordRef) { v.HomeID = mint(t, ids.Home) }), []string{"not-found"}},
		"another schema":                 {ref(func(v *envelope.RecordRef) { v.Schema = "pool.item" }), []string{"malformed"}},
		"no identity":                    {ref(func(v *envelope.RecordRef) { v.ID = "" }), []string{"malformed"}},
		"no revision":                    {ref(func(v *envelope.RecordRef) { v.RecordRevision = 0 }), []string{"malformed"}},
		"a revision beyond the question": {ref(func(v *envelope.RecordRef) { v.RecordRevision = 9 }), []string{"malformed"}},
		"no reference":                   {envelope.Authority{Kind: envelope.AuthorityWord}, []string{"malformed"}},
		"another kind of authority": {envelope.Authority{Kind: envelope.AuthorityRule, Ref: &envelope.RecordRef{
			Schema: "pool.question", ID: r.question, HomeID: h.homeID, RecordRevision: 2}}, []string{"not-word"}},
	} {
		t.Run(name, func(t *testing.T) { is(t, h.standing(scope, tc.auth), tc.reasons...) })
	}

	t.Run("a question record that cannot be read", func(t *testing.T) {
		id := mint(t, ids.Question)
		path := filepath.Join(h.dir, "pool", "records", "question", id+".json")
		h.put(path, []byte("not a record\n"))
		is(t, h.stands(scope, id, 1), "malformed")
	})
}
