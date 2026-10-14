package specification

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// The operation close of hive-spec (spec/pool.md, Specifications). After a
// verified landing it records, in one transaction, that the accepted
// requirements were met: the landed head, the landing receipt, the proof
// receipts and the resolved findings the caller names, bound to the accepted
// revision, the item and the generation. Landing and closure are separate: a
// closure that does not commit stays pending for replay under the same request,
// which the harness keys on the request identity, and nothing reports the
// requirements landed because the code merged.

// Close records the closure of the accepted requirements.
func Close(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	specID, _ := body["specification_id"].(string)
	taskID, _ := body["task_id"].(string)
	revision, _ := body["revision"].(int64)
	generation, _ := body["generation"].(int64)

	g, err := loadGraph(c)
	if err != nil {
		return nil, err
	}
	n := g.Items[taskID]
	if n == nil {
		return nil, notFound("item")
	}
	cur, err := loadSpecification(c, specID)
	if err != nil {
		return nil, err
	}
	if err := checkScope(c, n.Data); err != nil {
		return nil, err
	}
	if err := checkScope(c, cur.Data); err != nil {
		return nil, err
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	// The revision is the one the item carries: its own accepted binding or
	// the one it resolves through its parent.
	effective := g.Effective(taskID)
	if effective == nil || effective["specification_id"] != specID || effective["revision"] != revision {
		return nil, refuse(codeSpecificationUnaccepted, "the item carries no accepted binding to the revision")
	}
	known := false
	for _, v := range listOf(n.Data["generations"]) {
		known = known || mapOf(v)["generation"] == generation
	}
	if !known {
		return nil, notFound("generation of the item")
	}
	for _, v := range listOf(cur.Data["closures"]) {
		if cl := mapOf(v); cl["revision"] == revision && cl["task_id"] == taskID && cl["generation"] == generation {
			return nil, envelope.Common(envelope.CodeAlreadyExists, "the requirements of the revision are closed for this item and generation already")
		}
	}

	now, err := c.Stamp()
	if err != nil {
		return nil, internal("a closure cannot be given a time", err)
	}
	receiptID, err := ids.Mint(c.Clock, ids.Receipt)
	if err != nil {
		return nil, internal("a receipt cannot be given an identity", err)
	}
	closure := map[string]any{
		"revision": revision, "task_id": taskID, "generation": generation,
		"landed_revision": body["landed_revision"], "landing": copyValue(body["landing"]),
		"proof": copyValue(body["proof"]), "resolved_findings": copyValue(body["resolved_findings"]),
		"closed_at": now, "closed_by": callerTree(c), "receipt_id": receiptID,
	}
	rec := copyMap(cur.Data)
	rec["closures"] = append(listOf(rec["closures"]), closure)

	b := &builder{c: c}
	final, ref, err := b.put("specification", specID, rec, cur.Revision)
	if err != nil {
		return nil, err
	}
	evidence := []envelope.Evidence{evidenceRef("landing", mapOf(body["landing"]))}
	for _, v := range listOf(body["proof"]) {
		evidence = append(evidence, evidenceRef("proof", mapOf(v)))
	}
	for _, v := range listOf(body["resolved_findings"]) {
		evidence = append(evidence, evidenceRef("finding", mapOf(v)))
	}
	scope := scopeOf(n.Data)
	scope.Generation = generation
	if err := b.receiptAs(receiptID, "pool.specification.closed", ref, scope, taskID, evidence); err != nil {
		return nil, err
	}
	return b.stage(map[string]any{"specification": final, "closure": closure})
}
