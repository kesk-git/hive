package memory

import (
	"time"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// What an Imperative reads of the generated knowledge and records into it
// (spec/pool.md, The Imperative): the projection of a subject as it stands, for
// the rendering to embed, and the receipt of an embedded projection by the
// worker's generation, staged into the transaction of the delivery. Both call
// the code the operations of this package are made of and change no behaviour
// of any of them.

// SubjectProjection is the stored projection of the key, a subject projection,
// as it stands: its identity, its current rendering with the text, digest, rule
// inventory and sequence the Imperative embeds, and the record it was read from.
// The rules for the key's scope and audience are resolved at the instant, as
// hive-memory render resolves them; where two rules govern one subject at one
// scope the projection has no resolved-rule digest and is not given, and the
// failure is pool.rule_conflict. A subject that does not exist is
// pool.subject_invalid, and a projection that was never rendered is not found.
// Whether the projection is fresh is Fresh's to say, on the projection returned.
func (s *Snapshot) SubjectProjection(k Key, at time.Time) (Existing, error) {
	if err := k.Valid(); err != nil {
		return Existing{}, invalid(err.Error())
	}
	if k.Tier != TierSubject {
		return Existing{}, invalid("the projection an Imperative embeds is a subject projection")
	}
	sub := s.subject(k.SubjectID)
	if sub == nil {
		return Existing{}, noSubject()
	}
	if !within(sub.Scope, k.Scope) {
		return Existing{}, subjectScope()
	}
	res, err := s.resolve(k.Scope, k.Audience, at)
	if err != nil {
		return Existing{}, internal("the rules of the projection cannot be resolved", err)
	}
	if len(res.Conflicts) > 0 {
		return Existing{}, conflictRefusal(k, len(res.Conflicts))
	}
	e, ok := s.projection(k)
	if !ok {
		return Existing{}, envelope.Common(envelope.CodeNotFound, "no projection has been rendered for the subject")
	}
	return e, nil
}

// ReceivedStep is the receipt of one rendering of a projection by one consumer,
// as hive-memory propagate records it for the step received: the consumer, the
// rendering and its digest, the time, and the consumer's own record where it
// gave one.
type ReceivedStep struct {
	ProjectionID string
	Rendering    int64
	Digest       string
	Consumer     map[string]any
	At           string
	Evidence     map[string]any
}

// StageReceived adds to the transaction the record of the step and its receipt,
// as hive-memory propagate writes them, and returns the transaction. The
// rendering named must be one the projection has, current or earlier, at the
// digest named, or the failure is pool.delivery_inventory. A step already
// recorded for the same consumer, rendering and digest adds nothing, and the
// result says so.
func StageReceived(c *cli.Call, tx poolstore.Transaction, step ReceivedStep) (poolstore.Transaction, bool, error) {
	tx, out, err := stageStep(c, tx, projectionStep{
		ProjectionID: step.ProjectionID, Step: "received", Rendering: step.Rendering, Digest: step.Digest,
		Consumer: step.Consumer, At: step.At, Evidence: step.Evidence,
	})
	return tx, out.Changed, err
}
