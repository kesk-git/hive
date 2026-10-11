package envelope

import (
	"fmt"
)

// Response is the one document a command writes for each invocation
// (spec/commands.md, The response). Its schema and schema_version are fixed:
// hive.response at SchemaVersion. Its exit code is that of its Status. An
// empty RequestID is written null, and so is an empty Operation; Receipts
// nil is written as an empty array. Body is present only with ok and Error
// with every other status.
type Response struct {
	RequestID   string
	Command     string
	Operation   string
	Status      Status
	Responder   Responder
	CompletedAt string
	Replayed    bool
	Receipts    []Receipt
	Body        map[string]any
	Error       *Error
}

// ExitCode is the exit code the response's status fixes.
func (r *Response) ExitCode() int { return r.Status.ExitCode() }

// Check checks the response against the rules the schema and
// spec/commands.md fix for it, before it is encoded: a status, a body with ok
// and only then, an error otherwise with what its status requires.
func (r *Response) Check() error {
	if !r.Status.Valid() {
		return fmt.Errorf("envelope: %q is not a status", string(r.Status))
	}
	if r.Status == Ok {
		if r.Body == nil {
			return fmt.Errorf("envelope: a response with status ok carries a body")
		}
		if r.Error != nil {
			return fmt.Errorf("envelope: a response with status ok carries no error")
		}
		return nil
	}
	if r.Body != nil {
		return fmt.Errorf("envelope: with status %s there is no body", r.Status)
	}
	if r.Error == nil {
		return fmt.Errorf("envelope: a response with status %s carries an error", r.Status)
	}
	return (&Failure{Status: r.Status, Err: *r.Error}).Check()
}

// Tree is the response as a JSON tree, with schema, schema_version and
// exit_code set. The body, the error's detail and the receipts are copied.
func (r *Response) Tree() map[string]any {
	m := map[string]any{
		"schema":         ResponseSchema,
		"schema_version": SchemaVersion,
		"command":        r.Command,
		"status":         string(r.Status),
		"exit_code":      int64(r.ExitCode()),
		"responder":      r.Responder.tree(),
		"completed_at":   r.CompletedAt,
		"replayed":       r.Replayed,
	}
	if r.RequestID == "" {
		m["request_id"] = nil
	} else {
		m["request_id"] = r.RequestID
	}
	if r.Operation == "" {
		m["operation"] = nil
	} else {
		m["operation"] = r.Operation
	}
	receipts := make([]any, 0, len(r.Receipts))
	for i := range r.Receipts {
		receipts = append(receipts, r.Receipts[i].Tree())
	}
	m["receipts"] = receipts
	if r.Status == Ok {
		body := cloneObject(r.Body)
		if body == nil {
			body = map[string]any{}
		}
		m["body"] = body
	} else if r.Error != nil {
		m["error"] = r.Error.tree()
	}
	return m
}

func (e *Error) tree() map[string]any {
	m := map[string]any{
		"code":      e.Code,
		"message":   e.Message,
		"retryable": e.Retryable,
	}
	if e.Hold != nil {
		m["hold"] = e.Hold.tree()
	}
	if e.LimitBytes != nil {
		m["limit_bytes"] = *e.LimitBytes
	}
	if e.ActualBytes != nil {
		m["actual_bytes"] = *e.ActualBytes
	}
	if e.Detail != nil {
		m["detail"] = cloneObject(e.Detail)
	}
	return m
}

// Encode writes the response in canonical form with one final line feed,
// after checking it against response.v1: one document followed by a line
// feed, which is the whole of what a command writes.
func (r *Response) Encode() ([]byte, error) {
	if err := r.Check(); err != nil {
		return nil, err
	}
	return encodeDocument(r.Tree(), ResponseSchemaID)
}

// DecodeResponse reads a response: a Hive record, of a version this build
// reads, valid under response.v1. It fails with a *Failure of status invalid
// as DecodeRequest does; Accept is the caller's rule, which turns such a
// failure into unknown.
func DecodeResponse(data []byte) (*Response, error) {
	obj, err := decodeDocument(data, ResponseSchema, ResponseSchemaID, SchemaVersion)
	if err != nil {
		return nil, err
	}
	r, err := readResponse(fields{obj, "response"})
	if err != nil {
		return nil, Common(CodeSchemaViolation, err.Error())
	}
	return r, nil
}

func readResponse(f fields) (*Response, error) {
	r := &Response{}
	var err error
	if r.RequestID, err = f.nullStr("request_id"); err != nil {
		return nil, err
	}
	if r.Command, err = f.str("command"); err != nil {
		return nil, err
	}
	if r.Operation, err = f.nullStr("operation"); err != nil {
		return nil, err
	}
	status, err := f.str("status")
	if err != nil {
		return nil, err
	}
	r.Status = Status(status)
	exit, err := f.whole("exit_code")
	if err != nil {
		return nil, err
	}
	if !r.Status.Valid() || int64(r.Status.ExitCode()) != exit {
		return nil, fmt.Errorf("envelope: status %s does not agree with exit code %d", status, exit)
	}
	responder, err := f.object("responder")
	if err != nil {
		return nil, err
	}
	if r.Responder, err = readResponder(responder); err != nil {
		return nil, err
	}
	if r.CompletedAt, err = f.str("completed_at"); err != nil {
		return nil, err
	}
	if r.Replayed, err = f.boolean("replayed"); err != nil {
		return nil, err
	}
	list, err := f.objects("receipts")
	if err != nil {
		return nil, err
	}
	r.Receipts = make([]Receipt, 0, len(list))
	for _, e := range list {
		rc, err := readReceipt(e)
		if err != nil {
			return nil, err
		}
		r.Receipts = append(r.Receipts, *rc)
	}
	if f.has("body") {
		body, err := f.object("body")
		if err != nil {
			return nil, err
		}
		r.Body = body.m
	}
	if f.has("error") {
		e, err := f.object("error")
		if err != nil {
			return nil, err
		}
		if r.Error, err = readError(e); err != nil {
			return nil, err
		}
	}
	return r, r.Check()
}

func readError(f fields) (*Error, error) {
	e := &Error{}
	var err error
	if e.Code, err = f.str("code"); err != nil {
		return nil, err
	}
	if e.Message, err = f.str("message"); err != nil {
		return nil, err
	}
	if e.Retryable, err = f.boolean("retryable"); err != nil {
		return nil, err
	}
	if e.Hold, err = readOptRecordRef(f, "hold"); err != nil {
		return nil, err
	}
	for key, dst := range map[string]**int64{"limit_bytes": &e.LimitBytes, "actual_bytes": &e.ActualBytes} {
		if !f.has(key) {
			continue
		}
		n, err := f.whole(key)
		if err != nil {
			return nil, err
		}
		*dst = &n
	}
	if f.has("detail") {
		d, err := f.object("detail")
		if err != nil {
			return nil, err
		}
		e.Detail = d.m
	}
	return e, nil
}

// Accept is the caller's rule for a response (spec/commands.md, The
// response): the whole output parses, follows the schema, echoes the
// caller's request_id and agrees with the process's exit code. A request_id
// that is not null is the caller's in every case. A null one is accepted only
// where the callee answers before it can read the request's identity or the
// invocation carries no request: usage, invalid with code malformed_request,
// too_large because the request exceeded the limit, and the capabilities
// invocation. Anything else is a *Failure of status unknown, never success
// and never an empty result; a process that ended by a signal or a timeout
// gave no output and is unknown the same way.
func Accept(output []byte, exit int, requestID string) (*Response, error) {
	unknown := func(format string, args ...any) error {
		return Common(CodeOutcomeUnknown, fmt.Sprintf(format, args...))
	}
	if len(output) == 0 {
		return nil, unknown("the command wrote no response (exit code %d)", exit)
	}
	if output[len(output)-1] != '\n' {
		return nil, unknown("the response is not one document followed by a line feed")
	}
	r, err := DecodeResponse(output)
	if err != nil {
		return nil, unknown("the response is not a response: %v", err)
	}
	if r.RequestID == "" {
		if !r.mayCarryNoRequestID() {
			return nil, unknown("the response answers no request")
		}
	} else if r.RequestID != requestID {
		return nil, unknown("the response answers another request")
	}
	if r.ExitCode() != exit {
		return nil, unknown("the response says %s but the process exited with %d", r.Status, exit)
	}
	return r, nil
}

// mayCarryNoRequestID reports whether a response with a null request_id is
// one the contract allows: the capabilities invocation, which takes no
// request, or an answer given before the request's identity could be read.
func (r *Response) mayCarryNoRequestID() bool {
	if r.Operation == CapabilitiesOperation {
		return true
	}
	switch r.Status {
	case Usage:
		return true
	case Invalid:
		return r.Error != nil && r.Error.Code == CodeMalformedRequest
	case TooLarge:
		e := r.Error
		return e != nil && e.Code == CodeRecordTooLarge && e.Detail["record"] == "request" &&
			e.LimitBytes != nil && e.ActualBytes != nil && *e.ActualBytes > *e.LimitBytes
	}
	return false
}
