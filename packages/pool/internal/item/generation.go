package item

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/admission"
	"github.com/kesk-git/hive/packages/pool/internal/imperative"
	"github.com/kesk-git/hive/packages/pool/internal/read"
)

// The operations open-generation and close-generation of hive-pool
// (spec/pool.md, Generations). A generation is one execution of an item: it is
// opened in the Pool before anything is launched and closed when the caller
// establishes how it ended, and the Pool records only what it is told. The
// generations live in the item record, as read.OpenGeneration reads them.

const (
	codeAdmissionStale    = "pool.admission_stale"
	codeImperativeStale   = "pool.imperative_stale"
	codeGenerationNotOpen = "pool.generation_not_open"

	generationOpened = "pool.generation.opened"
	generationClosed = "pool.generation.closed"
)

// openGeneration opens the next generation of the item under an admitted
// admission and the Imperative revision it read, and moves a queued item to
// in-flight in the same transaction. The dispatch guards of the transition
// table apply at every open, to an in-flight item as to a queued one.
func openGeneration(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	taskID, _ := body["task_id"].(string)
	admissionID, _ := body["admission_id"].(string)
	g, err := loadGraph(c)
	if err != nil {
		return nil, err
	}
	n := g.Items[taskID]
	if n == nil {
		return nil, notFound("item")
	}
	if err := checkScope(c, n.Data); err != nil {
		return nil, err
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	if n.State != "queued" && n.State != "in-flight" {
		return nil, refuse(codeStateTransition, "a generation is opened on a queued or an in-flight item")
	}
	if read.OpenGeneration(n.Data) {
		return nil, envelope.Fail(envelope.Conflict, codeGenerationOpen, "a generation of the item is open")
	}
	if f := guardTransition(c, g, taskID, "in-flight"); f != nil {
		return nil, f
	}
	if g.Derived(taskID) {
		return nil, refuse(codeNotDispatchable, "a Target, and a Prey that has a Hunt beneath it, is never dispatched")
	}

	current, err := imperative.LoadCurrent(c.Store, taskID)
	if err != nil {
		return nil, err
	}
	if !sameCriteria(criteriaOf(current), g.Effective(taskID)) {
		return nil, refuse(codeImperativeStale, "the criteria of the Imperative differ from the effective specification of the item")
	}
	adm, err := admission.Load(c, admissionID)
	if err != nil {
		return nil, err
	}
	itemRead, impRead, ok := adm.LastCheckRead()
	if adm.TaskID() != taskID || !adm.IsAdmitted() || !ok || itemRead != n.Revision || impRead != current.Number {
		return nil, envelope.Fail(envelope.Conflict, codeAdmissionStale, "the admission is not an admitted one of the item whose last check read the current item and Imperative; the caller checks again")
	}
	place, ok := n.Data["placement_requested"].(string)
	if !ok {
		return nil, envelope.Common(envelope.CodeRecordMalformed, "the item carries no requested place").With("record", taskID)
	}

	now, err := c.Stamp()
	if err != nil {
		return nil, internal("a generation cannot be given a time", err)
	}
	next := copyMap(n.Data)
	list, _ := next["generations"].([]any)
	entry := map[string]any{
		"generation": lastGeneration(list) + 1, "state": "open", "opened_at": now,
		"placement_requested": place, "imperative_revision": current.Number, "admission_id": admissionID,
	}
	var evidence []envelope.Evidence
	evidence = append(evidence, envelope.Evidence{Role: "admission", Ref: envelope.RecordRef{
		Schema: "pool.admission", ID: admissionID, HomeID: c.Home.HomeID, RecordRevision: adm.Revision,
	}})
	if by, ok := body["opened_by"].(map[string]any); ok {
		entry["opened_by"] = copyValue(by)
		evidence = append(evidence, envelope.Evidence{Role: "opened-by", Ref: refFromMap(by)})
	}
	next["generations"] = append(list, entry)
	if n.State == "queued" {
		next["state"] = "in-flight"
	}

	b := &builder{c: c}
	final, ref, err := b.put("item", taskID, next, n.Revision)
	if err != nil {
		return nil, err
	}
	if _, err := b.receipt(generationOpened, ref, scopeOf(final), taskID, evidence); err != nil {
		return nil, err
	}
	return b.stage(map[string]any{"item": final, "generation": generationNumbered(final, entry["generation"].(int64))})
}

// closeGeneration records the outcome the caller reports for the open
// generation it names, with the receipt or record that establishes it. The
// item's state does not change: delivery is a transition with its own evidence.
func closeGeneration(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	taskID, _ := body["task_id"].(string)
	number, _ := body["generation"].(int64)
	g, err := loadGraph(c)
	if err != nil {
		return nil, err
	}
	n := g.Items[taskID]
	if n == nil {
		return nil, notFound("item")
	}
	if err := checkScope(c, n.Data); err != nil {
		return nil, err
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	next := copyMap(n.Data)
	list, _ := next["generations"].([]any)
	var entry map[string]any
	for _, v := range list {
		if m, ok := v.(map[string]any); ok && m["generation"] == number && m["state"] == "open" {
			entry = m
		}
	}
	if entry == nil {
		return nil, refuse(codeGenerationNotOpen, "the item has no open generation with that number")
	}

	now, err := c.Stamp()
	if err != nil {
		return nil, internal("a generation cannot be given a time", err)
	}
	closedBy, _ := body["closed_by"].(map[string]any)
	entry["state"], entry["closed_at"], entry["outcome"] = "closed", now, body["outcome"]
	entry["closed_by"] = copyValue(closedBy)

	b := &builder{c: c}
	final, ref, err := b.put("item", taskID, next, n.Revision)
	if err != nil {
		return nil, err
	}
	evidence := []envelope.Evidence{{Role: "closed-by", Ref: refFromMap(closedBy)}}
	if _, err := b.receipt(generationClosed, ref, scopeOf(final), taskID, evidence); err != nil {
		return nil, err
	}
	return b.stage(map[string]any{"item": final, "generation": generationNumbered(final, number)})
}

// lastGeneration is the number of the last generation in the list, 0 for none.
func lastGeneration(list []any) int64 {
	var last int64
	for _, v := range list {
		m, _ := v.(map[string]any)
		if n, _ := m["generation"].(int64); n > last {
			last = n
		}
	}
	return last
}

// generationNumbered is the entry of the stored item with the number.
func generationNumbered(rec map[string]any, number int64) any {
	list, _ := rec["generations"].([]any)
	for _, v := range list {
		if m, _ := v.(map[string]any); m["generation"] == number {
			return m
		}
	}
	return nil
}

// criteriaOf is the specification revision the current revision of the
// Imperative names as its criteria, nil where it names none.
func criteriaOf(cur *imperative.Current) map[string]any {
	task, _ := cur.Named(cur.Number)["task"].(map[string]any)
	standard, _ := task["standard"].(map[string]any)
	criteria, _ := standard["criteria"].(map[string]any)
	return criteria
}

// sameCriteria reports whether the criteria of the Imperative are the effective
// specification of the item: both absent, or both the same binding.
func sameCriteria(criteria, effective map[string]any) bool {
	if criteria == nil || effective == nil {
		return criteria == nil && effective == nil
	}
	return equalValues(criteria, effective)
}
