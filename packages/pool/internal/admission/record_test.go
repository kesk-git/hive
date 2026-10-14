package admission

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/envelope"
)

func fixtureRecord(t testing.TB, name string) *Record {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contract", "testdata", "schemas", "records_pool.admission", "valid", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return &Record{Data: v.(map[string]any), Revision: 1}
}

// The view of a record gives the task, the scope with the clone, and whether an
// approval is recorded.
func TestTheRecordViewReadsTheRecord(t *testing.T) {
	created := fixtureRecord(t, "created-no-checks")
	if created.TaskID() != "task_01jc7d8e9f0g1h2j3k4m5n6p7f" || created.Approved() {
		t.Errorf("task %q, approved %v", created.TaskID(), created.Approved())
	}
	want := envelope.Scope{
		Level: envelope.ScopeProject, HomeID: "home_01jc7d8e9f0g1h2j3k4m5n6p7a", WorkspaceID: "ws_01jc7d8e9f0g1h2j3k4m5n6p7b",
		ProjectID: "proj_01jc7d8e9f0g1h2j3k4m5n6p7c", CloneID: "clone_01jc7d8e9f0g1h2j3k4m5n6p7d",
	}
	if got := created.Scope(); got != want {
		t.Errorf("scope %+v, want %+v", got, want)
	}
	if hive := fixtureRecord(t, "refused-codex-cloud-hive").Scope(); hive.Level != envelope.ScopeHive || hive.WorkspaceID != "" || hive.ProjectID != "" {
		t.Errorf("scope %+v", hive)
	}
	if !fixtureRecord(t, "held-unpushed-then-approved-admitted").Approved() {
		t.Errorf("an approved record is not approved")
	}
}
