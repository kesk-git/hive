package envelope

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/schema"
)

// ContractVersion is the latest contract version whose demands on built code
// this build meets, which every responder and every capability declaration
// reports. A contract change that asks nothing of built code does not move
// it; when a rebase renumbers such a change, this constant is renumbered with
// it.
const ContractVersion = "1.12"

// SchemaVersion is the schema_version of the four envelopes. The envelope
// schemas are unchanged since the first version of the specification.
const SchemaVersion = "1.0"

// CapabilitiesSchemaVersion is the schema_version a capability declaration is
// written at. 1.1 adds the operation's optional platform member; a
// declaration at 1.0 is still read.
const CapabilitiesSchemaVersion = "1.1"

// The schema names and identifiers of the four documents.
const (
	RequestSchema      = "hive.request"
	RequestSchemaID    = "urn:hive:schema:request:v1"
	ResponseSchema     = "hive.response"
	ResponseSchemaID   = "urn:hive:schema:response:v1"
	ReceiptSchema      = "hive.receipt"
	ReceiptSchemaID    = "urn:hive:schema:receipt:v1"
	CapabilitiesSchema = "hive.capabilities"
	CapabilitiesID     = "urn:hive:schema:capabilities:v1"
)

var versionForm = regexp.MustCompile(`^([1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// ErrVersionForm reports a version that is not written MAJOR.MINOR.
var ErrVersionForm = errors.New("envelope: a version is written MAJOR.MINOR")

// CompareVersions orders two versions written MAJOR.MINOR: negative when a
// is older than b, zero when they are the same, positive when a is newer.
func CompareVersions(a, b string) (int, error) {
	ma, mb := versionForm.FindStringSubmatch(a), versionForm.FindStringSubmatch(b)
	if ma == nil || mb == nil {
		return 0, ErrVersionForm
	}
	if c := cmpNumbers(ma[1], mb[1]); c != 0 {
		return c, nil
	}
	return cmpNumbers(ma[2], mb[2]), nil
}

// cmpNumbers compares two decimal numbers written without leading zeros.
func cmpNumbers(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

// Newer reports whether version is newer, in its major or its minor part,
// than known. A version that is not of the form MAJOR.MINOR is not newer; it
// is left to the schema to refuse.
func Newer(version, known string) bool {
	c, err := CompareVersions(version, known)
	return err == nil && c > 0
}

// registry returns the embedded schemas.
func registry() (*schema.Registry, error) {
	r, err := schema.Embedded()
	if err != nil {
		return nil, fmt.Errorf("envelope: the embedded schemas do not load: %w", err)
	}
	return r, nil
}

// violation is the Failure of a document or body that breaks a schema: one
// sentence naming where the first violation is and what is wrong there, never
// a member's name or a value of the document. A document the command could not
// read as a request keeps the whole list of violations as its detail, which
// the diagnostic event records (spec/events.md, Diagnostic events); a body of a
// request that was read is a validation refusal naming the field, whose message
// already says everything, and keeps none.
func violation(what, ref string, ve *schema.ValidationError) *Failure {
	v := ve.Violations[0]
	at := "the " + what
	if v.Instance != "" {
		at = v.Instance
	}
	f := Common(CodeSchemaViolation, fmt.Sprintf("the %s breaks %s at %s (%s)", what, ref, at, v.Message))
	if what == "body" {
		return f
	}
	return f.Because(ve)
}

// decodeDocument reads data as the document of the named schema: a Hive
// record (malformed_request otherwise), of a version this build reads
// (unsupported_version otherwise) and valid under the schema
// (schema_violation otherwise). It returns the decoded tree, which is always
// an object.
func decodeDocument(data []byte, name, id, known string) (map[string]any, error) {
	v, err := schema.Decode(data)
	if err != nil {
		return nil, Common(CodeMalformedRequest, "the document is not a Hive record").Because(err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, Common(CodeSchemaViolation, "the document is not a JSON object")
	}
	if obj["schema"] == name {
		if version, _ := obj["schema_version"].(string); Newer(version, known) {
			return nil, Common(CodeUnsupportedVersion, "the document is of a version this build does not read").Because(fmt.Errorf("%s is at version %s; this build reads up to %s", name, version, known))
		}
	}
	reg, err := registry()
	if err != nil {
		return nil, Common(CodeInternal, "the embedded schemas do not load").Because(err)
	}
	if err := reg.Validate(id, data); err != nil {
		var ve *schema.ValidationError
		if errors.As(err, &ve) {
			return nil, violation("document", id, ve)
		}
		return nil, Common(CodeInternal, "the document cannot be checked against "+id).Because(err)
	}
	return obj, nil
}

// encodeDocument writes tree in canonical form with one final line feed,
// after checking it against the schema id: a document that would break its
// schema is never written.
func encodeDocument(tree map[string]any, id string) ([]byte, error) {
	out, err := canon.Encode(tree)
	if err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}
	if err := validateBytes(out, id); err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// validateBytes checks a canonical document against the schema id.
func validateBytes(data []byte, id string) error {
	reg, err := registry()
	if err != nil {
		return err
	}
	if err := reg.Validate(id, data); err != nil {
		return fmt.Errorf("envelope: the document breaks %s: %w", id, err)
	}
	return nil
}

// validateTree checks a tree against the schema id.
func validateTree(tree any, id string) error {
	out, err := canon.Encode(tree)
	if err != nil {
		return fmt.Errorf("envelope: %w", err)
	}
	return validateBytes(out, id)
}

// ValidateBody is the owner's validation of an operation's body, a decoded
// open member, against the schema its capability declaration names. The no-value
// check comes first: a body holding a string of a shape on the fixed list of
// the schema package, in a value or in a member name, is a *Failure of status
// invalid with code secret_shape_recognized, whose message names the nearest
// declared field and the shape and never the text. A body that breaks the
// schema is a *Failure of status invalid with code schema_violation; a schema
// the build does not hold is an internal failure.
func ValidateBody(body map[string]any, ref string) error {
	reg, err := registry()
	if err != nil {
		return Common(CodeInternal, "the embedded schemas do not load").Because(err)
	}
	out, err := canon.Encode(body)
	if err != nil {
		return Common(CodeInternal, "the body cannot be written").Because(err)
	}
	if err := reg.ValidateOwned(ref, out); err != nil {
		var ve *schema.ValidationError
		var se *schema.ShapeError
		switch {
		case errors.As(err, &se):
			return secretShape("body", se)
		case errors.As(err, &ve):
			return violation("body", ref, ve)
		}
		return Common(CodeInternal, "the body cannot be checked against "+ref).Because(err)
	}
	return nil
}

// secretShape is the Failure of a document or body that holds a string of a
// listed credential shape: one sentence naming the field and the shape, never
// the text.
func secretShape(what string, se *schema.ShapeError) *Failure {
	return Common(CodeSecretShapeRecognized, fmt.Sprintf("the %s holds a string of the credential shape %s at %s", what, se.Shape, se.WhereIn(what)))
}

// Validate checks a document against the schema ref names, decoding it as a
// Hive record first. It is the one validator every envelope is read through.
func Validate(ref string, data []byte) error {
	reg, err := registry()
	if err != nil {
		return err
	}
	return reg.Validate(ref, data)
}

// Digest is the digest of a document's canonical form, which is the digest
// the replay of spec/commands.md compares.
func Digest(data []byte) (string, error) {
	d, err := canon.RecordDigest(data)
	if err != nil {
		return "", fmt.Errorf("envelope: %w", err)
	}
	return d, nil
}
