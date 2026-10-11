// Package journal is the home's append-only event journal (spec/events.md,
// The journal): a sequence of segment files under <home>/journal/, each line
// one event in canonical form, with one order per home.
//
// A Journal is opened on a home by the package whose command uses it. Append
// takes locks/journal.lock through the store package's lock code, learns the
// next home_sequence from the last intact line, sets home_sequence and
// recorded_at on each event, writes each line with its line feed in one
// write, flushes it and releases the lock. It implements store.Journal, so a
// store opened with it appends the events of a transaction through it, and
// it skips an event whose event_id is already in the journal, so recovery may
// hand it the same events again.
//
// A segment is named events-<first home_sequence>.jsonl, the number written
// as twenty digits. A segment is closed when the next line would take it
// past the configured size, and a new one begins with that line; a line
// larger than the size stands alone in its segment. Closed segments are
// never written again. The journal removes nothing: retention is the home's
// to configure, and a reader whose cursor lies before the segments kept is
// told so with ErrGone.
//
// A final line without its line feed is an append that was never
// acknowledged. A reader ignores it. The next Append, holding the lock,
// removes it and appends one hive.journal.tail-repaired event ahead of its
// own, so the repair is itself in the journal. The repair line is written
// over the torn bytes before the file is cut to its new length, so an
// interruption between the two leaves at worst another torn tail and never a
// removal that no event records. The repair event's source.package is the
// package that opened the journal, its source_id is "journal", its basis is
// the journal's own record with schema hive.journal and the segment's file
// name as id, and its correlation_id is that of the first event of the
// append that found the tail.
//
// Reading takes no lock. A Reader resumes after a cursor, the last
// home_sequence its consumer handled, validates every line through the
// embedded schemas, requires the sequence to rise by exactly one across
// segments, and reports a line it cannot read as store.ErrRecordMalformed
// and a line of a newer envelope version as store.ErrUnsupportedVersion;
// neither is ever repaired by guessing.
package journal
