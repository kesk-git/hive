package store

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/schema"
	cstore "github.com/kesk-git/hive/contract/store"
)

// Package is the package whose store this is.
const Package = "pool"

// Kind is one kind of record the Pool keeps, as the table of spec/pool.md,
// The store, gives it: the directory it lives in, the schema its records
// carry and the field that holds its identity.
type Kind struct {
	// Name is the kind as a read names it, and the directory under
	// pool/records/ that holds it.
	Name string
	// Schema is the name every record of the kind carries in its schema member.
	Schema string
	// Version is the newest version of the schema this build reads. Its major
	// selects the schema file; a newer version is refused, never read.
	Version string
	// IDField is the member that holds the record's identity, which is the
	// name of its file.
	IDField string
	// Fixed is the one identity of a kind of which the home keeps one record.
	Fixed string
}

// kinds are the kinds the Pool stores, in the order of spec/pool.md.
var kinds = []Kind{
	{Name: "item", Schema: "pool.item", Version: "1.0", IDField: "task_id"},
	{Name: "hold", Schema: "pool.hold", Version: "1.0", IDField: "hold_id"},
	{Name: "admission", Schema: "pool.admission", Version: "1.0", IDField: "admission_id"},
	{Name: "imperative", Schema: "pool.imperative", Version: "1.1", IDField: "task_id"},
	{Name: "specification", Schema: "pool.specification", Version: "1.2", IDField: "specification_id"},
	{Name: "rule", Schema: "pool.rule", Version: "2.0", IDField: "rule_id"},
	{Name: "projection", Schema: "pool.projection", Version: "2.0", IDField: "projection_id"},
	{Name: "subject", Schema: "pool.subject", Version: "1.0", IDField: "subject_id"},
	{Name: "budget", Schema: "pool.budget", Version: "1.0", IDField: "budget_id", Fixed: "memory"},
	{Name: "note", Schema: "pool.note", Version: "1.0", IDField: "note_id"},
	{Name: "maintenance-queue", Schema: "pool.maintenance-queue", Version: "1.0", IDField: "queue_id", Fixed: "maintenance"},
	{Name: "miss", Schema: "pool.miss", Version: "1.0", IDField: "miss_id"},
	{Name: "migration", Schema: "pool.migration", Version: "1.0", IDField: "migration_id"},
	{Name: "question", Schema: "pool.question", Version: "1.0", IDField: "question_id"},
	{Name: "decision-round", Schema: "pool.decision-round", Version: "1.0", IDField: "decision_round_id"},
}

// Kinds returns the names of the kinds the Pool stores.
func Kinds() []string {
	names := make([]string, len(kinds))
	for i, k := range kinds {
		names[i] = k.Name
	}
	return names
}

// Lookup finds a kind by its name.
func Lookup(name string) (Kind, bool) {
	for _, k := range kinds {
		if k.Name == name {
			return k, true
		}
	}
	return Kind{}, false
}

// Dispatch finds the kind a record belongs to by the schema it names.
func Dispatch(schemaName string) (Kind, bool) {
	for _, k := range kinds {
		if k.Schema == schemaName {
			return k, true
		}
	}
	return Kind{}, false
}

// Ref is the schema file that holds the records of the kind: its major
// version selects it.
func (k Kind) Ref() string {
	major, _, _ := strings.Cut(k.Version, ".")
	return "urn:hive:schema:records:" + k.Schema + ":v" + major
}

// idForm is every identity the Pool writes into a file name: lowercase, one
// segment, and nothing a person could have typed into a path.
var idForm = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ID returns the identity a record of the kind is written under. A kind of
// which the home keeps one has only its fixed identity.
func (k Kind) ID(id string) (string, error) {
	if k.Fixed != "" {
		if id != "" && id != k.Fixed {
			return "", fmt.Errorf("the one %s record of a home is %s, not %q", k.Name, k.Fixed, id)
		}
		return k.Fixed, nil
	}
	if !idForm.MatchString(id) {
		return "", fmt.Errorf("%q is not an identity of a %s record", id, k.Name)
	}
	return id, nil
}

// Path is the path of the record of the kind, relative to the home.
func (k Kind) Path(id string) (string, error) {
	id, err := k.ID(id)
	if err != nil {
		return "", err
	}
	return cstore.RecordPath(Package, k.Name, id), nil
}

// Schemas is the registry of the embedded schemas.
func Schemas() (*schema.Registry, error) { return schema.Embedded() }

// Check holds a whole record of the kind to its schema and to the identity it
// is stored under. A record of a newer version than the build reads is
// cstore.ErrUnsupportedVersion; one of another schema, one that cannot be
// parsed or one stored under another identity is cstore.ErrRecordMalformed; a
// record that breaks the schema is the *schema.ValidationError. owned applies
// the owner's no-value check, as a write does: a *schema.ShapeError, which
// names the field and the shape and never the text. A read does not apply it,
// since it reads a record as it was written.
func (k Kind) Check(data []byte, id string, owned bool) error {
	v, err := canon.Parse(data)
	if err != nil {
		return fmt.Errorf("%w: %v", cstore.ErrRecordMalformed, err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: a record is a JSON object", cstore.ErrRecordMalformed)
	}
	if obj["schema"] != k.Schema {
		return fmt.Errorf("%w: a %s record names the schema %v", cstore.ErrRecordMalformed, k.Name, obj["schema"])
	}
	version, _ := obj["schema_version"].(string)
	if version != "" && envelope.Newer(version, k.Version) {
		return fmt.Errorf("%w: %s is at version %s, this build reads up to %s", cstore.ErrUnsupportedVersion, k.Schema, version, k.Version)
	}
	reg, err := Schemas()
	if err != nil {
		return err
	}
	if owned {
		err = reg.ValidateOwned(k.Ref(), data)
	} else {
		err = reg.Validate(k.Ref(), data)
	}
	if err != nil {
		return err
	}
	want, err := k.ID(id)
	if err != nil {
		return fmt.Errorf("%w: %v", cstore.ErrRecordMalformed, err)
	}
	if obj[k.IDField] != want {
		return fmt.Errorf("%w: the %s of the record is not %s, the identity it is stored under", cstore.ErrRecordMalformed, k.IDField, want)
	}
	return nil
}

// violation reports whether err is a record breaking its schema, as against a
// record that cannot be read at all.
func violation(err error) bool {
	var v *schema.ValidationError
	return errors.As(err, &v)
}
