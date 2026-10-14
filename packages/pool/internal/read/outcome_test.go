package read

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
)

const outcomeRequest = "req_01j9xk3m2nq4r5s6t7v8w9xyzf"

func outcomeFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contract", "testdata", "schemas", "request-outcome", "valid", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The replay record is read whole for the request it is stored under, for
// every kind of response the fixtures record; any other is malformed, and a
// version this build does not read is not malformed but newer.
func TestAnOutcomeIsParsedAsTheReplayRecord(t *testing.T) {
	for _, name := range []string{"ok.json", "held.json", "refused-with-receipt.json"} {
		obj, err := parseOutcome(name, outcomeFixture(t, name), outcomeRequest)
		if err != nil || obj["request_id"] != outcomeRequest || obj["request_digest"] == nil {
			t.Errorf("%s: %v, %v", name, obj, err)
		}
	}
	ok := string(outcomeFixture(t, "ok.json"))
	for name, tc := range map[string]struct {
		data, request string
		code          string
	}{
		"another request":            {ok, "req_01j9xk3m2nq4r5s6t7v8w9xyzg", envelope.CodeRecordMalformed},
		"no digest":                  {strings.Replace(ok, `"request_digest"`, `"digest"`, 1), outcomeRequest, envelope.CodeRecordMalformed},
		"an unnamed member":          {strings.Replace(ok, `"schema_version": "1.0",`, `"schema_version": "1.0", "extra": true,`, 1), outcomeRequest, envelope.CodeRecordMalformed},
		"another schema":             {strings.Replace(ok, `"hive.request-outcome"`, `"hive.response"`, 1), outcomeRequest, envelope.CodeRecordMalformed},
		"not a document":             {"not json", outcomeRequest, envelope.CodeRecordMalformed},
		"not an object":              {"[]", outcomeRequest, envelope.CodeRecordMalformed},
		"a response that is not one": {strings.Replace(ok, `"exit_code": 0`, `"exit_code": 3`, 1), outcomeRequest, envelope.CodeRecordMalformed},
		"a newer version":            {strings.Replace(ok, `"schema_version": "1.0",`, `"schema_version": "1.1",`, 1), outcomeRequest, envelope.CodeUnsupportedVersion},
	} {
		_, err := parseOutcome(name, []byte(tc.data), tc.request)
		if f := envelope.AsFailure(err); f == nil || f.Err.Code != tc.code {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// The reader selects on the request_id alone and is never read without the
// lock: the lock shared is what recovers the store.
func TestTheOutcomeReaderTakesTheIdentityAloneAndTheLock(t *testing.T) {
	r := NewRegistry()
	RegisterOutcomeReader(r)
	k, ok := r.Lookup(RequestOutcome)
	if !ok || !slicesEqual(k.Members, MemberID) || k.Standalone != nil {
		t.Errorf("the reader: %+v", k)
	}
}
