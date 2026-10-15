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
	// With normalize every item the selector matches is rendered canonically,
	// as without a migration; without it a migration is exported whole.
	if id, ok := body["migration_id"].(string); ok {
		if normalize, _ := body["normalize"].(bool); !normalize {
			return exportMigration(c, st, g, loc, id)
		}
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
	// named is the item each identifier the rendering writes stands for, or
	// "" for an edge the import kept to an identifier it held no item for.
	named map[string]string
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

// name is the identifier an item is written under, refused when the rendering
// writes it for another item too: a file that held it for two would be read
// back as one.
func (r *renderer) name(taskID string) (string, error) {
	id := r.identity(taskID)
	return id, r.claim(id, taskID)
}

// claim records that the identifier stands for the item where the rendering
// writes it, or for none, and refuses it where it already stands for another.
func (r *renderer) claim(id, taskID string) error {
	if prior, ok := r.named[id]; ok && prior != taskID {
		return envelope.Fail(envelope.Invalid, codeLegacyMalformed, "the legacy identifier "+id+" would stand for more than one item").With("legacy_id", id)
	}
	if r.named == nil {
		r.named = map[string]string{}
	}
	r.named[id] = taskID
	return nil
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
	var err error
	t := &Task{State: r.section(id)}
	if t.ID, err = r.name(id); err != nil {
		return nil, err
	}
	title, _ := data["title"].(string)
	if t.Title, err = NormalizeTitle(title); err != nil {
		return nil, malformed(err)
	}
	t.Body, _ = data["body"].(string)
	_, imported := data["legacy"].(map[string]any)
	switch kind := legacyMember(data, "kind"); {
	case kind != "":
		if t.Kind, err = NormalizeTagValue(kind, "kind"); err != nil {
			return nil, malformed(err)
		}
	case !imported:
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
		to, err := r.name(e.TaskID)
		if err != nil {
			return nil, err
		}
		d, err := NormalizeDep(t.ID, Dep{Type: e.Type, ID: to, Reason: e.Reason})
		if err != nil {
			return nil, malformed(err)
		}
		t.Deps = append(t.Deps, d)
	}
	// An edge to an identifier the file did not hold was kept in the legacy
	// members, and is written back where it was read.
	for _, m := range asList(legacyMeta(data)) {
		row, _ := m.(map[string]any)
		key, _ := row["key"].(string)
		value, _ := row["value"].(string)
		if key != "blocked-by" && key != "parent" && key != "discovered-from" {
			continue
		}
		dep := Dep{Type: key, ID: value}
		if id, reason, found := strings.Cut(value, " - "); found {
			dep.ID, dep.Reason = id, reason
		}
		d, err := NormalizeDep(t.ID, dep)
		if err != nil {
			return nil, malformed(err)
		}
		if err := r.claim(d.ID, ""); err != nil {
			return nil, err
		}
		t.Deps = append(t.Deps, d)
	}
	if t.Hold, err = r.hold(id); err != nil {
		return nil, malformed(err)
	}
	return t, nil
}

// legacyMeta is the free-form fields of the item's legacy member.
func legacyMeta(data map[string]any) any {
	m, _ := data["legacy"].(map[string]any)
	return m["meta"]
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
		if strings.ContainsAny(reason, "()") {
			return nil, fmt.Errorf("a hold reason containing parentheses cannot be written in a legacy tag")
		}
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
// sections, free lines and entries in the source's order, whole whatever the
// selector. An item unmodified since its import is emitted from its exact
// original lines and a modified one is written again canonically. Every item
// is emitted in the section its present state maps to: one whose state still
// maps to its source section keeps its place, and one whose state has changed
// goes to the end of the first section of the file that maps to its present
// state, or under a canonical header appended after the last section where
// the file has none. An excluded entry, a duplicate and every free line are
// emitted as they were.
func exportMigration(c *cli.Call, st *cstore.Store, g *read.Graph, loc *time.Location, id string) (*cli.Result, error) {
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
		if _, err := r.name(e.TaskID); err != nil {
			return nil, err
		}
		present := r.section(e.TaskID)
		inPlace := layout.Sections[secOf[i]].State == present
		if inPlace && unmodified(g, e.TaskID, e) {
			verbatim[i] = true
			continue
		}
		t, err := r.task(e.TaskID)
		if err != nil {
			return nil, err
		}
		canonical[i] = RenderTaskLines(t)
		if !inPlace {
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
		// entries are the entries the section emits, in their order.
		entries []int
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
			if b.Entry >= 0 && !isMoved[b.Entry] {
				os.entries = append(os.entries, b.Entry)
			}
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
		os.entries = append(os.entries, mv.entry)
	}
	// An import makes its item of the first entry of an identifier, and an
	// excluded entry excludes those after it: an item emitted after another
	// entry of its identifier would not be read back.
	seen := map[string]bool{}
	for _, os := range out {
		for _, i := range os.entries {
			e := m.Entries[i]
			if e.Disposition == DispositionItem && seen[e.LegacyID] {
				return nil, envelope.Fail(envelope.Invalid, codeLegacyMalformed, "the legacy identifier "+e.LegacyID+" would be read back from an entry other than its item's").With("task_id", e.TaskID).With("legacy_id", e.LegacyID)
			}
			seen[e.LegacyID] = true
		}
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
