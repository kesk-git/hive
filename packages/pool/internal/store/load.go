package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	cstore "github.com/kesk-git/hive/contract/store"
)

// Loaded is a record read from the store, whole, as it was written.
type Loaded struct {
	ID       string
	Data     map[string]any
	Revision int64
}

// The reasons a record is omitted from a read, as the response schema names
// them.
const (
	ReasonMalformed   = "record_malformed"
	ReasonUnsupported = "unsupported_version"
	ReasonLimit       = "limit"
)

// Omission names a record that could not be read, and why. A reader returns it
// in place of the record, never a guess.
type Omission struct{ ID, Reason string }

// ErrNotFound reports a record that is absent.
var ErrNotFound = errors.New("pool store: no such record")

// Load reads the record of the kind and identity, without a lock: a record is
// replaced atomically. An absent record is ErrNotFound. A record that cannot
// be read, breaks its schema or is stored under another identity is returned
// as an Omission and not as an error, and so is one of a newer version; the
// error is for what is not about the record, such as a failing disk.
func Load(st *cstore.Store, kind, id string) (*Loaded, *Omission, error) {
	k, ok := Lookup(kind)
	if !ok {
		return nil, nil, fmt.Errorf("pool store: %q is not a kind of record", kind)
	}
	path, err := k.Path(id)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	return load(st, k, path, strings.TrimSuffix(path[strings.LastIndex(path, "/")+1:], ".json"))
}

func load(st *cstore.Store, k Kind, path, id string) (*Loaded, *Omission, error) {
	rec, err := st.ReadRecord(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil, ErrNotFound
	case errors.Is(err, cstore.ErrRecordMalformed):
		return nil, &Omission{id, ReasonMalformed}, nil
	case errors.Is(err, cstore.ErrUnsupportedVersion):
		return nil, &Omission{id, ReasonUnsupported}, nil
	case err != nil:
		return nil, nil, err
	}
	if err := k.Check(rec.Data, id, false); err != nil {
		if errors.Is(err, cstore.ErrUnsupportedVersion) {
			return nil, &Omission{id, ReasonUnsupported}, nil
		}
		return nil, &Omission{id, ReasonMalformed}, nil
	}
	v, err := canon.Parse(rec.Data)
	if err != nil {
		return nil, &Omission{id, ReasonMalformed}, nil
	}
	return &Loaded{ID: id, Data: v.(map[string]any), Revision: rec.Revision}, nil, nil
}

// LoadAll reads every record of the kind, in the order of their identities,
// with the records that could not be read named beside them. A file in the
// kind's directory that is not named for a record is no record and is left
// alone.
func LoadAll(st *cstore.Store, kind string) ([]Loaded, []Omission, error) {
	k, ok := Lookup(kind)
	if !ok {
		return nil, nil, fmt.Errorf("pool store: %q is not a kind of record", kind)
	}
	dir, err := st.Path(Package + "/records/" + k.Name)
	if err != nil {
		return nil, nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			names = append(names, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	slices.Sort(names)
	var out []Loaded
	var omitted []Omission
	for _, id := range names {
		path, err := k.Path(id)
		if err != nil {
			omitted = append(omitted, Omission{id, ReasonMalformed})
			continue
		}
		l, om, err := load(st, k, path, id)
		switch {
		case errors.Is(err, ErrNotFound):
			continue // removed between the listing and the read
		case err != nil:
			return nil, nil, err
		case om != nil:
			omitted = append(omitted, *om)
		default:
			out = append(out, *l)
		}
	}
	return out, omitted, nil
}
