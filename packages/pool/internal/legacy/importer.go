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
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	cids "github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/schema"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
	"github.com/kesk-git/hive/packages/pool/internal/item"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// This file is translated from the load half of tasks-axi's
// src/backends/markdown.ts: reading a backlog file whole, and the mapping of
// its parsed entries to what the Pool makes of them. The two default backlog
// names of src/config.ts are not carried: the request names the path. With
// dry_run true the file is parsed with the grammar of this package and
// reported; with dry_run false it is imported in one transaction, which
// writes the migration, the items, the holds and their receipts. The source
// file is only ever read.

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

// importLegacy is the operation import-legacy. It reads the file at the path,
// parses it and checks that the Pool can make what the file says. With dry_run
// true it returns the report and writes nothing; with dry_run false it
// requires that the parsed file renders back byte for byte, and writes the
// migration, the items, the holds and the receipts in one transaction. It
// never writes the file.
func importLegacy(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	dry, _ := body["dry_run"].(bool)
	zone, _ := body["zone"].(string)
	loc, err := location(zone)
	if err != nil {
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
	// A file whose text holds a credential is refused whole before anything is
	// made of it, in the dry run as in the import; so is a second import of the
	// same text.
	if err := refuseCredential(src); err != nil {
		return nil, err
	}
	if !dry {
		if err := refuseSecondImport(c, digest); err != nil {
			return nil, err
		}
	}
	b, err := Parse(src)
	if err != nil {
		return nil, envelope.Fail(envelope.Invalid, codeLegacyMalformed, "the file is not a legacy backlog the grammar accepts").Because(err)
	}
	r, err := report(src, digest, b)
	if err != nil {
		return nil, err
	}
	if err := importable(walk(b)); err != nil {
		return nil, err
	}
	if dry {
		return &cli.Result{Body: map[string]any{"report": r}}, nil
	}
	if exact, _ := r["round_trip_exact"].(bool); !exact {
		return nil, envelope.Fail(envelope.Refused, codeNotRoundTrip, "the parsed file does not render back byte for byte, so it is not imported")
	}
	return writeImport(c, src, digest, path, zone, loc, b, r)
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
				"legacy_id": t.ID, "section": section, "state": stateWord(s.State),
			}
			row["disposition"], row["reason"] = disposition(t, seen)
			switch row["disposition"] {
			case DispositionItem:
				delete(row, "reason")
			case DispositionDuplicate:
				warn(at, "entry %q repeats an identity already seen; the later entry is kept in the migration and creates no item", t.ID)
			}
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
		"warnings": warnings, "round_trip_exact": roundTrips(src, b),
	}
	if err := representable(rep, places, warned); err != nil {
		return nil, err
	}
	return rep, nil
}

// disposition is what an entry of a backlog becomes, given the identities seen
// before it, and the reason for one that becomes no item.
func disposition(t *Task, seen map[string]bool) (string, string) {
	d, reason := DispositionItem, ""
	switch {
	case IsPersistentSupervisor(t):
		d, reason = DispositionExcluded, exclusionReason
	case seen[t.ID]:
		d, reason = DispositionDuplicate, "a later entry with an identity already seen; it creates no item"
	}
	seen[t.ID] = true
	return d, reason
}

// roundTrips reports whether the parsed backlog renders back to src byte for
// byte and src can be kept as it is: the migration holds the text in a JSON
// string, which holds no byte sequence that is not UTF-8.
func roundTrips(src []byte, b *Backlog) bool {
	return utf8.Valid(src) && bytes.Equal(b.Render(), src)
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

// ---------------------------------------------------------------------------
// The import that writes
// ---------------------------------------------------------------------------

// The codes the import that writes refuses with, besides those the report
// does.
const codeNotRoundTrip = "pool.legacy_not_round_trip"

// refuseCredential refuses a file that holds a string of a credential shape,
// naming the line where one is found and the shape, never the text.
func refuseCredential(src []byte) error {
	var shape *schema.ShapeError
	if err := schema.CheckShapes(map[string]any{"text": string(src)}); !errors.As(err, &shape) {
		return nil
	}
	f := envelope.Common(envelope.CodeSecretShapeRecognized, "the file holds a string of the credential shape "+shape.Shape+"; nothing is imported")
	for i, line := range strings.Split(string(src), "\n") {
		if schema.HoldsShape(line) {
			f = f.With("line", int64(i+1))
			break
		}
	}
	return f.With("shape", shape.Shape)
}

// refuseSecondImport refuses a file whose digest a migration of this home
// already holds.
func refuseSecondImport(c *cli.Call, digest string) error {
	all, omitted, err := poolstore.LoadAll(c.Store, "migration")
	if err != nil {
		return poolstore.Failure("the migrations of the Pool", err)
	}
	if len(omitted) > 0 {
		return envelope.Common(envelope.CodeRecordMalformed, "a migration of the Pool cannot be read, so a second import cannot be ruled out").With("record", omitted[0].ID)
	}
	for _, l := range all {
		if src, _ := l.Data["source"].(map[string]any); src["digest"] == digest {
			return envelope.Common(envelope.CodeAlreadyExists, "a migration of this home already holds a file with this digest").With("migration_id", l.ID)
		}
	}
	return nil
}

// holdKinds are the Pool's kinds for the kind words of a legacy hold. A hold
// that names none is a parked hold.
var holdKinds = map[string]string{
	"captain": "word", "external": "external", "load": "resource", "parked": "parked", "future": "future", "": "parked",
}

const maxTitle = 200

// importable refuses an entry the Pool cannot make an item of, naming its
// line and field: a title the item cannot keep, a hold with no reason, a future
// hold with no date, and an edge from an entry to itself.
func importable(w walked) error {
	seen := map[string]bool{}
	for _, e := range w.Entries {
		if d, _ := disposition(e.Task, seen); d != DispositionItem {
			continue
		}
		t := e.Task
		if n := utf8.RuneCountInString(t.Title); n == 0 || n > maxTitle {
			return unrepresentable(e.FirstLine, "title", "an item's title is 1 to "+strconv.Itoa(maxTitle)+" characters, and this one is "+strconv.Itoa(n))
		}
		if h := t.Hold; h != nil {
			if strings.TrimSpace(h.Reason) == "" {
				return unrepresentable(e.FirstLine, "hold", "a hold needs the reason it is held for, and this one has none")
			}
			if holdKinds[h.Kind] == "future" && h.Until == "" {
				return unrepresentable(e.FirstLine, "hold-until", "a future hold needs the date it is held until, and this one has none")
			}
		}
		for _, d := range t.Deps {
			if d.ID == t.ID {
				return unrepresentable(e.FirstLine, "dependency", "an entry cannot depend on itself")
			}
		}
	}
	return nil
}

// txn gathers the transaction of the import.
type txn struct {
	c  *cli.Call
	tx poolstore.Transaction
}

func writerTree(c *cli.Call) map[string]any {
	w := c.Writer()
	return map[string]any{
		"package": w.Package, "command": w.Command, "operation": w.Operation,
		"implementation_version": w.ImplementationVersion,
	}
}

// putScope writes the scope's flat members into a record or an event.
func putScope(m map[string]any, s envelope.Scope, clone bool) {
	m["scope_level"], m["home_id"] = s.Level, s.HomeID
	if s.WorkspaceID != "" {
		m["workspace_id"] = s.WorkspaceID
	}
	if s.ProjectID != "" {
		m["project_id"] = s.ProjectID
	}
	if clone && s.CloneID != "" {
		m["clone_id"] = s.CloneID
	}
}

// put adds the record, new, to the transaction, finalized, and returns it with
// the reference a receipt names it by.
func (t *txn) put(kind, id string, body map[string]any) (map[string]any, *envelope.RecordRef, error) {
	k, ok := poolstore.Lookup(kind)
	if !ok {
		return nil, nil, envelope.Common(envelope.CodeInternal, kind+" is not a kind of the Pool")
	}
	path, err := k.Path(id)
	if err != nil {
		return nil, nil, envelope.Common(envelope.CodeInternal, "a record has no identity it can be stored under").Because(err)
	}
	body["writer"] = writerTree(t.c)
	content, err := canon.Encode(body)
	if err != nil {
		return nil, nil, envelope.Common(envelope.CodeInternal, "a record cannot be encoded").Because(err)
	}
	rw, digest, err := t.c.Finalize(cstore.RecordWrite{Path: path, Content: content})
	if err != nil {
		return nil, nil, envelope.Common(envelope.CodeInternal, "a record cannot be finalized").Because(err)
	}
	v, err := canon.Parse(rw.Content)
	if err != nil {
		return nil, nil, envelope.Common(envelope.CodeInternal, "a record cannot be read back").Because(err)
	}
	final := v.(map[string]any)
	t.tx.Records = append(t.tx.Records, poolstore.Write{Kind: kind, ID: id, Body: final})
	ref := &envelope.RecordRef{Schema: "pool." + kind, ID: id, HomeID: t.c.Home.HomeID, RecordRevision: 1, Digest: digest}
	return final, ref, nil
}

// receipt adds the receipt of the kind about the subject to the transaction,
// with the event the Pool registers beside it. Both take the scope given and,
// where there is one, the task_id.
func (t *txn) receipt(kind string, subject *envelope.RecordRef, scope envelope.Scope, taskID string) error {
	internal := func(what string, err error) error { return envelope.Common(envelope.CodeInternal, what).Because(err) }
	r, err := t.c.NewReceipt(kind, "1.0", subject, nil, nil)
	if err != nil {
		return internal("a receipt cannot be made", err)
	}
	r.Scope = scope
	r.Scope.TaskID = taskID
	t.tx.Receipts = append(t.tx.Receipts, r)
	eventID, err := cids.Mint(t.c.Clock, cids.Event)
	if err != nil {
		return internal("an event cannot be made", err)
	}
	now, err := t.c.Stamp()
	if err != nil {
		return internal("an event cannot be made", err)
	}
	event := map[string]any{
		"schema": "hive.event", "schema_version": "1.0", "event_id": eventID,
		"kind": kind, "kind_version": "1.0",
		"source":         map[string]any{"package": poolstore.Package, "source_id": poolstore.Package},
		"correlation_id": t.c.Request.CorrelationID,
		"causation":      map[string]any{"schema": "hive.request", "id": t.c.Request.RequestID},
		"occurred_at":    now, "observed_at": now,
		"basis": map[string]any{"kind": "receipt", "ref": map[string]any{"schema": "hive.receipt", "id": r.ReceiptID}},
		"body":  map[string]any{},
	}
	putScope(event, scope, true)
	if taskID != "" {
		event["task_id"] = taskID
	}
	content, err := canon.Encode(event)
	if err != nil {
		return internal("an event cannot be written", err)
	}
	t.tx.Events = append(t.tx.Events, cstore.Event{ID: eventID, Content: content})
	return nil
}

// importer maps the entries of one file to the records of one migration.
type importer struct {
	c     *cli.Call
	loc   *time.Location
	zone  string
	now   string
	scope envelope.Scope
	migID string
	// taskOf is the item made of each legacy identifier: the first entry that
	// is an item.
	taskOf map[string]string
}

// at is the instant of the start of the calendar date in the zone of the
// import, the earliest instant whose date there is that date, or false for a
// date that is not one there. Such an instant is a midnight of the date in an
// offset the zone keeps at that midnight, or the change to an offset where a
// change skips the midnight; every local midnight of a date falls within 14
// hours of its midnight in UTC.
func (im *importer) at(date string) (string, bool) {
	day, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "", false
	}
	var first time.Time
	for x := day.Add(-14 * time.Hour); x.Before(day.Add(14 * time.Hour)); {
		local := x.In(im.loc)
		_, offset := local.Zone()
		start, end := local.ZoneBounds()
		for _, c := range []time.Time{day.Add(-time.Duration(offset) * time.Second), start} {
			inZone := !c.Before(start) && (end.IsZero() || c.Before(end))
			if inZone && c.In(im.loc).Format("2006-01-02") == date && (first.IsZero() || c.Before(first)) {
				first = c
			}
		}
		if end.IsZero() {
			break
		}
		x = end
	}
	if first.IsZero() {
		return "", false
	}
	s, err := clock.Format(first.In(im.loc))
	return s, err == nil
}

// itemRecord is the item an entry becomes (spec/pool.md, the table of Legacy
// import and export): the words of the entry as they are, the dates at the
// start of their day in the zone, no level and no parent, and the legacy
// identity beside them. The warnings are what it kept but could not read.
func (im *importer) itemRecord(e walkedEntry, taskID string) (map[string]any, []string) {
	t := e.Task
	var warns []string
	state := map[State]string{StateQueued: "queued", StateInFlight: "in-flight", StateDone: "done"}[t.State]
	rec := map[string]any{
		"schema": "pool.item", "schema_version": "1.0", "record_revision": int64(1),
		"task_id": taskID, "title": t.Title, "state": state, "definition": "not-required",
		"placement_requested": "policy", "links": []any{}, "generations": []any{},
		"created_at": im.now, "updated_at": im.now,
	}
	putScope(rec, im.scope, true)
	if t.Body != "" {
		rec["body"] = t.Body
	}
	if t.Priority != nil {
		rec["priority"] = int64(*t.Priority)
	}
	if t.Created != "" {
		if at, ok := im.at(t.Created); ok {
			rec["created_at"] = at
		} else {
			warns = append(warns, "the date "+t.Created+" is not a calendar date; it stays in the entry's lines and the item is created now")
		}
	}
	if t.State == StateDone {
		rec["completed"] = true
		at, ok := im.at(t.Closed)
		switch {
		case ok:
			rec["closed_at"] = at
		case t.Closed == "":
			rec["closed_at_unknown"] = true
			warns = append(warns, "the entry has no closing date; it is completed with its time unknown")
		default:
			rec["closed_at_unknown"] = true
			warns = append(warns, "the date "+t.Closed+" is not a calendar date; the entry is completed with its time unknown")
		}
	}
	legacy := map[string]any{"legacy_id": t.ID, "migration_id": im.migID}
	if t.Kind != "" {
		legacy["kind"] = t.Kind
	}
	if t.Repo != "" {
		legacy["repo"] = t.Repo
	}
	var meta []any
	deps := []any{}
	for _, d := range t.Deps {
		to, held := im.taskOf[d.ID]
		if !held {
			value := d.ID
			if d.Reason != "" {
				value += " - " + d.Reason
			}
			meta = append(meta, map[string]any{"key": d.Type, "value": value})
			warns = append(warns, "the "+d.Type+" edge names "+d.ID+", which the file holds no item for; it is kept in the item's legacy members and treated as resolved")
			continue
		}
		edge := map[string]any{"type": d.Type, "task_id": to}
		if d.Reason != "" {
			edge["reason"] = d.Reason
		}
		deps = append(deps, edge)
	}
	rec["deps"] = deps
	if meta != nil {
		legacy["meta"] = meta
	}
	rec["legacy"] = legacy
	links := []any{}
	for _, l := range t.Links {
		switch l.Kind {
		case item.LinkPR:
			links = append(links, map[string]any{"kind": "harvest", "url": l.URL})
		case item.LinkReport:
			links = append(links, map[string]any{"kind": "report", "path": l.URL})
		default:
			links = append(links, map[string]any{"kind": "doc", "url": l.URL})
		}
	}
	rec["links"] = links
	return rec, warns
}

// holdRecord is the hold an entry's hold tags become. The date is kept as the
// instant it starts at in the zone, the zone and the date as written.
func (im *importer) holdRecord(taskID, holdID string, h *Hold) (map[string]any, []string) {
	var warns []string
	rec := map[string]any{
		"schema": "pool.hold", "schema_version": "1.0", "record_revision": int64(1),
		"hold_id": holdID, "task_id": taskID, "kind": holdKinds[h.Kind], "reason": h.Reason, "state": "active",
		"placed_at":  im.now,
		"placed_by":  map[string]any{"request_id": im.c.Request.RequestID, "caller": im.c.Request.Tree()["caller"]},
		"created_at": im.now, "updated_at": im.now,
	}
	putScope(rec, im.scope, false)
	if h.Until != "" {
		if at, ok := im.at(h.Until); ok {
			rec["until"] = map[string]any{"at": at, "zone": im.zone, "date": h.Until}
		} else {
			warns = append(warns, "the date "+h.Until+" is not a calendar date; the hold is made with no date")
		}
	}
	return rec, warns
}

// writeImport is the import that writes: in one transaction the migration with
// the source text and every entry's exact lines, an item for each entry that is
// one with its legacy identifier kept, a hold for each entry with hold tags,
// and the receipts of each record. It reads the migration back through its own
// codec first and refuses the file if what is stored would not render back to
// the file, so that nothing is written that an export could not return.
func writeImport(c *cli.Call, src []byte, digest, path, zone string, loc *time.Location, b *Backlog, rep map[string]any) (*cli.Result, error) {
	now, err := c.Stamp()
	if err != nil {
		return nil, envelope.Common(envelope.CodeInternal, "the import cannot be given a time").Because(err)
	}
	scope := c.Request.Scope
	scope.TaskID, scope.Generation = "", 0
	migID, err := cids.Mint(c.Clock, cids.Migration)
	if err != nil {
		return nil, envelope.Common(envelope.CodeInternal, "a migration cannot be given an identity").Because(err)
	}
	im := &importer{c: c, loc: loc, zone: zone, now: now, scope: scope, migID: migID, taskOf: map[string]string{}}

	w := walk(b)
	made := make([]MigrationEntry, len(w.Entries))
	holdIDs := make([]string, len(w.Entries))
	seen := map[string]bool{}
	for i, e := range w.Entries {
		d, reason := disposition(e.Task, seen)
		made[i] = MigrationEntry{LegacyID: e.Task.ID, FirstLine: e.FirstLine, Lines: e.Lines, Section: e.Section, Disposition: d, Reason: reason}
		if d != DispositionItem {
			continue
		}
		if made[i].TaskID, err = cids.Mint(c.Clock, cids.Task); err != nil {
			return nil, envelope.Common(envelope.CodeInternal, "an item cannot be given an identity").Because(err)
		}
		if _, held := im.taskOf[e.Task.ID]; !held {
			im.taskOf[e.Task.ID] = made[i].TaskID
		}
	}

	t := &txn{c: c}
	var itemRecs, holdRecs []any
	type staged struct {
		kind, id string
		rec      map[string]any
		task     string
		scope    envelope.Scope
	}
	var rest []staged
	for i, e := range w.Entries {
		if made[i].Disposition != DispositionItem {
			continue
		}
		rec, warns := im.itemRecord(e, made[i].TaskID)
		made[i].Warnings = append(made[i].Warnings, warns...)
		rest = append(rest, staged{"item", made[i].TaskID, rec, made[i].TaskID, scope})
		if e.Task.Hold == nil {
			continue
		}
		if holdIDs[i], err = cids.Mint(c.Clock, cids.Hold); err != nil {
			return nil, envelope.Common(envelope.CodeInternal, "a hold cannot be given an identity").Because(err)
		}
		hold, warns := im.holdRecord(made[i].TaskID, holdIDs[i], e.Task.Hold)
		made[i].Warnings = append(made[i].Warnings, warns...)
		made[i].HoldID = holdIDs[i]
		holdScope := scope
		holdScope.CloneID = ""
		rest = append(rest, staged{"hold", holdIDs[i], hold, made[i].TaskID, holdScope})
	}

	// The migration is the first record of the transaction, and its receipt the
	// first receipt.
	mrec := encodeMigrationBody(path, digest, src, now, zone, b, made)
	for k, v := range map[string]any{
		"schema": "pool.migration", "schema_version": "1.0", "record_revision": int64(1),
		"migration_id": migID, "imported_at": now, "request_id": c.Request.RequestID,
		"created_at": now, "updated_at": now,
	} {
		mrec[k] = v
	}
	putScope(mrec, scope, false)
	migScope := scope
	migScope.CloneID = ""
	final, ref, err := t.put("migration", migID, mrec)
	if err != nil {
		return nil, err
	}
	// What is stored must lay the file out again and render it back exactly.
	m, err := DecodeMigration(migID, 1, final)
	if err == nil {
		var l *Layout
		if l, err = m.Layout(); err == nil && l.Text() != string(src) {
			err = errors.New("the layout renders another text")
		}
	}
	if err != nil {
		return nil, envelope.Fail(envelope.Refused, codeNotRoundTrip, "the parsed file does not render back byte for byte from the migration that would be stored, so it is not imported").Because(err)
	}
	if err := t.receipt("pool.legacy.imported", ref, migScope, ""); err != nil {
		return nil, err
	}
	for _, s := range rest {
		recFinal, recRef, err := t.put(s.kind, s.id, s.rec)
		if err != nil {
			return nil, err
		}
		receipt := "pool.item.added"
		if s.kind == "hold" {
			receipt = "pool.hold.placed"
			holdRecs = append(holdRecs, recFinal)
		} else {
			itemRecs = append(itemRecs, recFinal)
		}
		if err := t.receipt(receipt, recRef, s.scope, s.task); err != nil {
			return nil, err
		}
	}
	for _, rec := range t.tx.Records {
		if err := checkScope(c, rec.Body); err != nil {
			return nil, err
		}
	}
	st, err := poolstore.Stage(c, t.tx)
	if err != nil {
		return nil, err
	}

	// The report names the item each entry became.
	if rows, _ := rep["entries"].([]any); len(rows) == len(made) {
		for i, row := range rows {
			if made[i].TaskID != "" {
				row.(map[string]any)["task_id"] = made[i].TaskID
			}
		}
	}
	if itemRecs == nil {
		itemRecs = []any{}
	}
	if holdRecs == nil {
		holdRecs = []any{}
	}
	return st.Result(map[string]any{"report": rep, "migration": final, "items": itemRecs, "holds": holdRecs}), nil
}

// checkScope refuses a record staged outside the scope of the request.
func checkScope(c *cli.Call, rec map[string]any) error {
	if !inScope(c.Request.Scope, rec) {
		return envelope.Common(envelope.CodeScopeMismatch, "the record is outside the scope of the request")
	}
	return nil
}
