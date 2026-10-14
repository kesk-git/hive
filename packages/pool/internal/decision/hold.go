package decision

import (
	"cmp"
	"errors"
	"slices"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The operations hold-question and revise-question of hive-pool, two of the six
// the Mind's word persists through (spec/word.md, How word persists through the
// Pool, hold and revise). Each is one transaction under the store's lock, and
// each accepts only a handoff from hive-word. The package registers them from
// its init function, as it does the readers of its kinds, so that importing it
// builds them into the executable.

func init() { RegisterQuestions(entry.Default) }

// RegisterQuestions builds the operations hold-question and revise-question of
// hive-pool into the registry.
func RegisterQuestions(r *entry.Registry) {
	r.RegisterOperation("hive-pool", operation("hold-question", holdQuestion, "pool.question.held", "pool.hold.placed", "pool.item.added"))
	r.RegisterOperation("hive-pool", operation("revise-question", reviseQuestion, "pool.question.revised"))
}

// The codes of the Pool that these operations return (spec/word.md, Error
// codes), and the words the records and receipts they write name things by.
const (
	codeOptionUnknown     = "pool.option_unknown"
	codeRoundClosed       = "pool.round_closed"
	codeRoundIncomplete   = "pool.round_incomplete"
	codeHeldItemArchived  = "pool.held_item_archived"
	codeQuestionAnswered  = "pool.question_answered"
	codeQuestionClosed    = "pool.question_closed"
	versionOne            = "1.0"
	schemaReceipt         = "hive.receipt"
	schemaRequest         = "hive.request"
	handoffPackage        = "word"
	handoffCommand        = "hive-word"
	heldItemWork          = "work"
	heldItemDecisionItem  = "decision-item"
	holdKindWord          = "word"
	holdStateActive       = "active"
	itemStateArchived     = "archived"
	defaultPlacementValue = "policy"
)

func operation(name string, handle func(*cli.Call) (*cli.Result, error), receipts ...string) *cli.Operation {
	return &cli.Operation{
		OperationDecl: envelope.OperationDecl{
			Name:           name,
			State:          envelope.Supported,
			Mode:           envelope.Unary,
			Mutates:        true,
			Versions:       []string{versionOne},
			RequestSchema:  "urn:hive:schema:ops:hive-pool." + name + ".request:v1",
			ResponseSchema: "urn:hive:schema:ops:hive-pool." + name + ".response:v1",
			ReceiptKinds:   receipts,
			Executes:       []string{},
			Calls:          []string{},
		},
		Handle: handle,
	}
}

func refuse(code, message string) *envelope.Failure {
	return envelope.Fail(envelope.Refused, code, message)
}

func notFound(what string) *envelope.Failure {
	return envelope.Common(envelope.CodeNotFound, "no "+what+" has that identity")
}

func internal(what string, err error) *envelope.Failure {
	return envelope.Common(envelope.CodeInternal, what).Because(err)
}

func malformed(what string) *envelope.Failure {
	return envelope.Common(envelope.CodeRecordMalformed, "the "+what+" cannot be read")
}

// checkHandoff refuses, with authority_insufficient, any request that is not a
// handoff from hive-word: the caller names the package word and its command
// hive-word, and the causation is a hive.request of the request's own
// request_id with a digest (spec/commands.md, A command with no store). It is
// attribution and not authentication, and it is made before anything is read.
func checkHandoff(c *cli.Call) error {
	r, cause := c.Request, c.Request.Causation
	if r.Caller.Kind == envelope.CallerPackage && r.Caller.Package == handoffPackage && r.Caller.Command == handoffCommand &&
		cause != nil && cause.Schema == schemaRequest && cause.ID == r.RequestID && cause.Digest != "" {
		return nil
	}
	return envelope.Common(envelope.CodeAuthorityInsufficient, "this operation is accepted only as a handoff from hive-word")
}

// holdQuestion raises a question at revision 1, its card in a decision round
// and the word hold on the item it gates, in one transaction (spec/word.md,
// hold). Every check that reads a record is made here, under the lock.
func holdQuestion(c *cli.Call) (*cli.Result, error) {
	if err := checkHandoff(c); err != nil {
		return nil, err
	}
	body := c.Request.Body
	qb, rb := asMap(body["question"]), asMap(body["round"])
	if err := checkExpected(c); err != nil {
		return nil, err
	}

	// The item held: the one named, which must exist, lie within the request's
	// scope and not be archived; or the decision item this transaction adds.
	var item *poolstore.Loaded
	if id, ok := qb["held_task_id"].(string); ok {
		l, err := load(c, "item", id, "item")
		if err != nil {
			return nil, err
		}
		if err := checkScope(c, scopeOf(l.Data)); err != nil {
			return nil, err
		}
		if l.Data["state"] == itemStateArchived {
			return nil, refuse(codeHeldItemArchived, "a question cannot hold an archived item")
		}
		item = l
	}
	options := asList(qb["options"])
	recommendation, _ := qb["recommendation"].(string)
	if err := checkOptions(options, recommendation); err != nil {
		return nil, err
	}

	// Identities that already exist.
	questionID, _ := qb["question_id"].(string)
	roundID, _ := rb["decision_round_id"].(string)
	cardID, _ := body["card_id"].(string)
	newRound, _ := rb["new"].(bool)
	if found, err := exists(c, "question", questionID); err != nil || found {
		return nil, existsOr(err, "question")
	}
	if found, err := exists(c, "decision-round", roundID); err != nil || (found && newRound) {
		return nil, existsOr(err, "decision round")
	}
	if err := checkSubjectOrigin(c, asMap(qb["origin"])); err != nil {
		return nil, err
	}
	rounds, omitted, err := poolstore.LoadAll(c.Store, "decision-round")
	if err != nil {
		return nil, poolstore.Failure("the decision rounds", err)
	}
	if len(omitted) > 0 {
		return nil, malformed("decision round " + omitted[0].ID)
	}
	for _, r := range rounds {
		for _, card := range asList(r.Data["cards"]) {
			if asMap(card)["card_id"] == cardID {
				return nil, existsOr(nil, "card")
			}
		}
	}

	// The round the card joins: an existing one must be open and complete.
	var joined *poolstore.Loaded
	if !newRound {
		l, err := load(c, "decision-round", roundID, "decision round")
		if err != nil {
			return nil, err
		}
		if err := checkScope(c, scopeOf(l.Data)); err != nil {
			return nil, err
		}
		switch {
		case l.Data["state"] != RoundOpen:
			return nil, refuse(codeRoundClosed, "a card cannot join a closed round")
		case l.Data["complete"] != true:
			return nil, refuse(codeRoundIncomplete, "the round was not collected whole, so it takes no new card")
		}
		joined = l
	}

	now, err := c.Stamp()
	if err != nil {
		return nil, internal("a question cannot be given a time", err)
	}
	b := &builder{c: c}

	// The item, added for a question with its own scope.
	var (
		heldID, heldItem string
		heldRevision     int64
		itemRecord       map[string]any
	)
	if item != nil {
		heldID, heldItem, heldRevision, itemRecord = item.ID, heldItemWork, item.Revision, item.Data
	} else {
		heldItem = heldItemDecisionItem
		if heldID, itemRecord, err = b.addDecisionItem(asMap(qb["new_item"]), now); err != nil {
			return nil, err
		}
		heldRevision = 1
	}
	itemScope := scopeOf(itemRecord)

	// The hold: the held item's earliest active word hold that no question
	// names, or a new one.
	hold, bound, err := earliestUnnamedWordHold(c, heldID)
	if err != nil {
		return nil, err
	}
	requestedBy := asMap(body["requested_by"])
	if !bound {
		reason, _ := body["hold_reason"].(string)
		if reason == "" {
			reason, _ = qb["text"].(string)
		}
		var id string
		if id, hold, err = newWordHold(c, itemScope, heldID, reason, requestedBy, now); err != nil {
			return nil, err
		}
		if hold, err = b.write("hold", id, hold, 0, itemScope.envelope(), heldID, "pool.hold.placed"); err != nil {
			return nil, err
		}
	}
	holdID, _ := hold["hold_id"].(string)

	// The question, at revision 1.
	text, _ := qb["text"].(string)
	consequence, _ := qb["consequence"].(string)
	amount, _ := qb["amount"].(string)
	askedAt, _ := qb["asked_at"].(string)
	question := &Question{
		QuestionID: questionID, Scope: itemScope, HeldTaskID: heldID, HeldItem: heldItem, HoldID: holdID,
		DecisionRoundID: roundID, CardID: cardID, Origin: asMap(copyTree(qb["origin"])), CurrentRevision: 1,
		Revisions: []Revision{{
			Revision: 1, Text: text, Consequence: consequence, Recommendation: recommendation, Options: readOptions(options),
			Amount: amount, AskedAt: askedAt, AskedBy: asMap(copyTree(requestedBy)), RequestID: c.Request.RequestID,
		}},
		State: StateOpen, Answers: []Answer{}, CreatedAt: now, UpdatedAt: now,
	}
	card := Card{
		CardID: cardID, CardRevision: 1, QuestionID: questionID, QuestionRevision: 1,
		HeldTaskID: heldID, HeldTaskRevision: heldRevision, PresentedAt: now, State: CardOpen,
	}

	// The round: opened with this card, or the card added to it.
	var round *Round
	var roundRevision int64
	if newRound {
		round = &Round{
			DecisionRoundID: roundID, Scope: scopeOfRequest(c), Origin: asMap(copyTree(rb["origin"])),
			Attachments: readAttachments(asList(rb["attachments"])), Complete: rb["complete"] == true,
			State: RoundOpen, OpenedAt: now, CreatedAt: now, UpdatedAt: now,
		}
		round.IncompleteReason, _ = rb["incomplete_reason"].(string)
	} else {
		if round, err = ParseRound(joined.Data); err != nil {
			return nil, malformed("decision round").Because(err)
		}
		roundRevision = joined.Revision
		round.UpdatedAt = now
	}
	round.Cards = append(round.Cards, card)

	finalQuestion, err := b.write("question", questionID, question.Tree(), 0, question.Scope.envelope(), heldID, "pool.question.held")
	if err != nil {
		return nil, err
	}
	finalRound, err := b.write("decision-round", roundID, round.Tree(), roundRevision, envelope.Scope{}, "", "")
	if err != nil {
		return nil, err
	}
	return b.stage(map[string]any{"question": finalQuestion, "round": finalRound, "hold": hold, "item": itemRecord})
}

// subjectOf is the subject a question's origin names, the durable key of a
// second-miss question (spec/pool.md, Generated knowledge): an origin of kind
// rule whose ref is a pool.subject. ok is false for any other origin.
func subjectOf(origin map[string]any) (id string, ok bool) {
	ref := asMap(origin["ref"])
	if origin["kind"] != "rule" || ref["schema"] != "pool.subject" {
		return "", false
	}
	id, _ = ref["id"].(string)
	return id, id != ""
}

// checkSubjectOrigin refuses, with already_exists naming the earlier question,
// a question whose origin names a subject that a stored question already names,
// whatever the held item, decision item or round of either.
func checkSubjectOrigin(c *cli.Call, origin map[string]any) error {
	subject, ok := subjectOf(origin)
	if !ok {
		return nil
	}
	questions, omitted, err := poolstore.LoadAll(c.Store, "question")
	if err != nil {
		return poolstore.Failure("the questions", err)
	}
	if len(omitted) > 0 {
		return malformed("question " + omitted[0].ID)
	}
	for _, q := range questions {
		if earlier, ok := subjectOf(asMap(q.Data["origin"])); ok && earlier == subject {
			return envelope.Common(envelope.CodeAlreadyExists, "an earlier question already names that subject in its origin").With("question_id", q.ID)
		}
	}
	return nil
}

// addDecisionItem adds the decision item of a question with its own scope, in
// the request's scope: its title and body, deliverable decision, no level,
// definition not-required, queued. It returns the identity and the record as
// written.
func (b *builder) addDecisionItem(spec map[string]any, now string) (string, map[string]any, error) {
	id, err := ids.Mint(b.c.Clock, ids.Task)
	if err != nil {
		return "", nil, internal("an item cannot be given an identity", err)
	}
	rec := map[string]any{
		"schema": "pool.item", "schema_version": versionOne, "record_revision": int64(1),
		"task_id": id, "title": spec["title"], "state": "queued", "deliverable": "decision", "definition": "not-required",
		"placement_requested": defaultPlacementValue, "deps": []any{}, "links": []any{}, "generations": []any{},
		"created_at": now, "updated_at": now,
	}
	if text, ok := spec["body"]; ok {
		rec["body"] = text
	}
	scope := b.c.Request.Scope
	scope.TaskID, scope.Generation = "", 0
	putScope(rec, scope, true)
	final, err := b.write("item", id, rec, 0, scopeEnvelopeOf(rec), id, "pool.item.added")
	return id, final, err
}

// earliestUnnamedWordHold is the held item's active word hold that no question
// names, the earliest placed where there are several, as a legacy import
// leaves them. bound is false when there is none.
func earliestUnnamedWordHold(c *cli.Call, taskID string) (hold map[string]any, bound bool, err error) {
	holds, omitted, err := poolstore.LoadAll(c.Store, "hold")
	if err != nil {
		return nil, false, poolstore.Failure("the holds", err)
	}
	questions, omittedQuestions, err := poolstore.LoadAll(c.Store, "question")
	if err != nil {
		return nil, false, poolstore.Failure("the questions", err)
	}
	if len(omitted)+len(omittedQuestions) > 0 {
		return nil, false, envelope.Common(envelope.CodeRecordMalformed, "a hold or a question of the Pool cannot be read, so the holds a question names cannot be known")
	}
	named := map[string]bool{}
	for _, q := range questions {
		id, _ := q.Data["hold_id"].(string)
		named[id] = true
	}
	var candidates []poolstore.Loaded
	for _, h := range holds {
		if h.Data["task_id"] == taskID && h.Data["kind"] == holdKindWord && h.Data["state"] == holdStateActive && !named[h.ID] {
			candidates = append(candidates, h)
		}
	}
	if len(candidates) == 0 {
		return nil, false, nil
	}
	slices.SortFunc(candidates, func(a, b poolstore.Loaded) int {
		x, y := a.Data["placed_at"].(string), b.Data["placed_at"].(string)
		return cmp.Or(cmp.Compare(x, y), cmp.Compare(a.ID, b.ID))
	})
	return candidates[0].Data, true, nil
}

// newWordHold is a word hold record on the item, active, as the request placed
// it: the reason is for a person, and the caller of the hive-word request is
// the one who issued it.
func newWordHold(c *cli.Call, scope Scope, taskID, reason string, requestedBy map[string]any, now string) (string, map[string]any, error) {
	id, err := ids.Mint(c.Clock, ids.Hold)
	if err != nil {
		return "", nil, internal("a hold cannot be given an identity", err)
	}
	rec := map[string]any{
		"schema": "pool.hold", "schema_version": versionOne, "record_revision": int64(1),
		"hold_id": id, "task_id": taskID, "kind": holdKindWord, "reason": reason, "state": holdStateActive,
		"placed_at":  now,
		"placed_by":  map[string]any{"request_id": c.Request.RequestID, "caller": copyTree(requestedBy)},
		"created_at": now, "updated_at": now,
	}
	scope.into(rec)
	return id, rec, nil
}

// checkOptions refuses options whose identities repeat and a recommendation
// that is not one of them: pool.option_unknown.
func checkOptions(options []any, recommendation string) error {
	seen := map[string]bool{}
	for _, o := range options {
		id, _ := asMap(o)["option_id"].(string)
		if seen[id] {
			return envelope.Fail(envelope.Invalid, codeOptionUnknown, "the options of a question repeat an identity")
		}
		seen[id] = true
	}
	if len(options) > 0 && !seen[recommendation] {
		return envelope.Fail(envelope.Invalid, codeOptionUnknown, "the recommendation of a question is not one of its options")
	}
	return nil
}

func readOptions(options []any) []Option {
	out := make([]Option, 0, len(options))
	for _, v := range options {
		o := asMap(v)
		id, _ := o["option_id"].(string)
		label, _ := o["label"].(string)
		consequence, _ := o["consequence"].(string)
		out = append(out, Option{OptionID: id, Label: label, Consequence: consequence})
	}
	return out
}

func readAttachments(attachments []any) []Attachment {
	out := make([]Attachment, 0, len(attachments))
	for _, v := range attachments {
		a := asMap(v)
		fetched, _ := a["fetched"].(string)
		out = append(out, Attachment{File: asMap(copyTree(a["file"])), Fetched: fetched})
	}
	return out
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// scopeOf is the scope a record carries, without the clone, which these
// records do not.
func scopeOf(rec map[string]any) Scope {
	s := Scope{}
	s.Level, _ = rec["scope_level"].(string)
	s.HomeID, _ = rec["home_id"].(string)
	s.WorkspaceID, _ = rec["workspace_id"].(string)
	s.ProjectID, _ = rec["project_id"].(string)
	return s
}

// scopeOfRequest is the scope of the request, without the clone, the task and
// the generation: the scope a round is opened at.
func scopeOfRequest(c *cli.Call) Scope {
	r := c.Request.Scope
	return Scope{Level: r.Level, HomeID: r.HomeID, WorkspaceID: r.WorkspaceID, ProjectID: r.ProjectID}
}

func (s Scope) envelope() envelope.Scope {
	return envelope.Scope{Level: s.Level, HomeID: s.HomeID, WorkspaceID: s.WorkspaceID, ProjectID: s.ProjectID}
}

// putScope writes the scope's flat members into a record.
func putScope(m map[string]any, s envelope.Scope, clone bool) {
	m["scope_level"], m["home_id"] = s.Level, s.HomeID
	if s.WorkspaceID != "" {
		m["workspace_id"] = s.WorkspaceID
	}
	if s.ProjectID != "" {
		m["project_id"] = s.ProjectID
	}
	if clone && s.CloneID != "" {
		m["clone_id"] = s.CloneID
	}
}

// checkScope refuses a request whose scope does not contain the scope of the
// record it acts on, before anything is written: a hive-scope request contains
// every record of its home, a workspace-scope request its workspace and that
// workspace's projects, a project-scope request only that project.
func checkScope(c *cli.Call, have Scope) error {
	want := c.Request.Scope
	in := have.HomeID == want.HomeID
	switch want.Level {
	case envelope.ScopeWorkspace:
		in = in && have.WorkspaceID == want.WorkspaceID
	case envelope.ScopeProject:
		in = in && have.Level == envelope.ScopeProject && have.WorkspaceID == want.WorkspaceID && have.ProjectID == want.ProjectID
	}
	if !in {
		return envelope.Common(envelope.CodeScopeMismatch, "the record is outside the scope of the request")
	}
	return nil
}

// checkExpected compares every expected revision the request supplies with the
// record it names, whether or not the operation writes that record. A record
// the Pool does not hold is not at the revision named.
func checkExpected(c *cli.Call) error {
	for _, e := range c.Request.ExpectedRevisions {
		stale := envelope.Common(envelope.CodeRevisionConflict, "the record is not at the revision the request expects")
		kind, ok := strings.CutPrefix(e.Schema, "pool.")
		if _, known := poolstore.Lookup(kind); !ok || !known {
			return stale
		}
		l, om, err := poolstore.Load(c.Store, kind, e.ID)
		switch {
		case errors.Is(err, poolstore.ErrNotFound):
			return stale
		case err != nil:
			return poolstore.Failure("a record the request expects", err)
		case om != nil:
			return envelope.Common(envelope.CodeRecordMalformed, "a record the request expects cannot be read").With("record", om.ID)
		case l.Revision != e.RecordRevision:
			return stale
		}
	}
	return nil
}

// load reads a record that must exist and be readable.
func load(c *cli.Call, kind, id, what string) (*poolstore.Loaded, error) {
	l, om, err := poolstore.Load(c.Store, kind, id)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return nil, notFound(what)
	case err != nil:
		return nil, poolstore.Failure("the "+what, err)
	case om != nil:
		return nil, malformed(what)
	}
	return l, nil
}

// exists reports whether a record with the identity is stored, readable or
// not.
func exists(c *cli.Call, kind, id string) (bool, error) {
	_, _, err := poolstore.Load(c.Store, kind, id)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return false, nil
	case err != nil:
		return false, poolstore.Failure("the "+kind, err)
	}
	return true, nil
}

// existsOr is the failure of an identity that exists, or the error that kept
// the check from being made.
func existsOr(err error, what string) error {
	if err != nil {
		return err
	}
	return envelope.Common(envelope.CodeAlreadyExists, "a "+what+" already has that identity")
}

// builder gathers the transaction of one request.
type builder struct {
	c  *cli.Call
	tx poolstore.Transaction
}

// write adds the record to the transaction, finalized, and returns it as the
// store will write it. With a receipt kind it also adds the receipt and the
// event that establish it, about the record, in the scope and for the task
// given.
func (b *builder) write(kind, id string, body map[string]any, expected int64, scope envelope.Scope, taskID, receipt string) (map[string]any, error) {
	w := b.c.Writer()
	body["writer"] = map[string]any{"package": w.Package, "command": w.Command, "operation": w.Operation, "implementation_version": w.ImplementationVersion}
	k, ok := poolstore.Lookup(kind)
	if !ok {
		return nil, internal("a record cannot be finalized", errors.New(kind+" is not a kind"))
	}
	path, err := k.Path(id)
	if err != nil {
		return nil, internal("a record cannot be finalized", err)
	}
	content, err := canon.Encode(body)
	if err != nil {
		return nil, internal("a record cannot be finalized", err)
	}
	rw, digest, err := b.c.Finalize(cstore.RecordWrite{Path: path, Content: content, Expected: expected})
	if err != nil {
		return nil, internal("a record cannot be finalized", err)
	}
	v, err := canon.Parse(rw.Content)
	if err != nil {
		return nil, internal("a record cannot be finalized", err)
	}
	final := v.(map[string]any)
	b.tx.Records = append(b.tx.Records, poolstore.Write{Kind: kind, ID: id, Body: final, Expected: expected})
	if receipt == "" {
		return final, nil
	}
	ref := &envelope.RecordRef{Schema: "pool." + kind, ID: id, HomeID: b.c.Home.HomeID, RecordRevision: expected + 1, Digest: digest}
	if err := b.receipt(receipt, ref, scope, taskID); err != nil {
		return nil, err
	}
	return final, nil
}

// scopeEnvelopeOf is the scope of a record with the clone it carries.
func scopeEnvelopeOf(rec map[string]any) envelope.Scope {
	s := scopeOf(rec).envelope()
	s.CloneID, _ = rec["clone_id"].(string)
	return s
}

// receipt adds the receipt of the kind about the subject, with the event the
// Pool registers beside it, to the transaction. Both take the subject's scope
// and task_id.
func (b *builder) receipt(kind string, subject *envelope.RecordRef, scope envelope.Scope, taskID string) error {
	r, err := b.c.NewReceipt(kind, versionOne, subject, nil, nil)
	if err != nil {
		return internal("a receipt cannot be made", err)
	}
	r.Scope = scope
	r.Scope.TaskID = taskID
	b.tx.Receipts = append(b.tx.Receipts, r)
	eventID, err := ids.Mint(b.c.Clock, ids.Event)
	if err != nil {
		return internal("an event cannot be made", err)
	}
	now, err := b.c.Stamp()
	if err != nil {
		return internal("an event cannot be made", err)
	}
	event := map[string]any{
		"schema": "hive.event", "schema_version": versionOne, "event_id": eventID,
		"kind": kind, "kind_version": versionOne,
		"source":         map[string]any{"package": poolstore.Package, "source_id": poolstore.Package},
		"correlation_id": b.c.Request.CorrelationID,
		"causation":      map[string]any{"schema": schemaRequest, "id": b.c.Request.RequestID},
		"occurred_at":    now, "observed_at": now,
		"basis": map[string]any{"kind": "receipt", "ref": map[string]any{"schema": schemaReceipt, "id": r.ReceiptID}},
		"body":  map[string]any{},
	}
	putScope(event, scope, true)
	event["task_id"] = taskID
	content, err := canon.Encode(event)
	if err != nil {
		return internal("an event cannot be written", err)
	}
	b.tx.Events = append(b.tx.Events, cstore.Event{ID: eventID, Content: content})
	return nil
}

// stage checks the transaction and returns the handler's result. No record is
// staged that lies outside the scope of the request.
func (b *builder) stage(body map[string]any) (*cli.Result, error) {
	for _, w := range b.tx.Records {
		if err := checkScope(b.c, scopeOf(w.Body)); err != nil {
			return nil, err
		}
	}
	staged, err := poolstore.Stage(b.c, b.tx)
	if err != nil {
		return nil, err
	}
	return staged.Result(body), nil
}
