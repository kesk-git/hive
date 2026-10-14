package intake

import (
	"errors"
	"fmt"
	"io"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/exec"
	"github.com/kesk-git/hive/contract/ids"
)

// readResponseSchema is the schema of the body of the Pool's read.
const readResponseSchema = "urn:hive:schema:ops:hive-pool.read.response:v1"

// Pool is the callee of the hive-word executable: hive-pool, run as the
// external executable it is, through the one process-execution path and found
// by name on the search path (spec/commands.md, A command with no store). A
// hive-pool that is not found or cannot be started is unavailable with
// dependency_missing or dependency_unusable, and a run that ends with no
// response, or the Pool answering unknown, is unknown.
type Pool struct{}

// ReadOutcome is hive-pool read of kind request-outcome for the request_id.
func (Pool) ReadOutcome(c *cli.Call, requestID string) ([]byte, bool, error) {
	id, err := ids.Mint(c.Clock, ids.Request)
	if err != nil {
		return nil, false, err
	}
	issued, err := c.Stamp()
	if err != nil {
		return nil, false, err
	}
	digest, err := c.Request.Digest()
	if err != nil {
		return nil, false, err
	}
	req := envelope.Request{
		Command:          poolCommand,
		Operation:        "read",
		OperationVersion: bodyVersion,
		RequestID:        id,
		CorrelationID:    c.Request.CorrelationID,
		Causation:        &envelope.RecordRef{Schema: envelope.RequestSchema, ID: c.Request.RequestID, Digest: digest},
		IssuedAt:         issued,
		Caller:           envelope.Caller{Kind: envelope.CallerPackage, Package: c.Executable.Package, Command: c.Command.Name},
		Scope:            c.Request.Scope,
		Authority:        envelope.Authority{Kind: envelope.AuthorityNone},
		Body: map[string]any{
			"kind":     "request-outcome",
			"selector": map[string]any{"id": requestID},
		},
	}
	data, err := req.Encode()
	if err != nil {
		return nil, false, err
	}
	output, exit, err := run(c, req.Operation, data)
	if err != nil {
		return nil, false, err
	}
	resp, err := envelope.AcceptResponse(output, exit, id)
	if err != nil {
		return nil, false, err
	}
	return outcomeOf(resp)
}

// outcomeOf is the recorded outcome the Pool's answer to the read gives, and
// whether it gives one. The Pool recording none is not_found; its unknown and
// unavailable are returned as it gave them, and any other answer, or one that
// is not the one record asked for, leaves the outcome unknown.
func outcomeOf(resp *envelope.Response) ([]byte, bool, error) {
	switch {
	case resp.Status == envelope.NotFound && resp.Error != nil && resp.Error.Code == envelope.CodeNotFound:
		return nil, false, nil
	case resp.Status == envelope.Unknown || resp.Status == envelope.Unavailable:
		return nil, false, resp.Failure()
	case resp.Status != envelope.Ok:
		return nil, false, unreadable(resp.Failure())
	}
	if err := envelope.ValidateBody(resp.Body, readResponseSchema); err != nil {
		return nil, false, unreadable(err)
	}
	records, _ := resp.Body["records"].([]any)
	if resp.Body["kind"] != "request-outcome" || len(records) != 1 {
		return nil, false, unreadable(nil)
	}
	record, err := canon.Encode(records[0])
	if err != nil {
		return nil, false, unreadable(err)
	}
	return record, true, nil
}

// unreadable is the unknown outcome of a read the Pool answered in a way that
// gives no recorded outcome: nothing has been sent, and a retry reads again.
func unreadable(because error) error {
	return envelope.Common(envelope.CodeOutcomeUnknown, "the Pool's recorded outcome of the request could not be read").Because(because)
}

// Send is the one handoff to the operation of hive-pool the request names.
func (Pool) Send(c *cli.Call, request []byte) ([]byte, int, error) {
	req, err := envelope.DecodeRequest(request)
	if err != nil {
		return nil, 0, err
	}
	return run(c, req.Operation, request)
}

// run starts hive-pool at the home of the call with the operation, gives it
// the request on its standard input and returns what it wrote and the exit
// code it ended with. A run that was not started, or that ended with no exit
// code, is an error that is a failure the contract names.
func run(c *cli.Call, operation string, request []byte) ([]byte, int, error) {
	program := exec.Program{Kind: exec.Hive, Name: poolCommand}
	r, err := exec.Start(exec.Spec{
		Program: program,
		Args:    []string{operation, cli.HomeOption, c.Home.Dir},
		Dir:     c.Home.Dir,
		Stdin:   exec.Pipe(),
		Stdout:  exec.Pipe(),
	})
	if err != nil {
		var started *exec.StartError
		if errors.As(err, &started) {
			return nil, 0, started.Failure()
		}
		return nil, 0, err
	}
	defer r.Close()
	go func() {
		// A command that answers before it has read everything closes its end;
		// what it wrote is what is judged.
		r.Stdin().Write(request)
		r.Stdin().Close()
	}()
	limit := envelope.DefaultLimits().MaxResponseBytes
	output, err := io.ReadAll(io.LimitReader(r.Stdout(), limit+1))
	if err != nil {
		return nil, 0, err
	}
	if int64(len(output)) > limit {
		return nil, 0, fmt.Errorf("intake: %s wrote more than %d bytes", poolCommand, limit)
	}
	code, failure := r.Wait().External(program, true)
	if failure != nil {
		return nil, 0, failure
	}
	return output, int(code), nil
}
