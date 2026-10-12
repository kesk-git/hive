package cli

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/store"
)

// The names of the two invocations every command answers without an
// operation of its own.
const (
	CapabilitiesOperation = envelope.CapabilitiesOperation
	VersionFlag           = "--version"
)

// The options of an operation invocation.
const (
	HomeOption         = "--home"
	RequestFileOption  = "--request-file"
	ResponseFileOption = "--response-file"
)

var (
	commandForm   = regexp.MustCompile(`^hive-[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	operationForm = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
)

// Executable is one package's executable: the one built for the package,
// which acts as whichever of the package's commands it was started under.
type Executable struct {
	// Package is the owning package, a package_name of the common schema.
	Package string
	// Name is the executable's own command name, such as hive-dispatch.
	Name string
	// Version is the implementation version, one line of text.
	Version string
	// SourceRevision is the repository revision the executable was built
	// from, where known.
	SourceRevision string
	// Commands are the commands the executable owns, the one named Name
	// among them.
	Commands []*Command
	// Clock gives every completed_at and written_at. Nil is the system clock.
	Clock clock.Clock
	// Waiting says how a mutating operation waits for its store's lock.
	Waiting store.Waiting
	// Storeless declares that the package owns no part of the home and keeps
	// no store, as word persists through the Pool. Check refuses a storeless
	// executable with a supported mutating operation.
	Storeless bool
	// Journal opens the home journal of the home at dir for a transaction
	// that appends events. Nil means the executable appends none; a
	// transaction that names events then fails.
	Journal func(dir string) (store.Journal, error)
	// Fault, when set by a test, is installed on every store the executable
	// opens, which calls it before each step of a write or of a recovery with
	// the step's name; an error it returns stops the store there, as a crash
	// would. A delivered executable sets none.
	Fault func(step string) error
}

// Command is one command name of an executable, with its limits and its
// operations.
type Command struct {
	// Name is the command name, such as hive-fleet.
	Name string
	// Limits are the command's declared limits; a zero limit is the default.
	Limits envelope.Limits
	// Operations are every operation of the command other than
	// capabilities.
	Operations []*Operation
}

// Operation is one operation of a command: its declaration, as capabilities
// reports it, and its handler.
type Operation struct {
	envelope.OperationDecl
	// Provisions marks the operation that gives a home its identity record.
	// It alone is admitted to a home directory that holds no home record
	// yet; every other operation finds no home there.
	Provisions bool
	// Handle does the operation's own work. It is called once the whole
	// request is validated and the home, the scope, the state, the version
	// and the replay are checked, with the store's lock held for a mutating
	// operation. It returns a *envelope.Failure to end with another status.
	Handle func(*Call) (*Result, error)
}

// Call is one validated invocation of an operation, as the handler sees it.
type Call struct {
	// Request is the whole request, validated.
	Request *envelope.Request
	// Home is the home the command was pointed at, checked.
	Home *store.Home
	// Store is the package's store in the home, opened and locked for a
	// mutating operation and nil for a read-only one. The handler reads
	// through it; what it writes it returns in its Result.
	Store *store.Store
	// Clock is the executable's clock.
	Clock clock.Clock
	// Stderr carries diagnostics for a person. Nothing in it is parsed.
	Stderr io.Writer

	Executable *Executable
	Command    *Command
	Operation  *Operation

	// recordedAt is the one time at which the records of a mutating
	// invocation are written.
	recordedAt time.Time
	// writer holds the store's lock for a mutating operation.
	writer *store.Writer
	// intent are the receipts of the intents CommitIntent made durable, in
	// order, as read back from what was written, and intended says that one
	// was made durable, whether or not it had receipts.
	intent   []envelope.Receipt
	intended bool
}

// Intent is what an operation makes durable before an effect outside Hive:
// the records, receipts and events of its intent (spec/commands.md, What a
// command guarantees, 4).
type Intent struct {
	Records  []store.RecordWrite
	Receipts []envelope.Receipt
	Events   []store.Event
}

// CommitIntent makes the intent durable, as one transaction of the store,
// and returns when it is. The request has no recorded outcome then and
// stays open: the operation goes on to its effect, and ends with ok, with a
// refusal or a hold, or with an unknown outcome, which returns exactly the
// receipts of the intents committed here and records no outcome. A record of
// the intent is written at the revision it expects plus one; an operation
// that replaces it later expects the revision returned here.
//
// Nothing outside Hive may happen before CommitIntent has returned without
// an error. A failure it returns says why nothing was written, as the
// store's refusals do, and the operation returns it. An intent whose
// receipts, with those of the intents before it, the unknown answer could not
// carry within the command's response limit is refused as too large, since
// the receipts committed are always returned. An error that is an
// *store.InterruptedError means the transaction is durable and not complete:
// the operation returns it unchanged, makes no effect, and the harness
// answers unknown; the next command to lock the store completes it.
//
// Once an intent is committed, the operation may end only with ok, refused,
// held or unknown. Any other status it returns is answered as unknown, with
// the receipts committed, since the effect may have happened and the status
// would promise that nothing did.
func (c *Call) CommitIntent(in Intent) ([]store.Written, error) {
	if c.Store == nil || c.writer == nil {
		return nil, errors.New("cli: a read-only operation commits no intent")
	}
	if len(in.Records)+len(in.Receipts)+len(in.Events) == 0 {
		return nil, errors.New("cli: an intent records something")
	}
	records := make([]store.RecordWrite, len(in.Records))
	for i, r := range in.Records {
		rec, _, err := c.Finalize(r)
		if err != nil {
			return nil, err
		}
		records[i] = rec
	}
	receipts, err := encodeReceipts(in.Receipts)
	if err != nil {
		return nil, err
	}
	written, err := decodeReceipts(receipts)
	if err != nil {
		return nil, err
	}
	if f := c.Executable.unknownCarries(c.Command, c.Operation, c.Request.RequestID, append(slices.Clone(c.intent), written...)); f != nil {
		return nil, f
	}
	done, err := c.writer.Commit(store.Tx{RequestID: c.Request.RequestID, Records: records, Receipts: receipts, Events: in.Events})
	if err != nil {
		var interrupted *store.InterruptedError
		if errors.As(err, &interrupted) {
			return nil, err
		}
		return nil, storeFailure(err)
	}
	c.intent = append(c.intent, written...)
	c.intended = true
	return done.Records, nil
}

// Committed returns the receipts of the intents this call has made durable,
// as they are stored. They are copies: changing them changes nothing the
// harness returns.
func (c *Call) Committed() ([]envelope.Receipt, error) {
	receipts, err := encodeReceipts(c.intent)
	if err != nil {
		return nil, err
	}
	return decodeReceipts(receipts)
}

// Result is what a handler returns for ok.
type Result struct {
	// Body is the operation's own response. It is required.
	Body map[string]any
	// Records, Receipts and Events are what the operation writes, committed
	// in one transaction with the recorded outcome. A record is written as
	// Call.Finalize makes it. A read-only operation returns none.
	Records  []store.RecordWrite
	Receipts []envelope.Receipt
	Events   []store.Event
	// After runs once the response has been written and flushed, for a
	// resident operation, which then runs until stopped. Its error ends the
	// process with exit code 1.
	After func() error
}

// Writer is the writer of a record this call makes.
func (c *Call) Writer() envelope.Writer {
	return envelope.Writer{
		Package:               c.Executable.Package,
		Command:               c.Command.Name,
		Operation:             c.Operation.Name,
		ImplementationVersion: c.Executable.Version,
	}
}

// Finalize returns the record as the store writes it, with its
// record_revision at Expected+1 and its updated_at at the one time the
// invocation's records are written, and the digest of that record. A handler
// that names a record's digest in its body or in a receipt returns the
// finalized record and takes the digest from here, so that what it returns is
// what is stored. A record the handler returns without it is finalized the
// same way before anything is sealed.
func (c *Call) Finalize(w store.RecordWrite) (store.RecordWrite, string, error) {
	if c.Store == nil {
		return w, "", errors.New("cli: a read-only operation writes no record")
	}
	at, err := clock.Format(c.recordedAt)
	if err != nil {
		return w, "", err
	}
	v, err := canon.Parse(w.Content)
	if err != nil {
		return w, "", fmt.Errorf("cli: content for %s: %w", w.Path, err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return w, "", fmt.Errorf("cli: content for %s is not a JSON object", w.Path)
	}
	obj["record_revision"] = w.Expected + 1
	obj["updated_at"] = at
	out, err := canon.Encode(obj)
	if err != nil {
		return w, "", fmt.Errorf("cli: content for %s: %w", w.Path, err)
	}
	digest, err := canon.RecordDigest(out)
	if err != nil {
		return w, "", fmt.Errorf("cli: content for %s: %w", w.Path, err)
	}
	w.Content = out
	return w, digest, nil
}

// Stamp is the clock's time as a timestamp.
func (c *Call) Stamp() (string, error) { return clock.Stamp(c.Clock) }

// NewReceipt makes a receipt of the kind for this call: a fresh receipt_id,
// written_at now, the writer, request, correlation, scope, source revision
// and authority of the request, and the subject, evidence and body given.
// Nil evidence is written empty, and so is a nil body.
func (c *Call) NewReceipt(kind, kindVersion string, subject *envelope.RecordRef, evidence []envelope.Evidence, body map[string]any) (envelope.Receipt, error) {
	id, err := ids.Mint(c.Clock, ids.Receipt)
	if err != nil {
		return envelope.Receipt{}, err
	}
	at, err := c.Stamp()
	if err != nil {
		return envelope.Receipt{}, err
	}
	r := envelope.Receipt{
		ReceiptID:      id,
		Kind:           kind,
		KindVersion:    kindVersion,
		WrittenAt:      at,
		Writer:         c.Writer(),
		RequestID:      c.Request.RequestID,
		CorrelationID:  c.Request.CorrelationID,
		Scope:          c.Request.Scope,
		SourceRevision: c.Request.SourceRevision,
		Authority:      c.Request.Authority,
		Subject:        subject,
		Evidence:       evidence,
		Body:           body,
	}
	if r.Evidence == nil {
		r.Evidence = []envelope.Evidence{}
	}
	if r.Body == nil {
		r.Body = map[string]any{}
	}
	return r, nil
}

// clock returns the executable's clock.
func (e *Executable) clock() clock.Clock {
	if e.Clock == nil {
		return clock.System{}
	}
	return e.Clock
}

// command finds the command of the given name, or nil.
func (e *Executable) command(name string) *Command {
	for _, c := range e.Commands {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// operation finds the operation of the given name, or nil.
func (c *Command) operation(name string) *Operation {
	for _, op := range c.Operations {
		if op.Name == name {
			return op
		}
	}
	return nil
}

// Capabilities is the declaration of the command, as its capabilities
// operation answers it.
func (e *Executable) Capabilities(c *Command) *envelope.Capabilities {
	caps := &envelope.Capabilities{
		Command:               c.Name,
		Package:               e.Package,
		Executable:            e.Name,
		ContractVersion:       envelope.ContractVersion,
		ImplementationVersion: e.Version,
		SourceRevision:        e.SourceRevision,
		Limits:                c.Limits.Resolved(),
		Operations:            make([]envelope.OperationDecl, 0, len(c.Operations)),
	}
	for _, op := range c.Operations {
		caps.Operations = append(caps.Operations, op.OperationDecl)
	}
	return caps
}

// Check checks the executable's description against the contract: the
// names have their forms, the executable owns the command of its own name,
// every command and operation is named once, and every command's capability
// declaration is valid. Main runs it; a package's tests may too.
func (e *Executable) Check() error {
	if !commandForm.MatchString(e.Name) {
		return fmt.Errorf("cli: %q is not a command name", e.Name)
	}
	if n := utf8.RuneCountInString(e.Version); n == 0 || n > 100 {
		return fmt.Errorf("cli: the implementation version is one to one hundred characters")
	}
	if e.command(e.Name) == nil {
		return fmt.Errorf("cli: the executable %s does not list the command of its own name", e.Name)
	}
	var names []string
	for _, c := range e.Commands {
		if !commandForm.MatchString(c.Name) {
			return fmt.Errorf("cli: %q is not a command name", c.Name)
		}
		if slices.Contains(names, c.Name) {
			return fmt.Errorf("cli: the command %s is listed twice", c.Name)
		}
		names = append(names, c.Name)
		var ops []string
		for _, op := range c.Operations {
			if !operationForm.MatchString(op.Name) || op.Name == CapabilitiesOperation {
				return fmt.Errorf("cli: %s: %q is not an operation name", c.Name, op.Name)
			}
			if slices.Contains(ops, op.Name) {
				return fmt.Errorf("cli: %s: the operation %s is listed twice", c.Name, op.Name)
			}
			ops = append(ops, op.Name)
			if op.State == envelope.Supported && op.Handle == nil {
				return fmt.Errorf("cli: %s %s is supported and has no handler", c.Name, op.Name)
			}
			if e.Storeless && op.State == envelope.Supported && op.Mutates {
				return fmt.Errorf("cli: %s %s mutates, and the executable %s is storeless: the delegating handoff is defined with the package that needs it", c.Name, op.Name, e.Name)
			}
		}
		if err := e.Capabilities(c).Check(); err != nil {
			return fmt.Errorf("cli: %s: %w", c.Name, err)
		}
		for _, op := range c.Operations {
			if op.State == envelope.Supported && op.Mutates {
				if err := e.floor(c, op); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// floor checks that the command's response limit holds every answer of a
// durable transaction of the operation that is not the response as recorded:
// the outcome_unknown answers of spec/commands.md, Size, which are never
// replaced by too_large. Check runs it for every supported mutating
// operation, and the harness runs it again for the operation in hand before
// its store is touched, so that no entry can reach a transaction it could
// not answer for.
func (e *Executable) floor(c *Command, op *Operation) error {
	limit := c.Limits.Resolved().MaxResponseBytes
	for _, message := range []string{interruptedMessage, oversizeReplayMessage} {
		resp := e.unknownAnswer(c, op.Name, sampleRequestID, message)
		resp.CompletedAt = sampleTimestamp
		data, err := resp.Encode()
		if err != nil {
			return fmt.Errorf("cli: %s %s: %w", c.Name, op.Name, err)
		}
		if int64(len(data)) > limit {
			return fmt.Errorf("cli: %s: max_response_bytes %d cannot hold the %d-byte answer of an interrupted transaction of %s", c.Name, limit, len(data), op.Name)
		}
	}
	return nil
}

// The request identity and the time a response is sized with.
const (
	sampleRequestID = "req_00000000000000000000000000"
	sampleTimestamp = "2026-10-11T08:00:00.000Z"
)
