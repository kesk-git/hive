package memory

import (
	"github.com/kesk-git/hive/contract/envelope"
)

// The codes of the Pool that this package returns (spec/pool.md, Error codes).
const (
	CodeOverBudget      = "pool.projection_over_budget"
	CodeRuleConflict    = "pool.rule_conflict"
	CodeSubjectInvalid  = "pool.subject_invalid"
	CodeDeliveryInvent  = "pool.delivery_inventory"
	versionOne          = "1.0"
	receiptRendered     = "pool.projection.rendered"
	receiptReceived     = "pool.projection.received"
	receiptAcknowledged = "pool.projection.acknowledged"
)

// The three moves a recorder makes when a ruling does not fit, in order.
var moves = []any{"shorten-line-and-file-facts", "narrow-subject", "supersede-rule"}

func refuse(code, message string) *envelope.Failure {
	return envelope.Fail(envelope.Refused, code, message)
}

func invalid(message string) *envelope.Failure {
	return envelope.Common(envelope.CodeSchemaViolation, message)
}

func internal(what string, err error) *envelope.Failure {
	return envelope.Common(envelope.CodeInternal, what).Because(err)
}

func noSubject() *envelope.Failure {
	return refuse(CodeSubjectInvalid, "the subject the projection names is not a subject of the Pool")
}

func subjectScope() *envelope.Failure {
	return refuse(CodeSubjectInvalid, "the subject the projection names is not at the scope of the projection or above it")
}

func conflictRefusal(k Key, conflicts int) *envelope.Failure {
	return refuse(CodeRuleConflict, "two rules govern one subject at one scope, so the projection has no resolved-rule digest and is not rendered").
		With("projection", k.String()).With("conflicts", int64(conflicts))
}
