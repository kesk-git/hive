package clock

import (
	"errors"
	"regexp"
	"testing"
	"time"
)

var epoch = time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)

func TestFormatVectors(t *testing.T) {
	zone := time.FixedZone("plus-two", 2*60*60)
	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		{"whole second", epoch, "2026-10-11T08:00:00.000Z"},
		{"milliseconds", epoch.Add(123 * time.Millisecond), "2026-10-11T08:00:00.123Z"},
		{"truncates, never rounds", epoch.Add(999*time.Millisecond + 999*time.Microsecond), "2026-10-11T08:00:00.999Z"},
		{"converted to UTC", time.Date(2026, 10, 11, 10, 0, 0, 0, zone), "2026-10-11T08:00:00.000Z"},
		{"unix epoch", time.Unix(0, 0), "1970-01-01T00:00:00.000Z"},
		{"leap day", time.Date(2028, 2, 29, 23, 59, 59, 999e6, time.UTC), "2028-02-29T23:59:59.999Z"},
		{"first instant", time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), "0000-01-01T00:00:00.000Z"},
		{"last instant", time.Date(9999, 12, 31, 23, 59, 59, 999e6, time.UTC), "9999-12-31T23:59:59.999Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Format(tc.in)
			if err != nil || got != tc.want {
				t.Fatalf("Format = %q, %v; want %q", got, err, tc.want)
			}
			back, err := Parse(got)
			if err != nil {
				t.Fatalf("Parse(%q): %v", got, err)
			}
			if again, _ := Format(back); again != got {
				t.Fatalf("round trip = %q, want %q", again, got)
			}
		})
	}
}

func TestFormatRefusesOutOfRange(t *testing.T) {
	for _, in := range []time.Time{
		time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(-1, 12, 31, 23, 59, 59, 0, time.UTC),
	} {
		if got, err := Format(in); !errors.Is(err, ErrRange) || got != "" {
			t.Errorf("Format(%v) = %q, %v; want ErrRange", in, got, err)
		}
	}
}

func TestParseRefusesOtherSpellings(t *testing.T) {
	for _, in := range []string{
		"",
		"2026-10-11T08:00:00Z",
		"2026-10-11T08:00:00.0Z",
		"2026-10-11T08:00:00.0000Z",
		"2026-10-11T08:00:00.000z",
		"2026-10-11t08:00:00.000Z",
		"2026-10-11 08:00:00.000Z",
		"2026-10-11T08:00:00.000+00:00",
		"2026-10-11T10:00:00.000+02:00",
		"2026-10-11T08:00:00.000",
		"2026-10-11T24:00:00.000Z",
		"2026-10-11T08:60:00.000Z",
		"2026-10-11T08:00:60.000Z",
		"2026-13-11T08:00:00.000Z",
		"2026-02-30T08:00:00.000Z",
		"2027-02-29T08:00:00.000Z",
		"2026-10-11T08:00:00.000Z\n",
		" 2026-10-11T08:00:00.000Z",
		"2026-10-11T08:00:00,000Z",
		"+026-10-11T08:00:00.000Z",
	} {
		if _, err := Parse(in); !errors.Is(err, ErrForm) {
			t.Errorf("Parse(%q) error = %v; want ErrForm", in, err)
		}
		if Valid(in) {
			t.Errorf("Valid(%q) = true", in)
		}
	}
}

// The schema pattern for a timestamp is the form the library writes.
func TestTimestampMatchesSchemaPattern(t *testing.T) {
	re := regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3}Z$`)
	got, err := Format(epoch.Add(7 * time.Millisecond))
	if err != nil || !re.MatchString(got) || len(got) != Length {
		t.Fatalf("Format = %q, %v; want a match of %s of length %d", got, err, re, Length)
	}
}

func TestFakeClock(t *testing.T) {
	f := NewFake(epoch)
	var c Clock = f
	if got := c.Now(); !got.Equal(epoch) {
		t.Fatalf("Now = %v, want %v", got, epoch)
	}
	f.Advance(1500 * time.Millisecond)
	if got, _ := Stamp(c); got != "2026-10-11T08:00:01.500Z" {
		t.Fatalf("Stamp after Advance = %q", got)
	}
	f.Advance(-time.Second)
	if got, _ := Stamp(c); got != "2026-10-11T08:00:00.500Z" {
		t.Fatalf("Stamp after backwards Advance = %q", got)
	}
	f.Set(epoch.Add(time.Hour))
	if got, _ := Stamp(c); got != "2026-10-11T09:00:00.000Z" {
		t.Fatalf("Stamp after Set = %q", got)
	}
}

func TestSystemClockIsNow(t *testing.T) {
	before := time.Now()
	got := System{}.Now()
	if got.Before(before) || time.Since(got) > time.Minute {
		t.Fatalf("System.Now = %v, not near %v", got, before)
	}
}

func TestTimesLocal(t *testing.T) {
	f := NewFake(epoch.Add(1234567 * time.Nanosecond))
	got := Local(f)
	want := epoch.Add(time.Millisecond)
	if !got.Occurred.Equal(want) || !got.Observed.Equal(want) || !got.Recorded.Equal(want) || !got.OccurredKnown {
		t.Fatalf("Local = %+v, want all three %v and known", got, want)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTimesStayApart(t *testing.T) {
	f := NewFake(epoch)
	occurred := epoch.Add(-90 * time.Second)

	got := Observe(f, &occurred)
	f.Advance(2 * time.Second)
	got = got.Record(f)

	if !got.Occurred.Equal(occurred) || !got.Observed.Equal(epoch) || !got.Recorded.Equal(epoch.Add(2*time.Second)) {
		t.Fatalf("times merged: %+v", got)
	}
	if !got.OccurredKnown {
		t.Fatal("occurred was given and must be known")
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTimesUnknownOccurrence(t *testing.T) {
	f := NewFake(epoch)
	got := Observe(f, nil).Record(f)
	if got.OccurredKnown || !got.Occurred.Equal(got.Observed) {
		t.Fatalf("unknown occurrence must repeat observed: %+v", got)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTimesValidateRefuses(t *testing.T) {
	good := Local(NewFake(epoch))
	tests := map[string]func(Times) Times{
		"occurred missing": func(x Times) Times { x.Occurred = time.Time{}; return x },
		"observed missing": func(x Times) Times { x.Observed = time.Time{}; return x },
		"recorded missing": func(x Times) Times { x.Recorded = time.Time{}; return x },
		"unknown but different from observed": func(x Times) Times {
			x.OccurredKnown = false
			x.Occurred = x.Observed.Add(-time.Second)
			return x
		},
		"out of range": func(x Times) Times { x.Recorded = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC); return x },
	}
	for name, mutate := range tests {
		if err := mutate(good).Validate(); err == nil {
			t.Errorf("%s: Validate = nil", name)
		}
	}
}
