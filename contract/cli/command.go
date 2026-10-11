package cli

import (
	"fmt"
	"io"
	"regexp"
	"slices"

	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/store"
)

// The names of the two invocations every command answers without an
// operation of its own.
const (
	CapabilitiesOperation = "capabilities"
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
	// Journal opens the home journal of the home at dir for a transaction
	// that appends events. Nil means the executable appends none; a
	// transaction that names events then fails.
	Journal func(dir string) (store.Journal, error)
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
	// Store is the package's store in the home, opened for a mutating
	// operation and nil for a read-only one.
	Store *store.Store
	// Lock holds the store's transaction lock for a mutating operation. The
	// handler may replace single records through it; the records, receipts
	// and events it returns in its Result are committed with the outcome.
	Lock *store.Writer
	// Clock is the executable's clock.
	Clock clock.Clock
	// Stderr carries diagnostics for a person. Nothing in it is parsed.
	Stderr io.Writer

	Executable *Executable
	Command    *Command
	Operation  *Operation
}

// Result is what a handler returns for ok.
type Result struct {
	// Body is the operation's own response. It is required.
	Body map[string]any
	// Records, Receipts and Events are what the operation writes, committed
	// in one transaction with the recorded outcome. A read-only operation
	// returns none.
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
	if e.Version == "" || len(e.Version) > 100 {
		return fmt.Errorf("cli: the implementation version is one to one hundred bytes")
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
		}
		if err := e.Capabilities(c).Check(); err != nil {
			return fmt.Errorf("cli: %s: %w", c.Name, err)
		}
	}
	return nil
}
