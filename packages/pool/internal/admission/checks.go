package admission

import (
	"fmt"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/read"
)

// The checks of spec/pool.md, Admission, in the order of its table. Every check
// is run and every finding is recorded; the first check that does not pass
// fixes the result.

// The codes the checks find with: the Pool's own, from its table of error codes,
// and the hold reason codes a check places a hold with.
const (
	codeGuess             = "pool.admission_guess"
	codeAuthorityRequired = "pool.authority_required"
	codeImperativeStale   = "pool.imperative_stale"
	codeSetupContradict   = "pool.setup_contradiction"
	codeSpecUnaccepted    = "pool.specification_unaccepted"
	codeCriteriaMismatch  = "pool.criteria_mismatch"
	codePlacementParked   = "pool.placement_parked"
	codeLocalOnly         = "pool.placement_local_only"
	codeDeliveryMismatch  = "pool.delivery_mismatch"
	codeNeedsForge        = "pool.publication_needs_forge"

	reasonGuess      = "field_guess"
	reasonBranch     = "source_branch_not_pushed"
	reasonAuthority  = "authority_required"
	findingBranch    = "source_branch_not_pushed"
	findingInsuffice = envelope.CodeAuthorityInsufficient

	placeCodexCloud   = "codex-cloud"
	placeClaudeCloud  = "claude-cloud-session"
	placePolicy       = "policy"
	repositoryLocal   = "local-only"
	publicationForge  = "harvest"
	resultAdmitted    = "admitted"
	resultRefused     = "refused"
	resultHeld        = "held"
	maxMessageRunes   = 1900
	sensitiveBoundary = "rules.boundary.sensitive"
)

// finding is one check that did not pass.
type finding struct {
	code, field, message, consequence string
}

func (f finding) tree() map[string]any {
	m := map[string]any{"code": f.code, "message": f.message}
	if f.field != "" {
		m["field"] = f.field
	}
	if f.consequence != "" {
		m["consequence"] = f.consequence
	}
	return m
}

// row is one check of the table: how it ends when it does not pass, and what it
// found. A row that passes has no findings.
type row struct {
	// result is refused or held, and reason the reason code of the hold a held
	// row places.
	result, reason string
	findings       []finding
}

// facts is what the checks read: the admission, the item as it stands, the
// Imperative revision the admission is bound to, the project's repository, and
// the evidence in the request.
type facts struct {
	c       *cli.Call
	rec     *Record
	graph   *read.Graph
	item    *read.Node
	rev     map[string]any
	current int64
	// repository is the kind of repository the project binds.
	repository string
	// place is the place checked and branch the evidence of the source branch,
	// nil where the request gave none.
	place  string
	branch map[string]any
	// authority is what the record gives a sensitive boundary.
	authority standing
}

// run runs every check of the table in order and returns the rows that did not
// pass, in order.
func (f *facts) run() []row {
	var out []row
	for _, check := range []func() row{
		f.guesses, f.currency, f.contradiction, f.specification, f.placement,
		f.sourceBranch, f.sensitive, f.delivery,
	} {
		if r := check(); len(r.findings) > 0 {
			out = append(out, r)
		}
	}
	return out
}

// section reads a member of the Imperative revision by its path.
func (f *facts) section(path ...string) map[string]any {
	m := f.rev
	for _, p := range path {
		if m = tree(m[p]); m == nil {
			return nil
		}
	}
	return m
}

// guesses: no field is a guess. The Object, the Standard, the Output and the
// Boundary each carry a confidence, and a guess is held on its question.
func (f *facts) guesses() row {
	r := row{result: resultHeld, reason: reasonGuess}
	for _, s := range []struct {
		name string
		path []string
	}{
		{"Object", []string{"task", "object"}},
		{"Standard", []string{"task", "standard"}},
		{"Output", []string{"definition_of_done", "output"}},
		{"Boundary", []string{"rules", "boundary"}},
	} {
		c := tree(f.section(s.path...)["confidence"])
		if str(c["level"]) != "guess" {
			continue
		}
		field := s.path[0] + "." + s.path[1]
		r.findings = append(r.findings, finding{
			code: codeGuess, field: field, consequence: str(c["consequence"]),
			message: fmt.Sprintf("The %s is a guess. Question: %s", s.name, str(c["question"])),
		})
	}
	return r
}

// currency: the Imperative is current and agrees with the item. The admission's
// revision is the Imperative's current one, it is the revision whose digest the
// admission recorded, and its Setup copies the place the item now requests.
func (f *facts) currency() row {
	r := row{result: resultRefused}
	stale := func(field, message string) {
		r.findings = append(r.findings, finding{code: codeImperativeStale, field: field, message: message})
	}
	revision := integer(f.rec.Data["imperative_revision"])
	if revision != f.current {
		stale("", fmt.Sprintf("The admission is bound to Imperative revision %d and the current revision is %d; revise and admit again.", revision, f.current))
	}
	if str(f.rev["digest"]) != str(f.rec.Data["imperative_digest"]) {
		stale("", "The Imperative revision is not the one whose digest the admission recorded.")
	}
	if want := f.itemPlace(); str(f.section("setup", "placement")["requested"]) != want {
		stale("setup.placement", fmt.Sprintf("The Imperative's place is %s and the item now requests %s; revise.", str(f.section("setup", "placement")["requested"]), want))
	}
	return r
}

// itemPlace is the place the item requests, policy where it leaves it out.
func (f *facts) itemPlace() string {
	if p := str(f.item.Data["placement_requested"]); p != "" {
		return p
	}
	return placePolicy
}

// contradiction: harness and place do not contradict. A harness other than the
// one the place's backend names is a contradiction; today the only such pair is
// the Codex harness in a Claude cloud session. A harness left to the resolver
// contradicts nothing.
func (f *facts) contradiction() row {
	r := row{result: resultRefused}
	if harness := str(f.section("setup")["harness"]); f.place == placeClaudeCloud && harness == "codex" {
		r.findings = append(r.findings, finding{
			code: codeSetupContradict, field: "setup.harness",
			message: "The Codex harness cannot run in a Claude cloud session.",
		})
	}
	return r
}

// specification: an item that needs an accepted specification has an effective
// one, and one that has an effective specification, needed or not, is named by
// the Imperative's Standard.
func (f *facts) specification() row {
	r := row{result: resultRefused}
	id := f.item.ID
	effective := f.graph.Effective(id)
	criteria := tree(f.section("task", "standard")["criteria"])
	switch {
	case effective == nil && f.graph.Needs(id):
		r.findings = append(r.findings, finding{code: codeSpecUnaccepted, field: "task.standard.criteria",
			message: "The item needs an accepted specification and has no effective specification."})
	case effective == nil && criteria != nil:
		r.findings = append(r.findings, finding{code: codeCriteriaMismatch, field: "task.standard.criteria",
			message: "The Standard names a specification revision and the item has no effective specification."})
	case effective != nil && !sameCriteria(criteria, effective):
		r.findings = append(r.findings, finding{code: codeCriteriaMismatch, field: "task.standard.criteria",
			message: "The Standard does not name the item's effective specification revision."})
	}
	return r
}

// sameCriteria says whether two specification references name one revision.
func sameCriteria(a, b map[string]any) bool {
	return a != nil && b != nil && a["specification_id"] == b["specification_id"] &&
		integer(a["revision"]) == integer(b["revision"]) && a["digest"] == b["digest"]
}

// void says whether the place is in the Void.
func void(place string) bool { return place == placeClaudeCloud || place == placeCodexCloud }

// placement: the place is admissible for the project and the backend is not
// parked. codex-cloud is refused unconditionally, whatever the project, the
// rules, the approval or the authority; a Void place for a local-only project
// is refused.
func (f *facts) placement() row {
	r := row{result: resultRefused}
	if f.place == placeCodexCloud {
		r.findings = append(r.findings, finding{code: codePlacementParked, field: "setup.placement",
			message: "Codex Cloud is out of scope and admission refuses it."})
	}
	if void(f.place) && f.repository == repositoryLocal {
		r.findings = append(r.findings, finding{code: codeLocalOnly, field: "setup.placement",
			message: "A place in the Void is not admitted for a local-only project."})
	}
	return r
}

// sourceBranch: a Void place has a pushed source branch. The Pool runs no Git:
// the branch's state is evidence the caller supplies, and absent evidence is
// unknown, which holds.
func (f *facts) sourceBranch() row {
	r := row{result: resultHeld, reason: reasonBranch}
	if void(f.place) && str(f.branch["pushed"]) != "true" {
		r.findings = append(r.findings, finding{code: findingBranch, field: "setup.source_branch",
			message: "The caller's evidence does not show the source branch pushed."})
	}
	return r
}

// sensitive: each sensitive boundary has explicit, applicable authority. A
// posture, charter or instruction reference is insufficient for one.
func (f *facts) sensitive() row {
	r := row{result: resultHeld, reason: reasonAuthority}
	if f.authority == given {
		return r
	}
	for _, b := range list(f.section("rules", "boundary")["sensitive"]) {
		code, message := codeAuthorityRequired, fmt.Sprintf("The %s boundary needs the Hive Mind's explicit authority: an approval on the admission or an authority that stands.", str(b))
		if f.authority == insufficient {
			code, message = findingInsuffice, fmt.Sprintf("The %s boundary needs the Hive Mind's explicit authority, and a posture, charter or instruction reference is not accepted for one yet.", str(b))
		}
		r.findings = append(r.findings, finding{code: code, field: sensitiveBoundary, message: message})
	}
	return r
}

// delivery: the Output's validation and publication agree with the item's
// delivery, and a Harvest is not asked of a local-only project. An item that
// declares no delivery agrees with nothing.
func (f *facts) delivery() row {
	r := row{result: resultRefused}
	output := f.section("definition_of_done", "output")
	delivery := tree(f.item.Data["delivery"])
	for _, axis := range []string{"validation", "publication"} {
		if delivery == nil || output[axis] != delivery[axis] {
			r.findings = append(r.findings, finding{code: codeDeliveryMismatch, field: "definition_of_done.output." + axis,
				message: fmt.Sprintf("The Output's %s differs from the item's delivery.", axis)})
		}
	}
	if str(output["publication"]) == publicationForge && f.repository == repositoryLocal {
		r.findings = append(r.findings, finding{code: codeNeedsForge, field: "definition_of_done.output.publication",
			message: "A Harvest is asked of a project that binds no forge."})
	}
	return r
}

// present checks the first row of the table, which create cannot: Object,
// Standard, Output and Boundary are present and non-empty. A record that lost
// one is record_malformed, as the table says.
func (f *facts) present(id string) error {
	for _, s := range []struct {
		path   []string
		member string
	}{
		{[]string{"task", "object"}, "text"}, {[]string{"task", "standard"}, "conditions"},
		{[]string{"definition_of_done", "output"}, "deliverable"}, {[]string{"rules", "boundary"}, "scope"},
	} {
		if str(f.section(s.path...)[s.member]) == "" {
			return malformed("the Imperative revision lost a section the schema requires", id)
		}
	}
	return nil
}
