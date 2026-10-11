package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"runtime/debug"
	"slices"
	"unicode/utf8"

	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/store"
)

// Invocation is one operation invocation after its arguments are read: the
// command, the operation, the home and the request bytes.
type Invocation struct {
	Command   *Command
	Operation string
	Home      string
	Request   []byte
	// Stderr carries diagnostics for a person; nil discards them.
	Stderr io.Writer
}

// Outcome is what an invocation produced: the response, encoded as the one
// document the command writes, and for a resident operation what runs once
// the response is out.
type Outcome struct {
	Response *envelope.Response
	Encoded  []byte
	After    func() error
}

// Handle runs one operation invocation through the whole harness and
// returns its outcome, whose response is complete and encoded. A convenience
// form that builds a request from a person's arguments hands it here, so it
// is validated and handled exactly as a request read from a file. Handle
// never returns an error for what the contract turns into a status; the
// error it does return is one the executable cannot answer at all, which
// Main reports on standard error and ends with exit code 1.
func (e *Executable) Handle(inv Invocation) (*Outcome, error) {
	if inv.Command == nil {
		return nil, errors.New("cli: the invocation names no command")
	}
	if inv.Stderr == nil {
		inv.Stderr = io.Discard
	}
	resp, after := e.handle(inv)
	if err := e.stamp(resp); err != nil {
		return nil, err
	}
	data, err := e.finish(inv.Command, resp)
	if err != nil {
		return nil, err
	}
	return &Outcome{resp, data, after}, nil
}

// finish encodes the response, replacing it with too_large where it would
// exceed the command's limit, and checks it against the schema.
func (e *Executable) finish(c *Command, resp *envelope.Response) ([]byte, error) {
	limit := c.Limits.Resolved().MaxResponseBytes
	data, err := resp.Encode()
	if err != nil {
		return nil, fmt.Errorf("cli: the response cannot be written: %w", err)
	}
	if int64(len(data)) > limit {
		f := envelope.Oversize("response", limit, int64(len(data)))
		*resp = *e.failure(resp.RequestID, resp.Command, resp.Operation, f)
		if err := e.stamp(resp); err != nil {
			return nil, err
		}
		if data, err = resp.Encode(); err != nil {
			return nil, fmt.Errorf("cli: the response cannot be written: %w", err)
		}
	}
	return data, nil
}

// stamp sets completed_at to now where the response has none.
func (e *Executable) stamp(resp *envelope.Response) error {
	if resp.CompletedAt != "" {
		return nil
	}
	at, err := clock.Stamp(e.clock())
	if err != nil {
		return fmt.Errorf("cli: %w", err)
	}
	resp.CompletedAt = at
	return nil
}

// responder is the responder of every response of the command c.
func (e *Executable) responder(c *Command) envelope.Responder {
	name := e.Name
	if c != nil {
		name = c.Name
	}
	return envelope.Responder{
		Package:               e.Package,
		Command:               name,
		ImplementationVersion: e.Version,
		ContractVersion:       envelope.ContractVersion,
	}
}

// maxMessage is the longest error message the response schema admits.
const maxMessage = 2000

// clip cuts a message to what the response schema admits, on a character
// boundary, and puts or in place of an empty one.
func clip(msg, or string) string {
	if msg == "" {
		return or
	}
	if len(msg) > maxMessage {
		n := maxMessage
		for n > 0 && !utf8.ValidString(msg[:n]) {
			n--
		}
		msg = msg[:n]
	}
	return msg
}

// failure makes the response of a Failure.
func (e *Executable) failure(requestID, command, operation string, f *envelope.Failure) *envelope.Response {
	err := f.Err
	err.Message = clip(err.Message, string(f.Status))
	c := e.command(command)
	return &envelope.Response{
		RequestID: requestID,
		Command:   command,
		Operation: operation,
		Status:    f.Status,
		Responder: e.responder(c),
		Receipts:  f.Receipts,
		Error:     &err,
	}
}

// capabilities answers the capabilities operation of c.
func (e *Executable) capabilities(c *Command) *envelope.Response {
	caps := e.Capabilities(c)
	if err := caps.Check(); err != nil {
		return e.failure("", c.Name, CapabilitiesOperation, envelope.Common(envelope.CodeInternal, "the capability declaration is not valid: "+err.Error()))
	}
	return &envelope.Response{
		Command:   c.Name,
		Operation: CapabilitiesOperation,
		Status:    envelope.Ok,
		Responder: e.responder(c),
		Body:      caps.Tree(),
	}
}

// handle runs the harness for an operation invocation and returns the
// response, with completed_at unset, and the continuation of a resident
// operation.
func (e *Executable) handle(inv Invocation) (resp *envelope.Response, after func() error) {
	c := inv.Command
	fail := func(requestID string, f *envelope.Failure) *envelope.Response {
		return e.failure(requestID, c.Name, inv.Operation, f)
	}
	op := c.operation(inv.Operation)
	if op == nil {
		return fail("", envelope.Common(envelope.CodeUnknownOperation, fmt.Sprintf("%s has no operation %q", c.Name, inv.Operation))), nil
	}

	// Size, before the request is parsed.
	limits := c.Limits.Resolved()
	if int64(len(inv.Request)) > limits.MaxRequestBytes {
		return fail("", envelope.Oversize("request", limits.MaxRequestBytes, int64(len(inv.Request)))), nil
	}

	// The whole request, before anything else is read or written.
	req, err := envelope.DecodeRequest(inv.Request)
	if err != nil {
		return fail(e.requestID(inv.Request), envelope.AsFailure(err)), nil
	}
	if err := req.Addressed(c.Name, op.Name); err != nil {
		return fail(req.RequestID, envelope.AsFailure(err)), nil
	}
	switch op.State {
	case envelope.Unsupported:
		return fail(req.RequestID, envelope.Common(envelope.CodeUnsupportedOperation, reason(op, "the operation is declared and not built"))), nil
	case envelope.Parked:
		return fail(req.RequestID, envelope.Common(envelope.CodeOperationParked, reason(op, "the operation is held closed by a recorded decision"))), nil
	}
	if !slices.Contains(op.Versions, req.OperationVersion) {
		return fail(req.RequestID, envelope.Common(envelope.CodeUnsupportedVersion, fmt.Sprintf("%s %s accepts body versions %v, not %s", c.Name, op.Name, op.Versions, req.OperationVersion))), nil
	}
	if op.RequestSchema == "" {
		return fail(req.RequestID, envelope.Common(envelope.CodeInternal, "the operation declares no request schema")), nil
	}
	if err := envelope.ValidateBody(req.Body, op.RequestSchema); err != nil {
		return fail(req.RequestID, envelope.AsFailure(err)), nil
	}

	// The home and the scope, before anything changes.
	home, err := e.checkHome(inv.Home, req, op)
	if err != nil {
		return fail(req.RequestID, envelope.AsFailure(err)), nil
	}
	if err := checkScope(home, req.Scope); err != nil {
		return fail(req.RequestID, envelope.AsFailure(err)), nil
	}

	call := &Call{
		Request:    req,
		Home:       home,
		Clock:      e.clock(),
		Stderr:     inv.Stderr,
		Executable: e,
		Command:    c,
		Operation:  op,
	}
	ok := func(body map[string]any, receipts []envelope.Receipt) *envelope.Response {
		if receipts == nil {
			receipts = []envelope.Receipt{}
		}
		return &envelope.Response{
			RequestID: req.RequestID,
			Command:   c.Name,
			Operation: op.Name,
			Status:    envelope.Ok,
			Responder: e.responder(c),
			Receipts:  receipts,
			Body:      body,
		}
	}

	if !op.Mutates {
		res, err := e.run(call)
		if err != nil {
			f := envelope.AsFailure(err)
			if len(f.Receipts) > 0 {
				f = envelope.Common(envelope.CodeInternal, "a read-only operation records nothing, and it returned receipts")
			}
			return fail(req.RequestID, f), nil
		}
		if len(res.Records)+len(res.Receipts)+len(res.Events) > 0 {
			return fail(req.RequestID, envelope.Common(envelope.CodeInternal, "a read-only operation records nothing, and it returned records")), nil
		}
		return ok(res.Body, nil), res.After
	}

	// A mutating operation: the store's lock, then the replay check.
	st, w, err := e.open(home)
	if err != nil {
		return fail(req.RequestID, envelope.AsFailure(err)), nil
	}
	defer w.Release()
	digest, err := envelope.Digest(inv.Request)
	if err != nil {
		return fail(req.RequestID, envelope.Common(envelope.CodeInternal, err.Error())), nil
	}
	recorded, err := readOutcome(st, req.RequestID)
	if err != nil {
		return fail(req.RequestID, envelope.AsFailure(err)), nil
	}
	if recorded != nil {
		if recorded.digest != digest {
			return fail(req.RequestID, envelope.Common(envelope.CodeReplayMismatch, "the request_id was used before with another request")), nil
		}
		resp := recorded.response
		resp.Replayed = true
		return resp, nil
	}
	call.Store, call.Lock = st, w

	res, err := e.run(call)
	if err != nil {
		f := envelope.AsFailure(err)
		resp := fail(req.RequestID, f)
		// An unknown outcome is never recorded as complete: what it leaves
		// is its intent, which the operation itself records before the
		// outside effect, and the harness writes nothing for it.
		if len(f.Receipts) == 0 || f.Status == envelope.Unknown {
			return resp, nil
		}
		// A refusal or a hold that leaves a record: the receipts and the
		// outcome, in one transaction.
		if err := e.stamp(resp); err != nil {
			return fail(req.RequestID, envelope.Common(envelope.CodeInternal, err.Error())), nil
		}
		if err := e.commit(st, w, req.RequestID, digest, resp, nil, f.Receipts, nil); err != nil {
			return fail(req.RequestID, envelope.AsFailure(err)), nil
		}
		return resp, nil
	}
	resp = ok(res.Body, res.Receipts)
	if err := e.stamp(resp); err != nil {
		return fail(req.RequestID, envelope.Common(envelope.CodeInternal, err.Error())), nil
	}
	// The response is sized before anything is written, so that a response
	// too large to return leaves nothing behind.
	data, err := resp.Encode()
	if err != nil {
		return fail(req.RequestID, envelope.Common(envelope.CodeInternal, "the response cannot be written: "+err.Error())), nil
	}
	if limit := limits.MaxResponseBytes; int64(len(data)) > limit {
		return fail(req.RequestID, envelope.Oversize("response", limit, int64(len(data)))), nil
	}
	if err := e.commit(st, w, req.RequestID, digest, resp, res.Records, res.Receipts, res.Events); err != nil {
		return fail(req.RequestID, envelope.AsFailure(err)), nil
	}
	return resp, res.After
}

// reason is the operation's declared reason, or the fallback.
func reason(op *Operation, fallback string) string {
	if op.Reason != "" {
		return op.Reason
	}
	return fallback
}

// requestID reads the request_id of a request that could not be validated,
// where one of the right form can be read, so that the response echoes it.
func (e *Executable) requestID(data []byte) string {
	v, err := envelope.LooseRequestID(data)
	if err != nil {
		return ""
	}
	return v
}

// run calls the handler, turning a panic into a failed response rather than
// a process that gave none, and every error into a Failure.
func (e *Executable) run(c *Call) (res *Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(c.Stderr, "%s %s: panic: %v\n%s", c.Command.Name, c.Operation.Name, r, debug.Stack())
			res, err = nil, envelope.Common(envelope.CodeInternal, fmt.Sprintf("the operation stopped unexpectedly: %v", r))
		}
	}()
	if c.Operation.Handle == nil {
		return nil, envelope.Common(envelope.CodeInternal, "the operation has no handler")
	}
	res, err = c.Operation.Handle(c)
	if err != nil {
		if f := envelope.AsFailure(err); f != nil {
			f.Err.Message = clip(f.Err.Message, string(f.Status))
			if cerr := f.Check(); cerr != nil {
				return nil, envelope.Common(envelope.CodeInternal, "the operation returned a failure the contract does not admit: "+cerr.Error())
			}
			return nil, f
		}
		return nil, envelope.Common(envelope.CodeInternal, err.Error())
	}
	if res == nil || res.Body == nil {
		return nil, envelope.Common(envelope.CodeInternal, "the operation returned ok without a body")
	}
	// A command never answers with a body it would not itself accept.
	if c.Operation.ResponseSchema != "" {
		if err := envelope.ValidateBody(res.Body, c.Operation.ResponseSchema); err != nil {
			return nil, envelope.Common(envelope.CodeInternal, "the operation returned a body that breaks its schema: "+envelope.AsFailure(err).Err.Message)
		}
	}
	return res, nil
}

// checkHome checks the home the command was pointed at against the request:
// the directory is a home, it is not inside a repository, and its identity
// is the one the request names.
func (e *Executable) checkHome(dir string, req *envelope.Request, op *Operation) (*store.Home, error) {
	home, err := store.CheckHome(dir)
	if err != nil {
		return nil, homeFailure(err)
	}
	switch {
	case home.HomeID == "" && !op.Provisions:
		return nil, envelope.Common(envelope.CodeHomeNotFound, "the directory holds no home record")
	case home.HomeID != "" && home.HomeID != req.HomeID:
		return nil, envelope.Common(envelope.CodeHomeMismatch, "the request names another home than the one the command was pointed at")
	}
	return home, nil
}

// homeFailure maps an error of checking or opening a home to its status.
func homeFailure(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return envelope.Common(envelope.CodeHomeNotFound, "the home directory does not exist")
	case errors.Is(err, store.ErrInsideRepository):
		return envelope.Common(envelope.CodeHomeInsideRepository, "a home is never inside a Git working tree")
	case errors.Is(err, store.ErrUnsupportedVersion):
		return envelope.Common(envelope.CodeUnsupportedVersion, err.Error())
	case errors.Is(err, store.ErrRecordMalformed), errors.Is(err, store.ErrLayout):
		return envelope.Common(envelope.CodeRecordMalformed, err.Error())
	}
	return envelope.Common(envelope.CodeIOFailure, err.Error())
}

// open opens the package's store in the home and takes its lock.
func (e *Executable) open(home *store.Home) (*store.Store, *store.Writer, error) {
	var journal store.Journal
	if e.Journal != nil {
		j, err := e.Journal(home.Dir)
		if err != nil {
			return nil, nil, envelope.Common(envelope.CodeRecordMalformed, "the home journal cannot be opened: "+err.Error())
		}
		journal = j
	}
	st, err := store.Open(home.Dir, e.Package, store.Options{Clock: e.clock(), Journal: journal, Waiting: e.Waiting})
	if err != nil {
		return nil, nil, homeFailure(err)
	}
	w, err := st.Lock()
	if err != nil {
		return nil, nil, storeFailure(err)
	}
	return st, w, nil
}

// storeFailure maps an error of the store to its status: the common code
// the error carries, or failed for one that carries none.
func storeFailure(err error) error {
	if f := envelope.AsFailure(err); f != nil {
		return f
	}
	if code := store.Code(err); code != "" {
		f := envelope.Common(code, err.Error())
		if code == envelope.CodeLockHeld {
			f.Retry()
		}
		return f
	}
	if errors.Is(err, store.ErrNoJournal) {
		return envelope.Common(envelope.CodeInternal, err.Error())
	}
	return envelope.Common(envelope.CodeIOFailure, err.Error())
}

// commit writes the records, the receipts and the events of an invocation
// with its recorded outcome, in one transaction.
func (e *Executable) commit(st *store.Store, w *store.Writer, requestID, digest string, resp *envelope.Response, records []store.RecordWrite, receipts []envelope.Receipt, events []store.Event) error {
	tx := store.Tx{RequestID: requestID, Records: records, Events: events}
	for i := range receipts {
		data, err := receipts[i].Encode()
		if err != nil {
			return envelope.Common(envelope.CodeInternal, "a receipt is not valid: "+err.Error())
		}
		tx.Receipts = append(tx.Receipts, store.Receipt{ID: receipts[i].ReceiptID, Content: data})
	}
	outcome, err := encodeOutcome(requestID, digest, resp)
	if err != nil {
		return envelope.Common(envelope.CodeInternal, err.Error())
	}
	tx.Outcome = outcome
	if _, err := w.Commit(tx); err != nil {
		return storeFailure(err)
	}
	return nil
}
