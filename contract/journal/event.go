package journal

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/schema"
	"github.com/kesk-git/hive/contract/store"
)

// The journal's own repair event (spec/events.md, The journal): the one kind
// that belongs to no package. The contract library writes it, and a package
// never may.
const (
	// RepairKind is the kind of the event that records a torn tail's removal.
	RepairKind = "hive.journal.tail-repaired"
	// RepairSourceID is the source_id of a repair event.
	RepairSourceID = "journal"
	// RecordSchema is the schema of the journal's own record, which a repair
	// event's basis names with the segment's file name as id.
	RecordSchema = "hive.journal"

	// DiagnosticFact is the subject and fact of the one kind common to every
	// package, <package>.diagnostic.recorded, whose body schema is shared.
	DiagnosticFact = "diagnostic.recorded"

	repairKindVersion = "1.0"
	eventSchemaPrefix = "urn:hive:schema:events:"
)

var versionForm = regexp.MustCompile(`^([1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// decodeLine reads one line of the journal, without its line feed, from the
// segment named segment: it is canonical, it is an envelope of a version this
// build reads, it satisfies the envelope schema and the schema of its kind,
// and its members agree with one another and with the segment. A line that
// fails is store.ErrRecordMalformed, and one of a newer version is
// store.ErrUnsupportedVersion.
func (j *Journal) decodeLine(line []byte, segment string) (map[string]any, error) {
	v, err := canon.Parse(line)
	if err != nil {
		return nil, fmt.Errorf("%w: journal: %v", store.ErrRecordMalformed, err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: journal: a line is a JSON object", store.ErrRecordMalformed)
	}
	out, err := canon.Encode(obj)
	if err != nil || string(out) != string(line) {
		return nil, fmt.Errorf("%w: journal: a line is not in canonical form", store.ErrRecordMalformed)
	}
	if err := j.checkEvent(obj, line, segment); err != nil {
		if errors.Is(err, store.ErrUnsupportedVersion) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: journal: %v", store.ErrRecordMalformed, err)
	}
	return obj, nil
}

// checkEvent checks a whole event, obj with its canonical form data and the
// segment named segment that holds or will hold it: the envelope's version and
// schema, that source.source_id begins with source.package and that
// source.package is the first segment of kind, except for the repair event
// whose source_id is "journal", that the body satisfies the schema its kind
// registers or is empty when it registers none, that a repair event is of the
// version registered and based on the journal's record of the segment it is
// in, and that its three times are timestamps with an unknown occurred_at
// repeating observed_at.
func (j *Journal) checkEvent(obj map[string]any, data []byte, segment string) error {
	if obj["schema"] != eventRecord {
		return fmt.Errorf("schema is not %s", eventRecord)
	}
	version, _ := obj["schema_version"].(string)
	if newer(version, eventVersion) {
		return fmt.Errorf("%w: %s is at version %s, this build reads up to %s", store.ErrUnsupportedVersion, eventRecord, version, eventVersion)
	}
	if err := j.reg.Validate(eventSchema, data); err != nil {
		return err
	}
	kind, _ := obj["kind"].(string)
	source, _ := obj["source"].(map[string]any)
	pkg, _ := source["package"].(string)
	sourceID, _ := source["source_id"].(string)
	if kind == RepairKind {
		if sourceID != RepairSourceID {
			return fmt.Errorf("the source_id of %s is %q", RepairKind, RepairSourceID)
		}
		if err := checkRepairEvent(obj, segment); err != nil {
			return err
		}
	} else {
		if owner, _, _ := strings.Cut(kind, "."); owner != pkg {
			return fmt.Errorf("kind %s is not of source.package %s", kind, pkg)
		}
		if owner, _, _ := strings.Cut(sourceID, ":"); owner != pkg {
			return fmt.Errorf("source_id %s is not a source of source.package %s", sourceID, pkg)
		}
	}
	if err := j.checkBody(obj); err != nil {
		return err
	}
	for _, name := range []string{"occurred_at", "observed_at", "recorded_at"} {
		if s, _ := obj[name].(string); !clock.Valid(s) {
			return fmt.Errorf("%s is not a timestamp", name)
		}
	}
	if known, ok := obj["occurred_at_known"].(bool); ok && !known && obj["occurred_at"] != obj["observed_at"] {
		return errors.New("occurred_at is unknown, so it must repeat observed_at")
	}
	return nil
}

// checkRepairEvent checks what makes a repair event the journal's own: the one
// version of its kind this build knows, and a basis that is the journal's
// record of the segment the line is in, which its body names too.
func checkRepairEvent(obj map[string]any, segment string) error {
	if kindVersion, _ := obj["kind_version"].(string); kindVersion != repairKindVersion {
		if newer(kindVersion, repairKindVersion) {
			return fmt.Errorf("%w: %s is at version %s, this build reads %s", store.ErrUnsupportedVersion, RepairKind, kindVersion, repairKindVersion)
		}
		return fmt.Errorf("%s is at version %s, not %s", RepairKind, kindVersion, repairKindVersion)
	}
	body, _ := obj["body"].(map[string]any)
	if body["segment"] != segment {
		return fmt.Errorf("the repair names segment %v and is in %q", body["segment"], segment)
	}
	basis, _ := obj["basis"].(map[string]any)
	ref, _ := basis["ref"].(map[string]any)
	if basis["kind"] != "record" || ref["schema"] != RecordSchema || ref["id"] != segment {
		return fmt.Errorf("the basis of a repair is the %s record of %s", RecordSchema, segment)
	}
	return nil
}

// bodySchema is the schema that registers the body of a kind at a major
// version: spec/schemas/events/<kind>.v<major>.schema.json, except that the
// diagnostic kind of any of the seven packages is registered once, as
// diagnostic.recorded.
func (j *Journal) bodySchema(kind, major string) string {
	if owner, fact, ok := strings.Cut(kind, "."); ok && fact == DiagnosticFact {
		if name, err := canon.Encode(owner); err == nil && j.reg.Validate(packageName, name) == nil {
			kind = fact
		}
	}
	return eventSchemaPrefix + kind + ":v" + major
}

// checkBody validates the body against the schema registered for the kind
// at its major version, or requires it to be empty when the kind registers
// none.
func (j *Journal) checkBody(obj map[string]any) error {
	kind, _ := obj["kind"].(string)
	kindVersion, _ := obj["kind_version"].(string)
	major, _, _ := strings.Cut(kindVersion, ".")
	body, _ := obj["body"].(map[string]any)
	data, err := canon.Encode(body)
	if err != nil {
		return fmt.Errorf("body: %w", err)
	}
	err = j.reg.Validate(j.bodySchema(kind, major), data)
	switch {
	case errors.Is(err, schema.ErrUnknownSchema):
		if len(body) > 0 {
			return fmt.Errorf("kind %s registers no body schema, so its body is empty", kind)
		}
	case err != nil:
		return fmt.Errorf("body of %s: %w", kind, err)
	}
	return nil
}

// newer reports whether version, of the form MAJOR.MINOR, is newer than
// known in its major or its minor part. Text of another form is not newer;
// the schema refuses it.
func newer(version, known string) bool {
	got, want := versionForm.FindStringSubmatch(version), versionForm.FindStringSubmatch(known)
	if got == nil || want == nil {
		return false
	}
	if c := cmpNumbers(got[1], want[1]); c != 0 {
		return c > 0
	}
	return cmpNumbers(got[2], want[2]) > 0
}

// cmpNumbers compares two decimal numbers written without leading zeros.
func cmpNumbers(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}
