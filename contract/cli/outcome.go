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
// file requests/<request_id>.json of the store, holding the digest of the
// request's canonical form and the response as it was returned. No schema
// file under spec/schemas names it yet; the record carries the name and
// version a later file would fix, and this build reads version 1.0.
const (
	outcomeSchema  = "hive.request-outcome"
	outcomeVersion = "1.0"
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
	v, err := canon.Parse(data)
	if err != nil {
		return nil, malformed(err.Error())
	}
	obj, ok := v.(map[string]any)
	if !ok || obj["schema"] != outcomeSchema {
		return nil, malformed("it is not a recorded outcome")
	}
	version, _ := obj["schema_version"].(string)
	if envelope.Newer(version, outcomeVersion) {
		return nil, envelope.Common(envelope.CodeUnsupportedVersion, fmt.Sprintf("the recorded outcome %s is at version %s; this build reads up to %s", rel, version, outcomeVersion))
	}
	if obj["request_id"] != requestID {
		return nil, malformed("it records another request")
	}
	digest, ok := obj["request_digest"].(string)
	if !ok || !canon.ValidDigest(digest) {
		return nil, malformed("it holds no request digest")
	}
	tree, ok := obj["response"].(map[string]any)
	if !ok {
		return nil, malformed("it holds no response")
	}
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
