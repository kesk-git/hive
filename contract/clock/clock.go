// Package clock is Hive's one source of time and its one timestamp form.
//
// Code that needs the time takes a Clock as an input and never reads the
// system clock itself, so a test that depends on time supplies its own (see
// spec/test-targets.md, rule 5). Timestamps are written as RFC 3339 in UTC
// with millisecond precision and a literal Z (spec/records.md, Encoding).
package clock

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Clock reports the current time.
type Clock interface {
	Now() time.Time
}

// System is the operating system's clock.
type System struct{}

// Now returns the current time.
func (System) Now() time.Time { return time.Now() }

// Fake is a clock that moves only when told to. The zero value is not usable;
// create one with NewFake. It is safe for concurrent use.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// NewFake returns a Fake set to start.
func NewFake(start time.Time) *Fake { return &Fake{now: start} }

// Now returns the Fake's current time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Set moves the Fake to t.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}

// Advance moves the Fake forward by d, which may be negative.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// layout is the timestamp form: fixed width, UTC, three fractional digits.
const layout = "2006-01-02T15:04:05.000Z"

// Length is the length in bytes of every timestamp.
const Length = len(layout)

// ErrRange reports an instant that has no timestamp form: before the year
// 0000 or after the year 9999.
var ErrRange = errors.New("clock: instant outside the years 0000 to 9999")

// ErrForm reports text that is not a timestamp of the required form.
var ErrForm = errors.New("clock: not a timestamp of the form 2006-01-02T15:04:05.000Z")

// Format writes t as a timestamp, converted to UTC and truncated to the
// millisecond. It refuses an instant outside the years 0000 to 9999.
func Format(t time.Time) (string, error) {
	t = t.UTC()
	if y := t.Year(); y < 0 || y > 9999 {
		return "", ErrRange
	}
	return t.Truncate(time.Millisecond).Format(layout), nil
}

// Parse reads a timestamp. It accepts only the exact form Format writes: the
// same length, a literal Z, three fractional digits and a real calendar
// instant. Any other spelling of an instant, such as an offset in place of Z,
// is refused.
func Parse(s string) (time.Time, error) {
	if len(s) != Length {
		return time.Time{}, fmt.Errorf("%w: %q", ErrForm, s)
	}
	t, err := time.Parse(layout, s)
	if err != nil || t.Format(layout) != s {
		return time.Time{}, fmt.Errorf("%w: %q", ErrForm, s)
	}
	return t, nil
}

// Valid reports whether s is a timestamp.
func Valid(s string) bool {
	_, err := Parse(s)
	return err == nil
}

// Stamp reads c and writes its time as a timestamp.
func Stamp(c Clock) (string, error) { return Format(c.Now()) }

// Times are the three times of an event (spec/events.md, The three times).
// They are always present and are never merged. All three are held at
// millisecond precision in UTC.
type Times struct {
	// Occurred is when the thing happened at its source.
	Occurred time.Time
	// Observed is when Hive read it.
	Observed time.Time
	// Recorded is when the line became durable.
	Recorded time.Time
	// OccurredKnown is false when the source gave no time, in which case
	// Occurred repeats Observed.
	OccurredKnown bool
}

// Observe takes the clock's time as the moment Hive read something and
// returns the times of what it read. occurred is the time the source gave; a
// nil occurred means the source gave none, and Occurred then repeats Observed
// with OccurredKnown false. Recorded is left for Record to set.
func Observe(c Clock, occurred *time.Time) Times {
	obs := trim(c.Now())
	if occurred == nil {
		return Times{Occurred: obs, Observed: obs}
	}
	return Times{Occurred: trim(*occurred), Observed: obs, OccurredKnown: true}
}

// Record returns t with Recorded set to the clock's time.
func (t Times) Record(c Clock) Times {
	t.Recorded = trim(c.Now())
	return t
}

// Local returns the times of a local receipt or record, which is observed
// when it is written: all three are the clock's time and Occurred is known.
func Local(c Clock) Times {
	now := trim(c.Now())
	return Times{Occurred: now, Observed: now, Recorded: now, OccurredKnown: true}
}

// Validate reports whether the three times are well formed: each is set and
// has a timestamp form, and an unknown Occurred repeats Observed.
func (t Times) Validate() error {
	for _, f := range []struct {
		name string
		v    time.Time
	}{{"occurred", t.Occurred}, {"observed", t.Observed}, {"recorded", t.Recorded}} {
		if f.v.IsZero() {
			return fmt.Errorf("clock: %s time is not set", f.name)
		}
		if _, err := Format(f.v); err != nil {
			return fmt.Errorf("clock: %s time: %w", f.name, err)
		}
	}
	if !t.OccurredKnown && !t.Occurred.Equal(t.Observed) {
		return errors.New("clock: occurred time is unknown, so it must repeat the observed time")
	}
	return nil
}

func trim(t time.Time) time.Time { return t.UTC().Truncate(time.Millisecond) }
