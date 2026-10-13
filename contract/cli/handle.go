package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"runtime/debug"
	"slices"
	"time"
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
// returns its outcome, whose response is complete and encoded. The request
// document is the only form: Main hands Handle the one it read, and a
// package's tests may hand it one directly, past Main's Check and its read,
// so Handle validates and handles it whole whoever calls. Handle never
// returns an error for what the contract turns into a status; the error it
// does return is one the executable cannot answer at all, which Main
// reports on standard error and ends with exit code 1.
func (e *Executable) Handle(inv Invocation) (*Outcome, error) {
	if inv.Command == nil {
		return nil, errors.New("cli: the invocation names no command")
	}
	if inv.Stderr == nil {
		inv.Stderr = io.Discard
	}
	resp, after, err := e.handle(inv)
	if err != nil {
		return nil, err
	}
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

// clip cuts a message to what the response schema admits, in characters, and
// puts or in place of an empty one.
func clip(msg, or string) string {
	if msg == "" {
		return or
	}
	if utf8.RuneCountInString(msg) > envelope.MaxMessageChars {
		n := 0
		for i := range msg {
			if n == envelope.MaxMessageChars {
				return msg[:i]
			}
			n++
		}
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
// operation. Its error is the one refusal that has no response: a mutating
// operation whose command's response limit is below the floor.
func (e *Executable) handle(inv Invocation) (resp *envelope.Response, after func() error, err error) {
	c := inv.Command
	fail := func(requestID string, f *envelope.Failure) *envelope.Response {
		return e.failure(requestID, c.Name, inv.Operation, f)
	}
	op := c.operation(inv.Operation)
	if op == nil {
		return fail("", envelope.Common(envelope.CodeUnknownOperation, fmt.Sprintf("%s has no operation %q", c.Name, inv.Operation))), nil, nil
	}

	// Size, before the request is parsed.
	limits := c.Limits.Resolved()
	if int64(len(inv.Request)) > limits.MaxRequestBytes {
		return fail("", envelope.Oversize("request", limits.MaxRequestBytes, int64(len(inv.Request)))), nil, nil
	}

	// The whole request, before anything else is read or written.
	req, err := envelope.DecodeRequest(inv.Request)
	if err != nil {
		f := envelope.AsFailure(err)
		id := e.requestID(inv.Request)
		if id == "" && f.Err.Code != envelope.CodeMalformedRequest {
			f = envelope.Common(envelope.CodeMalformedRequest, "the request names no request_id that can be read: "+f.Err.Message)
		}
		return fail(id, f), nil, nil
	}
	if err := req.Addressed(c.Name, op.Name); err != nil {
		return fail(req.RequestID, envelope.AsFailure(err)), nil, nil
	}
	switch op.State {
	case envelope.Unsupported:
		return fail(req.RequestID, envelope.Common(envelope.CodeUnsupportedOperation, reason(op, "the operation is declared and not built"))), nil, nil
	case envelope.Parked:
		return fail(req.RequestID, envelope.Common(envelope.CodeOperationParked, reason(op, "the operation is held closed by a recorded decision"))), nil, nil
	}
	if !slices.Contains(op.Versions, req.OperationVersion) {
		return fail(req.RequestID, envelope.Common(envelope.CodeUnsupportedVersion, fmt.Sprintf("%s %s accepts body versions %v, not %s", c.Name, op.Name, op.Versions, req.OperationVersion))), nil, nil
	}
	if op.RequestSchema == "" {
		return fail(req.RequestID, envelope.Common(envelope.CodeInternal, "the operation declares no request schema")), nil, nil
	}
	if err := envelope.ValidateBody(req.Body, op.RequestSchema); err != nil {
		return fail(req.RequestID, envelope.AsFailure(err)), nil, nil
	}

	// The home and the scope, before anything changes.
	home, err := e.checkHome(inv.Home, req, op)
	if err != nil {
		return fail(req.RequestID, envelope.AsFailure(err)), nil, nil
	}
	if err := checkScope(home, req.Scope); err != nil {
		return fail(req.RequestID, envelope.AsFailure(err)), nil, nil
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
	// failed is the response of a handler's error, checked whole: the failure
	// that says why it cannot be given replaces one that cannot, and whole is
	// false then.
	failed := func(f *envelope.Failure) (resp *envelope.Response, whole bool) {
		resp = fail(req.RequestID, f)
		if bad := e.preflight(c, resp); bad != nil {
			return bad, false
		}
		return resp, true
	}

	if !op.Mutates {
		res, err := e.run(call)
		if err != nil {
			resp, _ := failed(envelope.AsFailure(err))
			return resp, nil, nil
		}
		if len(res.Records)+len(res.Receipts)+len(res.Events) > 0 {
			return fail(req.RequestID, envelope.Common(envelope.CodeInternal, "a read-only operation records nothing, and it returned records")), nil, nil
		}
		return ok(res.Body, nil), res.After, nil
	}

	// settle is the one answer to what the store reports of this request:
	// every path of a mutating operation that runs after the store is
	// touched answers through it.
	settle := func(resp *envelope.Response, err error) *envelope.Response {
		return e.settle(c, op, req.RequestID, resp, err, inv.Stderr, call)
	}
	// bail is the answer to a failure of the harness's own after the
	// operation ran: failed, when nothing of the request is durable, and
	// otherwise unknown with the receipts of the intents committed, since the
	// effect after them may have happened.
	bail := func(f *envelope.Failure) *envelope.Response {
		if !call.intended {
			return fail(req.RequestID, f)
		}
		return e.unknownAfterIntent(c, op, req.RequestID, f.Err.Message, call.intent)
	}

	// A mutating operation. The floor first: the limit holds every answer a
	// durable transaction can need, or the store is never touched, whichever
	// entry the invocation came through. Main refused the executable for
	// this at its start; Handle refuses the operation here, the same way,
	// with no response: no status it could give would fit a limit below the
	// floor without promising that nothing changed.
	if err := e.floor(c, op); err != nil {
		return nil, nil, fmt.Errorf("%w: the operation is refused before anything is written", err)
	}
	// Then the store's lock, for this request, and the replay check.
	at := &recordClock{}
	st, w, err := e.open(home, req.RequestID, at)
	if err != nil {
		return settle(nil, err), nil, nil
	}
	defer w.Release()
	digest, err := envelope.Digest(inv.Request)
	if err != nil {
		return fail(req.RequestID, envelope.Common(envelope.CodeInternal, err.Error())), nil, nil
	}
	recorded, err := readOutcome(st, req.RequestID)
	if err != nil {
		return fail(req.RequestID, envelope.AsFailure(err)), nil, nil
	}
	if recorded != nil {
		if recorded.digest != digest {
			return fail(req.RequestID, envelope.Common(envelope.CodeReplayMismatch, "the request_id was used before with another request")), nil, nil
		}
		resp := recorded.response
		resp.Replayed = true
		return settle(resp, nil), nil, nil
	}
	at.at = e.clock().Now()
	call.Store, call.recordedAt, call.writer = st, at.at, w

	res, err := e.run(call)
	if err != nil {
		// A transaction of this request that is durable and not complete: the
		// intent an operation committed, interrupted. The operation made no
		// effect after it, and the next lock completes it.
		var interrupted *store.InterruptedError
		if errors.As(err, &interrupted) && interrupted.RequestID == req.RequestID {
			return settle(nil, err), nil, nil
		}
		f, own, record := e.afterIntent(envelope.AsFailure(err), call, inv.Stderr, c, op)
		resp, whole := failed(f)
		// A refusal or a hold that leaves a record: the receipts and the
		// outcome, in one transaction, once the response is known to be
		// whole. A failure that leaves nothing, and an unknown outcome,
		// which is never recorded as complete, write nothing.
		if !whole || !record {
			if !whole && call.intended {
				return bail(f), nil, nil
			}
			return resp, nil, nil
		}
		return settle(resp, e.commit(w, req.RequestID, digest, resp, nil, own, nil)), nil, nil
	}
	// The records are final before anything is sealed.
	for i := range res.Records {
		rec, _, err := call.Finalize(res.Records[i])
		if err != nil {
			return bail(envelope.Common(envelope.CodeInternal, "the operation returned a record that cannot be written: "+err.Error())), nil, nil
		}
		res.Records[i] = rec
	}
	// Every receipt of the request is returned: those of the intents first.
	resp = ok(res.Body, append(slices.Clone(call.intent), res.Receipts...))
	// The response is checked and sized before anything is written, so that a
	// response too large to return leaves nothing behind. After an intent
	// something is behind, and the answer is unknown.
	if bad := e.preflight(c, resp); bad != nil {
		if call.intended {
			return bail(envelope.Common(envelope.CodeInternal, bad.Error.Message)), nil, nil
		}
		return bad, nil, nil
	}
	if err := e.commit(w, req.RequestID, digest, resp, res.Records, res.Receipts, res.Events); err != nil {
		return settle(resp, err), nil, nil
	}
	return settle(resp, nil), res.After, nil
}

// recordClock is the clock of the store: the one time at which the records of
// an invocation are written, set once its lock is held.
type recordClock struct{ at time.Time }

func (c *recordClock) Now() time.Time { return c.at }

// preflight stamps the response and checks that it can be written whole:
// valid under the response schema and within the command's limit. It returns
// the failure response to give in its place, or nil.
func (e *Executable) preflight(c *Command, resp *envelope.Response) *envelope.Response {
	instead := func(f *envelope.Failure) *envelope.Response {
		return e.failure(resp.RequestID, resp.Command, resp.Operation, f)
	}
	if err := e.stamp(resp); err != nil {
		return instead(envelope.Common(envelope.CodeInternal, err.Error()))
	}
	data, err := resp.Encode()
	if err != nil {
		return instead(envelope.Common(envelope.CodeInternal, "the response cannot be written: "+err.Error()))
	}
	if limit := c.Limits.Resolved().MaxResponseBytes; int64(len(data)) > limit {
		return instead(envelope.Oversize("response", limit, int64(len(data))))
	}
	return nil
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
		// The intent of this request, durable and not complete: handle answers it.
		var interrupted *store.InterruptedError
		if errors.As(err, &interrupted) {
			return nil, err
		}
		if f := envelope.AsFailure(err); f != nil {
			f.Err.Message = clip(f.Err.Message, string(f.Status))
			if cerr := f.Check(); cerr != nil {
				return nil, envelope.Common(envelope.CodeInternal, "the operation returned a failure the contract does not admit: "+cerr.Error())
			}
			// Only a refusal or a hold of a mutating operation leaves receipts.
			// An unknown outcome returns the receipts of the intents the call
			// committed, which the harness attaches, and none of its own.
			if len(f.Receipts) > 0 && (!c.Operation.Mutates || (f.Status != envelope.Refused && f.Status != envelope.Held)) {
				return nil, envelope.Common(envelope.CodeInternal, "the operation returned receipts with a status that leaves none")
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

// open opens the package's store in the home and takes its lock for the
// request, so that the store's report of a failure says whether the
// request's own transaction is durable.
func (e *Executable) open(home *store.Home, requestID string, clk clock.Clock) (*store.Store, *store.Writer, error) {
	var journal store.Journal
	if e.Journal != nil {
		j, err := e.Journal(home.Dir)
		if err != nil {
			return nil, nil, envelope.Common(envelope.CodeRecordMalformed, "the home journal cannot be opened: "+err.Error())
		}
		journal = j
	}
	st, err := store.Open(home.Dir, e.Package, store.Options{Clock: clk, Journal: journal, Waiting: e.Waiting})
	if err != nil {
		return nil, nil, homeFailure(err)
	}
	if e.Fault != nil {
		st.Fault(e.Fault)
	}
	w, err := st.LockFor(requestID)
	if err != nil {
		return nil, nil, err
	}
	return st, w, nil
}

// The whole of what a durable transaction says when its response cannot be
// given: one that is not known to be complete, and one that is complete and
// whose recorded response is larger than the limit now declared.
const (
	interruptedMessage    = "the transaction was recorded and is not known to be complete; sending the same request again completes it"
	oversizeReplayMessage = "the transaction is complete and its recorded response is larger than the response limit now declared; it stands as recorded"
)

// unknownAnswer is the answer of a durable transaction whose response cannot
// be given: outcome_unknown, not retryable, with no receipts and the message
// given. The floor holds it for every message a durable transaction can need.
func (e *Executable) unknownAnswer(c *Command, operation, requestID, message string) *envelope.Response {
	return e.failure(requestID, c.Name, operation, envelope.Common(envelope.CodeOutcomeUnknown, message))
}

// unknownMessage is the message of the unknown answer after a committed
// intent, which a reason for the failure may follow.
const unknownMessage = "the intent of this request is committed and its outcome is not established; reconcile before any retry"

// unknownWith is the unknown answer with the message and the receipts.
func (e *Executable) unknownWith(c *Command, op *Operation, requestID, message string, committed []envelope.Receipt) *envelope.Response {
	f := envelope.Common(envelope.CodeOutcomeUnknown, clip(message, string(envelope.Unknown)))
	f.Receipts = slices.Clone(committed)
	return e.failure(requestID, c.Name, op.Name, f)
}

// unknownAfterIntent is the answer of a request whose intent is committed and
// whose outcome is not established: outcome_unknown with exactly the receipts
// of the committed intents, which are on disk, and no outcome recorded. The
// reason is given where it fits; the answer without it always fits, since
// CommitIntent admits no intent whose receipts it could not carry.
func (e *Executable) unknownAfterIntent(c *Command, op *Operation, requestID, why string, committed []envelope.Receipt) *envelope.Response {
	if why != "" {
		if resp := e.unknownWith(c, op, requestID, unknownMessage+": "+why, committed); e.preflight(c, resp) == nil {
			return resp
		}
	}
	return e.unknownWith(c, op, requestID, unknownMessage, committed)
}

// unknownCarries checks that the unknown answers a committed intent can need,
// with the receipts of the intents, fit the response limit: the one after an
// effect and the one of an interrupted transaction. It returns the failure
// that refuses the intent, or nil.
func (e *Executable) unknownCarries(c *Command, op *Operation, requestID string, receipts []envelope.Receipt) *envelope.Failure {
	for _, message := range []string{unknownMessage, interruptedMessage} {
		bad := e.preflight(c, e.unknownWith(c, op, requestID, message, receipts))
		if bad == nil {
			continue
		}
		if bad.Error.LimitBytes != nil && bad.Error.ActualBytes != nil {
			return envelope.Oversize("the answer to an intent whose outcome is not established, with its receipts,", *bad.Error.LimitBytes, *bad.Error.ActualBytes)
		}
		return &envelope.Failure{Status: bad.Status, Err: *bad.Error}
	}
	return nil
}

// afterIntent shapes the failure an operation returned for what it committed
// before. Without a committed intent the failure stands, and a refusal or a
// hold records its own receipts. With one, the response carries the receipts
// of the intents: an unknown outcome returns exactly them and records
// nothing; a refusal or a hold returns them with its own and records its
// own and the outcome; any other status is answered as unknown, since the
// effect after the intent may have happened. own are the receipts to write,
// and record says whether a transaction records the outcome.
func (e *Executable) afterIntent(f *envelope.Failure, call *Call, stderr io.Writer, c *Command, op *Operation) (resp *envelope.Failure, own []envelope.Receipt, record bool) {
	committed := call.intent
	if !call.intended {
		return f, f.Receipts, len(f.Receipts) > 0
	}
	out := *f
	switch f.Status {
	case envelope.Unknown:
		out.Receipts = slices.Clone(committed)
		return &out, nil, false
	case envelope.Refused, envelope.Held:
		out.Receipts = append(slices.Clone(committed), f.Receipts...)
		return &out, f.Receipts, true
	}
	fmt.Fprintf(stderr, "%s %s: %s after its intent was committed; answered unknown: %s\n", c.Name, op.Name, f.Status, f.Err.Message)
	u := envelope.Common(envelope.CodeOutcomeUnknown, clip("the operation ended "+string(f.Status)+" after its intent was committed, and its effect is not established; reconcile before any retry", string(envelope.Unknown)))
	u.Receipts = slices.Clone(committed)
	return u, nil, false
}

// settle is the one function that turns what the store reports of a mutating
// request into its response, and every entry answers through it: err is what
// taking the lock with recovery, or committing, reported, and resp is, with
// err nil, the response as recorded for a replay or as just committed. The
// store reports a durable transaction of the request as an *InterruptedError
// naming it, and a recorded response is durable by being recorded. Once the
// transaction is durable the answer is only the response as recorded, when it
// fits the limit, or outcome_unknown, which fits by the floor checked before
// the store was touched; never too_large, which would promise that nothing
// changed. An error that left nothing of the request behind is answered by
// the code it carries, and too_large replaces that answer where it does not
// fit. Sizing is done here, so that no entry can replace a durable answer.
func (e *Executable) settle(c *Command, op *Operation, requestID string, resp *envelope.Response, err error, stderr io.Writer, call *Call) *envelope.Response {
	durable := true
	if err != nil {
		var interrupted *store.InterruptedError
		durable = errors.As(err, &interrupted) && interrupted.RequestID == requestID
		switch {
		case durable:
			fmt.Fprintln(stderr, err)
			resp = e.unknownAnswer(c, op.Name, requestID, interruptedMessage)
			resp.Receipts = slices.Clone(call.intent)
		case call.intended:
			// Nothing more of the request is durable, but its intent is, and
			// an effect may have followed it.
			fmt.Fprintln(stderr, err)
			return e.unknownAfterIntent(c, op, requestID, "", call.intent)
		default:
			resp = e.failure(requestID, c.Name, op.Name, storeFailure(err))
		}
	}
	if bad := e.preflight(c, resp); bad != nil {
		if !durable {
			return bad
		}
		fmt.Fprintf(stderr, "%s %s: the recorded response of %s is %s\n", c.Name, op.Name, requestID, bad.Error.Message)
		return e.unknownAnswer(c, op.Name, requestID, oversizeReplayMessage)
	}
	return resp
}

// storeFailure maps an error of the store, of opening it, of recovering it or
// of committing to it, that left nothing of the request in hand behind, to
// its failure: the transaction of an earlier request that stops the store
// before this request writes anything is record_malformed, and any other
// error is the common code it carries, or failed for one that carries none.
// A transaction of the request in hand that is durable never reaches it;
// settle answers that.
func storeFailure(err error) *envelope.Failure {
	var interrupted *store.InterruptedError
	if errors.As(err, &interrupted) {
		return envelope.Common(envelope.CodeRecordMalformed, "an earlier transaction of this store, "+interrupted.RequestID+", cannot be completed, and nothing of this request was written")
	}
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
func (e *Executable) commit(w *store.Writer, requestID, digest string, resp *envelope.Response, records []store.RecordWrite, receipts []envelope.Receipt, events []store.Event) error {
	tx := store.Tx{RequestID: requestID, Records: records, Events: events}
	var err error
	if tx.Receipts, err = encodeReceipts(receipts); err != nil {
		return envelope.Common(envelope.CodeInternal, err.Error())
	}
	outcome, err := encodeOutcome(requestID, digest, resp)
	if err != nil {
		return envelope.Common(envelope.CodeInternal, err.Error())
	}
	tx.Outcome = outcome
	_, err = w.Commit(tx)
	return err
}

// decodeReceipts reads receipts back from what a transaction writes, so that
// what is returned of them is what is stored, and shares nothing with the
// receipts they were made from.
func decodeReceipts(written []store.Receipt) ([]envelope.Receipt, error) {
	out := make([]envelope.Receipt, 0, len(written))
	for _, w := range written {
		r, err := envelope.DecodeReceipt(w.Content)
		if err != nil {
			return nil, fmt.Errorf("a receipt is not valid: %w", err)
		}
		out = append(out, *r)
	}
	return out, nil
}

// encodeReceipts makes the receipts of a transaction.
func encodeReceipts(receipts []envelope.Receipt) ([]store.Receipt, error) {
	var out []store.Receipt
	for i := range receipts {
		data, err := receipts[i].Encode()
		if err != nil {
			return nil, fmt.Errorf("a receipt is not valid: %w", err)
		}
		out = append(out, store.Receipt{ID: receipts[i].ReceiptID, Content: data})
	}
	return out, nil
}
