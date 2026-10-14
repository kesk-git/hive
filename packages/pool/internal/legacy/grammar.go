// Package legacy reads and writes the legacy Markdown backlog: the format the
// Pool imports from and exports to. It is pure and read-only here: nothing in
// it touches a file or a Pool store.
//
// The grammar is translated from tasks-axi (see imports/tasks-axi.json),
// src/backends/markdown-grammar.ts, and its round trip rule is the upstream's:
//
//   - Every entry keeps its exact source lines. An entry nobody changed, and
//     every free-form line, is emitted as read, so Render(Parse(src)) equals src
//     byte for byte: carriage returns, a missing final line end, the lines before
//     the first section and any bytes this grammar does not understand included.
//   - An entry marked dirty is rendered again from its fields into the canonical
//     form, which parses back to the same fields. The entries around it stay as
//     they were.
//
// A line without an identifier is never addressed by identity; it is kept.
package legacy

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kesk-git/hive/packages/pool/internal/item"
)

// State is the section a legacy entry stands in. The words are the legacy
// file's; the import maps them to the Pool's states.
type State string

const (
	StateQueued   State = "queued"
	StateInFlight State = "in_flight"
	StateDone     State = "done"
)

// HoldKinds are the words a (hold-kind: ...) tag may carry, as the legacy
// file writes them.
var HoldKinds = []string{"captain", "external", "load", "parked", "future"}

// Dep is one dependency edge with the free-text reason that may follow it.
type Dep struct {
	Type   string
	ID     string
	Reason string // empty when the edge has none
}

// Hold is a legacy hold: a reason, and optionally its kind word and a date.
type Hold struct {
	Reason string
	Kind   string // empty when the tag is absent
	Until  string // YYYY-MM-DD as written, empty when the tag is absent
}

// Task is the structured reading of one entry. A field that is absent is its
// zero value.
type Task struct {
	ID       string
	Title    string
	State    State
	Kind     string
	Repo     string
	Body     string
	Links    []item.Link
	Deps     []Dep
	Hold     *Hold
	Priority *int
	Created  string
	Closed   string
}

// Entry is one run of lines of a section: either an entry with an identifier,
// whose Task is set, or free-form lines, whose Task is nil.
type Entry struct {
	Task *Task
	// Lines are the exact source lines, a task's bullet and its continuation
	// and blank lines, or the free-form lines, each with any carriage return.
	Lines []string
	// Dirty makes Render write the task canonically instead of Lines.
	Dirty bool
}

// Section is a `## ` header and what follows it up to the next one.
type Section struct {
	// HeaderLine is the exact header line, such as "## Done (10 most recent)".
	HeaderLine string
	// State is the state of a recognized section and empty for any other,
	// whose lines all pass through as free-form.
	State   State
	Entries []*Entry
}

// Backlog is a parsed legacy file.
type Backlog struct {
	// FinalNewline is whether the source ended with a line feed.
	FinalNewline bool
	// Preamble holds the lines before the first section header.
	Preamble []string
	Sections []*Section
}

// ---------------------------------------------------------------------------
// Patterns
// ---------------------------------------------------------------------------

// dot is JavaScript's `.`: any character but a line terminator.
const dot = `[^\n\r\x{2028}\x{2029}]`

const idChars = `[A-Za-z0-9][A-Za-z0-9._-]*`

var (
	inFlightRe = regexp.MustCompile(`^- \*\*(` + idChars + `)\*\* - (` + dot + `*)$`)
	queuedRe   = regexp.MustCompile(`^- \[ \] (` + idChars + `) - (` + dot + `*)$`)
	doneRe     = regexp.MustCompile(`^- \[x\] (` + idChars + `) - (` + dot + `*)$`)
	idRe       = regexp.MustCompile(`^` + idChars + `$`)
)

// ValidID reports whether id is an identifier the grammar accepts: letters and
// digits first, then letters, digits and ".", "_" or "-", with no space. It is
// the one identifier form of the legacy file; minting one is not taken.
func ValidID(id string) bool {
	return idRe.MatchString(id)
}

// isWs reports whether r is white space as the legacy grammar counts it.
func isWs(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func trim(s string) string { return strings.TrimFunc(s, isWs) }

// semanticLine is a line without the one carriage return that ends it.
func semanticLine(line string) string {
	return strings.TrimSuffix(line, "\r")
}

type bullet struct {
	id, rest string
	state    State
}

func matchOne(line string, re *regexp.Regexp, state State) *bullet {
	m := re.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	return &bullet{id: m[1], rest: m[2], state: state}
}

// matchTaskBullet reads a task bullet of a section in the given state. An
// in-flight entry is written either as `- **id** - ` or, as the other tools
// that write this file do, as the same checkbox a queued entry has; the
// section header tells the states apart.
func matchTaskBullet(line string, state State) *bullet {
	switch state {
	case StateDone:
		return matchOne(line, doneRe, state)
	case StateQueued:
		return matchOne(line, queuedRe, state)
	}
	if b := matchOne(line, inFlightRe, state); b != nil {
		return b
	}
	return matchOne(line, queuedRe, state)
}

// ---------------------------------------------------------------------------
// Inline tags
// ---------------------------------------------------------------------------

// Canonical tags are recognized only in the trailing tag region of a line, so a
// parenthetical in the middle of a sentence stays in the prose and is never
// duplicated or moved when the entry is written again.
const (
	date      = `\d{4}-\d{2}-\d{2}`
	depMarker = `(?:blocked-by|parent|discovered-from)`
)

var (
	tailRepoRe     = regexp.MustCompile(item.Ws + `*\((?:[^()]*\+` + item.Ws + `*)?repo:` + item.Ws + `*([^)]+)\)` + item.Ws + `*$`)
	tailKindRe     = regexp.MustCompile(item.Ws + `*\(kind:` + item.Ws + `*([^)]+)\)` + item.Ws + `*$`)
	tailPriorityRe = regexp.MustCompile(item.Ws + `*\(priority:` + item.Ws + `*([0-4])\)` + item.Ws + `*$`)
	tailSinceRe    = regexp.MustCompile(item.Ws + `*\(since` + item.Ws + `+(` + date + `)\)` + item.Ws + `*$`)
	tailClosedRe   = regexp.MustCompile(item.Ws + `*\((?:merged|reported|done|closed)` + item.Ws + `+(` + date + `)\)` + item.Ws + `*$`)
	tailHoldRe     = regexp.MustCompile(item.Ws + `*\(hold:` + item.Ws + `*([^()]+)\)` + item.Ws + `*$`)
	tailHoldKindRe = regexp.MustCompile(item.Ws + `*\(hold-kind:` + item.Ws + `*(` + strings.Join(HoldKinds, "|") + `)\)` + item.Ws + `*$`)
	tailHoldUntil  = regexp.MustCompile(item.Ws + `*\(hold-until:` + item.Ws + `*(` + date + `)\)` + item.Ws + `*$`)

	depStartRe      = regexp.MustCompile(`^(` + depMarker + `):` + item.Ws + `*(` + idChars + `)`)
	depMarkerNextRe = regexp.MustCompile(`^` + item.Ws + `+` + depMarker + `:` + item.Ws)
)

// leadingKinds are the prose words that imply a kind, in the order tried.
var leadingKinds = []struct{ word, kind string }{
	{"PERSISTENT SECONDMATE", "secondmate"},
	{"SHIP", "ship"},
	{"SCOUT", "scout"},
	{"DOCS-ONLY", "docs"},
}

func isWordChar(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// LeadingKind is the kind a title's leading word implies, or "" when it has
// none. The word must end at a word boundary: SHIPPING is not SHIP.
func LeadingKind(title string) string {
	for _, lk := range leadingKinds {
		if !strings.HasPrefix(title, lk.word) {
			continue
		}
		if len(title) == len(lk.word) || !isWordChar(title[len(lk.word)]) {
			return lk.kind
		}
	}
	return ""
}

// Tags is what ExtractTags takes off the end of an entry's prose.
type Tags struct {
	Title    string
	Kind     string
	Repo     string
	Deps     []Dep
	Created  string
	Closed   string
	Priority *int
	Hold     *Hold
	Links    []item.Link
}

// matchTailDep finds the leftmost dependency edge that runs to the end of s, as
//
//	\s*(marker):\s*(id)(?:\s+-\s+(reason))?\s*$
//
// with the reason any text up to the end that holds no later `marker: `. It
// returns where the match starts, including the white space before it, and its
// parts. The reason is a free-text tail, so an edge that has one is the last
// thing on the line and an earlier one ends where the next begins.
func matchTailDep(s string) (start int, d Dep, ok bool) {
	for p := 0; p < len(s); p++ {
		// Candidates are tried leftmost first, and the pattern is not anchored
		// at its start, so a marker inside a word counts.
		loc := depStartRe.FindStringSubmatchIndex(s[p:])
		if loc == nil {
			continue
		}
		after := p + loc[1] // end of the id
		typ, id := s[p+loc[2]:p+loc[3]], s[p+loc[4]:p+loc[5]]
		if reason, matched := tailReason(s, after); matched {
			d = Dep{Type: typ, ID: id, Reason: reason}
		} else if allWs(s[after:]) {
			d = Dep{Type: typ, ID: id}
		} else {
			continue
		}
		start = p
		for start > 0 {
			r, size := lastRune(s[:start])
			if !isWs(r) {
				break
			}
			start -= size
		}
		return start, d, true
	}
	return 0, Dep{}, false
}

// tailReason reads `\s+-\s+(reason)\s*$` at s[at:], where the reason is at
// least one character, none of them a line terminator and none the start of
// white space followed by a later dependency marker, as short as the match to
// the end allows.
func tailReason(s string, at int) (string, bool) {
	i := at
	for i < len(s) {
		r, size := firstRune(s[i:])
		if !isWs(r) {
			break
		}
		i += size
	}
	if i == at || i >= len(s) || s[i] != '-' {
		return "", false
	}
	dash := i + 1
	wsEnd := dash
	for wsEnd < len(s) {
		r, size := firstRune(s[wsEnd:])
		if !isWs(r) {
			break
		}
		wsEnd += size
	}
	if wsEnd == dash {
		return "", false
	}
	// The white space after the dash is taken greedily and given back one
	// character at a time while the reason fails.
	for k := wsEnd; k > dash; {
		if reason, ok := reasonFrom(s, k); ok {
			return reason, true
		}
		_, size := lastRune(s[dash:k])
		k -= size
	}
	return "", false
}

// reasonFrom takes the shortest reason that starts at from and leaves only
// white space after it.
func reasonFrom(s string, from int) (string, bool) {
	end := len(s)
	for end > from {
		r, size := lastRune(s[from:end])
		if !isWs(r) {
			break
		}
		end -= size
	}
	if end == from {
		// Nothing but white space is left: the reason is its first character.
		if from >= len(s) {
			return "", false
		}
		_, size := firstRune(s[from:])
		end = from + size
	}
	for i := from; i < end; {
		r, size := firstRune(s[i:])
		if r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029 {
			return "", false
		}
		if depMarkerNextRe.MatchString(s[i:]) {
			return "", false
		}
		i += size
	}
	return s[from:end], true
}

func allWs(s string) bool {
	for _, r := range s {
		if !isWs(r) {
			return false
		}
	}
	return true
}

func firstRune(s string) (rune, int) { return utf8.DecodeRuneInString(s) }

func lastRune(s string) (rune, int) { return utf8.DecodeLastRuneInString(s) }

func stripTail(re *regexp.Regexp, title string) (cut string, group string, ok bool) {
	loc := re.FindStringSubmatchIndex(title)
	if loc == nil {
		return title, "", false
	}
	return title[:loc[0]], title[loc[2]:loc[3]], true
}

// ExtractTags takes the canonical inline tags off the trailing tag region of an
// entry's prose and returns the clean title with the fields they carried. Links
// and a leading kind word stay in the prose, so they are not repeated when the
// entry is written again, and a parenthetical in the middle is kept.
func ExtractTags(rest string) Tags {
	var (
		deps                            []Dep
		repo, kindTag, created, closed  string
		haveRepo, haveKind              bool
		haveCreated, haveClosed         bool
		priority                        *int
		holdReason, holdKind, holdUntil string
		haveHoldReason, haveHoldKind    bool
		haveHoldUntil                   bool
	)
	title := rest
	for stripping := true; stripping; {
		stripping = false
		if start, d, ok := matchTailDep(title); ok {
			deps = append([]Dep{d}, deps...)
			title = title[:start]
			stripping = true
			continue
		}
		if cut, g, ok := stripTail(tailRepoRe, title); ok {
			if !haveRepo {
				repo, haveRepo = trim(g), true
			}
			title, stripping = cut, true
			continue
		}
		if cut, g, ok := stripTail(tailKindRe, title); ok {
			if !haveKind {
				kindTag, haveKind = trim(g), true
			}
			title, stripping = cut, true
			continue
		}
		if cut, g, ok := stripTail(tailPriorityRe, title); ok {
			if priority == nil {
				n, _ := strconv.Atoi(g)
				priority = &n
			}
			title, stripping = cut, true
			continue
		}
		if cut, g, ok := stripTail(tailSinceRe, title); ok {
			if !haveCreated {
				created, haveCreated = g, true
			}
			title, stripping = cut, true
			continue
		}
		if cut, g, ok := stripTail(tailClosedRe, title); ok {
			if !haveClosed {
				closed, haveClosed = g, true
			}
			title, stripping = cut, true
			continue
		}
		if cut, g, ok := stripTail(tailHoldUntil, title); ok {
			if !haveHoldUntil {
				holdUntil, haveHoldUntil = g, true
			}
			title, stripping = cut, true
			continue
		}
		if cut, g, ok := stripTail(tailHoldKindRe, title); ok {
			if !haveHoldKind {
				holdKind, haveHoldKind = g, true
			}
			title, stripping = cut, true
			continue
		}
		if cut, g, ok := stripTail(tailHoldRe, title); ok {
			if !haveHoldReason {
				holdReason, haveHoldReason = trim(g), true
			}
			title, stripping = cut, true
			continue
		}
	}
	title = trim(title)
	kind := kindTag
	if !haveKind {
		kind = LeadingKind(title)
	}
	var hold *Hold
	if haveHoldReason {
		hold = &Hold{Reason: holdReason, Kind: holdKind, Until: holdUntil}
	}
	return Tags{
		Title: title, Kind: kind, Repo: repo, Deps: deps,
		Created: created, Closed: closed, Priority: priority, Hold: hold,
		Links: item.DeriveLinks(title),
	}
}

// ---------------------------------------------------------------------------
// Canonical rendering of one task
// ---------------------------------------------------------------------------

func closureVerb(t *Task) string {
	for _, l := range t.Links {
		if l.Kind == item.LinkPR {
			return "merged"
		}
	}
	for _, l := range t.Links {
		if l.Kind == item.LinkReport {
			return "reported"
		}
	}
	return "done"
}

// BuildProse is the canonical single line of prose: the clean title, then the
// canonical tags in a fixed order. An edge with a reason runs as free text to
// the end of the line, so it is written after the parenthetical tags, which is
// how the legacy file has it and what lets a re-parse strip the parentheticals
// first and keep the reason from swallowing a trailing tag.
func BuildProse(t *Task) string {
	parts := []string{trim(t.Title)}
	for _, d := range t.Deps {
		if d.Reason == "" {
			parts = append(parts, d.Type+": "+d.ID)
		}
	}
	if t.Repo != "" {
		parts = append(parts, "(repo: "+t.Repo+")")
	}
	if LeadingKind(t.Title) != t.Kind {
		parts = append(parts, "(kind: "+t.Kind+")")
	}
	if t.Priority != nil {
		parts = append(parts, "(priority: "+strconv.Itoa(*t.Priority)+")")
	}
	if t.State != StateDone && t.Created != "" {
		parts = append(parts, "(since "+t.Created+")")
	}
	if t.State == StateDone && t.Closed != "" {
		parts = append(parts, "("+closureVerb(t)+" "+t.Closed+")")
	}
	if t.Hold != nil {
		parts = append(parts, "(hold: "+t.Hold.Reason+")")
		if t.Hold.Kind != "" {
			parts = append(parts, "(hold-kind: "+t.Hold.Kind+")")
		}
		if t.Hold.Until != "" {
			parts = append(parts, "(hold-until: "+t.Hold.Until+")")
		}
	}
	for _, d := range t.Deps {
		if d.Reason != "" {
			parts = append(parts, d.Type+": "+d.ID+" - "+d.Reason)
		}
	}
	kept := parts[:0:0]
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " ")
}

// bulletPrefix is the canonical start of a task line. In flight and queued both
// write the checkbox; the section header carries the state. A `- **id**` line
// is still read, and is written back as a checkbox, never the other way.
func bulletPrefix(state State, id string) string {
	if state == StateDone {
		return "- [x] " + id + " - "
	}
	return "- [ ] " + id + " - "
}

// RenderTaskLines is a task's canonical source lines: the bullet, then the body
// as lines indented by two spaces. A blank body line stays blank.
func RenderTaskLines(t *Task) []string {
	lines := []string{bulletPrefix(t.State, t.ID) + BuildProse(t)}
	if t.Body != "" {
		for _, l := range strings.Split(t.Body, "\n") {
			if l == "" {
				lines = append(lines, "")
			} else {
				lines = append(lines, "  "+l)
			}
		}
	}
	return lines
}

// ---------------------------------------------------------------------------
// Parse
// ---------------------------------------------------------------------------

var (
	headerRe  = regexp.MustCompile(`^##` + item.Ws + `+`)
	sectionRe = regexp.MustCompile(`^##` + item.Ws + `+(` + dot + `*?)` + item.Ws + `*$`)
)

// lowerJS lowercases as JavaScript's toLowerCase does for the words compared
// here; the capital dotted I is the one character where Go differs.
func lowerJS(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "İ", "i̇"))
}

func sectionState(headerLine string) State {
	m := sectionRe.FindStringSubmatch(semanticLine(headerLine))
	if m == nil {
		return ""
	}
	text := lowerJS(m[1])
	switch {
	case text == "in flight":
		return StateInFlight
	case text == "queued":
		return StateQueued
	case strings.HasPrefix(text, "done"):
		return StateDone
	}
	return ""
}

// structuredBody is the body without the blank lines that end its block; those
// separate it from the next entry and stay in the entry's Lines.
func structuredBody(lines []string) string {
	end := len(lines)
	for end > 0 && lines[end-1] == "" {
		end--
	}
	return strings.Join(lines[:end], "\n")
}

func buildTask(b *bullet, bodyLines []string) *Task {
	tags := ExtractTags(b.rest)
	return &Task{
		ID: b.id, Title: tags.Title, State: b.state, Kind: tags.Kind, Repo: tags.Repo,
		Body: structuredBody(bodyLines), Links: tags.Links, Deps: tags.Deps,
		Hold: tags.Hold, Priority: tags.Priority, Created: tags.Created, Closed: tags.Closed,
	}
}

// parseEntries splits the lines of a section into task entries and runs of
// free-form lines. match reads a line as a task bullet.
func parseEntries(lines []string, match func(string) *bullet) []*Entry {
	var entries []*Entry
	var rawRun []string
	flush := func() {
		if len(rawRun) > 0 {
			entries = append(entries, &Entry{Lines: rawRun})
			rawRun = nil
		}
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		b := match(semanticLine(line))
		if b == nil {
			rawRun = append(rawRun, line)
			continue
		}
		flush()
		raw := []string{line}
		var body []string
		// The block is the bullet and every following indented or blank line up
		// to the next bullet or free-form line at column 0. Membership is by
		// position, so an indented `  ## Intent` is body and never a section
		// boundary, and blank lines between paragraphs, and any blank lines
		// before the next entry, belong to the block.
		for i+1 < len(lines) {
			next := semanticLine(lines[i+1])
			if trim(next) == "" {
				i++
				raw = append(raw, lines[i])
				body = append(body, "")
				continue
			}
			if strings.HasPrefix(next, "  ") {
				i++
				raw = append(raw, lines[i])
				body = append(body, next[2:])
				continue
			}
			break
		}
		entries = append(entries, &Entry{Task: buildTask(b, body), Lines: raw})
	}
	flush()
	return entries
}

func sectionMatcher(state State) func(string) *bullet {
	return func(line string) *bullet {
		if state == "" {
			return nil
		}
		return matchTaskBullet(line, state)
	}
}

// Parse reads a legacy backlog. Every byte sequence is a backlog, so the error
// is nil today: a line the grammar does not understand is kept as free-form,
// never refused.
func Parse(src []byte) (*Backlog, error) {
	return parse(src, func(s *Section) func(string) *bullet { return sectionMatcher(s.State) }), nil
}

// parse splits src into lines and sections. matcherOf says how the entries of
// a section are read.
func parse(src []byte, matcherOf func(*Section) func(string) *bullet) *Backlog {
	if len(src) == 0 {
		return &Backlog{}
	}
	s := string(src)
	b := &Backlog{FinalNewline: strings.HasSuffix(s, "\n")}
	if b.FinalNewline {
		s = s[:len(s)-1]
	}
	var current *Section
	var buffer []string
	closeSection := func() {
		if current != nil {
			current.Entries = parseEntries(buffer, matcherOf(current))
			b.Sections = append(b.Sections, current)
		}
		buffer = nil
	}
	for _, line := range strings.Split(s, "\n") {
		if headerRe.MatchString(semanticLine(line)) {
			closeSection()
			current = &Section{HeaderLine: line, State: sectionState(line)}
			continue
		}
		if current != nil {
			buffer = append(buffer, line)
		} else {
			b.Preamble = append(b.Preamble, line)
		}
	}
	closeSection()
	return b
}

// ---------------------------------------------------------------------------
// Render
// ---------------------------------------------------------------------------

func (e *Entry) renderLines() []string {
	if e.Task != nil && e.Dirty {
		return RenderTaskLines(e.Task)
	}
	return e.Lines
}

// Render writes the backlog. An entry that is not dirty and every free-form
// line come out exactly as Parse read them.
func (b *Backlog) Render() []byte {
	if len(b.Preamble) == 0 && len(b.Sections) == 0 {
		return []byte{}
	}
	lines := append([]string(nil), b.Preamble...)
	for _, s := range b.Sections {
		lines = append(lines, s.HeaderLine)
		for _, e := range s.Entries {
			lines = append(lines, e.renderLines()...)
		}
	}
	out := strings.Join(lines, "\n")
	if b.FinalNewline {
		out += "\n"
	}
	return []byte(out)
}

// Tasks are the backlog's tasks in file order.
func (b *Backlog) Tasks() []*Task {
	var out []*Task
	for _, s := range b.Sections {
		for _, e := range s.Entries {
			if e.Task != nil {
				out = append(out, e.Task)
			}
		}
	}
	return out
}

// Find is the task with the identifier id, or nil.
func (b *Backlog) Find(id string) *Task {
	for _, t := range b.Tasks() {
		if t.ID == id {
			return t
		}
	}
	return nil
}
