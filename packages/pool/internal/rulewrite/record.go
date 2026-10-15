package rulewrite

import (
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/pool/internal/memory"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// postureSubject is the conflict key of a project's Assimilation posture.
const postureSubject = "assimilation.posture"

// The step kinds a subject's loads_at may name; project is the one a subject
// outside project scope cannot have.
var steps = []string{"dispatch", "gate", "define", "stow", "project"}

// Record is hive-rule record (spec/pool.md, Rules, What a write refuses): it
// appends a rule and writes pool.rule.recorded, the words and dates given never
// changed afterwards. It refuses a line over 300 code points, a second rule of a
// kind on a subject that already has one of that kind in force at the scope,
// whole of its task generation, unless the request names that rule in changes,
// and a rule after which a projection would exceed its budget at the clock or at
// any later instant at which the live set changes. A
// subject created beside an operating rule, and the facts filed under its
// subject, commit with the rule or not at all.
func Record(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	if err := checkLevel(c); err != nil {
		return nil, err
	}
	// The posture check runs inside the transaction, under the store's lock,
	// before any record is read, so that the word it relies on cannot be
	// superseded between the check and the write.
	if str(body["subject"]) == postureSubject {
		if err := rule.CheckPosture(c.Store, c.Request.Scope, c.Request.Authority); err != nil {
			return nil, err
		}
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	if sc := c.Request.Scope; (sc.TaskID == "") != (sc.Generation == 0) {
		return nil, invalid("a rule is narrowed to a task generation by the request scope's task_id and generation, given together")
	}
	for _, f := range []string{"effective_at", "expires_at"} {
		if at := str(body[f]); at != "" {
			if _, err := clock.Parse(at); err != nil {
				return nil, invalid(f + " is not a calendar instant")
			}
		}
	}
	kind := str(body["kind"])
	if line := str(body["rule_line"]); kind == rule.KindOperating {
		if n := utf8.RuneCountInString(line); n > MaxRuleLine {
			return nil, refuse(CodeLineTooLong, "the rule line is "+strconv.Itoa(n)+" characters, over the cap of 300; the words are no part of it; the recorder's three moves are to shorten the line to one sentence and file its mechanics as a fact of its subject in the same request, to file the rule into a new, narrower subject created in the same request with its loads_at, or to supersede a rule in force with it").
				With("characters", int64(n)).With("max_characters", int64(MaxRuleLine)).With("moves", slices.Clone(moves))
		}
	}
	snap, err := memory.Load(c.Store, c.Home)
	if err != nil {
		return nil, err
	}
	now := c.Clock.Now()
	stamp, err := c.Stamp()
	if err != nil {
		return nil, internal("the time of the rule cannot be written", err)
	}
	id, err := ids.Mint(c.Clock, ids.Rule)
	if err != nil {
		return nil, internal("a rule cannot be given an identity", err)
	}
	r := &rule.Rule{
		RecordRevision: 1, RuleID: id, Scope: ruleScope(c.Request.Scope), TaskID: c.Request.Scope.TaskID, Generation: c.Request.Scope.Generation, Subject: str(body["subject"]), Words: str(body["words"]),
		Origin: copyValue(body["origin"]).(map[string]any), Applies: texts(body["applies"]), Status: rule.StatusInForce,
		EffectiveAt: stamp, ExpiresAt: str(body["expires_at"]), Authority: tree(c.Request.Tree()["authority"]),
		Amendments: []rule.Amendment{}, CreatedAt: stamp, UpdatedAt: stamp, Kind: kind,
	}
	if at := str(body["effective_at"]); at != "" {
		r.EffectiveAt = at
	}
	if l := tree(body["lift"]); l != nil {
		r.Lift = &rule.Lift{ReceiptKind: str(l["receipt_kind"]), Subject: copyValue(l["subject"]).(map[string]any)}
	}
	if ch := tree(body["changes"]); ch != nil {
		r.Changes = &rule.Changes{RuleID: str(ch["rule_id"]), RecordRevision: whole(ch["record_revision"]), Relation: str(ch["relation"])}
	}
	if in := tree(body["intake"]); in != nil {
		r.Intake = copyValue(in).(map[string]any)
	}
	for _, l := range list(body["links"]) {
		r.Links = append(r.Links, copyValue(l).(map[string]any))
	}
	if r.Changes != nil {
		if err := checkChanges(snap, r); err != nil {
			return nil, err
		}
	}

	var subjects []*rule.Subject
	switch kind {
	case rule.KindOperating:
		r.RuleLine, r.Tier, r.FiledUnder = str(body["rule_line"]), str(body["tier"]), str(body["filed_under"])
		if subjects, err = fileSubject(c, snap, r, stamp); err != nil {
			return nil, err
		}
	default:
		r.Governs = copyValue(body["governs"]).(map[string]any)
		r.Specifications = []map[string]any{}
		if err := checkGoverned(c, r); err != nil {
			return nil, err
		}
	}

	write := memory.Write{Rules: []*rule.Rule{r}, Subjects: subjects}
	if err := snap.RefuseConflicts(now, write); err != nil {
		return nil, err
	}
	if err := checkProductConflict(snap.Rules, now, r); err != nil {
		return nil, err
	}
	if _, err := snap.Plan(now, write); err != nil {
		return nil, err
	}

	b := &builder{c: c}
	for _, s := range subjects {
		expected := s.RecordRevision - 1
		if _, _, err := b.put("subject", s.SubjectID, s.Tree(), expected); err != nil {
			return nil, err
		}
	}
	final, ref, err := b.put("rule", id, r.Tree(), 0)
	if err != nil {
		return nil, err
	}
	receipt, err := b.receipt(receiptRecorded, nil)
	if err != nil {
		return nil, err
	}
	if err := b.seal(receipt, ref, scopeOfRule(r), true); err != nil {
		return nil, err
	}
	return b.stageRendered(snap, write, map[string]any{"rule": final})
}

// stageRendered adds to the transaction the re-rendering of the projections the
// write affects, in the same transaction, then stages it.
func (b *builder) stageRendered(snap *memory.Snapshot, w memory.Write, body map[string]any) (*cli.Result, error) {
	tx, err := snap.Rerender(b.c, w, b.tx)
	if err != nil {
		return nil, err
	}
	b.tx = tx
	return b.stage(body)
}

// stage checks the transaction and hands it to the harness.
func (b *builder) stage(body map[string]any) (*cli.Result, error) {
	staged, err := poolstore.Stage(b.c, b.tx)
	if err != nil {
		return nil, err
	}
	return staged.Result(body), nil
}

// fileSubject finds the subject the operating rule is filed under, or makes the
// one the request creates beside it, and files the request's facts under it. It
// returns the subjects as they will be, to be written: the created subject, or
// the existing one with the facts it gains.
func fileSubject(c *cli.Call, snap *memory.Snapshot, r *rule.Rule, stamp string) ([]*rule.Subject, error) {
	body := c.Request.Body
	var sub, created *rule.Subject
	if cs := tree(body["create_subject"]); cs != nil {
		if r.FiledUnder != str(cs["subject_id"]) {
			return nil, refuse(CodeSubjectInvalid, "filed_under is not the subject the request creates")
		}
		loads := texts(cs["loads_at"])
		if err := checkLoadsAt(loads, r.Scope); err != nil {
			return nil, err
		}
		for _, s := range snap.Subjects {
			if s.SubjectID == r.FiledUnder {
				return nil, refuse(CodeSubjectInvalid, "a subject already has the identity the request creates")
			}
			if s.Scope == r.Scope && s.Key == str(cs["key"]) {
				return nil, refuse(CodeSubjectInvalid, "a subject already has the key the request creates at this scope")
			}
		}
		created = &rule.Subject{
			RecordRevision: 1, SubjectID: r.FiledUnder, Scope: r.Scope, Key: str(cs["key"]), LoadsAt: loads,
			Facts: []rule.Fact{}, CreatedAt: stamp, UpdatedAt: stamp,
		}
		sub = created
	} else {
		for _, s := range snap.Subjects {
			if s.SubjectID == r.FiledUnder {
				// A copy: the snapshot is what the planner measures against.
				cp := *s
				cp.Facts = slices.Clone(s.Facts)
				cp.RecordRevision++
				sub = &cp
			}
		}
		if sub == nil || sub.Scope != r.Scope {
			return nil, refuse(CodeSubjectInvalid, "an operating rule is filed under an existing subject at exactly its own scope")
		}
	}
	added := false
	for _, v := range list(body["facts"]) {
		f := tree(v)
		fact := rule.Fact{FactID: str(f["fact_id"]), Text: str(f["text"]), Provenance: copyValue(f["provenance"]).(map[string]any)}
		if i := slices.IndexFunc(sub.Facts, func(e rule.Fact) bool { return e.FactID == fact.FactID }); i >= 0 {
			if sub.Facts[i].Text != fact.Text {
				return nil, refuse(CodeFactConflict, "a fact of the subject has that identity and different text").With("fact_id", fact.FactID)
			}
			continue
		}
		sub.Facts = append(sub.Facts, fact)
		added = true
	}
	if created == nil && !added {
		return nil, nil
	}
	return []*rule.Subject{sub}, nil
}

// checkLoadsAt refuses a loads_at that repeats a step, and the step project on a
// subject outside project scope.
func checkLoadsAt(loads []string, scope rule.Scope) error {
	for i, s := range loads {
		if !slices.Contains(steps, s) || slices.Contains(loads[:i], s) {
			return refuse(CodeSubjectInvalid, "loads_at is a duplicate-free list of the step kinds dispatch, gate, define, stow and project")
		}
		if s == "project" && scope.Level != envelope.ScopeProject {
			return refuse(CodeSubjectInvalid, "the step project is refused on a subject outside project scope")
		}
	}
	return nil
}

// checkChanges refuses a request whose changes names a rule the Pool does not
// hold at the revision named, or one of another conflict key or full scope.
func checkChanges(snap *memory.Snapshot, r *rule.Rule) error {
	for _, s := range snap.Rules {
		if s.RuleID == r.Changes.RuleID {
			if s.RecordRevision != r.Changes.RecordRevision {
				return envelope.Common(envelope.CodeRevisionConflict, "the rule that changes names is not at the revision named")
			}
			if s.Subject != r.Subject || s.Scope != r.Scope || s.TaskID != r.TaskID || s.Generation != r.Generation {
				return invalid("the rule that changes names is of another conflict key or full scope").With("rule", s.RuleID)
			}
			return nil
		}
	}
	return envelope.Common(envelope.CodeNotFound, "the rule that changes names is not a rule of the Pool")
}

// checkProductConflict refuses, with pool.rule_conflict, a write of a product
// rule after which two product rules of its conflict key and full scope that
// apply to a common role are in force together at the clock or at a later
// instant at which one takes effect or expires, neither naming the other in
// changes, where the rules before the write were not. A rule that a rule in
// force supersedes is not in force beside it. An operating rule is never
// compared with a product rule: the resolver weighs those.
func checkProductConflict(rules []*rule.Rule, now time.Time, w *rule.Rule) error {
	if w.Kind != rule.KindProduct {
		return nil
	}
	var before, after []*rule.Rule
	for _, s := range rules {
		if s.Kind != rule.KindProduct || s.Subject != w.Subject || s.Scope != w.Scope || s.TaskID != w.TaskID || s.Generation != w.Generation {
			continue
		}
		before = append(before, s)
		if s.RuleID != w.RuleID {
			after = append(after, s)
		}
	}
	after = append(after, w)
	instants := []time.Time{now}
	for _, s := range append(slices.Clone(before), w) {
		if t, err := clock.Parse(s.EffectiveAt); err == nil && t.After(now) {
			instants = append(instants, t)
		}
		if t, ok, err := s.EffectiveExpiry(); err == nil && ok && t.After(now) {
			instants = append(instants, t)
		}
	}
	slices.SortFunc(instants, func(a, b time.Time) int { return a.Compare(b) })
	for _, at := range instants {
		involved := productConflict(after, at)
		if len(involved) > 0 && !slices.Equal(involved, productConflict(before, at)) {
			instant, _ := clock.Format(at)
			ruleIDs := make([]any, len(involved))
			for i, id := range involved {
				ruleIDs[i] = id
			}
			return refuse(CodeRuleConflict, "after the write two product rules would be in force on the subject at this scope, neither naming the other in changes").
				With("subject", w.Subject).With("rules", ruleIDs).With("at", instant)
		}
	}
	return nil
}

// productConflict are the identities, in order, of the product rules of one
// conflict key and full scope in force at the instant, and not superseded by
// one in force, that have a partner applying to a common role that neither
// names the other in changes.
func productConflict(group []*rule.Rule, at time.Time) []string {
	var live []*rule.Rule
	superseded := map[string]bool{}
	for _, s := range group {
		if inForce(s, at) {
			live = append(live, s)
			if s.Changes != nil && s.Changes.Relation == rule.RelationSupersedes {
				superseded[s.Changes.RuleID] = true
			}
		}
	}
	related := func(a, b *rule.Rule) bool {
		return a.Changes != nil && a.Changes.RuleID == b.RuleID || b.Changes != nil && b.Changes.RuleID == a.RuleID
	}
	var involved []string
	for _, a := range live {
		if superseded[a.RuleID] {
			continue
		}
		for _, b := range live {
			if b != a && !superseded[b.RuleID] && !related(a, b) && overlaps(a.Applies, b.Applies) {
				involved = append(involved, a.RuleID)
				break
			}
		}
	}
	slices.Sort(involved)
	return involved
}

// inForce says whether the rule is in force at the instant: in force by its
// status, begun, and not past its effective expiry by the clock.
func inForce(s *rule.Rule, at time.Time) bool {
	if s.Status != rule.StatusInForce {
		return false
	}
	effective, err := clock.Parse(s.EffectiveAt)
	if err != nil || at.Before(effective) {
		return false
	}
	past, err := s.ExpiredAt(at)
	return err == nil && !past
}

// overlaps says whether two rules apply to a common role; none is every role.
func overlaps(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

// checkGoverned holds a product rule's governed work to the Pool: a governed
// Target exists at the ruling's scope and has level target, and a governed stage
// is a ruling at the Hive's scope.
func checkGoverned(c *cli.Call, r *rule.Rule) error {
	if target := str(r.Governs["target_id"]); target != "" {
		l, om, err := poolstore.Load(c.Store, "item", target)
		if err != nil || om != nil {
			return refuse(CodeSubjectInvalid, "the governed Target is not an item of the Pool")
		}
		d := l.Data
		got := rule.Scope{Level: str(d["scope_level"]), HomeID: str(d["home_id"]), WorkspaceID: str(d["workspace_id"]), ProjectID: str(d["project_id"])}
		if str(d["level"]) != "target" || got != r.Scope {
			return refuse(CodeSubjectInvalid, "the governed work is not a Target at the scope of the ruling")
		}
		return nil
	}
	if r.Scope.Level != envelope.ScopeHive {
		return refuse(CodeSubjectInvalid, "a governed stage is ruled at the Hive's scope")
	}
	return nil
}

func texts(v any) []string {
	out := []string{}
	for _, e := range list(v) {
		out = append(out, str(e))
	}
	return out
}

func whole(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}
