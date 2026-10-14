package intake_test

import (
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/word/internal/entry"
)

// The reasons combine, each once, in the order of the contract's list, and an
// answer that awaits reconciliation is a reason only until a later answer is
// accepted or deferred.
func TestVerifyCombinesReasonsInOrder(t *testing.T) {
	t.Run("a deferred question of another workspace", func(t *testing.T) {
		w := newWorld(t)
		p := w.pair("deferred")
		p.q["scope_level"], p.q["workspace_id"] = "workspace", mint(t, ids.Workspace)
		w.put(p)
		resp, exit := w.verify(authoritySubject("word", wordRef(p.qid(), p.q["record_revision"].(int64))), w.r.supervisor())
		w.expect(resp, exit, false, []string{"deferred", "out-of-scope"}, &records{questions: []map[string]any{p.q}})
		got := resp.Body["reasons"].([]any)
		if len(got) != 2 || got[0] != "deferred" || got[1] != "out-of-scope" {
			t.Errorf("the reasons are not in the order of the contract's list: %v", got)
		}
	})
	t.Run("an unreconciled answer settled by a later one", func(t *testing.T) {
		w := newWorld(t)
		p := w.pair("deferred")
		first := p.q["answers"].([]any)[0].(map[string]any)
		early := map[string]any{}
		for k, v := range first {
			early[k] = v
		}
		early["disposition"], early["sequence"] = "unreconciled", int64(1)
		delete(early, "later")
		early["option_id"], early["option_label"] = "keep-vendor", "Keep the vendor"
		first["sequence"] = int64(2)
		p.q["answers"] = []any{early, first}
		p.q["deferral"].(map[string]any)["answer_sequence"] = int64(2)
		w.put(p)
		resp, exit := w.verify(questionSubject(p.qid()), w.r.supervisor())
		w.expect(resp, exit, false, []string{"deferred"}, &records{questions: []map[string]any{p.q}})
	})
}

// The Pool answering a read incompletely is no verdict, and a callee that
// cannot read gives none.
func TestVerifyGivesNoVerdictWithoutACompleteRead(t *testing.T) {
	t.Run("an incomplete read", func(t *testing.T) {
		w := newWorld(t)
		p := w.pair("released")
		w.put(p)
		w.r.exe = entry.ExecutableWith(struct {
			cli.Callee
			incomplete
		}{w.r.pool, incomplete{w.p}})
		resp, exit := w.r.send("verify", verifyRequest(t, w, questionSubject(p.qid())))
		if exit != envelope.Unavailable.ExitCode() || resp.Error == nil || resp.Error.Code != envelope.CodeDependencyUnusable || resp.Body != nil {
			t.Errorf("exit %d, %s, %+v", exit, resp.Status, resp.Error)
		}
	})
	t.Run("a callee that cannot read", func(t *testing.T) {
		w := newWorld(t)
		w.r.exe = entry.ExecutableWith(w.r.pool)
		resp, exit := w.r.send("verify", verifyRequest(t, w, questionSubject(mint(t, ids.Question))))
		if exit != envelope.Unavailable.ExitCode() || resp.Error == nil || resp.Error.Code != envelope.CodeDependencyUnusable || resp.Body != nil {
			t.Errorf("exit %d, %s, %+v", exit, resp.Status, resp.Error)
		}
	})
}

// incomplete is the fake Pool answering every read as one a limit cut.
type incomplete struct{ pool *readingPool }

func (i incomplete) Read(c *cli.Call, request []byte) ([]byte, int, error) {
	output, exit, err := i.pool.Read(c, request)
	if err != nil {
		return nil, 0, err
	}
	resp, err := envelope.DecodeResponse(output)
	if err != nil {
		return nil, 0, err
	}
	resp.Body["projection"].(map[string]any)["complete"] = false
	out, err := resp.Encode()
	return out, exit, err
}

// verifyRequest is a request to verify the subject, as the test brings it.
func verifyRequest(t *testing.T, w *world, subject map[string]any) []byte {
	t.Helper()
	req, err := canon.Encode(map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": "hive-word", "operation": "verify", "operation_version": "1.0",
		"request_id": mint(t, ids.Request), "correlation_id": mint(t, ids.Correlation),
		"issued_at": "2026-10-12T11:00:00.123Z", "caller": w.r.supervisor(),
		"scope_level": "workspace", "home_id": w.r.homeID, "workspace_id": w.r.workspaceID,
		"authority": map[string]any{"kind": "none"}, "body": map[string]any{"subject": subject},
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(req, '\n')
}
