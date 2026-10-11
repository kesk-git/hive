package ids

import (
	"errors"
	"fmt"

	"github.com/kesk-git/hive/contract/canon"
)

// ErrNumber reports a generation, round or review revision that is not a
// whole number from 1 up to canon.MaxNumber.
var ErrNumber = errors.New("ids: not a whole number from 1")

// CheckNumber checks a generation, a round or a review revision. These are
// the identities that are not ULIDs: whole numbers from 1, scoped to their
// task, session or review, and never reused.
func CheckNumber(n int64) error {
	if n < 1 || n > canon.MaxNumber {
		return fmt.Errorf("%w: %d", ErrNumber, n)
	}
	return nil
}

// NextNumber returns the number after n, which must itself be a valid number
// or 0 for the first.
func NextNumber(n int64) (int64, error) {
	if n < 0 || n >= canon.MaxNumber {
		return 0, fmt.Errorf("%w: no number follows %d", ErrNumber, n)
	}
	return n + 1, nil
}
