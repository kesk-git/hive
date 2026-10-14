package legacy

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/schema"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
)

// This file is translated from the load half of tasks-axi's
// src/backends/markdown.ts: reading a backlog file whole, and the mapping of
// its parsed entries to what the Pool would make of them. The two default
// backlog names of src/config.ts are not carried: the request names the path.
// What is built here is the dry run, which parses the file with the grammar of
// this package and reports it; the live import, which writes the migration,
// the items and the holds, is a later lane's, and the file is only ever read.

func init() { entry.RegisterLegacy(entry.Default, importLegacy, exportLegacy) }

// persistentSupervisorKind is the kind the grammar reads for the entries the
// Pool never makes an item of: a PERSISTENT SECONDMATE entry, by its leading
// words or by its kind tag. A persistent Fleet is never an ordinary task.
const persistentSupervisorKind = "secondmate"

// IsPersistentSupervisor reports whether the entry is a persistent supervisor,
// which an import excludes.
func IsPersistentSupervisor(t *Task) bool {
	return t.Kind == persistentSupervisorKind || LeadingKind(t.Title) == persistentSupervisorKind
}

const exclusionReason = "a persistent supervisor entry is excluded: a persistent Fleet is never an ordinary task, and the export emits its lines verbatim"

// stateWord is the Pool's word for a section's state.
func stateWord(s State) string {
	switch s {
	case StateQueued:
		return "queued"
	case StateInFlight:
		return "in-flight"
	case StateDone:
		return "done"
	}
	return "none"
}

// importLegacy is the operation import-legacy. With dry_run true it reads the
// file at the path, parses it and returns the report; it writes nothing, and
// never the file. With dry_run false it is refused as an operation the build
// has not reached, before anything is read.
func importLegacy(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	if dry, _ := body["dry_run"].(bool); !dry {
		return nil, envelope.Common(envelope.CodeUnsupportedOperation, "the import that writes is not built yet; only dry_run true is")
	}
	zone, _ := body["zone"].(string)
	if _, err := location(zone); err != nil {
		return nil, envelope.Common(envelope.CodeSchemaViolation, "zone is not a zone of the IANA database").Because(err)
	}
	path, _ := body["path"].(string)
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, envelope.Common(envelope.CodeSourceUnreadable, "the legacy backlog cannot be read").Because(err)
	}
	digest := canon.FileDigest(src)
	if want, ok := body["expected_digest"].(string); ok && want != digest {
		return nil, envelope.Common(envelope.CodeRevisionConflict, "the file is not the one the caller inspected: its digest differs from expected_digest")
	}
	b, err := Parse(src)
	if err != nil {
		return nil, envelope.Fail(envelope.Invalid, codeLegacyMalformed, "the file is not a legacy backlog the grammar accepts").Because(err)
	}
	r, err := report(src, digest, b)
	if err != nil {
		return nil, err
	}
	return &cli.Result{Body: map[string]any{"report": r}}, nil
}

// location is the zone of the IANA database named, never the machine's own.
func location(zone string) (*time.Location, error) {
	if zone == "" || zone == "Local" {
		return nil, fmt.Errorf("%q is not a zone", zone)
	}
	return time.LoadLocation(zone)
}

// importResponse is the schema of the body the report is returned in.
const importResponse = "urn:hive:schema:ops:hive-pool.import-legacy.response:v1"

// place is where in the file an entry of the report was read: the line of its
// bullet and the line of its section's header.
type place struct{ line, header int }

// unrepresentable refuses a file whose entry or section the report cannot
// carry, naming the line and the field.
func unrepresentable(line int, field, why string) error {
	return envelope.Fail(envelope.Invalid, codeLegacyMalformed, "the report cannot carry the "+field+" at line "+strconv.Itoa(line)+": "+why).
		With("line", int64(line)).With("field", field)
}

// representable refuses a report the response cannot carry, checked as the
// response is: for a string of a credential shape and against the response
// schema. The refusal names the line and the field, never the value. entries
// are where the report's entries were read, and warned the line of the entry
// each warning is of.
func representable(rep map[string]any, entries []place, warned []int) error {
	reg, err := schema.Embedded()
	if err != nil {
		return envelope.Common(envelope.CodeInternal, "the embedded schemas do not load").Because(err)
	}
	data, err := canon.Encode(map[string]any{"report": rep})
	if err != nil {
		return envelope.Common(envelope.CodeInternal, "the report cannot be written").Because(err)
	}
	var at, why string
	var se *schema.ShapeError
	var ve *schema.ValidationError
	switch err := reg.ValidateOwned(importResponse, data); {
	case err == nil:
		return nil
	case errors.As(err, &se):
		at, why = se.Field, "it holds a string of the credential shape "+se.Shape
	case errors.As(err, &ve):
		at, why = ve.Violations[0].Instance, ve.Violations[0].Message
	default:
		return envelope.Common(envelope.CodeInternal, "the report cannot be checked").Because(err)
	}
	line, field := locate(at, rep, entries, warned)
	if line == 0 {
		return envelope.Common(envelope.CodeInternal, "the report breaks its response at "+at+": "+why)
	}
	return unrepresentable(line, field, why)
}

// locate is the line and the field of the file that a pointer into the body
// names, or a line of 0 for a pointer to nothing read from the file. A string
// of a credential shape in a warning is named at the list, so the warning is
// the first that holds one.
func locate(at string, rep map[string]any, entries []place, warned []int) (int, string) {
	p := strings.Split(at, "/")
	if len(p) < 3 || p[0] != "" || p[1] != "report" {
		return 0, ""
	}
	i := -1
	if len(p) > 3 {
		var err error
		if i, err = strconv.Atoi(p[3]); err != nil {
			return 0, ""
		}
	}
	switch {
	case p[2] == "entries" && len(p) == 5 && i >= 0 && i < len(entries):
		if p[4] == "section" {
			return entries[i].header, p[4]
		}
		return entries[i].line, p[4]
	case p[2] == "warnings" && len(p) <= 4:
		if len(p) == 3 {
			warnings, _ := rep["warnings"].([]any)
			i = slices.IndexFunc(warnings, func(w any) bool { s, _ := w.(string); return schema.HoldsShape(s) })
		}
		if i >= 0 && i < len(warned) {
			return warned[i], p[2]
		}
	}
	return 0, ""
}

// report is the parse report of a backlog read from src: one entry for every
// bullet with an identifier, the lines without one, what the parser kept but
// could not read canonically, and whether the backlog renders back to src. A
// file the report cannot carry is refused.
func report(src []byte, digest string, b *Backlog) (map[string]any, error) {
	held := map[string]bool{}
	for _, t := range b.Tasks() {
		held[t.ID] = true
	}
	free := int64(len(b.Preamble))
	entries := []any{}
	warnings := []any{}
	var places []place
	var warned []int
	warn := func(line int, format string, args ...any) {
		warnings = append(warnings, clip(fmt.Sprintf(format, args...), 500))
		warned = append(warned, line)
	}
	seen := map[string]bool{}
	line := len(b.Preamble)
	for _, s := range b.Sections {
		line++
		header := line
		section := semanticLine(s.HeaderLine)
		for _, e := range s.Entries {
			at := line + 1
			line += len(e.Lines)
			t := e.Task
			if t == nil {
				free += int64(len(e.Lines))
				continue
			}
			if !utf8.ValidString(section) {
				return nil, unrepresentable(header, "section", "the header is not UTF-8")
			}
			row := map[string]any{
				"legacy_id": t.ID, "section": section, "state": stateWord(s.State), "disposition": "item",
			}
			switch {
			case IsPersistentSupervisor(t):
				row["disposition"] = "excluded"
				row["reason"] = exclusionReason
			case seen[t.ID]:
				row["disposition"] = "duplicate"
				row["reason"] = "a later entry with an identity already seen; it creates no item"
				warn(at, "entry %q repeats an identity already seen; the later entry is kept in the migration and creates no item", t.ID)
			}
			seen[t.ID] = true
			entries = append(entries, row)
			places = append(places, place{line: at, header: header})
			for _, d := range t.Deps {
				if !held[d.ID] {
					warn(at, "entry %q names %q in a %s edge and the file does not hold it; the edge is kept and treated as resolved", t.ID, d.ID, d.Type)
				}
			}
			if s.State == StateDone && t.Closed == "" {
				warn(at, "done entry %q has no closing date; it is imported completed with its time unknown", t.ID)
			}
			for _, date := range []string{t.Created, t.Closed, holdUntil(t)} {
				if _, err := time.Parse("2006-01-02", date); date != "" && err != nil {
					warn(at, "entry %q gives the date %s, which is not a calendar date", t.ID, date)
				}
			}
		}
	}
	rep := map[string]any{
		"digest": digest, "bytes": int64(len(src)), "entries": entries, "free_lines": free,
		"warnings": warnings, "round_trip_exact": bytes.Equal(b.Render(), src),
	}
	if err := representable(rep, places, warned); err != nil {
		return nil, err
	}
	return rep, nil
}

// clip is s cut to at most n bytes at the start of a character, which holds the
// schema's limit on characters.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func holdUntil(t *Task) string {
	if t.Hold == nil {
		return ""
	}
	return t.Hold.Until
}
