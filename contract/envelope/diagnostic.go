package envelope

import "unicode/utf8"

// The bounds of a diagnostic (spec/events.md, Kinds): the chain holds at most
// MaxDiagnosticDetail strings of at most MaxDetailBytes bytes each, and the
// stack of a panic at most MaxStackBytes bytes.
const (
	MaxDiagnosticDetail = 16
	MaxDetailBytes      = 1024
	MaxStackBytes       = 8192
)

// Diagnostic is what a failure holds beyond its message: the lower-layer
// error chain, outermost first, and for a panic its stack, never its value. It
// is never written into a response: the harness keeps it for the home's event
// journal, one diagnostic event of the answering package, and drops it where
// there is no journal to receive it.
type Diagnostic struct {
	Chain []string
	Stack string
}

// Empty reports whether the diagnostic holds nothing to record.
func (d *Diagnostic) Empty() bool {
	return d == nil || len(d.Chain) == 0 && d.Stack == ""
}

// Diagnostic returns what the Failure holds beyond its message, or nil.
func (f *Failure) Diagnostic() *Diagnostic { return f.diagnostic }

// DiagnosticOf is the diagnostic of an error chain, outermost first, or nil
// for no error. A Failure in the chain keeps its own diagnostic there, beneath
// it.
func DiagnosticOf(err error) *Diagnostic {
	if err == nil {
		return nil
	}
	d := &Diagnostic{}
	d.add(err)
	return d
}

// add appends err to the chain and then what lies beneath it: the errors it
// wraps, in order, or the diagnostic of a Failure, which wraps none.
func (d *Diagnostic) add(err error) {
	if len(d.Chain) >= MaxDiagnosticDetail {
		return
	}
	d.Chain = append(d.Chain, clipBytes(err.Error(), MaxDetailBytes))
	switch e := err.(type) {
	case *Failure:
		if e.diagnostic != nil {
			d.Chain = append(d.Chain, e.diagnostic.Chain[:min(len(e.diagnostic.Chain), MaxDiagnosticDetail-len(d.Chain))]...)
			if d.Stack == "" {
				d.Stack = e.diagnostic.Stack
			}
		}
	case interface{ Unwrap() []error }:
		for _, w := range e.Unwrap() {
			if w != nil {
				d.add(w)
			}
		}
	case interface{ Unwrap() error }:
		if w := e.Unwrap(); w != nil {
			d.add(w)
		}
	}
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

// Detail is what a Failure that answers in this one's place keeps of it: its
// diagnostic, or where it has none its code and message, so that the reason a
// failure was replaced is never lost with it.
func (f *Failure) Detail() *Diagnostic {
	if f == nil {
		return nil
	}
	if f.diagnostic != nil {
		return f.diagnostic
	}
	return &Diagnostic{Chain: []string{clipBytes(f.Err.Code+": "+f.Err.Message, MaxDetailBytes)}}
}

// Carry takes the detail of another Failure that this one answers in its
// place, where it has none of its own, and returns this one.
func (f *Failure) Carry(from *Failure) *Failure {
	if f.diagnostic == nil {
		f.diagnostic = from.Detail()
	}
	return f
}

// Panicked records the stack of a recovered panic as the detail of the
// Failure, and returns it. The value itself is never kept.
func (f *Failure) Panicked(stack []byte) *Failure {
	if f.diagnostic == nil {
		f.diagnostic = &Diagnostic{}
	}
	f.diagnostic.Stack = clipBytes(string(stack), MaxStackBytes)
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
