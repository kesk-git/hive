// Package store is the Pool's transaction boundary: every write to pool/ is
// staged here, checked against its schema and against the revisions it expects
// before the contract library's store writes a byte.
package store

import (
	"errors"

	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	cstore "github.com/kesk-git/hive/contract/store"
)

// Write is one record of a transaction: the record of the kind and identity
// given, whole, as the revision after Expected.
type Write struct {
	Kind     string
	ID       string
	Body     map[string]any
	Expected int64
}

// Transaction is everything one request writes to pool/.
type Transaction struct {
	Records  []Write
	Receipts []envelope.Receipt
	Events   []cstore.Event
}

// Staged is a transaction that passed every check, ready for the harness.
type Staged struct {
	Records  []cstore.RecordWrite
	Receipts []envelope.Receipt
	Events   []cstore.Event
}

var errNotBuilt = errors.New("pool store: not built")

// Stage checks the transaction and returns it as the harness commits it.
func Stage(c *cli.Call, tx Transaction) (*Staged, error) { return nil, errNotBuilt }

// Result is the handler's result carrying the staged transaction.
func (s *Staged) Result(body map[string]any) *cli.Result { return nil }

// Intent is the staged transaction as an intent to commit before an effect.
func (s *Staged) Intent() cli.Intent { return cli.Intent{} }

// Loaded is a record read from the store, whole.
type Loaded struct {
	ID       string
	Data     map[string]any
	Revision int64
}

// Omission names a record that could not be read, and why.
type Omission struct{ ID, Reason string }

// ErrNotFound reports a record that is absent.
var ErrNotFound = errors.New("pool store: no such record")

// Load reads the record of the kind and identity.
func Load(st *cstore.Store, kind, id string) (*Loaded, *Omission, error) {
	return nil, nil, errNotBuilt
}

// LoadAll reads every record of the kind.
func LoadAll(st *cstore.Store, kind string) ([]Loaded, []Omission, error) {
	return nil, nil, errNotBuilt
}
