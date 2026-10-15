package admission

import (
	"fmt"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/pool/internal/imperative"
	"github.com/kesk-git/hive/packages/pool/internal/read"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The operation check of hive-admit (spec/pool.md, Admission). It runs the
// checks of the table against the Imperative revision the admission is bound
// to, the item as it stands and the evidence in the request, appends the check
// to the record and writes one receipt: pool.admission.admitted, refused or
// held. The response status follows: ok, refused with the first finding's code,
// or held with the hold in error.hold. An earlier check is never changed. A
// held check places a hold of kind admission; a check that admits releases the
// holds of kind admission on the item, by its own receipt.

// Check runs the checks against the admission the request names.
func Check(c *cli.Call) (*cli.Result, error) {
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	body := c.Request.Body
	id := str(body["admission_id"])
	rec, err := Load(c, id)
	if err != nil {
		return nil, err
	}
	if err := checkScope(c, rec.Scope()); err != nil {
		return nil, err
	}
	taskID := rec.TaskID()

	graph, omitted, err := read.LoadGraph(c.Store, c.Clock.Now())
	if err != nil {
		return nil, poolstore.Failure("the Pool", err)
	}
	if len(omitted) > 0 {
		return nil, malformed("a record of the Pool cannot be read, so the item cannot be known as it stands", omitted[0].ID)
	}
	item := graph.Items[taskID]
	if item == nil {
		return nil, notFound("item")
	}
	imp, err := imperative.LoadCurrent(c.Store, taskID)
	if err != nil {
		return nil, err
	}
	rev := imp.Named(integer(rec.Data["imperative_revision"]))
	if rev == nil {
		return nil, malformed("the Imperative revision the admission is bound to cannot be read", taskID)
	}

	f := &facts{c: c, rec: rec, graph: graph, item: item, rev: rev, current: imp.Number}
	if err := f.present(taskID); err != nil {
		return nil, err
	}
	placement := tree(body["placement"])
	if f.place, err = placeChecked(f, placement); err != nil {
		return nil, err
	}
	if f.branch = tree(body["source_branch"]); f.branch != nil {
		f.branch = copyTree(f.branch).(map[string]any)
	}
	if f.repository, err = repositoryOf(c, rec, rev); err != nil {
		return nil, err
	}
	f.authorityKind = str(tree(tree(rec.Data["request"])["authority"])["kind"])
	if f.authority, err = authorityStanding(c.Store, rec); err != nil {
		return nil, poolstore.Failure("the authority of the admission", err)
	}

	rows := f.run()
	result, reason := resultAdmitted, ""
	var findings []any
	for _, r := range rows {
		for _, fd := range r.findings {
			findings = append(findings, fd.tree())
		}
	}
	if len(rows) > 0 {
		result, reason = rows[0].result, rows[0].reason
	}
	if findings == nil {
		findings = []any{}
	}
	return conclude(c, f, placement, result, reason, findings)
}

// placeChecked is the place the check is about. Of kind requested it is the
// place the item requests, and the request names no other; of kind resolved it
// is the place the caller's policy resolved. A request for codex-cloud, by the
// item or by the Imperative's Setup, is never substituted: it is the place
// checked whatever the placement names.
func placeChecked(f *facts, placement map[string]any) (string, error) {
	value := str(placement["value"])
	if placement["kind"] == "requested" && value != f.itemPlace() {
		return "", envelope.Common(envelope.CodeSchemaViolation, "a placement of kind requested names the place the item requests")
	}
	if f.itemPlace() == placeCodexCloud || str(f.section("setup", "placement")["requested"]) == placeCodexCloud {
		return placeCodexCloud, nil
	}
	return value, nil
}

// repositoryOf is the kind of repository the work's project binds, read from
// the project record; for the Hive's own work at no project, the one the
// Imperative's Setup gives.
func repositoryOf(c *cli.Call, rec *Record, rev map[string]any) (string, error) {
	if project := rec.Scope().ProjectID; project != "" {
		return repositoryKind(c, project)
	}
	return str(tree(tree(rev["setup"])["repository"])["kind"]), nil
}

// conclude appends the check to the admission, places or releases holds, writes
// the receipt and the event, and ends the request with the status the result
// gives. An admitted check answers ok with its body; a refused or a held one
// ends with the refusal or the hold, which carries no body, and its writes,
// which the harness commits with the recorded outcome in one transaction, as
// it does an admitted one's.
func conclude(c *cli.Call, f *facts, placement map[string]any, result, reason string, findings []any) (*cli.Result, error) {
	rec, taskID := f.rec, f.rec.TaskID()
	now, err := c.Stamp()
	if err != nil {
		return nil, internal("a check cannot be given a time", err)
	}
	kindOfReceipt := map[string]string{resultAdmitted: receiptAdmitted, resultRefused: receiptRefused, resultHeld: receiptHeld}[result]
	b := &builder{c: c}
	r, err := b.receipt(kindOfReceipt, []envelope.Evidence{
		{Role: "item", Ref: envelope.RecordRef{Schema: "pool.item", ID: taskID, HomeID: c.Home.HomeID, RecordRevision: f.item.Revision}},
		{Role: "imperative", Ref: envelope.RecordRef{Schema: "pool.imperative", ID: taskID, HomeID: c.Home.HomeID}},
	})
	if err != nil {
		return nil, err
	}

	checks := list(rec.Data["checks"])
	check := map[string]any{
		"sequence": int64(len(checks) + 1), "checked_at": now, "request_id": c.Request.RequestID,
		"imperative_revision": rec.Data["imperative_revision"], "item_revision": f.item.Revision,
		"placement": copyTree(placement), "result": result, "findings": findings, "receipt_id": r.ReceiptID,
	}
	if f.branch != nil {
		check["source_branch"] = f.branch
	}
	if effective := f.graph.Effective(f.item.ID); effective != nil {
		check["specification"] = copyTree(effective)
	}
	if digest, ok := c.Request.Body["rules_digest"]; ok {
		check["rules_digest"] = digest
	}

	scope := rec.Scope()
	var holdRef *envelope.RecordRef
	if result == resultHeld {
		if holdRef, err = holdFor(b, f, reason, now, scope, r.ReceiptID, findings); err != nil {
			return nil, err
		}
		check["hold_id"] = holdRef.ID
	}
	if result == resultAdmitted {
		if err := releaseHolds(b, f, now, r.ReceiptID, scope); err != nil {
			return nil, err
		}
	}

	next := copyTree(rec.Data).(map[string]any)
	next["checks"] = append(checks, check)
	next["state"], next["updated_at"] = result, now
	final, ref, err := b.put(kind, str(rec.Data["admission_id"]), next, rec.Revision)
	if err != nil {
		return nil, err
	}
	scope.TaskID = taskID
	if err := b.seal(r, ref, scope, eventChecked); err != nil {
		return nil, err
	}
	staged, err := b.stage()
	if err != nil {
		return nil, err
	}
	if result == resultAdmitted {
		return staged.Result(map[string]any{"admission": final, "check": check}), nil
	}
	message := clip(findingMessage(findings))
	if result == resultHeld {
		return staged.Ended(envelope.Hold(*holdRef, message)), nil
	}
	return staged.Ended(envelope.Fail(envelope.Refused, str(tree(findings[0])["code"]), message)), nil
}

// holdFor names the hold a held check waits on. An active hold of kind
// admission that this admission already holds for the reason is reused and
// nothing is placed; otherwise one is placed, with the event that places a hold,
// whose basis is the check's receipt.
func holdFor(b *builder, f *facts, reason, now string, scope envelope.Scope, receiptID string, findings []any) (*envelope.RecordRef, error) {
	c, rec := b.c, f.rec
	for _, h := range f.graph.Holds[f.item.ID] {
		if h.Kind == "admission" && h.State == "active" && h.Data["admission_id"] == rec.Data["admission_id"] && h.Data["reason_code"] == reason {
			return &envelope.RecordRef{Schema: "pool.hold", ID: h.ID, HomeID: c.Home.HomeID, RecordRevision: h.Revision}, nil
		}
	}
	holdID, err := ids.Mint(c.Clock, ids.Hold)
	if err != nil {
		return nil, internal("a hold cannot be given an identity", err)
	}
	hold := map[string]any{
		"schema": "pool.hold", "schema_version": versionOne, "record_revision": int64(1),
		"hold_id": holdID, "task_id": rec.TaskID(), "kind": "admission", "state": "active",
		"reason": clip(fmt.Sprintf("The admission check held dispatch: %s", findingMessage(findings))), "reason_code": reason,
		"admission_id": rec.Data["admission_id"], "placed_at": now,
		"placed_by":  map[string]any{"request_id": c.Request.RequestID, "caller": c.Request.Tree()["caller"]},
		"created_at": now, "updated_at": now,
	}
	putScope(hold, scope, false)
	_, ref, err := b.put("hold", holdID, hold, 0)
	if err != nil {
		return nil, err
	}
	scope.TaskID = rec.TaskID()
	if err := b.event(eventPlaced, receiptID, scope); err != nil {
		return nil, err
	}
	return ref, nil
}

// releaseHolds releases every active hold of kind admission on the item,
// whichever admission of the item placed it, by the receipt that admits, with
// the event that releases a hold on that receipt: an admission of a corrected
// Imperative obsoletes the holds of the revisions it corrects.
func releaseHolds(b *builder, f *facts, now, receiptID string, scope envelope.Scope) error {
	scope.TaskID = f.rec.TaskID()
	for _, h := range f.graph.Holds[f.item.ID] {
		if h.Kind != "admission" || h.State != "active" {
			continue
		}
		next := copyTree(h.Data).(map[string]any)
		next["state"], next["released_at"], next["updated_at"] = "released", now, now
		next["released_by"] = map[string]any{"schema": schemaReceipt, "id": receiptID}
		if _, _, err := b.put("hold", h.ID, next, h.Revision); err != nil {
			return err
		}
		if err := b.event(eventReleased, receiptID, scope); err != nil {
			return err
		}
	}
	return nil
}

// findingMessage is the message of the first finding.
func findingMessage(findings []any) string {
	if len(findings) == 0 {
		return ""
	}
	return str(tree(findings[0])["message"])
}

// clip keeps a message within what a response and a hold take.
func clip(s string) string {
	if r := []rune(s); len(r) > maxMessageRunes {
		return string(r[:maxMessageRunes])
	}
	return s
}
