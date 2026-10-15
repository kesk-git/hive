package imperative

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/kesk-git/hive/packages/pool/internal/memory"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
)

// The text of a rendered Imperative (spec/pool.md, The Imperative). The
// contract fixes its order and headings, and leaves the layout beyond them to the
// renderer, under the rule that the same inputs give the same text.
//
// The text keeps the sources it was made from in labels a delivery can read
// back: the rules of the playbook, each as an entry of its own, written as the
// rule wrote it with its lines after the first indented and its source and
// revision at the end, and each embedded subject projection under a heading
// that names it, with the inventory of its rules on the line after. Only the
// renderer writes at the first column. Everything an author, a rule or a fact
// wrote is indented, so that none of it can pass for a heading or a label.

const (
	headingPlaybook   = "### Playbook"
	headingProjection = "### Subject projection "
	inventoryLabel    = "Rules in this projection: "
	noRules           = "none"
)

// boundary is the worker role boundary the text opens with, by role.
var boundary = map[string]string{
	"swarm": "You are a Swarm worker. This Imperative is everything you are given of the home.\n" +
		"Work inside the Boundary in Rules and nothing beyond it; Setup implements the Boundary and never widens it.\n" +
		"Stop at the stopping point in Definition of done.",
	"lictor": "You are a Lictor. This Imperative is everything you are given of the home.\n" +
		"Inspect and report inside the Boundary in Rules and nothing beyond it; Setup implements the Boundary and never widens it.\n" +
		"Stop at the stopping point in Definition of done.",
}

// projectionBlock is a subject projection as the text embeds it.
type projectionBlock struct {
	ID     string
	Number int64
	Digest string
	Rules  []memory.RuleRef
	Text   string
}

// textInput is everything a rendering is made from.
type textInput struct {
	Task        string
	Revision    map[string]any
	Criteria    *criteriaBlock
	Playbook    []rule.Resolved
	Projections []projectionBlock
}

// criteriaBlock is the specification revision the Standard is rendered from.
type criteriaBlock struct {
	SpecificationID string
	Revision        int64
	Criteria        []any
}

type out struct{ b strings.Builder }

func (o *out) line(s string) { o.b.WriteString(s + "\n") }

// indent indents every line of s by n spaces, leaving empty lines empty.
func indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = pad + l
		}
	}
	return strings.Join(lines, "\n")
}

// kv writes a labelled value, indented under its section; a value that is empty
// is written as none.
func (o *out) kv(label, value string) {
	if value == "" {
		value = noRules
	}
	o.line(indent(label+": "+value, 2))
}

// bullets writes a labelled list, indented under its section.
func (o *out) bullets(label string, items []string) {
	if len(items) == 0 {
		o.kv(label, "none")
		return
	}
	o.line(indent(label+":", 2))
	for _, it := range items {
		o.line(indent("- "+strings.ReplaceAll(it, "\n", "\n  "), 2))
	}
}

func texts(v any) []string {
	var out []string
	for _, x := range listOf(v) {
		out = append(out, strOf(x))
	}
	return out
}

func confidence(c map[string]any) string {
	if c["level"] != "guess" {
		return "high"
	}
	return fmt.Sprintf("a guess; to ask: %s; if wrong: %s", strOf(c["question"]), strOf(c["consequence"]))
}

func repositoryText(r map[string]any) string {
	if r["kind"] == "github" {
		return fmt.Sprintf("github %s/%s/%s", strOf(r["host"]), strOf(r["owner"]), strOf(r["name"]))
	}
	return "local-only, no forge"
}

// renderText writes the text of the rendering. It reads nothing but its input,
// and holds no time and no identity of the rendering, so the same input gives
// the same text.
func renderText(in textInput) string {
	rev := in.Revision
	task, setup, rules, done := mapOf(rev["task"]), mapOf(rev["setup"]), mapOf(rev["rules"]), mapOf(rev["definition_of_done"])
	role := strOf(setup["role"])
	o := &out{}

	o.line("# Worker role boundary")
	o.line(boundary[role])
	o.line(fmt.Sprintf("Imperative of %s, revision %d, class %d.", in.Task, intOf(rev["revision"]), intOf(rev["class"])))
	o.line("")

	o.line("## Task")
	o.line("### Intent")
	o.line(indent(strOf(task["intent"]), 2))
	o.line("### Object")
	object := mapOf(task["object"])
	o.line(indent(strOf(object["text"]), 2))
	o.kv("Artifact", strOf(object["artifact"]))
	o.kv("Confidence", confidence(mapOf(object["confidence"])))
	o.line("### Standard")
	standard := mapOf(task["standard"])
	o.kv("Conditions", strOf(standard["conditions"]))
	o.bullets("Evidence", texts(standard["evidence"]))
	o.kv("Confidence", confidence(mapOf(standard["confidence"])))
	if in.Criteria != nil {
		o.line(indent(fmt.Sprintf("Criteria of specification %s, revision %d:", in.Criteria.SpecificationID, in.Criteria.Revision), 2))
		for _, v := range in.Criteria.Criteria {
			c := mapOf(v)
			o.line(indent(fmt.Sprintf("- %s: %s", strOf(c["criterion_id"]), strings.ReplaceAll(strOf(c["text"]), "\n", "\n  ")), 2))
			o.line(indent("Evidence: "+strings.ReplaceAll(strOf(c["evidence"]), "\n", "\n  "), 4))
		}
	}
	o.line("")

	o.line("## Setup")
	placement := mapOf(setup["placement"])
	o.kv("Placement requested", fmt.Sprintf("%s (item revision %d)", strOf(placement["requested"]), intOf(placement["item_revision"])))
	o.kv("Repository", repositoryText(mapOf(setup["repository"])))
	o.kv("Base branch", branch(setup, "base"))
	o.kv("Source branch", branch(setup, "source"))
	o.kv("Output branch rule", strOf(setup["output_branch_rule"]))
	o.kv("Role", role)
	o.kv("Harness", strOf(setup["harness"]))
	o.bullets("Dependencies", texts(setup["dependencies"]))
	o.kv("Tools", strings.Join(texts(setup["tools"]), ", "))
	access := mapOf(setup["access"])
	var spoors []string
	for _, v := range listOf(access["credentials"]) {
		spoors = append(spoors, strOf(mapOf(v)["spoor_id"]))
	}
	o.kv("Credentials, by reference", strings.Join(spoors, ", "))
	o.kv("Network", strOf(access["network"]))
	o.bullets("Isolation", texts(setup["isolation"]))
	o.line("")

	o.line("## Rules")
	o.line("### Boundary")
	bound := mapOf(rules["boundary"])
	o.kv("Scope", strOf(bound["scope"]))
	o.bullets("Exclusions", texts(bound["exclusions"]))
	o.bullets("Allowed effects", texts(bound["allowed_effects"]))
	o.kv("Resources", strOf(bound["resources"]))
	o.kv("Permissions", strOf(bound["permissions"]))
	o.bullets("Stop conditions", texts(bound["stop_conditions"]))
	o.kv("Repair scope", strOf(bound["repair_scope"]))
	o.kv("Retry limit", strconv.FormatInt(intOf(bound["retry_limit"]), 10))
	o.kv("Sensitive boundaries", strings.Join(texts(bound["sensitive"]), ", "))
	o.kv("Confidence", confidence(mapOf(bound["confidence"])))
	o.line("### Validation commands")
	for _, v := range listOf(rules["validation_commands"]) {
		cmd := mapOf(v)
		quoted := make([]string, 0, len(listOf(cmd["argv"])))
		for _, a := range texts(cmd["argv"]) {
			quoted = append(quoted, strconv.Quote(a))
		}
		argv := "[" + strings.Join(quoted, ", ") + "]"
		where := ""
		if dir := strOf(cmd["working_directory"]); dir != "" {
			where = " (in " + dir + ")"
		}
		o.line(indent(fmt.Sprintf("- %s: %s%s", strOf(cmd["name"]), argv, where), 2))
	}
	o.line("### Channels")
	channels := mapOf(rules["channels"])
	o.kv("Delivery", strOf(channels["delivery"]))
	o.kv("Decisions", strOf(channels["decisions"]))
	o.kv("Blocker owner", strOf(channels["blocker_owner"]))
	o.line(fmt.Sprintf("%s (class %d)", headingPlaybook, intOf(rev["class"])))
	if len(in.Playbook) == 0 {
		o.line(indent("No rule is in force for this class.", 2))
	}
	for _, r := range in.Playbook {
		o.line("- " + strings.ReplaceAll(r.Line, "\n", "\n  "))
	}
	for _, p := range in.Projections {
		o.line(fmt.Sprintf("%s%s digest %s", headingProjection, p.ID, p.Digest))
		o.line(inventoryLabel + inventoryText(p.Rules))
		o.line(indent(strings.TrimRight(p.Text, "\n"), 2))
	}
	o.line("")

	o.line("## Definition of done")
	output := mapOf(done["output"])
	o.line("### Output")
	o.kv("Deliverable", strOf(output["deliverable"]))
	o.kv("Recipient", strOf(output["recipient"]))
	o.kv("Validation", strOf(output["validation"]))
	o.kv("Publication", strOf(output["publication"]))
	o.bullets("Completion evidence", texts(output["completion_evidence"]))
	o.kv("Confidence", confidence(mapOf(output["confidence"])))
	o.line("### Stopping point")
	o.line(indent(strOf(done["stopping_point"]), 2))
	return o.b.String()
}

func branch(setup map[string]any, which string) string {
	s := strOf(setup[which+"_branch"])
	if r := strOf(setup[which+"_revision"]); r != "" {
		s += " at " + r
	}
	return s
}

func inventoryText(rules []memory.RuleRef) string {
	if len(rules) == 0 {
		return noRules
	}
	parts := make([]string, len(rules))
	for i, r := range rules {
		parts[i] = ruleKey{r.RuleID, r.Revision}.String()
	}
	return strings.Join(parts, ", ")
}

// ruleKey is a rule at a record revision, as an inventory names it.
type ruleKey struct {
	ID       string
	Revision int64
}

func (k ruleKey) String() string { return k.ID + "@" + strconv.FormatInt(k.Revision, 10) }

// labeled is what a text says it was made from.
type labeled struct {
	Playbook    []ruleKey
	Projections []labeledProjection
}

type labeledProjection struct {
	ID     string
	Digest string
	Rules  []ruleKey
}

var (
	playbookLine    = regexp.MustCompile(`(?s)^- .*\[(rule_[0-9a-z]+)@([0-9]+)\]$`)
	projectionHead  = regexp.MustCompile(`^### Subject projection (mem_[0-9a-z]+) digest (sha256:[0-9a-f]{64})$`)
	ruleReference   = regexp.MustCompile(`^(rule_[0-9a-z]+)@([0-9]+)$`)
	errNotLabelable = fmt.Errorf("the text does not carry its sources in labels")
)

// labelsOf reads the labeled source inventories of a rendered text: the rules
// of the playbook and the projections embedded with their rules. A label the
// renderer would not have written is an error.
func labelsOf(text string) (*labeled, error) {
	l := &labeled{}
	const (
		none = iota
		inPlaybook
		inProjection
	)
	mode, seenPlaybook := none, false
	var entry []string
	flush := func() error {
		if entry == nil {
			return nil
		}
		m := playbookLine.FindStringSubmatch(strings.Join(entry, "\n"))
		entry = nil
		if m == nil {
			return errNotLabelable
		}
		n, _ := strconv.ParseInt(m[2], 10, 64)
		l.Playbook = append(l.Playbook, ruleKey{m[1], n})
		return nil
	}
	for _, ln := range strings.Split(text, "\n") {
		if entry != nil && strings.HasPrefix(ln, " ") {
			entry = append(entry, ln)
			continue
		}
		if err := flush(); err != nil {
			return nil, err
		}
		switch {
		case strings.HasPrefix(ln, headingPlaybook):
			if seenPlaybook {
				return nil, errNotLabelable
			}
			mode, seenPlaybook = inPlaybook, true
		case strings.HasPrefix(ln, headingProjection):
			m := projectionHead.FindStringSubmatch(ln)
			if m == nil {
				return nil, errNotLabelable
			}
			l.Projections = append(l.Projections, labeledProjection{ID: m[1], Digest: m[2]})
			mode = inProjection
		case strings.HasPrefix(ln, "# "), strings.HasPrefix(ln, "## "), strings.HasPrefix(ln, "### "):
			mode = none
		case mode == inPlaybook && strings.HasPrefix(ln, "- "):
			entry = []string{ln}
		case mode == inProjection && strings.HasPrefix(ln, inventoryLabel):
			p := &l.Projections[len(l.Projections)-1]
			if p.Rules != nil {
				return nil, errNotLabelable
			}
			p.Rules = []ruleKey{}
			if rest := strings.TrimPrefix(ln, inventoryLabel); rest != noRules {
				for _, part := range strings.Split(rest, ", ") {
					m := ruleReference.FindStringSubmatch(part)
					if m == nil {
						return nil, errNotLabelable
					}
					n, _ := strconv.ParseInt(m[2], 10, 64)
					p.Rules = append(p.Rules, ruleKey{m[1], n})
				}
			}
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	for _, p := range l.Projections {
		if p.Rules == nil {
			return nil, errNotLabelable
		}
	}
	return l, nil
}
