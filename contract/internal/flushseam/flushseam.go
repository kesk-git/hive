// Package flushseam is the one switch tests use to skip the real flush to the
// medium, and nothing else.
//
// On macOS Go's flush of a file is the full flush to the medium, which costs
// milliseconds a call and dominates the time of the contract's tests. No test
// can observe a flush reaching the medium: the recovery tests prove the order
// of the writes and flushes through the steps the store and the journal report
// to a test, and those steps are all kept. A test binary calls Skip from its
// TestMain, so that the flush itself is not made; every other step stays.
//
// The package is under contract/internal/, so Go lets no package outside
// contract/ import it. Test access from outside contract/ is documented by
// contract/flushtest.
// The store only reads it, through Skipped.
package flushseam

// skip is false in every delivered executable, which so makes the real flush.
var skip bool

// Skip makes the store's flush of a file or a directory return without asking
// the file system to flush, until Real. A test binary calls it from its
// TestMain.
func Skip() { skip = true }

// Real makes the store's flush the real flush again, for the test that runs
// it.
func Real() { skip = false }

// Skipped reports whether a test has turned the real flush off.
func Skipped() bool { return skip }
