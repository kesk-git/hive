package cli

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/store"
)

// The recorded outcome of a mutating request (spec/commands.md, Replay): the
// file requests/<request_id>.json of the store, a record of the schema
// urn:hive:schema:request-outcome:v1 holding the digest of the request's
// canonical form and the response as it was returned. This build reads
// version 1.0.
const (
	outcomeSchema   = "hive.request-outcome"
	outcomeSchemaID = "urn:hive:schema:request-outcome:v1"
	outcomeVersion  = "1.0"
)

// recorded is a recorded outcome as read back.
type recorded struct {
	digest   string
	response *envelope.Response
}

// encodeOutcome writes the recorded outcome of a request as a JSON object.
func encodeOutcome(requestID, digest string, resp *envelope.Response) ([]byte, error) {
	tree := map[string]any{
		"schema":         outcomeSchema,
		"schema_version": outcomeVersion,
		"request_id":     requestID,
		"request_digest": digest,
		"response":       resp.Tree(),
	}
	out, err := canon.Encode(tree)
	if err != nil {
		return nil, fmt.Errorf("cli: the outcome cannot be written: %w", err)
	}
	if err := envelope.Validate(outcomeSchemaID, out); err != nil {
		return nil, fmt.Errorf("cli: the outcome cannot be written: %w", err)
	}
	return out, nil
}

// readOutcome reads the recorded outcome of the request, or nil when none
// is recorded. An outcome that cannot be read is record_malformed; one of a
// newer version is unsupported_version.
func readOutcome(st *store.Store, requestID string) (*recorded, error) {
	rel := store.OutcomePath(st.Package(), requestID)
	data, err := st.Read(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, envelope.Common(envelope.CodeRecordMalformed, fmt.Sprintf("the recorded outcome %s cannot be read: %v", rel, err))
	}
	malformed := func(what string) error {
		return envelope.Common(envelope.CodeRecordMalformed, fmt.Sprintf("the recorded outcome %s is malformed: %s", rel, what))
	}
	v, perr := canon.Parse(data)
	obj, _ := v.(map[string]any)
	// A version this build does not read is not malformed; it is newer.
	if perr == nil && obj["schema"] == outcomeSchema {
		if version, _ := obj["schema_version"].(string); envelope.Newer(version, outcomeVersion) {
			return nil, envelope.Common(envelope.CodeUnsupportedVersion, fmt.Sprintf("the recorded outcome %s is at version %s; this build reads up to %s", rel, version, outcomeVersion))
		}
	}
	if err := envelope.Validate(outcomeSchemaID, data); err != nil {
		return nil, malformed(err.Error())
	}
	if obj["request_id"] != requestID {
		return nil, malformed("it records another request")
	}
	digest := obj["request_digest"].(string)
	tree := obj["response"].(map[string]any)
	encoded, err := canon.Encode(tree)
	if err != nil {
		return nil, malformed(err.Error())
	}
	resp, err := envelope.DecodeResponse(append(encoded, '\n'))
	if err != nil {
		return nil, malformed("its response is not a response: " + err.Error())
	}
	if resp.RequestID != requestID {
		return nil, malformed("its response answers another request")
	}
	return &recorded{digest, resp}, nil
}
