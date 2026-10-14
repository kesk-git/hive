package envelope

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"unicode/utf8"
)

// A failure keeps its error chain, outermost first, beside its message and
// never in it or in the response it makes.
func TestBecauseKeepsTheChainApartFromTheMessage(t *testing.T) {
	err := fmt.Errorf("reading a record: %w", fmt.Errorf("open a path: %w", fs.ErrNotExist))
	f := Common(CodeIOFailure, "the record cannot be read").Because(err)
	d := f.Diagnostic()
	if d == nil || len(d.Chain) != 3 || d.Chain[0] != err.Error() || d.Chain[2] != fs.ErrNotExist.Error() || d.Stack != "" {
		t.Fatalf("diagnostic %+v", d)
	}
	if f.Err.Message != "the record cannot be read" {
		t.Errorf("message %q", f.Err.Message)
	}
	r := &Response{Status: f.Status, Error: &f.Err, Diagnostic: d, CompletedAt: "2026-10-11T08:00:00.000Z",
		Command: "hive-pool", RequestID: "req_00000000000000000000000001", Responder: Responder{Package: "pool", Command: "hive-pool", ImplementationVersion: "1", ContractVersion: ContractVersion}}
	data, err := r.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "open a path") {
		t.Errorf("the chain is in the response: %s", data)
	}
	back, err := DecodeResponse(data)
	if err != nil || back.Diagnostic != nil {
		t.Errorf("%v %+v", err, back)
	}
	if !(&Failure{}).Diagnostic().Empty() || Common(CodeInternal, "x").Because(nil).Diagnostic() != nil {
		t.Error("a failure with no cause holds a diagnostic")
	}
}

// The chain is cut to sixteen strings of at most 1,024 bytes, on a character
// boundary, and the stack of a panic to 8,192 bytes; its value is never kept.
func TestDiagnosticIsBounded(t *testing.T) {
	var err error = errors.New(strings.Repeat("é", 800))
	for i := 0; i < 20; i++ {
		err = fmt.Errorf("layer %d: %w", i, err)
	}
	d := Common(CodeInternal, "x").Because(err).Diagnostic()
	if len(d.Chain) != MaxDiagnosticDetail {
		t.Errorf("%d strings", len(d.Chain))
	}
	long := DiagnosticOf(errors.New(strings.Repeat("é", 800))).Chain[0]
	if len(long) > MaxDetailBytes || !utf8.ValidString(long) {
		t.Errorf("a string of %d bytes, valid %v", len(long), utf8.ValidString(long))
	}

	d = Common(CodeInternal, "x").Panicked([]byte(strings.Repeat("é", 5000))).Diagnostic()
	if len(d.Stack) > MaxStackBytes || !utf8.ValidString(d.Stack) || len(d.Chain) != 0 {
		t.Errorf("stack %d bytes", len(d.Stack))
	}
}

// A failure that answers in place of another takes its detail, or where it has
// none its code and message, and keeps a detail of its own.
func TestCarryTakesTheDetail(t *testing.T) {
	from := Common(CodeInternal, "x").Because(errors.New("lower layer"))
	to := Common(CodeOutcomeUnknown, "y").Carry(from)
	if to.Diagnostic() != from.Diagnostic() || Common(CodeInternal, "z").Carry(nil).Diagnostic() != nil {
		t.Error("detail not carried")
	}
	bare := Common(CodeDependencyMissing, "the program is gone")
	d := Common(CodeOutcomeUnknown, "y").Carry(bare).Diagnostic()
	if d == nil || len(d.Chain) != 1 || d.Chain[0] != "dependency_missing: the program is gone" {
		t.Errorf("diagnostic %+v", d)
	}
	own := Common(CodeOutcomeUnknown, "y").Because(errors.New("own"))
	if got := own.Carry(from).Diagnostic(); got.Chain[0] != "own" {
		t.Errorf("diagnostic %+v", got)
	}
}

// A Failure wrapped by another error keeps its own diagnostic beneath it,
// whether it is wrapped alone or beside another error.
func TestAWrappedFailureKeepsItsDiagnostic(t *testing.T) {
	inner := Common(CodeSchemaViolation, "the body breaks a schema").Because(errors.New("every violation"))
	d := Common(CodeInternal, "x").Because(inner).Diagnostic()
	if len(d.Chain) != 2 || d.Chain[0] != inner.Error() || d.Chain[1] != "every violation" {
		t.Errorf("chain %q", d.Chain)
	}
	check := errors.New("the check")
	d = DiagnosticOf(fmt.Errorf("%w: %w", inner, check))
	if len(d.Chain) != 4 || d.Chain[1] != inner.Error() || d.Chain[2] != "every violation" || d.Chain[3] != "the check" {
		t.Errorf("chain %q", d.Chain)
	}
	panicked := Common(CodeInternal, "stopped").Panicked([]byte("goroutine 1"))
	if d := DiagnosticOf(fmt.Errorf("wrapped: %w", panicked)); d.Stack != "goroutine 1" || len(d.Chain) != 2 {
		t.Errorf("diagnostic %+v", d)
	}
	var deep error = Common(CodeInternal, "x").Because(errors.New(strings.Repeat("y", 10)))
	for i := 0; i < 20; i++ {
		deep = fmt.Errorf("layer %d: %w", i, deep)
	}
	if n := len(DiagnosticOf(deep).Chain); n != MaxDiagnosticDetail {
		t.Errorf("%d strings", n)
	}
}
