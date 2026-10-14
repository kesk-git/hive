package legacy

import (
	"bytes"
	"fmt"
	"os"
	"time"
	"unicode/utf8"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
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
func IsPersistentSupervisor(t *Task) bool { return t.Kind == persistentSupervisorKind }

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
		return nil, envelope.Fail(envelope.Invalid, "pool.legacy_malformed", "the file is not a legacy backlog the grammar accepts").Because(err)
	}
	return &cli.Result{Body: map[string]any{"report": report(src, digest, b)}}, nil
}

// location is the zone of the IANA database named, never the machine's own.
func location(zone string) (*time.Location, error) {
	if zone == "" || zone == "Local" {
		return nil, fmt.Errorf("%q is not a zone", zone)
	}
	return time.LoadLocation(zone)
}

// report is the parse report of a backlog read from src: one entry for every
// bullet with an identifier, the lines without one, what the parser kept but
// could not read canonically, and whether the backlog renders back to src.
func report(src []byte, digest string, b *Backlog) map[string]any {
	held := map[string]bool{}
	for _, t := range b.Tasks() {
		held[t.ID] = true
	}
	free := int64(len(b.Preamble))
	entries := []any{}
	warnings := []any{}
	warn := func(format string, args ...any) {
		warnings = append(warnings, clip(fmt.Sprintf(format, args...), 500))
	}
	seen := map[string]bool{}
	for _, s := range b.Sections {
		for _, e := range s.Entries {
			t := e.Task
			if t == nil {
				free += int64(len(e.Lines))
				continue
			}
			row := map[string]any{
				"legacy_id": t.ID, "section": semanticLine(s.HeaderLine), "state": stateWord(s.State), "disposition": "item",
			}
			switch {
			case seen[t.ID]:
				row["disposition"] = "duplicate"
				row["reason"] = "a later entry with an identity already seen; it creates no item"
				warn("entry %q repeats an identity already seen; the later entry is kept in the migration and creates no item", t.ID)
			case IsPersistentSupervisor(t):
				row["disposition"] = "excluded"
				row["reason"] = exclusionReason
			}
			seen[t.ID] = true
			entries = append(entries, row)
			for _, d := range t.Deps {
				if !held[d.ID] {
					warn("entry %q names %q in a %s edge and the file does not hold it; the edge is kept and treated as resolved", t.ID, d.ID, d.Type)
				}
			}
			if s.State == StateDone && t.Closed == "" {
				warn("done entry %q has no closing date; it is imported completed with its time unknown", t.ID)
			}
			for _, date := range []string{t.Created, t.Closed, holdUntil(t)} {
				if _, err := time.Parse("2006-01-02", date); date != "" && err != nil {
					warn("entry %q gives the date %s, which is not a calendar date", t.ID, date)
				}
			}
		}
	}
	return map[string]any{
		"digest": digest, "bytes": int64(len(src)), "entries": entries, "free_lines": free,
		"warnings": warnings, "round_trip_exact": bytes.Equal(b.Render(), src),
	}
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
