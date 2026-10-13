package entry

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// hive-fleet provision is the one operation built: its capabilities say so,
// and the executable started under the name hive-fleet routes the request to
// the registry, which creates the home record in the home it is pointed at.
func TestProvisionIsSupportedAndRouted(t *testing.T) {
	code, out := invoke(t, "hive-fleet", nil, "capabilities")
	resp, err := envelope.AcceptCapabilities(out, code)
	if err != nil {
		t.Fatal(err)
	}
	caps, err := envelope.DecodeCapabilities(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, op := range caps.Operations {
		if op.Name != "provision" {
			if op.State == envelope.Supported {
				t.Errorf("%s is supported", op.Name)
			}
			continue
		}
		found = true
		if op.State != envelope.Supported || !op.Mutates || op.Mode != envelope.Unary || !slices.Equal(op.Versions, []string{"1.0"}) {
			t.Errorf("provision is %s, mutates %v, mode %s, versions %v", op.State, op.Mutates, op.Mode, op.Versions)
		}
		if op.RequestSchema != "urn:hive:schema:ops:hive-fleet.provision.request:v1" || op.ResponseSchema != "urn:hive:schema:ops:hive-fleet.provision.response:v1" {
			t.Errorf("schemas %s %s", op.RequestSchema, op.ResponseSchema)
		}
		if !slices.Equal(op.Executes, []string{"git"}) || len(op.Calls) != 0 {
			t.Errorf("executes %v, calls %v", op.Executes, op.Calls)
		}
		kinds := slices.Sorted(slices.Values(op.ReceiptKinds))
		if want := []string{"dispatch.clone.registered", "dispatch.home.created", "dispatch.project.registered", "dispatch.workspace.registered"}; !slices.Equal(kinds, want) {
			t.Errorf("receipt kinds %v", kinds)
		}
	}
	if !found {
		t.Fatal("hive-fleet does not declare provision")
	}

	// The operation as started: a request in on standard input, the home
	// named by --home, the response out on standard output.
	mint := func(p ids.Prefix) string {
		id, err := ids.Mint(clock.System{}, p)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	issued, err := clock.Stamp(clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	requestID, homeID := mint(ids.Request), mint(ids.Home)
	req, err := canon.Encode(map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": "hive-fleet", "operation": "provision", "operation_version": "1.0",
		"request_id": requestID, "correlation_id": mint(ids.Correlation),
		"issued_at": issued, "caller": map[string]any{"kind": "test"},
		"scope_level": "hive", "home_id": homeID, "authority": map[string]any{"kind": "none"},
		"body": map[string]any{"home": map[string]any{"role": "hive", "name": "Hive"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	code, out = invoke(t, "hive-fleet", append(req, '\n'), "provision", "--home", home)
	resp, err = envelope.AcceptResponse(out, code, requestID)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != envelope.Ok || code != 0 || len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "dispatch.home.created" {
		t.Fatalf("status %s, exit %d, %d receipts: %+v", resp.Status, code, len(resp.Receipts), resp.Error)
	}
	if _, err := os.Stat(filepath.Join(home, "home.json")); err != nil {
		t.Error(err)
	}
}
