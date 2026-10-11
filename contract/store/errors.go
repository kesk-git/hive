package store

import "errors"

// The errors of the store. Each that crosses a command boundary carries a
// common error code of spec/commands.md, which Code reads back. Test for one
// with errors.Is.
var (
	// ErrRecordMalformed reports a record that cannot be read or fails its
	// form, or an intent that recovery cannot complete. Nothing repairs it by
	// guessing (spec/records.md, Writing one record).
	ErrRecordMalformed = errors.New("store: record is malformed")
	// ErrUnsupportedVersion reports a record whose schema_version is newer,
	// major or minor, than this build reads. It is refused and never
	// rewritten, and it is not a malformed record (spec/records.md,
	// Versioning).
	ErrUnsupportedVersion = errors.New("store: record version is newer than this build reads")
	// ErrRevisionConflict reports a record whose revision on disk is not the
	// one a mutation expected. Nothing is written.
	ErrRevisionConflict = errors.New("store: record revision differs from the one expected")
	// ErrAlreadyExists reports a file a mutation expected to be absent.
	ErrAlreadyExists = errors.New("store: file already exists")
	// ErrLockHeld reports a transaction lock that was not released within the
	// bounded wait.
	ErrLockHeld = errors.New("store: lock held by another command")
	// ErrInsideRepository reports a home inside a Git working tree, which a
	// command refuses (spec/identities.md, The four identity records).
	ErrInsideRepository = errors.New("store: home is inside a Git working tree")
	// ErrLayout reports a home whose top level is not the one spec/records.md
	// fixes: a known entry of the wrong kind, or access beyond the owner.
	ErrLayout = errors.New("store: home layout")
	// ErrNoJournal reports a transaction that names events while the store
	// was opened without a Journal to append them to.
	ErrNoJournal = errors.New("store: the transaction names events but the store has no journal")
)

// InterruptedError reports a transaction that is durable and is not known to
// be complete: its intent is on disk, or its outcome is, and something
// stopped the store before it could say which. Whatever the cause, the
// transaction is no longer one that wrote nothing, and the next lock
// completes what is left of it. A failure before the intent is durable is
// never an InterruptedError. Err is the cause, which errors.Is and Code see
// through.
//
// Commit returns one naming its own request. The recovery Lock runs returns
// one naming the request whose intent stopped it. LockFor returns one naming
// the request in hand from every error it can fail with, whichever step
// failed and whichever intent stopped it, whenever that request's own
// transaction is durable; the error it wraps may be an InterruptedError
// naming another request's intent.
type InterruptedError struct {
	// RequestID is the request whose transaction is durable and was
	// interrupted.
	RequestID string
	Err       error
}

func (e *InterruptedError) Error() string { return e.Err.Error() }

func (e *InterruptedError) Unwrap() error { return e.Err }

// Code returns the common error code of spec/commands.md that err carries,
// or "" for an error that has none.
func Code(err error) string {
	switch {
	case errors.Is(err, ErrRecordMalformed):
		return "record_malformed"
	case errors.Is(err, ErrUnsupportedVersion):
		return "unsupported_version"
	case errors.Is(err, ErrRevisionConflict):
		return "revision_conflict"
	case errors.Is(err, ErrAlreadyExists):
		return "already_exists"
	case errors.Is(err, ErrLockHeld):
		return "lock_held"
	case errors.Is(err, ErrInsideRepository):
		return "home_inside_repository"
	}
	return ""
}
