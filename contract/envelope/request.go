package envelope

import (
	"fmt"

	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/schema"
)

// Request is the one document a caller sends to a command (spec/commands.md,
// The request). Its schema and schema_version are fixed: hive.request at
// ContractVersion. An empty string or a zero number is an absent field; nil
// ExpectedRevisions is absent, an empty non-nil list is present and empty.
type Request struct {
	Command          string
	Operation        string
	OperationVersion string
	RequestID        string
	CorrelationID    string
	Causation        *RecordRef
	IssuedAt         string
	Caller           Caller
	Scope
	SourceRevision    string
	ExpectedRevisions []ExpectedRevision
	Authority         Authority
	// Body is the operation's own request, kept as decoded: it is validated
	// against the schema the operation's capability declaration names.
	Body map[string]any
}

// DecodeRequest reads a request: a Hive record, of a version this build
// reads, valid under request.v1. It fails with a *Failure of status invalid:
// malformed_request for bytes that are not a record, unsupported_version for
// a newer envelope, schema_violation otherwise.
func DecodeRequest(data []byte) (*Request, error) {
	obj, err := decodeDocument(data, RequestSchema, RequestSchemaID, ContractVersion)
	if err != nil {
		return nil, err
	}
	r, err := readRequest(fields{obj, "request"})
	if err != nil {
		return nil, Common(CodeSchemaViolation, err.Error())
	}
	return r, nil
}

func readRequest(f fields) (*Request, error) {
	r := &Request{}
	var err error
	if r.Command, err = f.str("command"); err != nil {
		return nil, err
	}
	if r.Operation, err = f.str("operation"); err != nil {
		return nil, err
	}
	if r.OperationVersion, err = f.str("operation_version"); err != nil {
		return nil, err
	}
	if r.RequestID, err = f.str("request_id"); err != nil {
		return nil, err
	}
	if r.CorrelationID, err = f.str("correlation_id"); err != nil {
		return nil, err
	}
	if r.Causation, err = readOptRecordRef(f, "causation"); err != nil {
		return nil, err
	}
	if r.IssuedAt, err = f.str("issued_at"); err != nil {
		return nil, err
	}
	caller, err := f.object("caller")
	if err != nil {
		return nil, err
	}
	if r.Caller, err = readCaller(caller); err != nil {
		return nil, err
	}
	if r.Scope, err = readScope(f); err != nil {
		return nil, err
	}
	if r.SourceRevision, err = f.optStr("source_revision"); err != nil {
		return nil, err
	}
	if f.has("expected_revisions") {
		list, err := f.objects("expected_revisions")
		if err != nil {
			return nil, err
		}
		r.ExpectedRevisions = make([]ExpectedRevision, 0, len(list))
		for _, e := range list {
			var x ExpectedRevision
			if x.Schema, err = e.str("schema"); err != nil {
				return nil, err
			}
			if x.ID, err = e.str("id"); err != nil {
				return nil, err
			}
			if x.RecordRevision, err = e.whole("record_revision"); err != nil {
				return nil, err
			}
			r.ExpectedRevisions = append(r.ExpectedRevisions, x)
		}
	}
	authority, err := f.object("authority")
	if err != nil {
		return nil, err
	}
	if r.Authority, err = readAuthority(authority); err != nil {
		return nil, err
	}
	body, err := f.object("body")
	if err != nil {
		return nil, err
	}
	r.Body = body.m
	return r, nil
}

// Tree is the request as a JSON tree, with schema and schema_version set.
// The body is copied.
func (r *Request) Tree() map[string]any {
	m := map[string]any{
		"schema":            RequestSchema,
		"schema_version":    ContractVersion,
		"command":           r.Command,
		"operation":         r.Operation,
		"operation_version": r.OperationVersion,
		"request_id":        r.RequestID,
		"correlation_id":    r.CorrelationID,
		"issued_at":         r.IssuedAt,
		"caller":            r.Caller.tree(),
		"authority":         r.Authority.tree(),
	}
	if r.Causation != nil {
		m["causation"] = r.Causation.tree()
	}
	r.Scope.into(m)
	put(m, "source_revision", r.SourceRevision)
	if r.ExpectedRevisions != nil {
		list := make([]any, 0, len(r.ExpectedRevisions))
		for _, e := range r.ExpectedRevisions {
			list = append(list, map[string]any{"schema": e.Schema, "id": e.ID, "record_revision": e.RecordRevision})
		}
		m["expected_revisions"] = list
	}
	body := cloneObject(r.Body)
	if body == nil {
		body = map[string]any{}
	}
	m["body"] = body
	return m
}

// Encode writes the request in canonical form with one final line feed,
// after checking it against request.v1.
func (r *Request) Encode() ([]byte, error) {
	return encodeDocument(r.Tree(), RequestSchemaID)
}

// Digest is the digest of the request's canonical form, the one a mutating
// command records with the outcome and compares on replay.
func (r *Request) Digest() (string, error) {
	data, err := r.Encode()
	if err != nil {
		return "", err
	}
	return Digest(data)
}

// LooseRequestID reads the request_id of a document that may not be a valid
// request, so that a response to it can echo the identity where one can be
// read: the bytes decode as a record, are an object and hold a request_id
// of the right form. It fails otherwise, and the response's request_id is
// then null.
func LooseRequestID(data []byte) (string, error) {
	v, err := schema.Decode(data)
	if err != nil {
		return "", err
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return "", fmt.Errorf("envelope: the document is not an object")
	}
	id, ok := obj["request_id"].(string)
	if !ok || ids.Check(id, ids.Request) != nil {
		return "", fmt.Errorf("envelope: the document holds no request_id of the right form")
	}
	return id, nil
}

// Addressed checks that the request names the command and operation it was
// sent to. A difference is invalid with code envelope_mismatch.
func (r *Request) Addressed(command, operation string) error {
	if r.Command != command || r.Operation != operation {
		return Common(CodeEnvelopeMismatch, fmt.Sprintf("the request names %s %s; it was sent to %s %s", r.Command, r.Operation, command, operation))
	}
	return nil
}
