package rule_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
)

// The acceptance of hive-rule resolve and of the Pool's read of rules and
// subjects (spec/pool.md, Rules and Subjects, Reading the Pool). Rules are
// seeded into the store as the rule recorder, a later lane, would leave them.

func same(t testing.TB, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s: got %v, want %v", what, got, want)
	}
}

func hasDigest(body map[string]any) bool {
	_, ok := body["digest"]
	return ok
}

// A rule of the Hive, the workspace, the project and the task generation are
// collected for the scope that names them, the narrower governing a subject
// the wider also governs, in order from the widest scope to the narrowest and
// then by effective time.
func TestTheNarrowerScopeGoverns(t *testing.T) {
	h := newHome(t)
	task := mint(t, "task")
	hiveModel := h.rule("dispatch.model", "hive", nil)
	wsModel := h.rule("dispatch.model", "workspace", nil)
	projectModel := h.rule("dispatch.model", "project", nil)
	taskModel := h.rule("dispatch.model", "project", R{"task_id": task, "generation": int64(1)})
	effort := h.rule("dispatch.effort", "hive", R{"effective_at": d05})
	gate := h.rule("gate.limit", "workspace", R{"effective_at": d05})
	stow := h.rule("stow.mode", "project", R{"effective_at": d05})
	// A rule of another project is no rule of this scope.
	other := h.at(nil)
	other.project = h.register("projects", mint(t, "proj"), map[string]any{"schema": "hive.project", "workspace_id": h.workspace, "repository": map[string]any{"kind": "local-only"}}, "project_id")
	other.rule("dispatch.model", "project", nil)

	p := h.inProject()
	body := p.resolved("class-3", "", map[string]any{"task_id": task, "generation": int64(1)})
	same(t, "project and task", applied(body), []string{effort, gate, stow, taskModel})
	if got := excluded(body); len(got) != 3 || got[hiveModel] != "overridden" || got[wsModel] != "overridden" || got[projectModel] != "overridden" {
		t.Errorf("excluded %v", got)
	}
	if len(conflicts(body)) != 0 || !hasDigest(body) {
		t.Errorf("conflicts %v, digest %v", conflicts(body), hasDigest(body))
	}

	same(t, "project", applied(p.resolved("class-3", "", nil)), []string{effort, gate, projectModel, stow})
	same(t, "another generation", applied(p.resolved("class-3", "", map[string]any{"task_id": task, "generation": int64(2)})), []string{effort, gate, projectModel, stow})
	same(t, "workspace", applied(h.inWorkspace().resolved("class-3", "", nil)), []string{effort, wsModel, gate})
	same(t, "hive", applied(h.resolved("class-3", "", nil)), []string{hiveModel, effort})
}

// A superseding rule retires the one it supersedes only while it is in force:
// not once it has expired, been lifted, ended, or before it begins.
func TestASupersedingRuleRetiresOnlyWhileInForce(t *testing.T) {
	for _, tc := range []struct {
		name, at string
		over     R
		reason   string
	}{
		{"in force", d15, nil, ""},
		{"expired", d25, nil, "expired-by-clock"},
		{"not yet begun", d05, nil, "not-yet-effective"},
		{"lifted", d15, R{"lift": liftCondition(), "amendments": []any{lift(1, receipt(liftKind, liftSubject))}}, "lifted"},
		{"ended", d15, R{"status": "ended"}, "ended"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHome(t)
			standing := h.rule("dispatch.model", "hive", nil)
			over := R{"effective_at": d10, "expires_at": d20}
			for k, v := range changes(standing, "supersedes") {
				over[k] = v
			}
			for k, v := range tc.over {
				over[k] = v
			}
			temporary := h.rule("dispatch.model", "hive", over)
			body := h.resolved("class-3", tc.at, nil)
			if tc.reason == "" {
				same(t, "rules", applied(body), []string{temporary})
				if got := excluded(body); len(got) != 1 || got[standing] != "superseded" {
					t.Errorf("excluded %v", got)
				}
				return
			}
			same(t, "rules", applied(body), []string{standing})
			if got := excluded(body); len(got) != 1 || got[temporary] != tc.reason {
				t.Errorf("excluded %v, want %s", got, tc.reason)
			}
		})
	}
}

// A rule that qualifies another is in force beside it, across scopes and at one
// scope, and is no conflict.
func TestAQualifyingRuleStaysBesideTheRuleItQualifies(t *testing.T) {
	h := newHome(t)
	ws := h.rule("dispatch.model", "workspace", nil)
	project := h.rule("dispatch.model", "project", changes(ws, "qualifies"))
	body := h.inProject().resolved("class-3", "", nil)
	same(t, "across scopes", applied(body), []string{ws, project})
	if len(excluded(body)) != 0 || len(conflicts(body)) != 0 || !hasDigest(body) {
		t.Errorf("excluded %v, conflicts %v, digest %v", excluded(body), conflicts(body), hasDigest(body))
	}

	h = newHome(t)
	first := h.rule("dispatch.model", "hive", nil)
	qualifying := changes(first, "qualifies")
	qualifying["effective_at"] = d05
	second := h.rule("dispatch.model", "hive", qualifying)
	body = h.resolved("class-3", "", nil)
	same(t, "at one scope", applied(body), []string{first, second})
	if len(conflicts(body)) != 0 || !hasDigest(body) {
		t.Errorf("conflicts %v, digest %v", conflicts(body), hasDigest(body))
	}
}

// Two rules of one subject at one scope that do not name each other are a
// conflict, both are still returned, and no digest is given.
func TestTwoRulesOfOneSubjectAtOneScopeConflict(t *testing.T) {
	h := newHome(t)
	a := h.rule("dispatch.model", "hive", nil)
	b := h.rule("dispatch.model", "hive", nil)
	h.rule("dispatch.effort", "hive", nil)
	body := h.resolved("class-3", "", nil)
	list := conflicts(body)
	if len(list) != 1 {
		t.Fatalf("conflicts %v", list)
	}
	c := list[0].(map[string]any)
	want := []any{a, b}
	if strings.Compare(a, b) > 0 {
		want = []any{b, a}
	}
	if c["subject"] != "dispatch.model" || c["scope_level"] != "hive" || !slices.Equal(c["rules"].([]any), want) {
		t.Errorf("conflict %v", c)
	}
	if len(applied(body)) != 3 || hasDigest(body) {
		t.Errorf("rules %v, digest %v", applied(body), hasDigest(body))
	}
	// Naming the other in changes is no conflict.
	h = newHome(t)
	standing := h.rule("dispatch.model", "hive", nil)
	h.rule("dispatch.model", "hive", changes(standing, "supersedes"))
	if body = h.resolved("class-3", "", nil); len(conflicts(body)) != 0 || !hasDigest(body) {
		t.Errorf("conflicts %v, digest %v", conflicts(body), hasDigest(body))
	}
}

// A rule is excluded by the clock before any expiry receipt exists, the latest
// amendment's new expiry is read in place of the original, and a lift is
// honoured only by its recorded receipt.
func TestTheClockExcludesAndALiftNeedsItsReceipt(t *testing.T) {
	h := newHome(t)
	expiring := h.rule("a.subject", "hive", R{"expires_at": d20})
	extended := h.rule("b.subject", "hive", R{"expires_at": d20, "amendments": []any{
		amendment(1, "extend", map[string]any{"new_expires_at": d25}),
		amendment(2, "extend", map[string]any{"new_expires_at": d30}),
		amendment(3, "note", nil),
	}})
	ended := h.rule("c.subject", "hive", R{"expires_at": d30, "amendments": []any{amendment(1, "end", map[string]any{"new_expires_at": d12})}})
	for _, tc := range []struct {
		at   string
		live []string
	}{
		{d10, []string{expiring, extended, ended}},
		{d12, []string{expiring, extended}},
		{d15, []string{expiring, extended}},
		{d20, []string{extended}},
		{d25, []string{extended}},
		{d30, nil},
	} {
		body := h.resolved("class-3", tc.at, nil)
		got, want := applied(body), slices.Clone(tc.live)
		slices.Sort(got)
		slices.Sort(want)
		same(t, tc.at+" rules", got, want)
		for _, id := range []string{expiring, extended, ended} {
			if !slices.Contains(tc.live, id) && excluded(body)[id] != "expired-by-clock" {
				t.Errorf("%s: %s excluded as %q", tc.at, id, excluded(body)[id])
			}
		}
	}

	// A lift is honoured by the receipt of the kind, about the subject, that was
	// recorded with it, and by nothing else.
	h = newHome(t)
	rules := map[string]string{
		"met":           h.rule("a.subject", "hive", R{"lift": liftCondition(), "amendments": []any{lift(1, receipt(liftKind, liftSubject))}}),
		"no receipt":    h.rule("b.subject", "hive", R{"lift": liftCondition(), "amendments": []any{lift(1, nil)}}),
		"wrong kind":    h.rule("c.subject", "hive", R{"lift": liftCondition(), "amendments": []any{lift(1, receipt("dispatch.project.retired", liftSubject))}}),
		"wrong subject": h.rule("d.subject", "hive", R{"lift": liftCondition(), "amendments": []any{lift(1, receipt(liftKind, "proj_01j9xk3m2nq4r5s6t7v8w9xyzz"))}}),
		"not met":       h.rule("e.subject", "hive", R{"lift": liftCondition()}),
	}
	body := h.resolved("class-3", "", nil)
	if got := excluded(body); len(got) != 1 || got[rules["met"]] != "lifted" {
		t.Errorf("excluded %v", got)
	}
	if len(applied(body)) != 4 {
		t.Errorf("rules %v", applied(body))
	}
}

// A lift condition that names a record revision or a digest is met only by a
// receipt whose subject carries each one it names.
func TestALiftNamingARevisionOrDigestNeedsThatState(t *testing.T) {
	h := newHome(t)
	const digest = "sha256:0000000000000000000000000000000000000000000000000000000000000001"
	at := func(revision int64, digest string) map[string]any {
		r := receipt(liftKind, liftSubject)
		subject := r["subject"].(map[string]any)
		subject["record_revision"] = revision
		if digest != "" {
			subject["digest"] = digest
		}
		return r
	}
	condition := func(over map[string]any) map[string]any {
		c := liftCondition()
		for k, v := range over {
			c["subject"].(map[string]any)[k] = v
		}
		return c
	}
	named := condition(map[string]any{"record_revision": int64(2), "digest": digest})
	rules := map[string]string{
		"state":          h.rule("a.subject", "hive", R{"lift": named, "amendments": []any{lift(1, at(2, digest))}}),
		"other revision": h.rule("b.subject", "hive", R{"lift": named, "amendments": []any{lift(1, at(1, digest))}}),
		"other digest":   h.rule("c.subject", "hive", R{"lift": named, "amendments": []any{lift(1, at(2, digest[:len(digest)-1]+"2"))}}),
		"no digest":      h.rule("d.subject", "hive", R{"lift": named, "amendments": []any{lift(1, at(2, ""))}}),
		"revision only":  h.rule("e.subject", "hive", R{"lift": condition(map[string]any{"record_revision": int64(2)}), "amendments": []any{lift(1, at(2, digest))}}),
		"unnamed":        h.rule("f.subject", "hive", R{"lift": liftCondition(), "amendments": []any{lift(1, at(7, digest))}}),
	}
	got := excluded(h.resolved("class-3", "", nil))
	want := map[string]string{rules["state"]: "lifted", rules["revision only"]: "lifted", rules["unnamed"]: "lifted"}
	if len(got) != len(want) {
		t.Fatalf("excluded %v, want %v", got, want)
	}
	for id, reason := range want {
		if got[id] != reason {
			t.Errorf("%s excluded as %q", id, got[id])
		}
	}
}

// Whether a rule has expired at the instant is read from its effective expiry
// alone: a rule whose expiry was noticed is still in force before it.
func TestANoticedExpiryDoesNotReachBackBeforeTheExpiryTime(t *testing.T) {
	h := newHome(t)
	noticed := h.rule("a.subject", "hive", R{"status": "expired", "expires_at": d20, "expiry": map[string]any{
		"receipt_id": "rcpt_01j9xk3m2nq4r5s6t7v8w9xyzh", "recorded_at": d20, "noticed_at": d20,
	}})
	same(t, "before", applied(h.resolved("class-3", d15, nil)), []string{noticed})
	body := h.resolved("class-3", d20, nil)
	if len(applied(body)) != 0 || excluded(body)[noticed] != "expired-by-clock" {
		t.Errorf("rules %v, excluded %v", applied(body), excluded(body))
	}
}

// A rule that does not apply to the role is left out; one that names no role
// applies to every role.
func TestARuleOfAnotherRoleIsLeftOut(t *testing.T) {
	h := newHome(t)
	every := h.rule("a.subject", "hive", nil)
	classThree := h.rule("b.subject", "hive", R{"applies": []any{"class-3", "reviewer"}})
	// Both rules are of the Hive layer with one effective time, so resolve
	// orders them by rule identity, not by the order they were made in.
	ids := []string{every, classThree}
	slices.Sort(ids)
	same(t, "class-3", applied(h.resolved("class-3", "", nil)), ids)
	body := h.resolved("class-4", "", nil)
	same(t, "class-4", applied(body), []string{every})
	if got := excluded(body); len(got) != 1 || got[classThree] != "role" {
		t.Errorf("excluded %v", got)
	}
}

// The resolved-rule digest is the digest of the canonical form of the rules
// array, equal across runs for the same rules and different when one rule
// changes; resolving writes nothing.
func TestTheResolvedRuleDigest(t *testing.T) {
	h := newHome(t)
	id := h.rule("a.subject", "hive", nil)
	h.rule("b.subject", "hive", R{"effective_at": d05})
	var first map[string]any
	snapshot := func() map[string]string {
		out := map[string]string{}
		filepath.WalkDir(filepath.Join(h.dir, "pool"), func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && !strings.Contains(filepath.ToSlash(path), "/pool/requests/") {
				data, _ := os.ReadFile(path)
				out[path] = string(data)
			}
			return nil
		})
		return out
	}
	before := snapshot()
	first = h.resolved("class-3", d15, nil)
	again := h.resolved("class-3", d15, nil)
	if !hasDigest(first) || first["digest"] != again["digest"] {
		t.Errorf("digests %v and %v", first["digest"], again["digest"])
	}
	if after := snapshot(); len(after) != len(before) {
		t.Errorf("resolving changed the Pool: %d files, then %d", len(before), len(after))
	} else {
		for k, v := range before {
			if after[k] != v {
				t.Errorf("resolving changed %s", k)
			}
		}
	}
	array, err := canon.Encode(first["rules"])
	if err != nil {
		t.Fatal(err)
	}
	if first["digest"] != canon.FileDigest(array) {
		t.Errorf("the digest is not that of the canonical rules array")
	}
	for _, line := range first["rules"].([]any) {
		if r := line.(map[string]any); r["rule_id"] == id && !strings.HasPrefix(r["line"].(string), "Line of "+id+" [") {
			t.Errorf("line %q carries no source", r["line"])
		}
	}

	// One rule changes: its line is reworded in a revision of the record.
	rec := h.stored("rule", id)
	rec["rule_line"], rec["record_revision"] = "Another line.", int64(2)
	h.plant("rule", id, rec)
	if changed := h.resolved("class-3", d15, nil); changed["digest"] == first["digest"] {
		t.Error("the digest did not change with the rule")
	}
}

func (h *home) stored(kind, id string) map[string]any {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, "pool", "records", kind, id+".json"))
	if err != nil {
		h.t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		h.t.Fatal(err)
	}
	return v.(map[string]any)
}

// A request that names no instant resolves for the clock's.
func TestAnInstantNotGivenIsTheClocks(t *testing.T) {
	h := newHome(t)
	h.rule("a.subject", "hive", R{"expires_at": d15})
	body := h.resolved("class-3", "", nil)
	if body["at"] != "2026-10-14T09:00:00.000Z" || len(applied(body)) != 1 {
		t.Errorf("at %v, rules %v", body["at"], applied(body))
	}
	h.clk.Advance(48 * time.Hour)
	if body = h.resolved("class-3", "", nil); len(applied(body)) != 0 {
		t.Errorf("rules %v after the expiry", applied(body))
	}
}

// The Pool's read returns rules and subjects by identity and by query, with
// due_before among the selectors for rules.
func TestTheReadReturnsRulesAndSubjects(t *testing.T) {
	h := newHome(t)
	read := func(kind string, selector map[string]any) []string {
		t.Helper()
		body := h.ok(h.send("hive-pool", "read", map[string]any{"kind": kind, "selector": selector}))
		out := []string{}
		for _, r := range body["records"].([]any) {
			m := r.(map[string]any)
			if id, ok := m["rule_id"].(string); ok {
				out = append(out, id)
			} else {
				out = append(out, m["subject_id"].(string))
			}
		}
		slices.Sort(out)
		return out
	}
	sorted := func(ids ...string) []string { slices.Sort(ids); return ids }

	hive := h.rule("a.subject", "hive", nil)
	project := h.rule("b.subject", "project", nil)
	lifted := h.rule("c.subject", "hive", R{"status": "lifted"})
	same(t, "by identity", read("rule", map[string]any{"id": hive}), []string{hive})
	same(t, "all", read("rule", map[string]any{}), sorted(hive, project, lifted))
	same(t, "by project", read("rule", map[string]any{"project_id": h.project}), []string{project})
	same(t, "by states", read("rule", map[string]any{"states": []any{"lifted", "ended"}}), []string{lifted})
	if resp := h.send("hive-pool", "read", map[string]any{"kind": "rule", "selector": map[string]any{"id": "rule_01000000000000000000000000"}}); resp.Status != envelope.NotFound {
		t.Errorf("an absent rule: %s", resp.Status)
	}

	// Due at or before an instant, by the expiry the clock reads.
	h = newHome(t)
	a := h.rule("a.subject", "hive", R{"expires_at": d20})
	b := h.rule("b.subject", "hive", R{"expires_at": d20, "amendments": []any{amendment(1, "extend", map[string]any{"new_expires_at": d31})}})
	d := h.rule("d.subject", "hive", R{"expires_at": d30})
	h.rule("e.subject", "hive", nil)
	same(t, "due d15", read("rule", map[string]any{"due_before": d15}), []string{})
	same(t, "due d25", read("rule", map[string]any{"due_before": d25}), []string{a})
	same(t, "due d30", read("rule", map[string]any{"due_before": d30}), sorted(a, d))
	same(t, "due d31", read("rule", map[string]any{"due_before": d31}), sorted(a, b, d))

	// Subjects by identity and by query.
	h = newHome(t)
	other := h.subject(map[string]any{"scope_level": "project", "workspace_id": h.workspace, "project_id": h.project, "key": "class.4"})
	same(t, "subject by identity", read("subject", map[string]any{"id": h.filed}), []string{h.filed})
	same(t, "subjects", read("subject", map[string]any{}), sorted(h.filed, other))
	same(t, "subjects by project", read("subject", map[string]any{"project_id": h.project}), []string{other})
}
