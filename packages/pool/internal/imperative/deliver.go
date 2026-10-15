package imperative

import (
	"errors"
	"slices"
	"unicode/utf8"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/memory"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The operation deliver of hive-imperative (spec/pool.md, The Imperative). It
// records exactly what one generation was given: the revision, the rendered
// digest and size, the renderer version, the transport and the dispatcher's
// receipt, with the rules and revisions the rendering wrote and the subject
// projections it embedded. The request carries the exact rendered text
// transiently: the Pool checks its digest, its size in UTF-8 bytes and that its
// labeled inventories agree with the ones the request gives, and discards it
// without keeping or logging it. In the same transaction it records each
// embedded projection received for the generation at the digest rendered, so that
// a rule a worker was given in its Imperative is never counted as missed. It is
// the dispatcher's to call at a later stage; here a test caller proves it.

// Deliver records one delivery of a revision to a generation.
func Deliver(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	taskID, generation, number := strOf(body["task_id"]), intOf(body["generation"]), intOf(body["revision"])

	// A new delivery carries the exact text and both inventories and the instant
	// of the rendering; a record without them proves no rule received.
	for _, name := range []string{"rendered_text", "rules", "subject_projections", "rendered_at"} {
		if _, ok := body[name]; !ok {
			return nil, invalid("a deliver request carries " + name + ", which the rendering returned")
		}
	}
	if err := checkExpected(c); err != nil {
		return nil, err
	}
	l, err := loadImperative(c.Store, taskID)
	if err != nil {
		return nil, err
	}
	if err := checkScope(c, l.Data); err != nil {
		return nil, err
	}
	if revisionNamed(l.Data, number) == nil {
		return nil, notFound("revision")
	}

	if err := checkOpen(c, taskID, generation); err != nil {
		return nil, err
	}

	text := strOf(body["rendered_text"])
	if !utf8.ValidString(text) || canon.FileDigest([]byte(text)) != strOf(body["text_digest"]) {
		return nil, refuse(codeDeliveryInventory, "the text supplied is not the text of the digest delivered")
	}
	if int64(len(text)) != intOf(body["text_bytes"]) {
		return nil, refuse(codeDeliveryInventory, "the text supplied is not of the size delivered")
	}
	if err := agree(text, body); err != nil {
		return nil, err
	}

	now, err := c.Stamp()
	if err != nil {
		return nil, internal("a delivery cannot be given a time", err)
	}
	delivery := map[string]any{"delivered_at": now}
	for _, name := range []string{"generation", "revision", "renderer_version", "text_digest", "text_bytes", "transport", "evidence", "rules", "subject_projections", "rendered_at"} {
		delivery[name] = copyValue(body[name])
	}
	rec := copyMap(l.Data)
	rec["deliveries"] = append(listOf(rec["deliveries"]), delivery)
	rec["updated_at"], rec["schema_version"] = now, schemaVersion

	b := &builder{c: c}
	final, ref, err := b.put(kindImp, taskID, rec, l.Revision)
	if err != nil {
		return nil, err
	}
	scope := scopeOf(final)
	scope.TaskID, scope.Generation = taskID, generation
	evidence := mapOf(body["evidence"])
	if err := b.receipt(receiptDelivered, ref, scope, []envelope.Evidence{evidenceRef("delivery", evidence)}); err != nil {
		return nil, err
	}

	// Each embedded projection is received for the worker's generation at the
	// digest rendered, through the step propagate records, in this transaction.
	consumer := map[string]any{"kind": "generation", "task_id": taskID, "generation": generation}
	for _, v := range listOf(body["subject_projections"]) {
		p := mapOf(v)
		tx, _, err := memory.StageReceived(c, b.tx, memory.ReceivedStep{
			ProjectionID: strOf(p["projection_id"]), Rendering: intOf(p["rendering"]), Digest: strOf(p["digest"]),
			Consumer: consumer, At: now, Evidence: copyMap(evidence),
		})
		if err != nil {
			return nil, err
		}
		b.tx = tx
	}
	return b.stage(map[string]any{"imperative": final})
}

// checkOpen refuses a delivery to a generation that the item does not have or
// that is not open: a generation is delivered to while it is open on the item.
func checkOpen(c *cli.Call, taskID string, generation int64) error {
	l, om, err := poolstore.Load(c.Store, "item", taskID)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return notFound("item")
	case err != nil:
		return poolstore.Failure("the item", err)
	case om != nil:
		return malformed("the item cannot be read", om.ID)
	}
	if err := checkScope(c, l.Data); err != nil {
		return err
	}
	for _, v := range listOf(l.Data["generations"]) {
		if g := mapOf(v); intOf(g["generation"]) == generation && g["state"] == "open" {
			return nil
		}
	}
	return refuse(codeNotOpen, "the generation is not open on the item, so nothing is delivered to it").
		With("task_id", taskID).With("generation", generation)
}

// agree checks that the inventories of the request are those the text is
// labeled with: the rules are the duplicate-free union of the playbook's and of
// the embedded projections', and the projections are those embedded, each named
// once with its digest and rules; its rendering is the inventory's, not the
// text's, and the digest is checked against it when the receipt is staged.
func agree(text string, body map[string]any) error {
	disagree := func(what string) error {
		return refuse(codeDeliveryInventory, "the inventory delivered does not agree with the text supplied: "+what)
	}
	l, err := labelsOf(text)
	if err != nil {
		return disagree("the text does not carry its sources in labels")
	}

	want := map[ruleKey]bool{}
	for _, k := range l.Playbook {
		want[k] = true
	}
	for _, p := range l.Projections {
		for _, k := range p.Rules {
			want[k] = true
		}
	}
	got, err := ruleKeys(listOf(body["rules"]))
	if err != nil {
		return disagree("a rule is named twice")
	}
	if len(got) != len(want) {
		return disagree("the rules differ")
	}
	for _, k := range got {
		if !want[k] {
			return disagree("the rules differ")
		}
	}

	projections := listOf(body["subject_projections"])
	if len(projections) != len(l.Projections) {
		return disagree("the projections differ")
	}
	named := map[string]bool{}
	for _, v := range projections {
		p := mapOf(v)
		if named[strOf(p["projection_id"])] {
			return disagree("a projection is named twice")
		}
		named[strOf(p["projection_id"])] = true
		i := slices.IndexFunc(l.Projections, func(lp labeledProjection) bool { return lp.ID == strOf(p["projection_id"]) })
		if i < 0 {
			return disagree("a projection is not in the text")
		}
		lp := l.Projections[i]
		if lp.Digest != strOf(p["digest"]) {
			return disagree("a projection's digest differs")
		}
		mine, err := ruleKeys(listOf(p["rules"]))
		if err != nil || len(mine) != len(lp.Rules) {
			return disagree("a projection's rules differ")
		}
		for _, k := range mine {
			if !slices.Contains(lp.Rules, k) {
				return disagree("a projection's rules differ")
			}
		}
	}
	return nil
}

// ruleKeys reads an inventory of rules, and fails where one is named twice.
func ruleKeys(inventory []any) ([]ruleKey, error) {
	var keys []ruleKey
	for _, v := range inventory {
		m := mapOf(v)
		k := ruleKey{strOf(m["rule_id"]), intOf(m["record_revision"])}
		if slices.Contains(keys, k) {
			return nil, errDuplicate
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// evidenceRef is a record_ref of a request body, as the evidence of a receipt
// in the role given.
func evidenceRef(role string, ref map[string]any) envelope.Evidence {
	r := envelope.RecordRef{}
	r.Schema, _ = ref["schema"].(string)
	r.ID, _ = ref["id"].(string)
	r.HomeID, _ = ref["home_id"].(string)
	r.RecordRevision, _ = ref["record_revision"].(int64)
	r.Digest, _ = ref["digest"].(string)
	return envelope.Evidence{Role: role, Ref: r}
}
