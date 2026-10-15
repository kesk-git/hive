// Package flushtest lets a test binary of another module package skip the
// real flush to the medium, which contract/internal/flushseam cannot offer
// outside contract/.
//
// On macOS a flush of a file is the full flush to the medium and costs
// milliseconds a call. A package whose tests write many records calls Skip
// from its TestMain so that the flush itself is not made. Every step the store
// reports to a test stays, so the recovery tests still observe the order of
// each write and flush. Skip panics unless the running binary is a test
// binary, so no delivered executable can turn the real flush off.
package flushtest

import (
	"testing"

	"github.com/kesk-git/hive/contract/internal/flushseam"
)

// Skip makes the store's flush of a file or a directory return without asking
// the file system to flush, for the rest of the test binary. It panics when
// the running binary is not a test binary.
func Skip() {
	if !testing.Testing() {
		panic("flushtest.Skip: called outside a test binary")
	}
	flushseam.Skip()
}
