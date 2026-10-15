package memory

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/rule"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// ReportRefusal is the one call a Pool refusal that cites a rule makes
// (spec/pool.md, Generated knowledge, Misses): a refusal of the Pool's own is
// reported as a miss by the Pool itself, when the request names the acting
// session or task generation. It takes the refusal the writer is about to return,
// and returns the result the writer ends with.
//
// The refusal is cited rules by the identities its detail names: the member rule,
// the member rules, and the sources of the kind pool.rule that a stale projection
// names. For each rule the Pool holds that is filed under a subject, and which the
// actor was given at its stored revision neither in a projection nor in an
// Imperative, the Pool writes a miss and pool.rule.missed in the transaction of
// the refusal. The incident is the request, the observer the Pool, and the
// independence independent: the Pool is the owner that met the refusal and
// verifies it.
//
// The refusal's own answer is never changed. Where there is nothing to record,
// because the request names no actor, the refusal is not of a mutating operation
// or has a status that leaves no write, it cites no rule filed under a subject, or
// the actor holds every rule it cites, the result is the refusal as it was.
func ReportRefusal(c *cli.Call, refusal error) (*cli.Result, error) {
	f := envelope.AsFailure(refusal)
	if f == nil || f.Status != envelope.Refused || c.Store == nil || !c.Operation.Mutates {
		return nil, refusal
	}
	actor := actorOf(c.Request.Caller)
	if actor == nil {
		return nil, refusal
	}
	stamp, err := c.Stamp()
	if err != nil {
		return nil, refusal
	}
	observed, err := parseInstant(stamp)
	if err != nil {
		return nil, refusal
	}
	incident := map[string]any{"schema": schemaRequest, "id": c.Request.RequestID, "home_id": c.Home.HomeID}
	b := &builder{c: c}
	for _, id := range citedRuleIDs(f) {
		l, om, err := poolstore.Load(c.Store, "rule", id)
		if err != nil || om != nil {
			continue
		}
		r, err := rule.ParseRule(l.Data)
		if err != nil {
			continue
		}
		rl, err := readCited(c.Store, id, r.RecordRevision)
		if err != nil {
			continue
		}
		held, err := receivedRule(c.Store, actor, rl.ID, rl.Revision, observed)
		if err != nil || held {
			continue
		}
		provenance := map[string]any{
			"incident":     copyValue(incident),
			"observer":     map[string]any{"kind": envelope.CallerPackage, "package": poolstore.Package, "command": c.Command.Name},
			"independence": "independent", "derived_from": []any{}, "evidence": []any{copyValue(incident)},
		}
		if _, err := b.stageMiss(rl, actor, StepOfPoolRefusals, provenance, stamp, stamp); err != nil {
			return nil, refusal
		}
	}
	if len(b.tx.Records) == 0 {
		return nil, refusal
	}
	staged, err := poolstore.Stage(c, b.tx)
	if err != nil {
		return nil, refusal
	}
	return staged.Ended(f), nil
}

// actorOf is the acting session or task generation the caller names: a
// supervisor's session and its generation, or a worker's task and its generation.
// Any other caller names none.
func actorOf(caller envelope.Caller) map[string]any {
	switch {
	case caller.Kind == envelope.CallerSupervisor && caller.SessionID != "" && caller.SessionGeneration > 0:
		return map[string]any{"kind": "session", "session_id": caller.SessionID, "session_generation": caller.SessionGeneration}
	case caller.Kind == envelope.CallerWorker && caller.TaskID != "" && caller.Generation > 0:
		return map[string]any{"kind": "generation", "task_id": caller.TaskID, "generation": caller.Generation}
	}
	return nil
}

// citedRuleIDs are the rules a refusal names, in the order it names them and
// each once.
func citedRuleIDs(f *envelope.Failure) []string {
	var out []string
	add := func(id string) {
		if id == "" {
			return
		}
		for _, o := range out {
			if o == id {
				return
			}
		}
		out = append(out, id)
	}
	d := f.Err.Detail
	add(str(d["rule"]))
	for _, v := range list(d["rules"]) {
		add(str(v))
	}
	for _, v := range list(d["stale_sources"]) {
		if m := tree(v); str(m["schema"]) == rule.SchemaRule {
			add(str(m["id"]))
		}
	}
	return out
}
