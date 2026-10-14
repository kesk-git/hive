package legacy

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/schema"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/item"
	"github.com/kesk-git/hive/packages/pool/internal/read"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// This file is translated from the render half of tasks-axi's
// src/backends/markdown.ts and its `render` verb (src/commands/maintain.ts):
// the Pool's items written in the legacy form, each in the section of its
// state and canonically, the legacy words taken from the item's legacy member,
// and a hold tag only for an active hold. The export from a migration keeps the
// file the migration kept, and writes again only the items that changed since
// the import.

// codeLegacyMalformed is the code of a value the legacy form cannot carry.
const codeLegacyMalformed = "pool.legacy_malformed"

// holdWords are the legacy words of the Pool's hold kinds. An admission hold
// and a definition hold are the Pool's own and have none.
var holdWords = map[string]string{
	"word": "captain", "external": "external", "resource": "load", "parked": "parked", "future": "future",
}

// exportLegacy is the operation export-legacy: it renders the items the
// selector matches in the legacy form and returns the text with its digest and
// size. It writes no file and no record.
func exportLegacy(c *cli.Call) (*cli.Result, error) {
	body := c.Request.Body
	zone, _ := body["zone"].(string)
	loc, err := location(zone)
	if err != nil {
		return nil, envelope.Common(envelope.CodeSchemaViolation, "zone is not a zone of the IANA database").Because(err)
	}
	st, err := openStore(c)
	if err != nil {
		return nil, err
	}
	lock, err := st.LockShared()
	if err != nil {
		return nil, poolstore.Failure("the Pool", err)
	}
	defer lock.Release()
	g, omitted, err := read.LoadGraph(st, c.Clock.Now())
	if err != nil {
		return nil, poolstore.Failure("the Pool", err)
	}
	if len(omitted) > 0 {
		return nil, envelope.Common(envelope.CodeRecordMalformed, "a record of the Pool cannot be read, so the backlog cannot be rendered whole").With("record", omitted[0].ID)
	}
	if id, ok := body["migration_id"].(string); ok {
		normalize, _ := body["normalize"].(bool)
		return exportMigration(c, st, g, loc, id, normalize)
	}
	selected, err := selection(c, g)
	if err != nil {
		return nil, err
	}
	if err := noCredential(g, selected); err != nil {
		return nil, err
	}

	r := &renderer{g: g, loc: loc}
	out := &Backlog{FinalNewline: true, Preamble: []string{"# Backlog", ""}}
	out.EnsureSections()
	listed := []any{}
	for _, id := range selected {
		t, err := r.task(id)
		if err != nil {
			return nil, err
		}
		for _, s := range out.Sections {
			if s.State == t.State {
				s.Entries = append(s.Entries, &Entry{Task: t, Dirty: true})
			}
		}
		row := map[string]any{"task_id": id, "form": "rendered"}
		if legacyID := legacyMember(g.Items[id].Data, "legacy_id"); legacyID != "" {
			row["legacy_id"] = legacyID
		}
		listed = append(listed, row)
	}
	// A blank line stands between the sections, as a person writes the file.
	for _, s := range out.Sections[:len(out.Sections)-1] {
		s.Entries = append(s.Entries, &Entry{Lines: []string{""}})
	}
	text := out.Render()
	return &cli.Result{Body: map[string]any{
		"text": string(text), "digest": canon.FileDigest(text), "bytes": int64(len(text)),
		"exact": false, "items": listed,
	}}, nil
}

// openStore opens the Pool's store in the home for a read, with the journal
// that recovery appends to, as hive-pool read does.
func openStore(c *cli.Call) (*cstore.Store, error) {
	e := c.Executable
	var j cstore.Journal
	if e.Journal != nil {
		var err error
		if j, err = e.Journal(c.Home.Dir); err != nil {
			return nil, envelope.Common(envelope.CodeRecordMalformed, "the home journal cannot be opened").Because(err)
		}
	}
	st, err := cstore.Open(c.Home.Dir, poolstore.Package, cstore.Options{Clock: c.Clock, Journal: j, Waiting: e.Waiting})
	if err != nil {
		return nil, poolstore.Failure("the home", err)
	}
	if e.Fault != nil {
		st.Fault(e.Fault)
	}
	return st, nil
}

// noCredential refuses an export when a record it writes from holds a string
// of a credential shape, as the owner's no-value check finds it in the record
// as stored, before any of it is rendered: an item selected, an item one of
// their edges names, or a hold on an item selected. The refusal names the
// record and the field, never the value.
func noCredential(g *read.Graph, selected []string) error {
	reg, err := poolstore.Schemas()
	if err != nil {
		return envelope.Common(envelope.CodeInternal, "the embedded schemas do not load").Because(err)
	}
	itemKind, _ := poolstore.Lookup("item")
	holdKind, _ := poolstore.Lookup("hold")
	check := func(k poolstore.Kind, id string, data map[string]any) error {
		var se *schema.ShapeError
		switch err := reg.CheckShapes(k.Ref(), data); {
		case err == nil:
			return nil
		case errors.As(err, &se):
			where := se.WhereIn("record")
			return envelope.Common(envelope.CodeSecretShapeRecognized, fmt.Sprintf("the %s record %s holds a string of the credential shape %s at %s", k.Name, id, se.Shape, where)).
				With("record", id).With("field", where)
		default:
			return envelope.Common(envelope.CodeInternal, "a record cannot be checked for a credential").Because(err)
		}
	}
	checked := map[string]bool{}
	checkItem := func(id string) error {
		n := g.Items[id]
		if n == nil || checked[id] {
			return nil
		}
		checked[id] = true
		return check(itemKind, id, n.Data)
	}
	for _, id := range selected {
		if err := checkItem(id); err != nil {
			return err
		}
		for _, e := range read.Edges(g.Items[id].Data) {
			if err := checkItem(e.TaskID); err != nil {
				return err
			}
		}
		for _, h := range g.Holds[id] {
			if err := check(holdKind, h.ID, h.Data); err != nil {
				return err
			}
		}
	}
	return nil
}

// selection is the identities of the items the request's selector matches, in
// the order the Pool read them. The items outside the scope of the request are
// not among them; an identity that names none is not found.
func selection(c *cli.Call, g *read.Graph) ([]string, error) {
	sel, _ := c.Request.Body["selector"].(map[string]any)
	taskID, _ := sel["task_id"].(string)
	projectID, _ := sel["project_id"].(string)
	rawStates, wantStates := sel["states"].([]any)
	var states []string
	for _, v := range rawStates {
		s, _ := v.(string)
		states = append(states, s)
	}
	ready, wantReady := sel["ready"].(bool)
	limit, _ := sel["limit"].(int64)
	var out []string
	for _, id := range g.Order {
		n := g.Items[id]
		switch {
		case !inScope(c.Request.Scope, n.Data):
			continue
		case taskID != "" && id != taskID:
			continue
		case projectID != "" && n.Data["project_id"] != projectID:
			continue
		case wantStates && !slices.Contains(states, n.State):
			continue
		case wantReady && g.Facts(id).Ready != ready:
			continue
		}
		out = append(out, id)
	}
	if taskID != "" && len(out) == 0 {
		return nil, envelope.Common(envelope.CodeNotFound, "no item has that identity")
	}
	if limit > 0 && int64(len(out)) > limit {
		out = out[:limit]
	}
	return out, nil
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// inScope reports whether the scope of the request contains the record: a hive
// scope every record of its home, a workspace scope its workspace and that
// workspace's projects, a project scope only that project. The clone is not
// compared.
func inScope(want envelope.Scope, rec map[string]any) bool {
	str := func(k string) string { s, _ := rec[k].(string); return s }
	if str("home_id") != want.HomeID {
		return false
	}
	switch want.Level {
	case envelope.ScopeWorkspace:
		return str("workspace_id") == want.WorkspaceID
	case envelope.ScopeProject:
		return str("scope_level") == envelope.ScopeProject && str("workspace_id") == want.WorkspaceID && str("project_id") == want.ProjectID
	}
	return true
}

// legacyMember is a member of the item's legacy member, or "".
func legacyMember(data map[string]any, key string) string {
	m, _ := data["legacy"].(map[string]any)
	s, _ := m[key].(string)
	return s
}

// renderer reads items out of a graph into the legacy form.
type renderer struct {
	g   *read.Graph
	loc *time.Location
}

// identity is the identifier an item is written under: its legacy one, and for
// an item that was never imported its own identity, which the grammar accepts.
func (r *renderer) identity(taskID string) string {
	if n := r.g.Items[taskID]; n != nil {
		if id := legacyMember(n.Data, "legacy_id"); id != "" {
			return id
		}
	}
	return taskID
}

// section is the state of the section an item stands in. An item that has
// landed is read as done, a Target and a Prey with Hunts included, because
// they store no completion of their own; one archived without having landed
// has no section of its own, and stands in the queue.
func (r *renderer) section(id string) State {
	n := r.g.Items[id]
	switch {
	case n.State == "done", r.g.Landed(id):
		return StateDone
	case n.State == "in-flight":
		return StateInFlight
	}
	return StateQueued
}

// task is the legacy reading of an item: its words from the legacy member, its
// dates in the zone of the request, the tags of its active hold and nothing of
// Hive's own.
func (r *renderer) task(id string) (*Task, error) {
	n := r.g.Items[id]
	data := n.Data
	malformed := func(err error) error {
		return envelope.Fail(envelope.Invalid, codeLegacyMalformed, "an item cannot be written in the legacy form").With("task_id", id).Because(err)
	}
	t := &Task{ID: r.identity(id), State: r.section(id)}
	var err error
	title, _ := data["title"].(string)
	if t.Title, err = NormalizeTitle(title); err != nil {
		return nil, malformed(err)
	}
	t.Body, _ = data["body"].(string)
	if kind := legacyMember(data, "kind"); kind != "" {
		if t.Kind, err = NormalizeTagValue(kind, "kind"); err != nil {
			return nil, malformed(err)
		}
	} else {
		t.Kind = LeadingKind(t.Title)
	}
	if repo := legacyMember(data, "repo"); repo != "" {
		if t.Repo, err = NormalizeTagValue(repo, "repo"); err != nil {
			return nil, malformed(err)
		}
	}
	if p, ok := data["priority"].(int64); ok {
		v, err := NormalizePriority(int(p))
		if err != nil {
			return nil, malformed(err)
		}
		t.Priority = &v
	}
	if t.Created, err = r.date(data["created_at"]); err != nil {
		return nil, malformed(err)
	}
	if unknown, _ := data["closed_at_unknown"].(bool); !unknown {
		if t.Closed, err = r.date(data["closed_at"]); err != nil {
			return nil, malformed(err)
		}
	}
	if t.Title, t.Links, err = r.links(t.Title, data); err != nil {
		return nil, malformed(err)
	}
	for _, e := range read.Edges(data) {
		d, err := NormalizeDep(t.ID, Dep{Type: e.Type, ID: r.identity(e.TaskID), Reason: e.Reason})
		if err != nil {
			return nil, malformed(err)
		}
		t.Deps = append(t.Deps, d)
	}
	if t.Hold, err = r.hold(id); err != nil {
		return nil, malformed(err)
	}
	return t, nil
}

// date is the calendar date of a timestamp in the zone, or "" for none.
func (r *renderer) date(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", nil
	}
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return "", err
	}
	return at.In(r.loc).Format("2006-01-02"), nil
}

// links returns the title with the links the item carries that it does not yet
// hold, which is where the legacy form keeps them, and the links that
// title holds as the legacy words name their kinds, which a re-parse derives.
// A link the legacy form cannot hold, a record reference or a path that is no
// report, is left out.
func (r *renderer) links(title string, data map[string]any) (string, []item.Link, error) {
	for _, v := range asList(data["links"]) {
		m, _ := v.(map[string]any)
		url, _ := m["url"].(string)
		if url == "" {
			url, _ = m["path"].(string)
		}
		if url == "" || m["kind"] == "record" {
			continue
		}
		// The legacy words are those the prose derives for the URL itself.
		derived := item.DeriveLinks(url)
		i := slices.IndexFunc(derived, func(l item.Link) bool { return l.URL == url })
		if i < 0 {
			continue
		}
		link := derived[i]
		if _, err := NormalizeTypedLink(link); err != nil {
			continue
		}
		var err error
		if title, err = AppendTitleLink(title, link); err != nil {
			return "", nil, err
		}
	}
	return title, item.DeriveLinks(title), nil
}

// hold is the hold tags of the item's first hold in force that has a legacy
// word, or nil. The legacy form carries one hold to an entry; a hold released
// is never written back.
func (r *renderer) hold(id string) (*Hold, error) {
	for _, h := range r.g.Holds[id] {
		word, ok := holdWords[h.Kind]
		if !r.g.InForce(h) || !ok {
			continue
		}
		reason, _ := h.Data["reason"].(string)
		out := Hold{Reason: legacyReason(reason), Kind: word}
		if until, ok := h.Data["until"].(map[string]any); ok {
			if _, timed := until["at"]; timed {
				var err error
				if out.Until, err = r.date(until["at"]); err != nil {
					return nil, err
				}
			} else if date, _ := until["date"].(string); date != "" {
				out.Until = date
			}
		}
		norm, err := NormalizeHold(out)
		if err != nil {
			return nil, err
		}
		return &norm, nil
	}
	return nil, nil
}

// legacyReason is a hold reason as a tag can hold it: one line without
// parentheses, which the tag could not carry.
func legacyReason(s string) string {
	s = strings.NewReplacer("(", "[", ")", "]", "\r", " ", "\n", " ", " ", " ", " ", " ").Replace(s)
	return strings.TrimSpace(s)
}

// ---------------------------------------------------------------------------
// The export from a migration
// ---------------------------------------------------------------------------

// importedRevision is the revision of every record the import wrote.
const importedRevision = 1

// unmodified reports whether the item stands as the import wrote it: it is at
// the revision the import wrote, and the holds on it are exactly the one the
// import placed for it, if it placed one, at the revision it wrote. A hold
// placed or released since makes the item modified, because the legacy form
// carries a hold in the entry's own tags.
func unmodified(g *read.Graph, taskID string, e MigrationEntry) bool {
	if g.Items[taskID].Revision != importedRevision {
		return false
	}
	holds := g.Holds[taskID]
	if e.HoldID == "" {
		return len(holds) == 0
	}
	return len(holds) == 1 && holds[0].ID == e.HoldID && holds[0].Revision == importedRevision
}

// movedEntry is an item whose present state is not its source section's.
type movedEntry struct {
	entry int
	state State
}

// exportMigration is the export-legacy of a migration: the source's preamble,
// sections, free lines and entries in the source's order. An item unmodified
// since its import is emitted from its exact original lines and a modified
// one is written again canonically, with normalize every item is. Every item
// is emitted in the section its present state maps to: one whose state still
// maps to its source section keeps its place, and one whose state has changed
// goes to the end of the first section of the file that maps to its present
// state, or under a canonical header appended after the last section where
// the file has none. An excluded entry, a duplicate and every free line are
// emitted as they were.
func exportMigration(c *cli.Call, st *cstore.Store, g *read.Graph, loc *time.Location, id string, normalize bool) (*cli.Result, error) {
	m, err := LoadMigration(st, id)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return nil, envelope.Common(envelope.CodeNotFound, "no migration has that identity")
	case err != nil:
		return nil, envelope.Common(envelope.CodeRecordMalformed, "the migration cannot be read").With("record", id).Because(err)
	}
	if !inScope(c.Request.Scope, m.Data) {
		return nil, envelope.Common(envelope.CodeScopeMismatch, "the migration is outside the scope of the request")
	}
	layout, err := m.Layout()
	if err != nil {
		return nil, envelope.Common(envelope.CodeRecordMalformed, "the migration does not lay out its source").With("record", id).Because(err)
	}
	var taskIDs []string
	for _, e := range m.Entries {
		if e.Disposition != DispositionItem {
			continue
		}
		if g.Items[e.TaskID] == nil {
			return nil, envelope.Common(envelope.CodeRecordMalformed, "the migration names an item the Pool does not hold").With("record", id).With("task_id", e.TaskID)
		}
		taskIDs = append(taskIDs, e.TaskID)
	}
	if err := noCredential(g, taskIDs); err != nil {
		return nil, err
	}

	r := &renderer{g: g, loc: loc}
	// The section each entry stands in, and what becomes of each item.
	secOf := map[int]int{}
	for si, sec := range layout.Sections {
		for _, b := range sec.Blocks {
			if b.Entry >= 0 {
				secOf[b.Entry] = si
			}
		}
	}
	canonical := map[int][]string{}
	verbatim := map[int]bool{}
	var moved []movedEntry
	for i, e := range m.Entries {
		if e.Disposition != DispositionItem {
			continue
		}
		present := r.section(e.TaskID)
		t, err := r.task(e.TaskID)
		if err != nil {
			return nil, err
		}
		// An entry that gave no date was created at the time of the import,
		// which is the migration's fact and no date the file wrote.
		if original := originalTask(layout.Sections[secOf[i]], e); original != nil && original.Created == "" &&
			g.Items[e.TaskID].Data["created_at"] == m.Data["imported_at"] {
			t.Created = ""
		}
		inPlace := layout.Sections[secOf[i]].State == present
		switch {
		case !normalize && inPlace && unmodified(g, e.TaskID, e):
			verbatim[i] = true
		case inPlace:
			canonical[i] = RenderTaskLines(t)
		default:
			canonical[i] = RenderTaskLines(t)
			moved = append(moved, movedEntry{entry: i, state: present})
		}
	}
	isMoved := map[int]bool{}
	for _, mv := range moved {
		isMoved[mv.entry] = true
	}

	type outSection struct {
		header string
		state  State
		lines  []string
		rows   []any
		// newSection says that the file had no section for the state.
		newSection bool
	}
	row := func(i int, form string) map[string]any {
		e := m.Entries[i]
		out := map[string]any{"task_id": e.TaskID, "form": form}
		if legacyID := legacyMember(g.Items[e.TaskID].Data, "legacy_id"); legacyID != "" {
			out["legacy_id"] = legacyID
		}
		return out
	}
	var out []*outSection
	for _, sec := range layout.Sections {
		os := &outSection{header: sec.Header, state: sec.State}
		for _, b := range sec.Blocks {
			switch {
			case b.Entry < 0 || m.Entries[b.Entry].Disposition != DispositionItem:
				os.lines = append(os.lines, b.Lines...)
			case isMoved[b.Entry]:
			case verbatim[b.Entry]:
				os.lines = append(os.lines, b.Lines...)
				os.rows = append(os.rows, row(b.Entry, "verbatim"))
			default:
				os.lines = append(os.lines, canonical[b.Entry]...)
				os.rows = append(os.rows, row(b.Entry, "rendered"))
			}
		}
		out = append(out, os)
	}
	// An item whose state changed goes to the end of the first section that
	// maps to its present state, before the blank lines that end the section.
	for _, mv := range moved {
		target := slices.IndexFunc(out, func(s *outSection) bool { return s.state == mv.state })
		if target < 0 {
			out = append(out, &outSection{header: sectionHeaders[mv.state], state: mv.state, newSection: true})
			target = len(out) - 1
		}
		os := out[target]
		at := len(os.lines)
		for at > 0 && isBlank(os.lines[at-1]) {
			at--
		}
		os.lines = slices.Insert(os.lines, at, canonical[mv.entry]...)
		os.rows = append(os.rows, row(mv.entry, "rendered"))
	}

	lines := slicesClone(layout.Preamble)
	for _, os := range out {
		if os.newSection && len(lines) > 0 && !isBlank(lines[len(lines)-1]) {
			lines = append(lines, "")
		}
		lines = append(lines, os.header)
		lines = append(lines, os.lines...)
	}
	text := strings.Join(lines, "\n")
	if layout.FinalNewline || (len(layout.Sections) == 0 && len(out) > 0) {
		text += "\n"
	}
	if len(lines) == 0 {
		text = ""
	}
	listed := []any{}
	for _, os := range out {
		listed = append(listed, os.rows...)
	}
	return &cli.Result{Body: map[string]any{
		"text": text, "digest": canon.FileDigest([]byte(text)), "bytes": int64(len(text)),
		"exact": text == m.SourceText, "items": listed,
	}}, nil
}

// originalTask is the task the entry's exact lines say, read again under the
// header of the section it stands in, or nil if they say none.
func originalTask(sec LayoutSection, e MigrationEntry) *Task {
	b, err := Parse([]byte(sec.Header + "\n" + strings.Join(e.Lines, "\n")))
	if err != nil || len(b.Sections) != 1 {
		return nil
	}
	tasks := b.Tasks()
	if len(tasks) == 0 {
		return nil
	}
	return tasks[0]
}
