// Package flushseam is the one switch tests use to skip the real flush to the
// medium, and nothing else.
//
// On macOS Go's flush of a file is the full flush to the medium, which costs
// milliseconds a call and dominates the time of the contract's tests. No test
// can observe a flush reaching the medium: the recovery tests prove the order
// of the writes and flushes through the steps the store and the journal report
// to a test, and those steps are all kept. A test binary sets Skip from its
// TestMain, so that the flush itself is not made; every other step stays.
//
// The package is under contract/internal/, so only code of the contract can
// import it, and only the store reads Skip. No delivered package writes it:
// no executable, flag or environment variable selects it, and a test checks
// that only test files assign it.
package flushseam

// Skip, when true, makes the store's flush of a file or a directory return
// without asking the file system to flush. It is false in every delivered
// executable, and is set only from the TestMain of a package's tests.
var Skip bool
