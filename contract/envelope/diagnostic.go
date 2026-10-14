package envelope

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// The bounds of a diagnostic (spec/events.md, Kinds): the chain holds at most
// MaxDiagnosticDetail strings of at most MaxDetailBytes bytes each, and the
// stack of a panic at most MaxStackBytes bytes.
const (
	MaxDiagnosticDetail = 16
	MaxDetailBytes      = 1024
	MaxStackBytes       = 8192
)

// Diagnostic is what a failure holds beyond its message: the lower-layer
// error chain, outermost first, and for a panic its stack and the Go type of
// its value, never the value. It is never written into a response: the
// harness keeps it for the home's event journal, one diagnostic event of the
// answering package, and drops it where there is no journal to receive it.
type Diagnostic struct {
	Chain     []string
	Stack     string
	PanicType string
}

// Empty reports whether the diagnostic holds nothing to record.
func (d *Diagnostic) Empty() bool {
	return d == nil || len(d.Chain) == 0 && d.Stack == "" && d.PanicType == ""
}

// Diagnostic returns what the Failure holds beyond its message, or nil.
func (f *Failure) Diagnostic() *Diagnostic { return f.diagnostic }

// DiagnosticOf is the diagnostic of an error chain, outermost first, or nil
// for no error.
func DiagnosticOf(err error) *Diagnostic {
	if err == nil {
		return nil
	}
	var chain []string
	for e := err; e != nil && len(chain) < MaxDiagnosticDetail; e = errors.Unwrap(e) {
		chain = append(chain, clipBytes(e.Error(), MaxDetailBytes))
	}
	return &Diagnostic{Chain: chain}
}

// Because records the error chain of err, outermost first, as the detail of
// the Failure, and returns it. The message stays the sentence it was made
// with: the chain is never part of it.
func (f *Failure) Because(err error) *Failure {
	if err != nil {
		f.diagnostic = DiagnosticOf(err)
	}
	return f
}

// Carry takes the detail of another Failure that this one answers in its
// place, and returns this one.
func (f *Failure) Carry(from *Failure) *Failure {
	if from != nil {
		f.diagnostic = from.diagnostic
	}
	return f
}

// Panicked records the stack and the Go type of a recovered panic's value as
// the detail of the Failure, and returns it. The value itself is never kept.
func (f *Failure) Panicked(value any, stack []byte) *Failure {
	if f.diagnostic == nil {
		f.diagnostic = &Diagnostic{}
	}
	f.diagnostic.Stack = clipBytes(string(stack), MaxStackBytes)
	f.diagnostic.PanicType = fmt.Sprintf("%T", value)
	return f
}

// clipBytes cuts s to at most n bytes, at a character boundary.
func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
