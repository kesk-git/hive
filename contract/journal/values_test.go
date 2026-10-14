package journal

import (
	"errors"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/schema"
	"github.com/kesk-git/hive/contract/store"
)

// shapes are strings of listed shapes, built at run time from a prefix and a
// repeated character so that no file holds one.
var shapes = map[string]string{
	"github.pat-classic":             "ghp_" + strings.Repeat("a", 36),
	"github.oauth-access":            "gho_" + strings.Repeat("a", 36),
	"github.app-user-access":         "ghu_" + strings.Repeat("a", 36),
	"github.app-installation-opaque": "ghs_" + strings.Repeat("a", 255),
	"github.app-refresh-scanner40":   "ghr_" + strings.Repeat("a", 36),
	"github.pat-fine-grained":        "github_pat_" + strings.Repeat("a", 82),
	"private-key.pkcs8":              "-----BEGIN PRIVATE KEY-----",
	"private-key.pgp2-secret":        "first line\r\n-----BEGIN PGP SECRET KEY BLOCK-----\r\nlast",
}

// refused checks that appending e fails with the shape named at the field
// named, wrapping ErrEvent, with the text in neither the error nor its
// message, and that the journal holds nothing.
func refused(t *testing.T, j *Journal, home string, e store.Event, field, shape, text string) {
	t.Helper()
	before := segmentFiles(t, home)
	err := j.Append([]store.Event{e})
	var se *schema.ShapeError
	if !errors.Is(err, ErrEvent) || !errors.As(err, &se) {
		t.Fatalf("got %v, want a refusal that names a shape", err)
	}
	if se.Field != field || se.Shape != shape {
		t.Errorf("the refusal names %q and %q, want %q and %q", se.Field, se.Shape, field, shape)
	}
	if strings.Contains(err.Error(), text) {
		t.Errorf("the error holds the text: %v", err)
	}
	if head, err := j.Head(); err != nil || head != 0 {
		t.Errorf("head %d (%v)", head, err)
	}
	if got := segmentFiles(t, home); len(got) != len(before) {
		t.Errorf("segments %v, were %v", got, before)
	}
}

// An event holding a string of any listed shape is refused whole, before
// anything is written, naming the field within the event and the shape.
func TestAppendRefusesEveryListedShape(t *testing.T) {
	for shape, text := range shapes {
		t.Run(shape, func(t *testing.T) {
			home := newHome(t, false)
			j, _ := open(t, home, "pool", 0)
			body := map[string]any{"command": "hive-pool", "status": "failed", "code": "internal", "detail": []any{"reading a record", "refused " + text + " twice"}}
			if strings.HasPrefix(shape, "private-key.") {
				body["detail"] = []any{text}
			}
			refused(t, j, home, encoded(t, diagnosticEvent("pool", 1, body)), "/body/detail", shape, text)
		})
	}
}

// Every string of the event is checked: the envelope's besides the body's, the
// member names beside the values, and the body of a kind that registers no
// schema, which is otherwise refused for having a body at all.
func TestAppendChecksEveryStringOfTheEvent(t *testing.T) {
	text := shapes["github.pat-classic"]
	for name, c := range map[string]struct {
		edit  func(m map[string]any)
		field string
	}{
		"a declared envelope member": {func(m map[string]any) { m["basis"].(map[string]any)["ref"].(map[string]any)["id"] = text }, "/basis/ref/id"},
		"the stack":                  {func(m map[string]any) { m["body"].(map[string]any)["stack"] = "at " + text }, "/body/stack"},
		"the command":                {func(m map[string]any) { m["body"].(map[string]any)["command"] = text }, "/body/command"},
		"a body member name":         {func(m map[string]any) { m["body"].(map[string]any)[text] = 1 }, "/body"},
		"an envelope member name":    {func(m map[string]any) { m[text] = 1 }, ""},
		"a body of a kind with no schema": {func(m map[string]any) {
			m["kind"] = "pool.thing.recorded"
			m["body"] = map[string]any{"note": text}
		}, "/body"},
		"a member name of a kind with no schema": {func(m map[string]any) {
			m["kind"] = "pool.thing.recorded"
			m["body"] = map[string]any{text: 1}
		}, "/body"},
		"a nested string of a kind with no schema": {func(m map[string]any) {
			m["kind"] = "pool.thing.recorded"
			m["body"] = map[string]any{"a": []any{map[string]any{"b": text}}}
		}, "/body"},
	} {
		t.Run(name, func(t *testing.T) {
			home := newHome(t, false)
			j, _ := open(t, home, "pool", 0)
			m := diagnosticEvent("pool", 1, map[string]any{"command": "hive-pool", "status": "failed", "code": "internal"})
			c.edit(m)
			refused(t, j, home, encoded(t, m), c.field, "github.pat-classic", text)
		})
	}
}

// A sentence that mentions a prefix, and a string one character off a shape,
// are kept exactly.
func TestAppendKeepsWhatIsNotAShape(t *testing.T) {
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 0)
	for n, s := range []string{
		"a token begins ghp_ and then thirty-six letters",
		shapes["github.pat-classic"] + "x",
		shapes["github.pat-classic"][:39],
		shapes["github.pat-classic"] + ".",
		"-" + shapes["private-key.pkcs8"],
		shapes["private-key.pkcs8"] + " ",
	} {
		body := map[string]any{"command": "hive-pool", "status": "failed", "code": "internal", "detail": []any{s}}
		append_(t, j, encoded(t, diagnosticEvent("pool", n+1, body)))
	}
	got := readAll(t, j, 0)
	if len(got) != 6 || got[0].Event["body"].(map[string]any)["detail"].([]any)[0] != "a token begins ghp_ and then thirty-six letters" {
		t.Errorf("read %d entries", len(got))
	}
}

// Recovery rolls the same events forward and applies the same check; and a
// refused event leaves a torn tail alone, with no repair line written ahead of
// it.
func TestRecoverAndRepairCheckBeforeAnyWrite(t *testing.T) {
	home := newHome(t, false)
	j, _ := open(t, home, "pool", 0)
	append_(t, j, event(t, "pool", 1), event(t, "pool", 2))
	name := SegmentName(1)
	tear(t, home, name, 5)
	torn := segmentBytes(t, home, name)

	text := shapes["github.oauth-access"]
	body := map[string]any{"command": "hive-pool", "status": "failed", "code": "internal", "detail": []any{text}}
	e := encoded(t, diagnosticEvent("pool", 3, body))
	for _, call := range []func([]store.Event) error{j.Append, j.Recover} {
		err := call([]store.Event{event(t, "pool", 4), e})
		var se *schema.ShapeError
		if !errors.Is(err, ErrEvent) || !errors.As(err, &se) || se.Shape != "github.oauth-access" || strings.Contains(err.Error(), text) {
			t.Fatalf("got %v", err)
		}
		if got := segmentBytes(t, home, name); string(got) != string(torn) {
			t.Fatal("the segment changed")
		}
		if len(segmentFiles(t, home)) != 1 {
			t.Fatalf("segments %v", segmentFiles(t, home))
		}
	}

	// Without the shape the repair is written ahead of the event.
	append_(t, j, event(t, "pool", 3))
	if got := readAll(t, j, 0); len(got) != 3 || got[1].Kind != RepairKind {
		t.Errorf("read %d entries", len(got))
	}
}
