package imperative

import (
	"errors"
	"fmt"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/memory"
	"github.com/kesk-git/hive/packages/pool/internal/read"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The operation render of hive-imperative (spec/pool.md, The Imperative). It is
// read-only and deterministic: the same revision, the same specification
// revision, the same rules in force for the revision's class, the same rules and
// facts of its subjects and the same renderer version give the same text and
// digest. It opens with the worker role boundary and writes the four sections in
// order, the Standard's criteria from the specification revision the Standard
// names, and in Rules, after the Boundary, the playbook of the revision's class:
// the rules the resolver gives for the Imperative's scope and the class's role
// at the instant of rendering, in the resolver's order, each as its rule line
// with its source and revision. A conflict among them refuses the rendering with
// pool.rule_conflict. After the playbook it writes the projection of each subject
// the specification revision names, read at the same instant. It returns the
// text, its digest and size, the rules it wrote with their revisions, the
// playbook's and each subject projection's, and the launch parameters the
// Imperative itself fixes.

// Render renders one revision of the item's Imperative.
func Render(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	taskID := strOf(body["task_id"])
	st, release, err := read.OpenShared(c)
	if err != nil {
		return nil, err
	}
	defer release()

	l, err := loadImperative(st, taskID)
	if err != nil {
		return nil, err
	}
	if err := checkScope(c, l.Data); err != nil {
		return nil, err
	}
	number := intOf(l.Data["current_revision"])
	if v, named := body["revision"].(int64); named {
		number = v
	}
	rev := revisionNamed(l.Data, number)
	if rev == nil {
		return nil, notFound("revision")
	}
	now := c.Clock.Now()
	renderedAt, err := clock.Format(now)
	if err != nil {
		return nil, internal("the time of the rendering cannot be written", err)
	}
	class := intOf(rev["class"])
	role := fmt.Sprintf("class-%d", class)
	scope := scopeOf(l.Data)

	in := textInput{Task: taskID, Revision: rev}
	var touched []string
	standard := mapOf(mapOf(rev["task"])["standard"])
	if ref := mapOf(standard["criteria"]); ref != nil {
		if in.Criteria, touched, err = criteriaOf(st, ref); err != nil {
			return nil, err
		}
	}

	if in.Playbook, err = playbookOf(st, scope, role, now); err != nil {
		return nil, err
	}

	var embedded []memory.Existing
	if len(touched) > 0 {
		snap, err := memory.Load(st, c.Home)
		if err != nil {
			return nil, err
		}
		for _, id := range touched {
			key := memory.Key{
				Scope: rule.Scope{Level: scope.Level, HomeID: scope.HomeID, WorkspaceID: scope.WorkspaceID, ProjectID: scope.ProjectID},
				Tier:  memory.TierSubject, Audience: role, SubjectID: id,
			}
			// A projection that is not fresh is read as it stands: whether a
			// rendering is refused for it is not settled by the contract, and
			// is not decided here.
			e, err := snap.SubjectProjection(key, now)
			if err != nil {
				return nil, err
			}
			embedded = append(embedded, e)
			in.Projections = append(in.Projections, projectionBlock{
				ID: e.ID, Number: e.Current.Number, Digest: e.Current.Digest, Rules: e.Current.Rules, Text: e.Current.Text,
			})
		}
	}

	text := renderText(in)
	resp := map[string]any{
		"revision": number, "revision_digest": rev["digest"], "renderer_version": RendererVersion,
		"text": text, "text_digest": canon.FileDigest([]byte(text)), "text_bytes": int64(len(text)),
		"rendered_at": renderedAt,
	}
	setup, output := mapOf(rev["setup"]), mapOf(mapOf(rev["definition_of_done"])["output"])
	resp["launch_parameters"] = map[string]any{
		"role": setup["role"], "harness": setup["harness"], "placement_requested": mapOf(setup["placement"])["requested"],
		"class": class, "validation": output["validation"], "publication": output["publication"],
	}
	if in.Criteria != nil {
		resp["criteria"] = copyValue(standard["criteria"])
	}

	// The rules the text wrote: the playbook's, in the resolver's order, and the
	// rules of each embedded projection that the playbook does not hold.
	seen := map[ruleKey]bool{}
	rules := []any{}
	add := func(k ruleKey) {
		if !seen[k] {
			seen[k] = true
			rules = append(rules, map[string]any{"rule_id": k.ID, "record_revision": k.Revision})
		}
	}
	for _, r := range in.Playbook {
		add(ruleKey{r.RuleID, r.RecordRevision})
	}
	projections := []any{}
	for _, p := range in.Projections {
		mine := []any{}
		for _, r := range p.Rules {
			add(ruleKey{r.RuleID, r.Revision})
			mine = append(mine, map[string]any{"rule_id": r.RuleID, "record_revision": r.Revision})
		}
		projections = append(projections, map[string]any{"projection_id": p.ID, "rendering": p.Number, "digest": p.Digest, "rules": mine})
	}
	resp["rules"], resp["subject_projections"] = rules, projections
	return &cli.Result{Body: resp}, nil
}

// criteriaOf reads the specification revision the Standard names: its criteria,
// to be rendered into the Standard, and the subjects it touches, whose
// projections the rendering embeds.
func criteriaOf(st *cstore.Store, ref map[string]any) (*criteriaBlock, []string, error) {
	id := strOf(ref["specification_id"])
	spec, om, err := poolstore.Load(st, "specification", id)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return nil, nil, notFound("specification")
	case err != nil:
		return nil, nil, poolstore.Failure("the specification", err)
	case om != nil:
		return nil, nil, malformed("the specification cannot be read", om.ID)
	}
	number := intOf(ref["revision"])
	revision := revisionNamed(spec.Data, number)
	if revision == nil {
		return nil, nil, malformed("the Standard names a revision the specification does not hold", id)
	}
	var touched []string
	for _, v := range listOf(revision["touched_subjects"]) {
		touched = append(touched, strOf(v))
	}
	return &criteriaBlock{SpecificationID: id, Revision: number, Criteria: listOf(revision["criteria"])}, touched, nil
}

// playbookOf is the playbook of the class: the rules the resolver gives for
// the scope and the class's role at the instant, in its order, each as its rule
// line with its source and revision. A rule that cannot be read ends the
// rendering, as it ends a resolution, since the rules in force cannot be known
// over a hole in them. A conflict among the rules refuses it.
func playbookOf(st *cstore.Store, scope envelope.Scope, role string, at time.Time) ([]rule.Resolved, error) {
	all, omitted, err := poolstore.LoadAll(st, "rule")
	if err != nil {
		return nil, poolstore.Failure("the rules", err)
	}
	if len(omitted) > 0 {
		return nil, malformed("a rule cannot be read, so the rules in force cannot be known", omitted[0].ID)
	}
	rules := make([]*rule.Rule, len(all))
	for i, l := range all {
		if rules[i], err = rule.ParseRule(l.Data); err != nil {
			return nil, malformed("a rule cannot be read, so the rules in force cannot be known", l.ID)
		}
	}
	res, err := rule.Compute(rule.Request{Scope: scope, Role: role, At: at}, rules)
	if err != nil {
		return nil, envelope.Common(envelope.CodeRecordMalformed, "a rule cannot be placed in time").Because(err)
	}
	if len(res.Conflicts) > 0 {
		return nil, refuse(codeRuleConflict, "two rules govern one subject at one scope, so the playbook of the class is unresolved and the Imperative is not rendered").
			With("class_role", role).With("conflicts", int64(len(res.Conflicts))).With("subject", res.Conflicts[0].Subject)
	}
	return res.Rules, nil
}
