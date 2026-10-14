package legacy

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kesk-git/hive/packages/pool/internal/item"
)

// This file is translated from the normalization half of tasks-axi's
// src/backends/markdown.ts: what a title, tag, link, hold, date or dependency
// reason must be before an entry is rendered canonically, so that what is
// written parses back to the same fields. The checks are pure; storing the
// entry is not done here.

// ValidationError is a refusal of a value that the legacy form cannot carry.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

var (
	dateRe          = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	depReasonEdgeRe = regexp.MustCompile(`(?:^|` + item.Ws + `)` + depMarker + `:` + item.Ws)
)

func hasLineEnd(s string) bool { return strings.ContainsAny(s, "\r\n") }

// CheckID refuses an identifier the grammar would not read back.
func CheckID(id string) error {
	if !ValidID(id) {
		return invalid(`Invalid id %q - ids must be slug-shaped (letters, digits, "._-") with no spaces`, id)
	}
	return nil
}

// NormalizeTitle is the title trimmed. It must be one line, not empty, and must
// not end in canonical tags, which a re-parse would take off it.
func NormalizeTitle(title string) (string, error) {
	if hasLineEnd(title) {
		return "", invalid("Task title must be a single line")
	}
	trimmed := trim(title)
	if trimmed == "" {
		return "", invalid("Task title must not be empty")
	}
	if ExtractTags(trimmed).Title != trimmed {
		return "", invalid("Task title must not end with canonical task tags")
	}
	return trimmed, nil
}

// NormalizeTagValue is the value of a kind or repo tag trimmed, or "" when it
// is blank. It must be one line without parentheses. field names the tag.
func NormalizeTagValue(value, field string) (string, error) {
	if strings.ContainsAny(value, "()\r\n") {
		return "", invalid("Task %s must be a single line without parentheses", field)
	}
	return trim(value), nil
}

func normalizeLinkURL(url string) (string, error) {
	if hasLineEnd(url) {
		return "", invalid("Task link must be a single line")
	}
	trimmed := trim(url)
	if trimmed == "" {
		return "", invalid("Task link must not be empty")
	}
	return trimmed, nil
}

// NormalizeTypedLink checks that a link is what its kind says. A pull request
// URL must already be canonical: padded input is refused, not trimmed.
func NormalizeTypedLink(link item.Link) (item.Link, error) {
	normalized, err := normalizeLinkURL(link.URL)
	if err != nil {
		return item.Link{}, err
	}
	url := normalized
	if link.Kind == item.LinkPR {
		url = link.URL
	}
	for _, c := range item.DeriveLinks(url) {
		if c.Kind == link.Kind && c.URL == url {
			return item.Link{Kind: link.Kind, URL: url}, nil
		}
	}
	expected := "an http(s) URL"
	switch link.Kind {
	case item.LinkPR:
		expected = item.PRURLExpected
	case item.LinkReport:
		expected = "a data/<id>/report.md path"
	}
	return item.Link{}, invalid("Task %s link must be %s", link.Kind, expected)
}

// NormalizePriority refuses a priority outside 0 to 4.
func NormalizePriority(priority int) (int, error) {
	if priority < 0 || priority > 4 {
		return 0, invalid("Task priority must be an integer 0-4")
	}
	return priority, nil
}

// NormalizeDate refuses a date that is not written YYYY-MM-DD. field names it.
// Only the shape is checked, as the legacy form does.
func NormalizeDate(value, field string) (string, error) {
	if !dateRe.MatchString(value) {
		return "", invalid("Task %s must be YYYY-MM-DD", field)
	}
	return value, nil
}

// NormalizeHold checks a hold: a reason on one line without parentheses, a kind
// from the legacy words if it has one, and a date if it has one.
func NormalizeHold(h Hold) (Hold, error) {
	if strings.ContainsAny(h.Reason, "()\r\n") {
		return Hold{}, invalid("Task hold reason must be a single line without parentheses")
	}
	reason := trim(h.Reason)
	if reason == "" {
		return Hold{}, invalid("Task hold reason must not be empty")
	}
	out := Hold{Reason: reason}
	if h.Kind != "" {
		known := false
		for _, k := range HoldKinds {
			known = known || k == h.Kind
		}
		if !known {
			return Hold{}, invalid("Task hold kind must be one of %s", strings.Join(HoldKinds, ", "))
		}
		out.Kind = h.Kind
	}
	if h.Until != "" {
		until, err := NormalizeDate(h.Until, "hold-until date")
		if err != nil {
			return Hold{}, err
		}
		out.Until = until
	}
	return out, nil
}

// NormalizeDepReason is a dependency reason trimmed, or "" when blank. It must
// be one line and must not hold a dependency marker, which a re-parse would
// read as another edge.
func NormalizeDepReason(reason string) (string, error) {
	if hasLineEnd(reason) {
		return "", invalid("Task dependency reason must be a single line")
	}
	trimmed := trim(reason)
	if depReasonEdgeRe.MatchString(trimmed) {
		return "", invalid("Task dependency reason must not contain dependency markers")
	}
	return trimmed, nil
}

// NormalizeDep checks an edge of the task ownerID: a valid identifier that is
// not the task itself, and a reason that normalizes.
func NormalizeDep(ownerID string, d Dep) (Dep, error) {
	reason, err := NormalizeDepReason(d.Reason)
	if err != nil {
		return Dep{}, err
	}
	if err := CheckID(d.ID); err != nil {
		return Dep{}, err
	}
	if d.ID == ownerID {
		return Dep{}, invalid("A task cannot block itself")
	}
	return Dep{Type: d.Type, ID: d.ID, Reason: reason}, nil
}

// AppendTitleLink adds a link to the prose of a title, where links live,
// unless the title already holds that URL.
func AppendTitleLink(title string, link item.Link) (string, error) {
	typed, err := NormalizeTypedLink(link)
	if err != nil {
		return "", err
	}
	for _, c := range item.DeriveLinks(title) {
		if c.URL == typed.URL {
			return title, nil
		}
	}
	return NormalizeTitle(title + " " + typed.URL)
}

// The three sections of a backlog in the order they are written, with their
// canonical headers.
var sectionOrder = []State{StateInFlight, StateQueued, StateDone}

var sectionHeaders = map[State]string{
	StateInFlight: "## In flight",
	StateQueued:   "## Queued",
	StateDone:     "## Done",
}

// EnsureSections gives an empty backlog its heading and every missing section
// of the three, appended in order after the ones it has.
func (b *Backlog) EnsureSections() {
	if len(b.Preamble) == 0 && len(b.Sections) == 0 {
		b.Preamble = []string{"# Backlog", ""}
		b.FinalNewline = true
	}
	for _, st := range sectionOrder {
		found := false
		for _, s := range b.Sections {
			found = found || s.State == st
		}
		if !found {
			b.Sections = append(b.Sections, &Section{HeaderLine: sectionHeaders[st], State: st})
		}
	}
}

// Normalize marks every task dirty so that Render writes each in canonical
// form, and returns how many it marked. Free-form lines are left as they are.
// Normalizing twice writes the same bytes as normalizing once.
func (b *Backlog) Normalize() int {
	b.EnsureSections()
	n := 0
	for _, s := range b.Sections {
		for _, e := range s.Entries {
			if e.Task != nil {
				e.Dirty = true
				n++
			}
		}
	}
	return n
}
