package legacy

import (
	"errors"
	"fmt"
	"strings"

	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/read"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// This file is the codec and the read adapter of the pool.migration record
// (spec/pool.md, Legacy import and export; records/pool.migration.v1). The
// record keeps the source text and, in the pieces the schema names, every
// line of it: the lines before the first section, the section headers, each
// entry's exact lines with the line it starts on, and every other line by its
// line number. The pieces cover the text, so that the layout of the file can be
// rebuilt from the record alone and is held to the source text it kept.
//
// The pieces divide the lines of the file this way, as the contract's
// fixtures do: the preamble holds the lines before the first header and is
// not repeated in free_lines; a section header is in sections and in no other
// piece, without the carriage return that ends it, which the source text
// restores; an entry holds its bullet, its continuation lines and the blank
// lines that follow them, as the grammar's block does; and free_lines holds
// every line of a section that belongs to no entry.

// The dispositions of an entry of a migration.
const (
	DispositionItem      = "item"
	DispositionExcluded  = "excluded"
	DispositionDuplicate = "duplicate"
)

// MigrationSection is a section header of the file, as the migration keeps it.
type MigrationSection struct {
	// Header is the header line without the carriage return that ends it.
	Header string
	State  State
}

// MigrationEntry is one legacy bullet with an identifier.
type MigrationEntry struct {
	LegacyID    string
	FirstLine   int
	Lines       []string
	Section     string
	Disposition string
	TaskID      string
	HoldID      string
	Reason      string
	Warnings    []string
}

// FreeLine is a line of a section that belongs to no entry.
type FreeLine struct {
	Line int
	Text string
}

// Migration is a pool.migration record read.
type Migration struct {
	ID       string
	Revision int64
	// Data is the record as it is stored.
	Data map[string]any

	SourcePath   string
	SourceDigest string
	SourceText   string
	Zone         string
	Preamble     []string
	Sections     []MigrationSection
	Entries      []MigrationEntry
	FreeLines    []FreeLine
}

// stateOfWord is the grammar's state for the word a migration keeps.
func stateOfWord(w string) (State, bool) {
	switch w {
	case "queued":
		return StateQueued, true
	case "in-flight":
		return StateInFlight, true
	case "done":
		return StateDone, true
	case "none":
		return "", true
	}
	return "", false
}

// walkedEntry is an entry of a backlog as it stands in the file.
type walkedEntry struct {
	Task      *Task
	Lines     []string
	FirstLine int
	Section   string
}

// walked is a backlog laid out on its line numbers.
type walked struct {
	Sections []MigrationSection
	Entries  []walkedEntry
	Free     []FreeLine
}

// walk lays the backlog out on the line numbers of the file it was read from.
func walk(b *Backlog) walked {
	var w walked
	line := len(b.Preamble)
	for _, s := range b.Sections {
		line++
		header := semanticLine(s.HeaderLine)
		w.Sections = append(w.Sections, MigrationSection{Header: header, State: s.State})
		for _, e := range s.Entries {
			if e.Task != nil {
				w.Entries = append(w.Entries, walkedEntry{Task: e.Task, Lines: e.Lines, FirstLine: line + 1, Section: header})
			} else {
				for i, l := range e.Lines {
					w.Free = append(w.Free, FreeLine{Line: line + 1 + i, Text: l})
				}
			}
			line += len(e.Lines)
		}
	}
	return w
}

// stateWordOf is the word the migration keeps for a section's state.
func stateWordOf(s State) string {
	if s == "" {
		return "none"
	}
	return stateWord(s)
}

// encodeMigrationBody is the members of the record that come from the file,
// without its identity, scope, time or writer: the source, the zone and the
// pieces of the layout. entries are the migration's entries in file order,
// each with what the import made of it.
func encodeMigrationBody(path, digest string, text []byte, readAt, zone string, b *Backlog, made []MigrationEntry) map[string]any {
	w := walk(b)
	pre := make([]any, len(b.Preamble))
	for i, l := range b.Preamble {
		pre[i] = l
	}
	sections := make([]any, len(w.Sections))
	for i, s := range w.Sections {
		sections[i] = map[string]any{"header": s.Header, "state": stateWordOf(s.State)}
	}
	entries := make([]any, len(made))
	for i, e := range made {
		lines := make([]any, len(e.Lines))
		for j, l := range e.Lines {
			lines[j] = l
		}
		row := map[string]any{
			"legacy_id": e.LegacyID, "first_line": int64(e.FirstLine), "lines": lines,
			"section": e.Section, "disposition": e.Disposition,
		}
		if e.TaskID != "" {
			row["task_id"] = e.TaskID
		}
		if e.HoldID != "" {
			row["hold_id"] = e.HoldID
		}
		if e.Reason != "" {
			row["reason"] = e.Reason
		}
		if len(e.Warnings) > 0 {
			ws := make([]any, len(e.Warnings))
			for j, m := range e.Warnings {
				ws[j] = m
			}
			row["warnings"] = ws
		}
		entries[i] = row
	}
	free := make([]any, len(w.Free))
	for i, f := range w.Free {
		free[i] = map[string]any{"line": int64(f.Line), "text": f.Text}
	}
	return map[string]any{
		"source": map[string]any{
			"path": path, "digest": digest, "bytes": int64(len(text)), "text": string(text), "read_at": readAt,
		},
		"zone": zone, "preamble": pre, "sections": sections, "entries": entries, "free_lines": free,
	}
}

// DecodeMigration reads a stored pool.migration record, which has passed its
// schema, into the migration it is. It refuses a record that is not whole:
// pieces that overlap, lines that no piece covers, or pieces that are not the
// source text kept beside them, since the export of the migration is only
// exact if they are.
func DecodeMigration(id string, revision int64, data map[string]any) (*Migration, error) {
	m := &Migration{ID: id, Revision: revision, Data: data}
	bad := func(format string, args ...any) error {
		return fmt.Errorf("the migration %s is not whole: %s", id, fmt.Sprintf(format, args...))
	}
	src, _ := data["source"].(map[string]any)
	var ok bool
	if m.SourcePath, ok = src["path"].(string); !ok {
		return nil, bad("no source path")
	}
	if m.SourceDigest, ok = src["digest"].(string); !ok {
		return nil, bad("no source digest")
	}
	if m.SourceText, ok = src["text"].(string); !ok {
		return nil, bad("no source text")
	}
	if m.Zone, ok = data["zone"].(string); !ok {
		return nil, bad("no zone")
	}
	pre, _ := data["preamble"].([]any)
	for _, v := range pre {
		s, ok := v.(string)
		if !ok {
			return nil, bad("a preamble line is not text")
		}
		m.Preamble = append(m.Preamble, s)
	}
	secs, _ := data["sections"].([]any)
	for _, v := range secs {
		row, _ := v.(map[string]any)
		header, ok1 := row["header"].(string)
		word, _ := row["state"].(string)
		state, ok2 := stateOfWord(word)
		if !ok1 || !ok2 {
			return nil, bad("a section is %v", v)
		}
		m.Sections = append(m.Sections, MigrationSection{Header: header, State: state})
	}
	entries, _ := data["entries"].([]any)
	for _, v := range entries {
		row, _ := v.(map[string]any)
		e := MigrationEntry{}
		e.LegacyID, _ = row["legacy_id"].(string)
		first, _ := row["first_line"].(int64)
		e.FirstLine = int(first)
		e.Section, _ = row["section"].(string)
		e.Disposition, _ = row["disposition"].(string)
		e.TaskID, _ = row["task_id"].(string)
		e.HoldID, _ = row["hold_id"].(string)
		e.Reason, _ = row["reason"].(string)
		lines, _ := row["lines"].([]any)
		for _, l := range lines {
			s, ok := l.(string)
			if !ok {
				return nil, bad("a line of the entry %s is not text", e.LegacyID)
			}
			e.Lines = append(e.Lines, s)
		}
		if e.LegacyID == "" || e.FirstLine < 1 || len(e.Lines) == 0 {
			return nil, bad("an entry is %v", v)
		}
		ws, _ := row["warnings"].([]any)
		for _, w := range ws {
			s, _ := w.(string)
			e.Warnings = append(e.Warnings, s)
		}
		m.Entries = append(m.Entries, e)
	}
	free, _ := data["free_lines"].([]any)
	for _, v := range free {
		row, _ := v.(map[string]any)
		line, _ := row["line"].(int64)
		text, ok := row["text"].(string)
		if !ok || line < 1 {
			return nil, bad("a free line is %v", v)
		}
		m.FreeLines = append(m.FreeLines, FreeLine{Line: int(line), Text: text})
	}
	return m, nil
}

// LoadMigration reads the migration of the identity from the Pool's store.
func LoadMigration(st *cstore.Store, id string) (*Migration, error) {
	l, om, err := poolstore.Load(st, "migration", id)
	switch {
	case errors.Is(err, poolstore.ErrNotFound):
		return nil, poolstore.ErrNotFound
	case err != nil:
		return nil, err
	case om != nil:
		return nil, fmt.Errorf("the migration %s cannot be read: %s", id, om.Reason)
	}
	return DecodeMigration(l.ID, l.Revision, l.Data)
}

// Block is a run of lines of a section: the lines of one entry, which are the
// entry's own up to its last line with content, or free lines that belong to
// no entry, among them the blank lines that end an entry's block, which stay
// where they stand whatever becomes of the entry.
type Block struct {
	Lines []string
	// Entry is the index of the entry in Migration.Entries, or -1 for free
	// lines.
	Entry int
}

// LayoutSection is a section of the file laid out.
type LayoutSection struct {
	// Header is the exact header line, with the carriage return that ends it.
	Header string
	State  State
	Blocks []Block
}

// Layout is the file a migration kept, laid out in the order of its lines.
type Layout struct {
	FinalNewline bool
	Preamble     []string
	Sections     []LayoutSection
}

// isBlank reports whether the line is empty as the grammar's block counts it.
func isBlank(line string) bool { return trim(semanticLine(line)) == "" }

// sourceLines splits the text of a source the way the grammar reads it.
func sourceLines(text string) (lines []string, finalNewline bool) {
	if text == "" {
		return nil, false
	}
	finalNewline = strings.HasSuffix(text, "\n")
	if finalNewline {
		text = text[:len(text)-1]
	}
	return strings.Split(text, "\n"), finalNewline
}

// Layout rebuilds the file from the pieces of the record. It is the read side
// of the record's codec and holds the pieces to the source text: a line that
// is placed twice, a line no piece holds, a header the source does not have
// where the pieces say, or a line that differs from the source line it stands
// for is an error, and the layout it returns renders back the source text.
func (m *Migration) Layout() (*Layout, error) {
	lines, final := sourceLines(m.SourceText)
	n := len(lines)
	bad := func(format string, args ...any) (*Layout, error) {
		return nil, fmt.Errorf("the migration %s does not lay out its source: %s", m.ID, fmt.Sprintf(format, args...))
	}
	const (
		unplaced = iota
		inPreamble
		inHeader
		inFree
		inEntry
	)
	owner := make([]int, n+1)
	entryOf := make([]int, n+1)
	place := func(line int, text string, as int) error {
		if line < 1 || line > n {
			return fmt.Errorf("line %d is outside the source's %d", line, n)
		}
		if owner[line] != unplaced {
			return fmt.Errorf("line %d is placed twice", line)
		}
		if lines[line-1] != text {
			return fmt.Errorf("line %d is not the source's", line)
		}
		owner[line] = as
		return nil
	}
	for i, l := range m.Preamble {
		if err := place(i+1, l, inPreamble); err != nil {
			return bad("%v", err)
		}
	}
	for i, e := range m.Entries {
		for k, l := range e.Lines {
			if err := place(e.FirstLine+k, l, inEntry); err != nil {
				return bad("entry %s: %v", e.LegacyID, err)
			}
			entryOf[e.FirstLine+k] = i
		}
	}
	for _, f := range m.FreeLines {
		if err := place(f.Line, f.Text, inFree); err != nil {
			return bad("%v", err)
		}
	}
	// What no piece holds is the section headers, in order.
	var headers []int
	for line := 1; line <= n; line++ {
		if owner[line] == unplaced {
			headers = append(headers, line)
		}
	}
	if len(headers) != len(m.Sections) {
		return bad("%d lines are held by no piece and the migration names %d sections", len(headers), len(m.Sections))
	}
	exact := make([]string, len(headers))
	for k, line := range headers {
		h := m.Sections[k].Header
		if got := lines[line-1]; got != h && got != h+"\r" {
			return bad("the header of section %d is not the source's line %d", k+1, line)
		}
		exact[k] = lines[line-1]
		owner[line] = inHeader
	}
	for line := 1; line <= len(m.Preamble); line++ {
		if owner[line] != inPreamble {
			return bad("the preamble is not the first lines of the file")
		}
	}
	l := &Layout{FinalNewline: final, Preamble: slicesClone(m.Preamble)}
	for k, hdr := range headers {
		end := n
		if k+1 < len(headers) {
			end = headers[k+1] - 1
		}
		sec := LayoutSection{Header: exact[k], State: m.Sections[k].State}
		for line := hdr + 1; line <= end; line++ {
			idx := -1
			if owner[line] == inEntry {
				idx = entryOf[line]
			}
			if len(sec.Blocks) > 0 && sec.Blocks[len(sec.Blocks)-1].Entry == idx {
				b := &sec.Blocks[len(sec.Blocks)-1]
				b.Lines = append(b.Lines, lines[line-1])
				continue
			}
			sec.Blocks = append(sec.Blocks, Block{Lines: []string{lines[line-1]}, Entry: idx})
		}
		sec.Blocks = splitTrailingBlanks(sec.Blocks)
		l.Sections = append(l.Sections, sec)
	}
	if got := l.Text(); got != m.SourceText {
		return bad("the pieces render %d bytes, not the source's %d", len(got), len(m.SourceText))
	}
	return l, nil
}

// splitTrailingBlanks gives the blank lines that end an entry's block a block
// of their own, so that an entry that is moved or written again leaves them
// where they stand.
func splitTrailingBlanks(blocks []Block) []Block {
	var out []Block
	for _, b := range blocks {
		if b.Entry < 0 {
			out = append(out, b)
			continue
		}
		end := len(b.Lines)
		for end > 1 && isBlank(b.Lines[end-1]) {
			end--
		}
		out = append(out, Block{Lines: b.Lines[:end], Entry: b.Entry})
		if end < len(b.Lines) {
			out = append(out, Block{Lines: b.Lines[end:], Entry: -1})
		}
	}
	return out
}

func slicesClone(s []string) []string { return append([]string(nil), s...) }

// Text is the file the layout is, with its blocks as they stand.
func (l *Layout) Text() string {
	if len(l.Preamble) == 0 && len(l.Sections) == 0 {
		return ""
	}
	lines := slicesClone(l.Preamble)
	for _, s := range l.Sections {
		lines = append(lines, s.Header)
		for _, b := range s.Blocks {
			lines = append(lines, b.Lines...)
		}
	}
	out := strings.Join(lines, "\n")
	if l.FinalNewline {
		out += "\n"
	}
	return out
}

func init() {
	read.Register(read.Kind{
		Name:       "migration",
		Members:    []string{read.MemberID, read.MemberLimit},
		Standalone: func(s read.Selector) bool { return s.ID != "" },
		Read:       readMigrations,
	})
}

// readMigrations is the reader of the kind migration of hive-pool read: the
// records whole, as they are stored.
func readMigrations(c *read.Context) (*read.Result, error) {
	res := &read.Result{}
	if id := c.Selector.ID; id != "" {
		l, om, err := poolstore.Load(c.Store, "migration", id)
		switch {
		case errors.Is(err, poolstore.ErrNotFound):
		case err != nil:
			return nil, err
		case om != nil:
			res.Omitted = append(res.Omitted, *om)
		default:
			res.Records = append(res.Records, read.Record{ID: l.ID, Data: l.Data})
		}
		return res, nil
	}
	all, omitted, err := poolstore.LoadAll(c.Store, "migration")
	if err != nil {
		return nil, err
	}
	for _, l := range all {
		res.Records = append(res.Records, read.Record{ID: l.ID, Data: l.Data})
	}
	res.Omitted = omitted
	return res, nil
}
