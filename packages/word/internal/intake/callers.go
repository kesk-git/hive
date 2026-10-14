package intake

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
)

// notAuthorized is the code of a request whose caller the operation does not
// accept (spec/word.md, Error codes).
const notAuthorized = "word.not_authorized"

// Authorized reports whether the operation accepts the caller, by the table
// of spec/word.md, Operations: hold and revise a supervisor, the operator or
// a package other than ui; release, complete and close a supervisor or the
// operator. Any other caller, a worker, the package ui and a test among
// them, is refused. The caller is attribution and proves nothing about who
// typed.
func Authorized(operation string, caller envelope.Caller) bool {
	switch caller.Kind {
	case envelope.CallerSupervisor, envelope.CallerOperator:
		return true
	case envelope.CallerPackage:
		return (operation == Hold || operation == Revise) && caller.Package != "ui"
	}
	return false
}

// refuse is the refusal of a caller the operation does not accept. Its message
// names the field and nothing the caller wrote.
func refuse() *envelope.Failure {
	return envelope.Fail(envelope.Refused, notAuthorized, "the caller is not one this operation accepts")
}

// guarded is a callee that checks who brings a request before it reaches the
// Pool.
type guarded struct{ cli.Callee }

// Guard makes the callee check the caller of a request before anything is
// read from or sent to the Pool. The harness reads the Pool's recorded
// outcome of a request before the operation's own handler runs, and the
// caller is checked before that read (spec/word.md, How word persists
// through the Pool), so the check sits on the read the harness makes first: a
// caller the operation does not accept is refused with word.not_authorized,
// and nothing is read or sent.
func Guard(next cli.Callee) cli.Callee { return guarded{next} }

func (g guarded) ReadOutcome(c *cli.Call, requestID string) ([]byte, bool, error) {
	if !Authorized(c.Operation.Name, c.Request.Caller) {
		return nil, false, refuse()
	}
	record, found, err := g.Callee.ReadOutcome(c, requestID)
	if err != nil && envelope.AsFailure(err) == nil {
		// The Pool gave no answer to the read, so what became of the request
		// cannot be established, and nothing has been sent (spec/commands.md,
		// A command with no store).
		return nil, false, envelope.Common(envelope.CodeOutcomeUnknown, "the Pool's recorded outcome of the request could not be read").Because(err)
	}
	return record, found, err
}
