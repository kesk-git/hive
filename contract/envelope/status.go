package envelope

import (
	"errors"
	"fmt"
	"regexp"
)

// Status is one of the eleven exit states of spec/commands.md. Each has one
// exit code and one meaning.
type Status string

// The eleven statuses, in the order of the table.
const (
	Ok          Status = "ok"
	Failed      Status = "failed"
	Usage       Status = "usage"
	Invalid     Status = "invalid"
	Refused     Status = "refused"
	Conflict    Status = "conflict"
	NotFound    Status = "not_found"
	TooLarge    Status = "too_large"
	Unavailable Status = "unavailable"
	Unknown     Status = "unknown"
	Held        Status = "held"
)

// statuses lists every status at the index of its exit code.
var statuses = []Status{Ok, Failed, Usage, Invalid, Refused, Conflict, NotFound, TooLarge, Unavailable, Unknown, Held}

// MaxExitCode is the largest exit code of the contract. Codes 11 to 63 are
// reserved for it, and Hive never exits with 64 or above.
const MaxExitCode = 10

// ReservedExitCode is the first exit code reserved for a later state.
const ReservedExitCode = 11

// Statuses returns the eleven statuses in exit-code order.
func Statuses() []Status { return append([]Status(nil), statuses...) }

// Valid reports whether s is one of the eleven statuses.
func (s Status) Valid() bool {
	for _, x := range statuses {
		if x == s {
			return true
		}
	}
	return false
}

// ExitCode is the exit code of s. It panics for a value that is not a status.
func (s Status) ExitCode() int {
	for i, x := range statuses {
		if x == s {
			return i
		}
	}
	panic(fmt.Sprintf("envelope: %q is not a status", string(s)))
}

// StatusOf returns the status of an exit code, and false for a code that
// names none.
func StatusOf(exit int) (Status, bool) {
	if exit < 0 || exit >= len(statuses) {
		return "", false
	}
	return statuses[exit], true
}

// Changed reports whether an invocation that ended in s may have changed
// anything: ok changes what the operation defines, refused and held may
// leave a record of the refusal or the hold, and unknown may have changed
// anything at all. Every other status changes nothing.
func (s Status) Changed() bool {
	switch s {
	case Ok, Refused, Held, Unknown:
		return true
	}
	return false
}

// The common error codes of spec/commands.md, each written bare. A package's
// own codes are written <package>.<code> and never redefine one of these.
const (
	CodeInternal              = "internal"
	CodeIOFailure             = "io_failure"
	CodeUsage                 = "usage"
	CodeUnknownOperation      = "unknown_operation"
	CodeArgumentTooLarge      = "argument_too_large"
	CodeMalformedRequest      = "malformed_request"
	CodeSchemaViolation       = "schema_violation"
	CodeUnsupportedVersion    = "unsupported_version"
	CodeEnvelopeMismatch      = "envelope_mismatch"
	CodeUnsupportedOperation  = "unsupported_operation"
	CodeOperationParked       = "operation_parked"
	CodeAuthorityMissing      = "authority_missing"
	CodeAuthorityInsufficient = "authority_insufficient"
	CodeHomeMismatch          = "home_mismatch"
	CodeHomeInsideRepository  = "home_inside_repository"
	CodeScopeMismatch         = "scope_mismatch"
	CodeCloneIdentityMismatch = "clone_identity_mismatch"
	CodeRevisionConflict      = "revision_conflict"
	CodeLockHeld              = "lock_held"
	CodeAlreadyExists         = "already_exists"
	CodeReplayMismatch        = "replay_mismatch"
	CodeNotFound              = "not_found"
	CodeHomeNotFound          = "home_not_found"
	CodeRecordTooLarge        = "record_too_large"
	CodeDependencyMissing     = "dependency_missing"
	CodeDependencyUnusable    = "dependency_unusable"
	CodeAuthenticationRefused = "authentication_refused"
	CodeSourceUnreadable      = "source_unreadable"
	CodeRecordMalformed       = "record_malformed"
	CodeOutcomeUnknown        = "outcome_unknown"
	CodeHeld                  = "held"
)

// commonCodes maps each common code to its status, in the order of the table.
var commonCodes = []struct {
	code   string
	status Status
}{
	{CodeInternal, Failed},
	{CodeIOFailure, Failed},
	{CodeUsage, Usage},
	{CodeUnknownOperation, Usage},
	{CodeArgumentTooLarge, Usage},
	{CodeMalformedRequest, Invalid},
	{CodeSchemaViolation, Invalid},
	{CodeUnsupportedVersion, Invalid},
	{CodeEnvelopeMismatch, Invalid},
	{CodeUnsupportedOperation, Refused},
	{CodeOperationParked, Refused},
	{CodeAuthorityMissing, Refused},
	{CodeAuthorityInsufficient, Refused},
	{CodeHomeMismatch, Refused},
	{CodeHomeInsideRepository, Refused},
	{CodeScopeMismatch, Refused},
	{CodeCloneIdentityMismatch, Refused},
	{CodeRevisionConflict, Conflict},
	{CodeLockHeld, Conflict},
	{CodeAlreadyExists, Conflict},
	{CodeReplayMismatch, Conflict},
	{CodeNotFound, NotFound},
	{CodeHomeNotFound, NotFound},
	{CodeRecordTooLarge, TooLarge},
	{CodeDependencyMissing, Unavailable},
	{CodeDependencyUnusable, Unavailable},
	{CodeAuthenticationRefused, Unavailable},
	{CodeSourceUnreadable, Unavailable},
	{CodeRecordMalformed, Unavailable},
	{CodeOutcomeUnknown, Unknown},
	{CodeHeld, Held},
}

// CommonCodes returns the common error codes in the order of the table.
func CommonCodes() []string {
	out := make([]string, len(commonCodes))
	for i, c := range commonCodes {
		out[i] = c.code
	}
	return out
}

// StatusOfCode returns the status a common code belongs to, and false for a
// code that is not a common one.
func StatusOfCode(code string) (Status, bool) {
	for _, c := range commonCodes {
		if c.code == code {
			return c.status, true
		}
	}
	return "", false
}

var codeForm = regexp.MustCompile(`^([a-z][a-z0-9-]*\.)?[a-z][a-z0-9_]*$`)

// ValidCode reports whether code has the form of an error code: a common code
// written bare, or a package's code written <package>.<code>.
func ValidCode(code string) bool { return codeForm.MatchString(code) }

// Error is the error member of a response: a code, a message for a person,
// whether repeating the same request unchanged can succeed, and the fields
// the status requires.
type Error struct {
	Code      string
	Message   string
	Retryable bool
	// Hold names the recorded hold a held operation waits on.
	Hold *RecordRef
	// LimitBytes and ActualBytes are given with too_large. Nil is absent.
	LimitBytes  *int64
	ActualBytes *int64
	// Detail is the open member of an error: what the command knows beyond
	// the fields above, such as which record or field was too large.
	Detail map[string]any
}

// Failure is the Go error that ends an invocation with a status other than
// ok. A command returns one from an operation; the harness makes it the
// response's status and error. Receipts are the records a refusal or a hold
// leaves, which the harness writes and returns.
type Failure struct {
	Status   Status
	Err      Error
	Receipts []Receipt
}

// Fail makes a Failure of the given status and code with a message for a
// person. It panics for a status other than the ten that carry an error, or
// for a code that has no error-code form.
func Fail(status Status, code, message string) *Failure {
	if !status.Valid() || status == Ok {
		panic(fmt.Sprintf("envelope: %q is not a failing status", string(status)))
	}
	if !ValidCode(code) {
		panic(fmt.Sprintf("envelope: %q is not an error code", code))
	}
	return &Failure{Status: status, Err: Error{Code: code, Message: message}}
}

// Common makes a Failure of a common code, with the status the table gives
// that code. It panics for a code that is not a common one.
func Common(code, message string) *Failure {
	status, ok := StatusOfCode(code)
	if !ok {
		panic(fmt.Sprintf("envelope: %q is not a common error code", code))
	}
	return Fail(status, code, message)
}

// Oversize makes the too_large Failure of spec/commands.md, Size: code
// record_too_large, the limit and the actual size, and the record or field
// that was too large in detail when the command knows it ("" names none).
func Oversize(what string, limit, actual int64) *Failure {
	f := Common(CodeRecordTooLarge, fmt.Sprintf("%s is %d bytes; the limit is %d", nonEmpty(what, "the record"), actual, limit))
	f.Err.LimitBytes = &limit
	f.Err.ActualBytes = &actual
	if what != "" {
		f.Err.Detail = map[string]any{"record": what}
	}
	return f
}

// Hold makes the held Failure: code held and the hold it waits on.
func Hold(hold RecordRef, message string) *Failure {
	f := Common(CodeHeld, message)
	h := hold
	f.Err.Hold = &h
	return f
}

func nonEmpty(s, or string) string {
	if s == "" {
		return or
	}
	return s
}

// Retry marks the Failure as one a caller may repeat unchanged, and returns
// it. It is never set on unknown: an unknown outcome is reconciled, not
// retried.
func (f *Failure) Retry() *Failure {
	if f.Status != Unknown {
		f.Err.Retryable = true
	}
	return f
}

// With adds a detail member to the Failure's error and returns it.
func (f *Failure) With(key string, value any) *Failure {
	if f.Err.Detail == nil {
		f.Err.Detail = map[string]any{}
	}
	f.Err.Detail[key] = value
	return f
}

// Error makes Failure a Go error.
func (f *Failure) Error() string {
	return fmt.Sprintf("%s (%s): %s", f.Status, f.Err.Code, f.Err.Message)
}

// Check checks that the Failure is one the response schema admits: a status
// other than ok, a code of the right form, a message, the limit and actual
// sizes with too_large, the hold with held, and retryable false with unknown.
func (f *Failure) Check() error {
	switch {
	case !f.Status.Valid() || f.Status == Ok:
		return errors.New("envelope: a failure has a status other than ok")
	case !ValidCode(f.Err.Code):
		return errors.New("envelope: the error code has no error-code form")
	case f.Err.Message == "" || len(f.Err.Message) > 2000:
		return errors.New("envelope: the error message is empty or longer than 2000 bytes")
	case f.Status == TooLarge && (f.Err.LimitBytes == nil || f.Err.ActualBytes == nil):
		return errors.New("envelope: too_large carries limit_bytes and actual_bytes")
	case f.Status == Held && f.Err.Hold == nil:
		return errors.New("envelope: held names the hold it waits on")
	case f.Status == Unknown && f.Err.Retryable:
		return errors.New("envelope: an unknown outcome is reconciled, never retried")
	}
	return nil
}

// AsFailure returns the Failure err is or wraps, or nil.
func AsFailure(err error) *Failure {
	var f *Failure
	if errors.As(err, &f) {
		return f
	}
	return nil
}
