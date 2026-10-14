package intake

import (
	"fmt"
	"slices"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// Verify answers, read-only, whether the Mind's word stands: from the record
// and never from the reference, by the rule of spec/word.md, The word stands.
const Verify = "verify"

// readRequestSchema is the schema of the body of the Pool's read.
const readRequestSchema = "urn:hive:schema:ops:hive-pool.read.request:v1"

// The reasons the word does not stand, in the order the contract lists them.
const (
	unanswered         = "unanswered"
	deferred           = "deferred"
	unreconciled       = "unreconciled"
	dispositionPending = "disposition-pending"
	superseded         = "superseded"
	moot               = "moot"
	noQuestion         = "no-question"
	notWord            = "not-word"
	outOfScope         = "out-of-scope"
	notFound           = "not-found"
	malformed          = "malformed"
)

var reasonOrder = []string{
	unanswered, deferred, unreconciled, dispositionPending, superseded, moot,
	noQuestion, notWord, outOfScope, notFound, malformed,
}

// Reader is how verify reads the Pool: hive-pool read, the encoded request in
// and the response document and exit code out, as the callee sends a handoff.
// The contract library's Callee has no read; Pool and the fake Pool of a test
// implement both.
type Reader interface {
	Read(c *cli.Call, request []byte) ([]byte, int, error)
}

// Read is hive-pool read, run as the external executable it is.
func (Pool) Read(c *cli.Call, request []byte) ([]byte, int, error) {
	return run(c, "read", request)
}

// VerifyOperation is the operation verify of hive-word: supported, read-only,
// keeping no store, recording nothing and calling hive-pool to read. It
// reaches the Pool through r.
func VerifyOperation(r Reader) *cli.Operation {
	return &cli.Operation{
		OperationDecl: envelope.OperationDecl{
			Name:           Verify,
			State:          envelope.Supported,
			Mode:           envelope.Unary,
			Versions:       []string{bodyVersion},
			RequestSchema:  fmt.Sprintf("urn:hive:schema:ops:hive-word.%s.request:v1", Verify),
			ResponseSchema: fmt.Sprintf("urn:hive:schema:ops:hive-word.%s.response:v1", Verify),
			ReceiptKinds:   []string{},
			Executes:       []string{},
			Calls:          []string{poolCommand},
		},
		Handle: func(c *cli.Call) (*cli.Result, error) {
			if r == nil {
				return nil, envelope.Common(envelope.CodeDependencyUnusable, "the Pool cannot be read")
			}
			return verify(c, r)
		},
	}
}

// verifier is one verification: the records it read, whole and in the order
// read, each once, and the questions it found for a task, so that a task read
// twice is read once.
type verifier struct {
	c *cli.Call
	r Reader

	holds     []any
	questions []any
	seenHold  map[string]bool
	seenQ     map[string]bool
	byTask    map[string][]map[string]any
}

// verdict is the answer: the reasons the word does not stand, each once.
type verdict map[string]bool

func (v verdict) add(reasons ...string) {
	for _, r := range reasons {
		v[r] = true
	}
}

func verify(c *cli.Call, r Reader) (*cli.Result, error) {
	v := &verifier{
		c: c, r: r,
		seenHold: map[string]bool{}, seenQ: map[string]bool{}, byTask: map[string][]map[string]any{},
	}
	subject, _ := c.Request.Body["subject"].(map[string]any)
	var got verdict
	var err error
	switch subject["kind"] {
	case "authority":
		got, err = v.authority(subject)
	case "task":
		got, err = v.task(subject)
	case "hold":
		got, err = v.hold(subject)
	case "question":
		got, err = v.question(subject)
	default:
		err = envelope.Common(envelope.CodeSchemaViolation, "the subject is not one verify knows")
	}
	if err != nil {
		return nil, err
	}
	at, err := c.Stamp()
	if err != nil {
		return nil, err
	}
	reasons := []any{}
	for _, reason := range reasonOrder {
		if got[reason] {
			reasons = append(reasons, reason)
		}
	}
	return &cli.Result{Body: map[string]any{
		"verified_at": at, "stands": len(reasons) == 0, "reasons": reasons,
		"holds": orEmpty(v.holds), "questions": orEmpty(v.questions),
	}}, nil
}

func orEmpty(list []any) []any {
	if list == nil {
		return []any{}
	}
	return list
}

// errNotFound is the Pool's answer that it holds no record of the identity
// asked for.
var errNotFound = fmt.Errorf("intake: the Pool holds no such record")

// page is what a read of the Pool gave: the records, and for an item the
// facts derived beside each.
type page struct {
	records []map[string]any
	derived []map[string]any
}

// read is hive-pool read of the kind by the selector, sent as the handoffs are
// and under a request_id of its own. The Pool's failure of any kind is
// returned as the Pool gave it, and so is a read that is not complete: no
// answer is no verdict. errNotFound is the Pool holding no such record.
func (v *verifier) read(kind string, selector map[string]any) (*page, error) {
	c := v.c
	body := map[string]any{"kind": kind, "selector": selector}
	if err := envelope.ValidateBody(body, readRequestSchema); err != nil {
		return nil, err
	}
	id, err := ids.Mint(c.Clock, ids.Request)
	if err != nil {
		return nil, err
	}
	issued, err := c.Stamp()
	if err != nil {
		return nil, err
	}
	req := envelope.Request{
		Command:          poolCommand,
		Operation:        "read",
		OperationVersion: bodyVersion,
		RequestID:        id,
		CorrelationID:    c.Request.CorrelationID,
		IssuedAt:         issued,
		Caller:           envelope.Caller{Kind: envelope.CallerPackage, Package: c.Executable.Package, Command: c.Command.Name},
		Scope:            c.Request.Scope,
		Authority:        envelope.Authority{Kind: envelope.AuthorityNone},
		Body:             body,
	}
	data, err := req.Encode()
	if err != nil {
		return nil, err
	}
	output, exit, err := v.r.Read(c, data)
	if err != nil {
		if f := envelope.AsFailure(err); f != nil {
			return nil, f
		}
		return nil, unusable(err)
	}
	resp, err := envelope.AcceptResponse(output, exit, id)
	if err != nil {
		return nil, unusable(err)
	}
	switch {
	case resp.Status == envelope.NotFound && resp.Error != nil && resp.Error.Code == envelope.CodeNotFound && len(selector) == 1 && selector["id"] != nil:
		return nil, errNotFound
	case resp.Status != envelope.Ok:
		if f := resp.Failure(); f != nil {
			return nil, f
		}
		return nil, unusable(nil)
	}
	return pageOf(resp.Body, kind)
}

// unusable is the Pool giving no answer a verdict can rest on.
func unusable(because error) error {
	return envelope.Common(envelope.CodeDependencyUnusable, "the Pool gave no answer to the read that a verdict can rest on").Because(because)
}

// pageOf is the page the body of the Pool's read is, once it is held to the
// read's response schema and is complete and of the kind asked for.
func pageOf(body map[string]any, kind string) (*page, error) {
	if err := envelope.ValidateBody(body, readResponseSchema); err != nil {
		return nil, unusable(err)
	}
	header, _ := body["projection"].(map[string]any)
	if body["kind"] != kind || header["complete"] != true {
		return nil, unusable(fmt.Errorf("the read is incomplete or of another kind"))
	}
	p := &page{}
	list, _ := body["records"].([]any)
	for _, rec := range list {
		m, ok := rec.(map[string]any)
		if !ok {
			return nil, unusable(nil)
		}
		p.records = append(p.records, m)
	}
	derived, _ := body["derived"].([]any)
	for _, d := range derived {
		m, ok := d.(map[string]any)
		if !ok {
			return nil, unusable(nil)
		}
		p.derived = append(p.derived, m)
	}
	if kind == "item" && len(p.derived) != len(p.records) {
		return nil, unusable(fmt.Errorf("an item is read with no derived facts"))
	}
	return p, nil
}

// one is the one record of the kind with the identity, or errNotFound.
func (v *verifier) one(kind, field, id string) (*page, error) {
	p, err := v.read(kind, map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	if len(p.records) != 1 || p.records[0][field] != id {
		return nil, unusable(fmt.Errorf("the read of one %s gave another record", kind))
	}
	return p, nil
}

// keepHold adds the hold to the records returned, once.
func (v *verifier) keepHold(h map[string]any) {
	if id, _ := h["hold_id"].(string); !v.seenHold[id] {
		v.seenHold[id] = true
		v.holds = append(v.holds, h)
	}
}

func (v *verifier) keepQuestion(q map[string]any) {
	if id, _ := q["question_id"].(string); !v.seenQ[id] {
		v.seenQ[id] = true
		v.questions = append(v.questions, q)
	}
}

// getQuestion reads the question of the identity and returns it, or nil when
// the Pool holds none.
func (v *verifier) getQuestion(id string) (map[string]any, error) {
	p, err := v.one("question", "question_id", id)
	if err == errNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return p.records[0], nil
}

// getHold reads the hold of the identity and returns it, or nil when the Pool
// holds none. A hold read is kept among the records returned.
func (v *verifier) getHold(id string) (map[string]any, error) {
	p, err := v.one("hold", "hold_id", id)
	if err == errNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v.keepHold(p.records[0])
	return p.records[0], nil
}

// questionOf reads the question that answers for the hold: the question of
// the hold's task whose hold_id is the hold's, the Pool having no selector by
// hold. Several, which no operation produces, are none. A question found is
// kept among the records returned.
func (v *verifier) questionOf(hold map[string]any) (q map[string]any, several bool, err error) {
	task, _ := hold["task_id"].(string)
	list, ok := v.byTask[task]
	if !ok {
		p, err := v.read("question", map[string]any{"task_id": task})
		if err != nil && err != errNotFound {
			return nil, false, err
		}
		if p != nil {
			list = p.records
		}
		v.byTask[task] = list
		for _, cand := range list {
			v.keepQuestion(cand)
		}
	}
	for _, cand := range list {
		if cand["hold_id"] != hold["hold_id"] {
			continue
		}
		if q != nil {
			return nil, true, nil
		}
		q = cand
	}
	return q, false, nil
}

// awaiting reports whether an answer of the question awaits reconciliation:
// one of disposition unreconciled whose sequence is greater than that of every
// accepted or deferred answer.
func awaiting(q map[string]any) bool {
	answers, _ := q["answers"].([]any)
	var applied, late int64
	for _, a := range answers {
		answer, _ := a.(map[string]any)
		seq, _ := answer["sequence"].(int64)
		switch answer["disposition"] {
		case "accepted", "deferred":
			applied = max(applied, seq)
		case "unreconciled":
			late = max(late, seq)
		}
	}
	return late > applied
}

// stateReasons are the reasons a question that is not answered, released or
// completed gives, with unreconciled beside them when an answer awaits.
func stateReasons(q map[string]any, got verdict) {
	switch q["state"] {
	case "open":
		got.add(unanswered)
	case "deferred":
		got.add(deferred)
	case "moot":
		got.add(moot)
	default:
		got.add(malformed)
		return
	}
	if awaiting(q) {
		got.add(unreconciled)
	}
}

// question is the verdict on a question: it stands when it has an applied
// answer, answered, released or completed.
func (v *verifier) question(subject map[string]any) (verdict, error) {
	got := verdict{}
	id, _ := subject["question_id"].(string)
	q, err := v.getQuestion(id)
	if err != nil {
		return nil, err
	}
	if q == nil {
		got.add(notFound)
		return got, nil
	}
	v.keepQuestion(q)
	if !slices.Contains([]any{"answered", "released", "completed"}, q["state"]) {
		stateReasons(q, got)
	}
	return got, nil
}

// hold is the verdict on a hold: it stands when it is a word hold released by
// the disposition of an applied answer, release or complete.
func (v *verifier) hold(subject map[string]any) (verdict, error) {
	got := verdict{}
	id, _ := subject["hold_id"].(string)
	h, err := v.getHold(id)
	if err != nil {
		return nil, err
	}
	if h == nil {
		got.add(notFound)
		return got, nil
	}
	return got, v.judge(h, got)
}

// judge adds the reasons the word hold gives: those of the question it answers
// for, read against the state of the hold. A hold that is not a word hold is
// not the word.
func (v *verifier) judge(h map[string]any, got verdict) error {
	if h["kind"] != "word" {
		got.add(notWord)
		return nil
	}
	q, several, err := v.questionOf(h)
	if err != nil {
		return err
	}
	switch {
	case several:
		got.add(malformed)
		return nil
	case q == nil:
		got.add(noQuestion)
		return nil
	}
	state := q["state"]
	switch h["state"] {
	case "released":
		switch state {
		case "released", "completed":
		case "moot":
			stateReasons(q, got)
		default:
			got.add(malformed)
		}
	case "active":
		switch state {
		case "answered":
			got.add(dispositionPending)
			if awaiting(q) {
				got.add(unreconciled)
			}
		case "open", "deferred":
			stateReasons(q, got)
		default:
			got.add(malformed)
		}
	default:
		got.add(malformed)
	}
	return nil
}

// task is the verdict on a task: it stands when no word hold on the item or on
// an ancestor is active. The holds are the active ones the Pool derives for the
// item, its own and its ancestors'.
func (v *verifier) task(subject map[string]any) (verdict, error) {
	got := verdict{}
	id, _ := subject["task_id"].(string)
	p, err := v.one("item", "task_id", id)
	if err == errNotFound {
		got.add(notFound)
		return got, nil
	}
	if err != nil {
		return nil, err
	}
	active, _ := p.derived[0]["active_holds"].([]any)
	for _, a := range active {
		holdID, _ := a.(string)
		h, err := v.getHold(holdID)
		if err != nil {
			return nil, err
		}
		switch {
		case h == nil:
			// The Pool derived a hold it does not hold.
			got.add(malformed)
		case h["kind"] != "word":
		case h["state"] != "active":
			// The Pool derived a word hold from records it does not hold as active.
			got.add(malformed)
		default:
			if err := v.judge(h, got); err != nil {
				return nil, err
			}
		}
	}
	return got, nil
}

// authority is the verdict on an authority reference of kind word, by the four
// rules of spec/word.md, The word stands, against the question as the Pool
// holds it.
func (v *verifier) authority(subject map[string]any) (verdict, error) {
	got := verdict{}
	auth, _ := subject["authority"].(map[string]any)
	if auth["kind"] != "word" {
		got.add(notWord)
		return got, nil
	}
	ref, _ := auth["ref"].(map[string]any)
	id, _ := ref["id"].(string)
	revision, hasRevision := ref["record_revision"].(int64)
	if ref["schema"] != "pool.question" || id == "" || !hasRevision {
		got.add(malformed)
		return got, nil
	}
	home := v.c.Request.Scope.HomeID
	if h, ok := ref["home_id"].(string); ok && h != home {
		got.add(notFound)
		return got, nil
	}
	q, err := v.getQuestion(id)
	if err != nil {
		return nil, err
	}
	if q == nil || q["home_id"] != home {
		got.add(notFound)
		return got, nil
	}
	v.keepQuestion(q)
	if current, _ := q["record_revision"].(int64); revision > current {
		got.add(malformed)
		return got, nil
	}
	switch q["state"] {
	case "answered", "released", "completed":
		got.add(appliedBy(q, revision)...)
	default:
		stateReasons(q, got)
	}
	if !inScope(q, v.c.Request.Scope) {
		got.add(outOfScope)
	}
	if len(got) > 0 && !got[malformed] && awaiting(q) {
		got.add(unreconciled)
	}
	return got, nil
}

// appliedBy are the reasons the answer applied to the question does not stand
// for the revision the reference names: none when it was recorded at or before
// that revision; superseded when an answer it replaced, reached by following
// supersedes back from it, was recorded at or before; unanswered when none was.
func appliedBy(q map[string]any, revision int64) []string {
	applied, _ := q["applied"].(map[string]any)
	at, _ := applied["record_revision"].(int64)
	seq, ok := applied["answer_sequence"].(int64)
	if !ok {
		return []string{malformed}
	}
	if at <= revision {
		return nil
	}
	answers, _ := q["answers"].([]any)
	find := func(seq int64) map[string]any {
		for _, a := range answers {
			if answer, _ := a.(map[string]any); answer["sequence"] == seq {
				return answer
			}
		}
		return nil
	}
	cur := find(seq)
	for steps := 0; cur != nil && steps <= len(answers); steps++ {
		replaced, ok := cur["supersedes"].(int64)
		if !ok {
			return []string{unanswered}
		}
		if cur = find(replaced); cur == nil {
			break
		}
		if r, _ := cur["record_revision"].(int64); r <= revision {
			return []string{superseded}
		}
	}
	if cur == nil {
		return []string{malformed}
	}
	return []string{unanswered}
}

// inScope reports whether the scope of the question contains the envelope's:
// a hive question any scope of its home, a workspace question its workspace
// and that workspace's projects, a project question that project.
func inScope(q map[string]any, s envelope.Scope) bool {
	if q["home_id"] != s.HomeID {
		return false
	}
	switch q["scope_level"] {
	case envelope.ScopeHive:
		return true
	case envelope.ScopeWorkspace:
		return s.Level != envelope.ScopeHive && q["workspace_id"] == s.WorkspaceID
	case envelope.ScopeProject:
		return s.Level == envelope.ScopeProject && q["workspace_id"] == s.WorkspaceID && q["project_id"] == s.ProjectID
	}
	return false
}
