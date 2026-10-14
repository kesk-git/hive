package exec

import (
	"fmt"

	"github.com/kesk-git/hive/contract/envelope"
)

// Reason is why a run was not started.
type Reason string

// The reasons a run is not started: each leaves no process behind.
const (
	// Missing: nothing of the name was found on the search path.
	Missing Reason = "missing"
	// NotExecutable: a regular file of the name was found and none has an
	// execute bit.
	NotExecutable Reason = "not_executable"
	// BatchFile: on Windows, no <name>.exe exists and a batch file of that
	// name does, which would run under cmd.exe, a shell.
	BatchFile Reason = "batch_file"
	// BreakawayRefused: on Windows, a resident process could not be started
	// outside every job.
	BreakawayRefused Reason = "breakaway_refused"
	// StartFailed: the start call failed.
	StartFailed Reason = "start_failed"
	// TooLarge: the argument list is over the platform's limit.
	TooLarge Reason = "too_large"
	// Refused: the run breaks a rule of the path: a name that is not a bare
	// word, an argument holding a NUL, on Windows one that is not valid
	// UTF-8, a working directory that is not absolute, or a stream or
	// variable the path does not admit.
	Refused Reason = "refused"
)

// StartError is the result not_started: the run was refused or the program
// could not be started, and nothing runs. Reason says why.
type StartError struct {
	Reason  Reason
	Program Program
	// File is the file found, for NotExecutable and BatchFile.
	File string
	// Limit and Actual are the limit and the size of an argument list, for
	// TooLarge, in bytes.
	Limit, Actual int64
	// Err is the platform's error, for StartFailed and BreakawayRefused, and
	// the rule broken for Refused.
	Err error
}

func (e *StartError) Error() string {
	switch e.Reason {
	case Missing:
		return fmt.Sprintf("exec: %s was not found on the search path", e.Program)
	case NotExecutable:
		return fmt.Sprintf("exec: %s was found as %s, which is not executable", e.Program, e.File)
	case BatchFile:
		return fmt.Sprintf("exec: %s was found only as the batch file %s, which would run under cmd.exe, a shell", e.Program, e.File)
	case BreakawayRefused:
		return fmt.Sprintf("exec: %s was not started: the breakaway from every job was refused: %v", e.Program, e.Err)
	case TooLarge:
		return fmt.Sprintf("exec: the arguments of %s are %d long; the limit is %d", e.Program, e.Actual, e.Limit)
	case StartFailed:
		return fmt.Sprintf("exec: %s could not be started: %v", e.Program, e.Err)
	}
	return fmt.Sprintf("exec: %s was refused: %v", e.Program, e.Err)
}

func (e *StartError) Unwrap() error { return e.Err }

// Result is the result of a run that was not started: not_started.
func (e *StartError) Result() Result { return Result{Outcome: NotStarted} }

// Failure maps the run that was not started to the exit state of the
// operation that made it (spec/distribution.md, What a run reports). A
// program that is missing is unavailable with dependency_missing, or
// platform_utility_missing for a platform utility; one found and not usable,
// a batch file on Windows among them, is unavailable with dependency_unusable,
// or platform_utility_unusable, and a refused breakaway is the same; a run
// over the platform's limit is too_large. A run that breaks a rule of the
// path is the operation's own fault and is failed with internal: its
// arguments are fixed by the operation, and nothing about the program is
// known. The message is one sentence naming the program by its declared name
// and the reason; the error itself, with the file found or the platform's
// error, is the failure's diagnostic.
func (e *StartError) Failure() *envelope.Failure {
	name := e.Program.Name
	var f *envelope.Failure
	switch e.Reason {
	case Missing:
		f = unavailable(e.Program.Kind, envelope.CodeDependencyMissing, codePlatformUtilityMissing, name+" was not found on the search path")
	case NotExecutable:
		f = unavailable(e.Program.Kind, envelope.CodeDependencyUnusable, codePlatformUtilityUnusable, name+" was found and is not executable")
	case BatchFile:
		f = unavailable(e.Program.Kind, envelope.CodeDependencyUnusable, codePlatformUtilityUnusable, name+" was found only as a batch file, which would run under cmd.exe, a shell")
	case BreakawayRefused:
		f = unavailable(e.Program.Kind, envelope.CodeDependencyUnusable, codePlatformUtilityUnusable, name+" was not started: the breakaway from every job was refused")
	case StartFailed:
		f = unavailable(e.Program.Kind, envelope.CodeDependencyUnusable, codePlatformUtilityUnusable, name+" could not be started")
	case TooLarge:
		f = envelope.Oversize("the argument list", e.Limit, e.Actual)
	default:
		f = envelope.Common(envelope.CodeInternal, name+" was refused by the rules of the process-execution path")
	}
	return f.Because(e)
}

// The codes of a platform utility that is missing or unusable, which
// spec/commands.md lists among the common codes of unavailable.
const (
	codePlatformUtilityMissing  = "platform_utility_missing"
	codePlatformUtilityUnusable = "platform_utility_unusable"
)

// unavailable makes the unavailable failure of a program of the kind: the
// code of a dependency, or of a platform utility.
func unavailable(kind Kind, dependency, utility, message string) *envelope.Failure {
	code := dependency
	if kind == Platform {
		code = utility
	}
	return envelope.Fail(envelope.Unavailable, code, message)
}
