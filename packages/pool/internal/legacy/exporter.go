package legacy

import (
	"slices"
	"strings"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/item"
	"github.com/kesk-git/hive/packages/pool/internal/read"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// This file is translated from the render half of tasks-axi's
// src/backends/markdown.ts and its `render` verb (src/commands/maintain.ts):
// the Pool's items written in the legacy form, each in the section of its
// state and canonically, the legacy words taken from the item's legacy member,
// and a hold tag only for an active hold. The export from a migration, which
// emits an unmodified item from its exact original lines, needs the migration
// record and is a later lane's.

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
	if _, ok := body["migration_id"]; ok {
		return nil, envelope.Common(envelope.CodeUnsupportedOperation, "the export from a migration is not built yet; the export of the items the selector matches is")
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
	selected, err := selection(c, g)
	if err != nil {
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
	if t.Title, t.Links, err = r.links(t.Title, t.Body, data); err != nil {
		return nil, malformed(err)
	}
	for _, e := range read.Edges(data) {
		if e.TaskID == id {
			continue
		}
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

// links returns the title with the links the item carries that its prose does
// not yet hold, which is where the legacy form keeps them, and the links as the
// legacy words name their kinds. A link the legacy form cannot hold, a record
// reference or a path that is no report, is left out.
func (r *renderer) links(title, body string, data map[string]any) (string, []item.Link, error) {
	var out []item.Link
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
		if len(derived) != 1 || derived[0].URL != url {
			continue
		}
		link := derived[0]
		if _, err := NormalizeTypedLink(link); err != nil {
			continue
		}
		out = append(out, link)
		if slices.ContainsFunc(item.DeriveLinks(body), func(l item.Link) bool { return l.URL == link.URL }) {
			continue
		}
		var err error
		if title, err = AppendTitleLink(title, link); err != nil {
			return "", nil, err
		}
	}
	return title, out, nil
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
