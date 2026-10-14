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
	if d == nil || len(d.Chain) != 3 || d.Chain[0] != err.Error() || d.Chain[2] != fs.ErrNotExist.Error() || d.Stack != "" || d.PanicType != "" {
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
// boundary, and the stack of a panic to 8,192 bytes; the panic's type is
// kept and its value is not.
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

	f := Common(CodeInternal, "x").Panicked("sk-live-secret", []byte(strings.Repeat("é", 5000)))
	d = f.Diagnostic()
	if d.PanicType != "string" || len(d.Stack) > MaxStackBytes || !utf8.ValidString(d.Stack) || len(d.Chain) != 0 {
		t.Errorf("type %q, stack %d bytes", d.PanicType, len(d.Stack))
	}
	if strings.Contains(d.PanicType+d.Stack, "secret") {
		t.Error("the value is kept")
	}
	if got := Common(CodeInternal, "x").Panicked(errors.New("secret"), nil).Diagnostic().PanicType; got != "*errors.errorString" {
		t.Errorf("type %q", got)
	}
}

// A failure that answers in place of another takes its detail.
func TestCarryTakesTheDetail(t *testing.T) {
	from := Common(CodeInternal, "x").Because(errors.New("lower layer"))
	to := Common(CodeOutcomeUnknown, "y").Carry(from)
	if to.Diagnostic() != from.Diagnostic() || Common(CodeInternal, "z").Carry(nil).Diagnostic() != nil {
		t.Error("detail not carried")
	}
}
