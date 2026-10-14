package exec

import (
	"fmt"

	"github.com/kesk-git/hive/contract/envelope"
)

// Outcome is how a run ended: exactly one of these
// (spec/distribution.md, What a run reports).
type Outcome string

// The ways a run ends.
const (
	// NotStarted: not found, found and not executable, refused by the lookup
	// rules, or the start call failed.
	NotStarted Outcome = "not_started"
	// Exited: the program exited with a code, and Hive did not end it. On
	// Windows a code below 0xC0000000; a process another program ended with
	// TerminateProcess reports the code that program chose and reads as
	// exited.
	Exited Outcome = "exited"
	// Ended: the platform ended the process: on macOS and Linux by a signal
	// it did not handle, which Signal names; on Windows with an exception
	// status, 0xC0000000 or above, such as an access violation or Control-C.
	Ended Outcome = "ended"
	// StoppedByHive: Hive ended the tree, as recorded before it did, whatever
	// exit code or signal the end gave.
	StoppedByHive Outcome = "stopped_by_hive"
	// Lost: waiting failed, so how the run ended cannot be known.
	Lost Outcome = "lost"
)

// Result is the result of a run.
type Result struct {
	Outcome Outcome
	// Code is the exit code of an exited run. For an ended run on Windows it
	// is the exception status, and on macOS and Linux the number of the
	// signal. For a run Hive stopped it is whatever the end gave.
	Code int64
	// Signal names the signal of a run a signal ended or Hive ended, on macOS
	// and Linux; empty elsewhere.
	Signal string
	// Err is why a run is lost.
	Err error
}

func (r Result) String() string {
	switch r.Outcome {
	case Exited:
		return fmt.Sprintf("exited with code %d", r.Code)
	case Ended:
		if r.Signal != "" {
			return "ended by " + r.Signal
		}
		return fmt.Sprintf("ended with status %#x", uint32(r.Code))
	case Lost:
		return fmt.Sprintf("lost: %v", r.Err)
	}
	return string(r.Outcome)
}

// Passed says whether a project's validation command passed: it exited with
// code 0. Every other result is not a pass, a command that was missing,
// cancelled or timed out among them (spec/distribution.md, What a run
// reports).
func (r Result) Passed() bool { return r.Outcome == Exited && r.Code == 0 }

// External maps the result of an external program, run for the operation,
// to the operation's exit state. For an exited run it returns the code, which
// the operation reads by that program's own contract; Hive gives no code a
// meaning of its own, and the failure is nil. For an ended, stopped or lost
// run, where the program gave no result, it returns the failure: where the
// run may have had an effect outside Hive, an intent recorded before it
// started, outcome_unknown, reconciled before any retry; where it could
// have had none, unavailable with dependency_unusable, or
// platform_utility_unusable for a platform utility. A run that was not
// started maps by its StartError instead and has no result here. The message
// names the program by its declared name and how the run ended; the result,
// with its signal, status or error, is the failure's diagnostic.
func (r Result) External(p Program, mayHaveHadEffect bool) (code int64, failure *envelope.Failure) {
	switch r.Outcome {
	case Exited:
		return r.Code, nil
	case NotStarted:
		return 0, envelope.Common(envelope.CodeInternal, "a run that was not started has no result to map; its StartError maps it")
	}
	detail := fmt.Errorf("%s %s", p, r)
	if mayHaveHadEffect {
		return 0, envelope.Common(envelope.CodeOutcomeUnknown, p.Name+" "+r.reason()+" and gave no result; reconcile before any retry").Because(detail)
	}
	return 0, unavailable(p.Kind, envelope.CodeDependencyUnusable, codePlatformUtilityUnusable, p.Name+" "+r.reason()+" and gave no result").Because(detail)
}

// reason is how a run that gave no result ended, in a fixed phrase that holds
// no error's text, no signal and no status.
func (r Result) reason() string {
	switch r.Outcome {
	case Ended:
		return "was ended by the platform"
	case StoppedByHive:
		return "was stopped by Hive"
	case Lost:
		return "was lost, so how it ended cannot be known"
	}
	return "ended"
}

// HiveResult reads the result of another Hive command from its complete
// response and never from its exit code: the output must be one whole
// document that follows the response schema, echoes the request and agrees
// with the exit code. A run that leaves no such response, whatever its
// result, is unknown with outcome_unknown. A run that was not started is read
// from its StartError and not here.
func (r Result) HiveResult(output []byte, requestID string) (*envelope.Response, *envelope.Failure) {
	if r.Outcome != Exited {
		return nil, envelope.Common(envelope.CodeOutcomeUnknown, fmt.Sprintf("the command %s and left no response", r))
	}
	resp, err := envelope.AcceptResponse(output, int(r.Code), requestID)
	if err != nil {
		f := envelope.AsFailure(err)
		if f == nil {
			f = envelope.Common(envelope.CodeOutcomeUnknown, err.Error())
		}
		return nil, f
	}
	return resp, nil
}
