package read

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/store"
)

// The reader of the kind request-outcome (spec/pool.md, Reading the Pool;
// spec/commands.md, Replay): the Pool's own replay record of one earlier
// mutating request, selected by its request_id, with the digest it is keyed on
// and the response as it was returned.
//
// The read takes the store's lock shared, which completes an interrupted
// transaction before the reader looks, so an outcome whose intent is durable
// is never read as absent. A request that is not recorded is not_found. A
// recorded outcome that cannot be read as the replay record is
// unavailable with the code record_malformed, and a newer version than this
// build reads is unsupported_version, as the harness reads the same record.

const (
	outcomeSchema   = "hive.request-outcome"
	outcomeSchemaID = "urn:hive:schema:request-outcome:v1"
	outcomeVersion  = "1.0"
)

// OutcomeMembers are the selector members of a read of kind request-outcome:
// the request_id, as the identity, and nothing else.
var OutcomeMembers = []string{MemberID}

// RegisterOutcomeReader registers the reader of the kind request-outcome. It
// is never Standalone: the lock shared is what recovers the store.
func RegisterOutcomeReader(r *Registry) {
	r.Register(Kind{Name: RequestOutcome, Members: OutcomeMembers, Read: readOutcome})
}

func readOutcome(c *Context) (*Result, error) {
	id := c.Selector.ID
	// What is not the identity of a request is the identity of no outcome.
	if ids.Check(id, ids.Request) != nil {
		return &Result{}, nil
	}
	rel := cstore.OutcomePath(store.Package, id)
	data, err := c.Store.Read(rel)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &Result{}, nil
	case errors.Is(err, cstore.ErrRecordMalformed):
		return nil, malformedOutcome(rel, err)
	case err != nil:
		return nil, err
	}
	obj, err := parseOutcome(rel, data, id)
	if err != nil {
		return nil, err
	}
	return &Result{Records: []Record{{ID: id, Data: obj}}}, nil
}

func malformedOutcome(where string, why error) error {
	return envelope.Common(envelope.CodeRecordMalformed, "the recorded outcome of this request is malformed").Because(fmt.Errorf("%s: %w", where, why))
}

// parseOutcome holds the bytes of a recorded outcome to the replay record's
// schema, to being the outcome of the request it is stored under and to
// holding a response that answers it.
func parseOutcome(where string, data []byte, requestID string) (map[string]any, error) {
	v, perr := canon.Parse(data)
	obj, _ := v.(map[string]any)
	// A version this build does not read is not malformed; it is newer.
	if perr == nil && obj["schema"] == outcomeSchema {
		if version, _ := obj["schema_version"].(string); envelope.Newer(version, outcomeVersion) {
			return nil, envelope.Common(envelope.CodeUnsupportedVersion, "the recorded outcome of this request is of a version this build does not read").Because(fmt.Errorf("%s is at version %s; this build reads up to %s", where, version, outcomeVersion))
		}
	}
	if perr != nil {
		return nil, malformedOutcome(where, perr)
	}
	if err := envelope.Validate(outcomeSchemaID, data); err != nil {
		return nil, malformedOutcome(where, err)
	}
	if obj["request_id"] != requestID {
		return nil, malformedOutcome(where, errors.New("it records another request"))
	}
	tree, _ := obj["response"].(map[string]any)
	encoded, err := canon.Encode(tree)
	if err != nil {
		return nil, malformedOutcome(where, err)
	}
	resp, err := envelope.DecodeResponse(append(encoded, '\n'))
	if err != nil {
		return nil, malformedOutcome(where, fmt.Errorf("its response is not a response: %w", err))
	}
	if resp.RequestID != requestID {
		return nil, malformedOutcome(where, errors.New("its response answers another request"))
	}
	return obj, nil
}
