package rule

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/read"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// The operation resolve of hive-rule (spec/pool.md, Rules): the rules
// effective for the envelope's scope, a role and an instant. It is read-only,
// holds the Pool's lock shared and writes nothing.

// Resolve resolves the rules for the request.
func Resolve(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	req := Request{Scope: c.Request.Scope}
	req.Role, _ = body["role"].(string)
	req.TaskID, _ = body["task_id"].(string)
	req.Generation, _ = body["generation"].(int64)
	if req.Generation != 0 && req.TaskID == "" {
		return nil, envelope.Common(envelope.CodeSchemaViolation, "a generation is of a task: it is given with task_id")
	}
	// The instant is the one the request gives, or the command's clock.
	at, _ := body["at"].(string)
	if at == "" {
		var err error
		if at, err = clock.Stamp(c.Clock); err != nil {
			return nil, envelope.Common(envelope.CodeInternal, "the time of the resolution cannot be written").Because(err)
		}
	}
	var err error
	if req.At, err = clock.Parse(at); err != nil {
		return nil, envelope.Common(envelope.CodeSchemaViolation, "at is not a calendar instant")
	}

	st, release, err := read.OpenShared(c)
	if err != nil {
		return nil, err
	}
	defer release()
	all, omitted, err := poolstore.LoadAll(st, "rule")
	if err != nil {
		return nil, poolstore.Failure("the rules", err)
	}
	// A rule that cannot be read might be one of this scope, and a resolution
	// that left it out would be a guess.
	if len(omitted) > 0 {
		return nil, envelope.Common(envelope.CodeRecordMalformed, "a rule cannot be read, so the rules in force cannot be known").With("record", omitted[0].ID)
	}
	rules := make([]*Rule, len(all))
	for i, l := range all {
		if rules[i], err = ParseRule(l.Data); err != nil {
			return nil, envelope.Common(envelope.CodeRecordMalformed, "a rule cannot be read, so the rules in force cannot be known").With("record", l.ID).Because(err)
		}
	}
	res, err := Compute(req, rules)
	if err != nil {
		return nil, envelope.Common(envelope.CodeRecordMalformed, "a rule cannot be placed in time").Because(err)
	}
	return &cli.Result{Body: res.Body(at, req.Role)}, nil
}
