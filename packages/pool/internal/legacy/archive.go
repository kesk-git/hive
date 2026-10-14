package legacy

import "strings"

// This file is translated from the archive half of tasks-axi's
// src/backends/markdown.ts (prune and the two archive files). The upstream
// trims a section by moving its surplus entries, with their exact lines, to an
// archive file beside the backlog, and never deletes one. Hive prunes nothing:
// its own records are never pruned, so this reads an archive and writes none.
// What a legacy backlog's archive files are for here is that a dependency
// edge, or an import, names an identifier the live file no longer holds, and
// the archive still resolves it.

// The names the upstream gives its two archive files, beside the backlog.
const (
	// DoneArchiveName holds the entries pruned out of a section.
	DoneArchiveName = "done-archive.md"
	// NoteArchiveName holds the earlier bodies a changed task replaced.
	NoteArchiveName = "note-archive.md"
)

const archivedPrefix = "Archived "

// Archive is a parsed archive file. It is appended to in blocks, each
// `## Archived <date>` and the exact lines that were archived, so it is read
// with the same grammar as a backlog and renders back byte for byte.
type Archive struct {
	Backlog *Backlog
}

// anyBullet reads a task bullet of any of the three forms. An archive keeps no
// section, so the bullet is all that says what the entry was: a ticked
// checkbox is done, a bold identifier was in flight, and an empty checkbox is
// read as queued, though an in-flight entry written that way reads the same.
func anyBullet(line string) *bullet {
	if b := matchOne(line, doneRe, StateDone); b != nil {
		return b
	}
	if b := matchOne(line, inFlightRe, StateInFlight); b != nil {
		return b
	}
	return matchOne(line, queuedRe, StateQueued)
}

// ParseArchive reads an archive file. Every byte sequence is an archive.
func ParseArchive(src []byte) (*Archive, error) {
	b := parse(src, func(*Section) func(string) *bullet { return anyBullet })
	for _, s := range b.Sections {
		// An archive block is not a backlog section.
		s.State = ""
	}
	return &Archive{Backlog: b}, nil
}

// Render writes the archive exactly as it was read.
func (a *Archive) Render() []byte { return a.Backlog.Render() }

// Stamp is the date a block was archived on, or "" for a header that is not an
// `## Archived <date>` line.
func (s *Section) Stamp() string {
	m := sectionRe.FindStringSubmatch(semanticLine(s.HeaderLine))
	if m == nil || !strings.HasPrefix(m[1], archivedPrefix) {
		return ""
	}
	return trim(strings.TrimPrefix(m[1], archivedPrefix))
}

// Archived is a task found in an archive file.
type Archived struct {
	Task  *Task
	Stamp string
	// Lines are the exact lines the upstream archived for it.
	Lines []string
}

// Find is the entry for id that was archived last, or false. An identifier
// that was archived more than once, because the task came back and was pruned
// again, resolves to its latest block.
func (a *Archive) Find(id string) (Archived, bool) {
	for i := len(a.Backlog.Sections) - 1; i >= 0; i-- {
		s := a.Backlog.Sections[i]
		for j := len(s.Entries) - 1; j >= 0; j-- {
			if e := s.Entries[j]; e.Task != nil && e.Task.ID == id {
				return Archived{Task: e.Task, Stamp: s.Stamp(), Lines: e.Lines}, true
			}
		}
	}
	return Archived{}, false
}

// Found is where a lookup resolved an identifier.
type Found struct {
	Task *Task
	// Archived is true when the live backlog no longer holds the task and an
	// archive did; Stamp is then the date of the block that held it.
	Archived bool
	Stamp    string
}

// Lookup resolves id in the live backlog, and failing that in the archives in
// the order given. A task the live file still holds wins over any archived
// copy of it.
func Lookup(id string, live *Backlog, archives ...*Archive) (Found, bool) {
	if live != nil {
		if t := live.Find(id); t != nil {
			return Found{Task: t}, true
		}
	}
	for _, a := range archives {
		if a == nil {
			continue
		}
		if f, ok := a.Find(id); ok {
			return Found{Task: f.Task, Archived: true, Stamp: f.Stamp}, true
		}
	}
	return Found{}, false
}
