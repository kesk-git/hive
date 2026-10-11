// Package store is Hive's durable record layer: atomic replacement of one
// record file, intent and roll-forward for several records, transaction
// locks and the check of the home's layout (spec/records.md).
//
// A Store is one package's part of one private home, <home>/<package>/. Its
// files are written only through a Writer, which holds the store's
// transaction lock for the length of one command. Taking that lock first
// completes any transaction a crash left behind and removes the temporary
// files a crash left, so every writer starts from a consistent store. A
// single record is read without a lock; a reader that needs several records
// to agree takes the lock shared.
//
// The store sets record_revision and updated_at on every record it writes,
// writes every file in canonical form with one final line feed, and sets no
// size limit of its own: a limit is given where a record crosses a boundary,
// and a record over it fails whole. It validates no record against its
// schema; the owner of a record does that with the schema package. The one
// exception is the home record, which the layout check reads through the
// embedded schema because the home's identity is what every command checks
// first.
//
// The events a transaction names are appended through a Journal the caller
// supplies: the home journal of spec/events.md belongs to its own package,
// and the store opens nothing under journal/ itself.
package store
