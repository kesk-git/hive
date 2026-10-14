package cli

import (
	"fmt"
	"slices"

	"github.com/kesk-git/hive/contract/envelope"
)

// A command with no store (spec/commands.md, A command with no store) keeps
// no store, takes no lock and records nothing, and persists through its
// callee. The harness gives it the replay of the callee's store in three
// steps: it reads the callee's recorded outcome of the request first, sends
// the callee exactly one handoff otherwise, and the callee's harness keys a
// handoff on the original request's digest, which the handoff carries as its
// causation.

// Callee is a storeless executable's way to the one package it persists
// through. It is the transport only: the harness decides what the callee's
// answers mean, and every answer is held to the contract before it is used.
// An adapter returns a *envelope.Failure for a failure the contract names;
// any other error is the harness's own.
type Callee interface {
	// ReadOutcome reads the callee's recorded outcome of the request: the
	// record of schema hive.request-outcome the callee's read of kind
	// request-outcome gives, which first recovers an interrupted transaction
	// of the callee. found is false when the callee records none.
	ReadOutcome(c *Call, requestID string) (record []byte, found bool, err error)
	// Send sends the callee one request, the encoded document, and returns
	// what the callee wrote and the exit code it ended with. An error is the
	// callee giving no response: the outcome is unknown.
	Send(c *Call, request []byte) (output []byte, exit int, err error)
}

// Handoff is the one mutating request an operation of a command with no store
// sends its callee. The request_id, correlation_id and scope are the original
// request's, the caller is the package and its command, and the causation is
// the original request by identity and digest; the harness sets them.
type Handoff struct {
	// Command and Operation address the callee's operation, and
	// OperationVersion is the body version it is sent. Command is among the
	// commands the calling operation declares it calls.
	Command          string
	Operation        string
	OperationVersion string
	// Authority is the authority the callee relies on. Nil sends the
	// original request's.
	Authority *envelope.Authority
	// ExpectedRevisions are the records the handoff depends on, as the
	// operation read them.
	ExpectedRevisions []envelope.ExpectedRevision
	// Body is the callee operation's own request.
	Body map[string]any
}

// handoffUnknownMessage is the message of a handoff the callee did not answer
// in a way the contract accepts.
const handoffUnknownMessage = "the callee gave no response the contract accepts; sending the same request again reads its recorded outcome first"

// handedMessage is the message of a request whose callee recorded it and
// whose command could not give the response.
const handedMessage = "the callee recorded the outcome of this request and the command could not give it; sending the same request again replays it"

// SendHandoff sends the callee the operation's one handoff and returns the
// callee's ok response, whose receipts the request's response carries in
// full. A callee response of any other status is returned as the *Failure it
// says, with its receipts, for the operation to return as it is; a callee
// that gives no response, or none the contract accepts, is unknown. An
// operation sends at most one, to a command it declares it calls.
func (c *Call) SendHandoff(h Handoff) (*envelope.Response, error) {
	callee := c.Executable.Callee
	switch {
	case callee == nil || c.Store != nil || !c.Operation.Mutates:
		return nil, fmt.Errorf("cli: only a mutating operation of a command with no store sends a handoff")
	case !slices.Contains(c.Operation.Calls, h.Command):
		return nil, fmt.Errorf("cli: %s %s declares no call to %s", c.Command.Name, c.Operation.Name, h.Command)
	case c.handoffs > 0:
		return nil, fmt.Errorf("cli: a request is handed off exactly once")
	}
	c.handoffs++
	issued, err := c.Stamp()
	if err != nil {
		return nil, err
	}
	req := envelope.Request{
		Command:           h.Command,
		Operation:         h.Operation,
		OperationVersion:  h.OperationVersion,
		RequestID:         c.Request.RequestID,
		CorrelationID:     c.Request.CorrelationID,
		Causation:         &envelope.RecordRef{Schema: envelope.RequestSchema, ID: c.Request.RequestID, Digest: c.digest},
		IssuedAt:          issued,
		Caller:            envelope.Caller{Kind: envelope.CallerPackage, Package: c.Executable.Package, Command: c.Command.Name},
		Scope:             c.Request.Scope,
		SourceRevision:    c.Request.SourceRevision,
		ExpectedRevisions: h.ExpectedRevisions,
		Authority:         c.Request.Authority,
		Body:              h.Body,
	}
	if h.Authority != nil {
		req.Authority = *h.Authority
	}
	data, err := req.Encode()
	if err != nil {
		return nil, fmt.Errorf("cli: the handoff is not a request: %w", err)
	}
	unknown := func(why error) error {
		return envelope.Common(envelope.CodeOutcomeUnknown, handoffUnknownMessage).Because(why)
	}
	output, exit, err := callee.Send(c, data)
	if err != nil {
		return nil, unknown(err)
	}
	resp, err := envelope.AcceptResponse(output, exit, c.Request.RequestID)
	if err != nil {
		return nil, unknown(err)
	}
	// An answer that echoes the request names the command and operation it
	// was sent to; one given before the request could be read does not.
	if resp.RequestID != "" && (resp.Command != h.Command || resp.Operation != h.Operation) {
		return nil, unknown(fmt.Errorf("the response answers another operation than the one it was sent to"))
	}
	c.handed = resp
	if resp.Status == envelope.Ok {
		return resp, nil
	}
	c.handedFailure = resp.Failure()
	return nil, c.handedFailure
}

// handoffKey is the digest a callee records as a request's request_digest and
// compares on a repeat: the causation's, for a handoff, and the request's own
// otherwise. A request is a handoff when its caller is a package that declares
// no home path and its causation is a hive.request of its own request_id with
// a digest.
func (e *Executable) handoffKey(req *envelope.Request, own string) string {
	c := req.Causation
	if req.Caller.Kind == envelope.CallerPackage && slices.Contains(e.Handoffs, req.Caller.Package) &&
		c != nil && c.Schema == envelope.RequestSchema && c.ID == req.RequestID && c.Digest != "" {
		return c.Digest
	}
	return own
}

// handleStoreless answers a mutating request of a command with no store, once
// the request, the home and the scope are checked and before the operation
// reads anything else or mints an identity. It reads the callee's recorded
// outcome of the request first and replays it; with none the operation runs,
// and sends the callee exactly one handoff. It takes no lock and writes no
// store: whatever is durable is the callee's.
func (e *Executable) handleStoreless(call *Call, digest string, fail func(*envelope.Failure) *envelope.Response) *envelope.Response {
	c, op, req := call.Command, call.Operation, call.Request
	call.digest = digest
	if e.Callee == nil {
		return fail(envelope.Common(envelope.CodeInternal, "the executable keeps no store and has no callee"))
	}
	record, found, err := e.Callee.ReadOutcome(call, req.RequestID)
	if err != nil {
		return fail(failureOf(err))
	}
	if found {
		rec, err := parseOutcome("the callee's recorded outcome", record, req.RequestID)
		if err != nil {
			return fail(envelope.AsFailure(err))
		}
		if rec.digest != digest {
			return fail(envelope.Common(envelope.CodeReplayMismatch, "the request_id was used before with another request"))
		}
		return e.replayHanded(call, rec.response, fail)
	}

	res, err := e.run(call)
	if err != nil {
		f := failureOf(err)
		if f == call.handedFailure && f != nil {
			// The callee's own answer, with its status and receipts.
			resp := fail(f)
			resp.Replayed = call.handed.Replayed
			return e.sizeHanded(call, resp, call.handed.Status == envelope.Ok || len(f.Receipts) > 0)
		}
		if len(f.Receipts) > 0 {
			return fail(envelope.Common(envelope.CodeInternal, "a command with no store writes no receipt of its own"))
		}
		if call.handed != nil && (call.handed.Status == envelope.Ok || len(call.handed.Receipts) > 0) {
			// The callee holds the request, and the command cannot answer it.
			return e.handedUnknown(call, call.handed.Receipts)
		}
		resp := fail(f)
		if bad := e.preflight(c, resp); bad != nil {
			return bad
		}
		return resp
	}
	switch {
	case call.handed == nil || call.handed.Status != envelope.Ok || call.handoffs != 1:
		return fail(envelope.Common(envelope.CodeInternal, "a mutating operation of a command with no store ends ok by handing off, exactly once"))
	case len(res.Records)+len(res.Receipts)+len(res.Events) > 0:
		return e.handedUnknown(call, call.handed.Receipts)
	}
	resp := &envelope.Response{
		RequestID: req.RequestID, Command: c.Name, Operation: op.Name, Status: envelope.Ok, Responder: e.responder(c),
		Receipts: call.handed.Receipts, Body: res.Body, Replayed: call.handed.Replayed,
	}
	return e.sizeHanded(call, resp, true)
}

// failureOf is the Failure an error is, or the harness's own failure for any
// other error.
func failureOf(err error) *envelope.Failure {
	if f := envelope.AsFailure(err); f != nil {
		return f
	}
	return envelope.Common(envelope.CodeInternal, "the operation failed for a reason of its own").Because(err)
}

// replayHanded answers a request from the callee's recorded outcome of it: an
// ok response adapted to the operation's own body, and any other status as the
// callee recorded it, with its receipts, all as a replay of this command's
// operation.
func (e *Executable) replayHanded(call *Call, callee *envelope.Response, fail func(*envelope.Failure) *envelope.Response) *envelope.Response {
	c, op, req := call.Command, call.Operation, call.Request
	if callee.Status != envelope.Ok {
		resp := fail(&envelope.Failure{Status: callee.Status, Err: *callee.Error, Receipts: callee.Receipts})
		resp.Replayed = true
		return e.sizeHanded(call, resp, true)
	}
	body, err := e.adapt(call, callee)
	if err != nil {
		return e.handedUnknown(call, callee.Receipts)
	}
	resp := &envelope.Response{
		RequestID: req.RequestID, Command: c.Name, Operation: op.Name, Status: envelope.Ok, Responder: e.responder(c),
		Receipts: callee.Receipts, Body: body, Replayed: true,
	}
	return e.sizeHanded(call, resp, true)
}

// adapt gives the operation's own body from the callee's response, and holds
// it to the operation's response schema.
func (e *Executable) adapt(call *Call, callee *envelope.Response) (map[string]any, error) {
	var body map[string]any
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("cli: the adaptation stopped unexpectedly")
			}
		}()
		body, err = call.Operation.Adapt(call, callee)
	}()
	if err != nil {
		return nil, err
	}
	if body == nil {
		return nil, fmt.Errorf("cli: the adaptation gave no body")
	}
	if call.Operation.ResponseSchema != "" {
		if err := envelope.ValidateBody(body, call.Operation.ResponseSchema); err != nil {
			return nil, err
		}
	}
	return body, nil
}

// sizeHanded checks that the response can be written whole. A response that
// cannot is too_large while the callee holds nothing of the request, and
// otherwise outcome_unknown, which fits by the floor: the callee's record is
// durable and too_large would promise that nothing changed.
func (e *Executable) sizeHanded(call *Call, resp *envelope.Response, durable bool) *envelope.Response {
	bad := e.preflight(call.Command, resp)
	switch {
	case bad == nil:
		return resp
	case !durable:
		return bad
	}
	return e.instead(resp, envelope.Common(envelope.CodeOutcomeUnknown, oversizeReplayMessage))
}

// handedUnknown is the answer of a request the callee holds and the command
// cannot answer: unknown with the callee's receipts where they fit, and
// retried by sending the same request again, which replays it.
func (e *Executable) handedUnknown(call *Call, receipts []envelope.Receipt) *envelope.Response {
	c, op, id := call.Command, call.Operation, call.Request.RequestID
	if resp := e.unknownWith(c, op, id, handedMessage, receipts); e.preflight(c, resp) == nil {
		return resp
	}
	return e.unknownAnswer(c, op.Name, id, handedMessage)
}
