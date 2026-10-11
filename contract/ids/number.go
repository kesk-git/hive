package ids

import (
	"errors"
	"fmt"
)

// MaxNumber is the largest whole number a record holds (spec/records.md,
// Encoding): 2^53 - 1.
const MaxNumber = 1<<53 - 1

// ErrNumber reports a generation, round or review revision that is not a
// whole number from 1 up to MaxNumber.
var ErrNumber = errors.New("ids: not a whole number from 1")

// CheckNumber checks a generation, a round or a review revision. These are
// the identities that are not ULIDs: whole numbers from 1, scoped to their
// task, session or review, and never reused.
func CheckNumber(n int64) error {
	if n < 1 || n > MaxNumber {
		return fmt.Errorf("%w: %d", ErrNumber, n)
	}
	return nil
}

// NextNumber returns the number after n, which must itself be a valid number
// or 0 for the first.
func NextNumber(n int64) (int64, error) {
	if n < 0 || n >= MaxNumber {
		return 0, fmt.Errorf("%w: no number follows %d", ErrNumber, n)
	}
	return n + 1, nil
}
