package intake

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
)

// The Pool's answer to the read of kind request-outcome: one recorded outcome
// is found and handed to the harness whole, none recorded is not found, the
// Pool's unknown and unavailable are returned as it gave them, and any other
// answer leaves the outcome unknown.
func TestTheReadOfARecordedOutcome(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contract", "testdata", "schemas", "ops_hive-pool.read.response", "valid", "request-outcome.json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	body := v.(map[string]any)
	record, err := canon.Encode(body["records"].([]any)[0])
	if err != nil {
		t.Fatal(err)
	}
	failure := func(f *envelope.Failure) *envelope.Response {
		return &envelope.Response{Status: f.Status, Error: &f.Err}
	}
	other := map[string]any{"projection": body["projection"], "kind": "question", "records": []any{}}

	tests := []struct {
		name  string
		resp  *envelope.Response
		found bool
		code  string
	}{
		{"recorded", &envelope.Response{Status: envelope.Ok, Body: body}, true, ""},
		{"none recorded", failure(envelope.Common(envelope.CodeNotFound, "none")), false, ""},
		{"the Pool unknown", failure(envelope.Common(envelope.CodeOutcomeUnknown, "unknown")), false, envelope.CodeOutcomeUnknown},
		{"the Pool unavailable", failure(envelope.Fail(envelope.Unavailable, envelope.CodeDependencyMissing, "gone")), false, envelope.CodeDependencyMissing},
		{"the home not found", failure(envelope.Common(envelope.CodeHomeNotFound, "none")), false, envelope.CodeOutcomeUnknown},
		{"refused", failure(envelope.Common(envelope.CodeAuthorityInsufficient, "no")), false, envelope.CodeOutcomeUnknown},
		{"ok with no record", &envelope.Response{Status: envelope.Ok, Body: map[string]any{"projection": body["projection"], "kind": "request-outcome", "records": []any{}}}, false, envelope.CodeOutcomeUnknown},
		{"ok of another kind", &envelope.Response{Status: envelope.Ok, Body: other}, false, envelope.CodeOutcomeUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, found, err := outcomeOf(tc.resp)
			if found != tc.found {
				t.Fatalf("found %v, error %v", found, err)
			}
			if f := envelope.AsFailure(err); tc.code == "" && err != nil || tc.code != "" && (f == nil || f.Err.Code != tc.code) {
				t.Fatalf("error %v, want code %q", err, tc.code)
			}
			if found && string(got) != string(record) {
				t.Errorf("the record is %s, want %s", got, record)
			}
		})
	}
}
