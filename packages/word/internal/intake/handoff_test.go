package intake_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/word/internal/poolfake"
)

// Who may send each operation (spec/word.md, Operations): hold and revise a
// supervisor, the operator or a package other than ui; release, complete and
// close a supervisor or the operator; no other caller.
func TestTheCallerTableIsApplied(t *testing.T) {
	r := newRig(t)
	callers := map[string]map[string]any{"supervisor": r.supervisor(), "operator": r.operator(), "package": pkg("dispatch"), "test": {"kind": "test"}}
	accepted := map[string][]string{
		"hold": {"supervisor", "operator", "package"}, "revise": {"supervisor", "operator", "package"},
		"release": {"supervisor", "operator"}, "complete": {"supervisor", "operator"}, "close": {"supervisor", "operator"},
	}
	for _, operation := range operations {
		for name, caller := range callers {
			t.Run(operation+" "+name, func(t *testing.T) {
				r := newRig(t)
				req := r.request(operation, mint(t, ids.Request), caller, r.body(operation, mint(t, ids.Question)))
				resp, _ := r.send(operation, req)
				want := envelope.Ok
				var code string
				if !slices.Contains(accepted[operation], name) {
					want, code = envelope.Refused, "word.not_authorized"
				}
				if resp.Status != want || (resp.Error != nil && resp.Error.Code != code) {
					t.Fatalf("%s %+v", resp.Status, resp.Error)
				}
				if sent := r.pool.Count(); (want == envelope.Ok) != (sent == 1) {
					t.Errorf("%d handoffs were sent", sent)
				}
			})
		}
	}
}

// A request holding a string of a listed credential shape is refused whole
// with secret_shape_recognized before anything is sent or read.
func TestACredentialShapeIsRefusedBeforeAnythingIsSent(t *testing.T) {
	for _, operation := range operations {
		t.Run(operation, func(t *testing.T) {
			r := newRig(t)
			body := r.body(operation, mint(t, ids.Question))
			for _, key := range []string{"text", "note", "reason"} {
				if _, ok := body[key]; ok {
					body[key] = "token " + "ghp_" + strings.Repeat("a", 36)
				}
			}
			if operation == "hold" {
				body["text"] = "token " + "ghp_" + strings.Repeat("a", 36)
			}
			resp, exit := r.send(operation, r.request(operation, mint(t, ids.Request), r.supervisor(), body))
			if exit != 3 || resp.Status != envelope.Invalid || resp.Error == nil || resp.Error.Code != envelope.CodeSecretShapeRecognized {
				t.Fatalf("exit %d, %s, %+v", exit, resp.Status, resp.Error)
			}
			if strings.Contains(resp.Error.Message, "ghp_") {
				t.Errorf("the refusal repeats the text: %s", resp.Error.Message)
			}
			if r.pool.Count() != 0 || len(r.pool.Reads) != 0 {
				t.Errorf("%d handoffs and %d reads were made", r.pool.Count(), len(r.pool.Reads))
			}
		})
	}
}

// A refusal, a conflict and a hold of the Pool come back with the Pool's code
// and receipts, unchanged.
func TestThePoolsRefusalsAreReturnedAsTheyAre(t *testing.T) {
	cases := []struct {
		status envelope.Status
		code   string
		exit   int
	}{
		{envelope.Refused, "pool.question_closed", 4},
		{envelope.Conflict, envelope.CodeRevisionConflict, 5},
		{envelope.NotFound, envelope.CodeNotFound, 6},
	}
	for _, operation := range operations {
		for _, tc := range cases {
			t.Run(operation+" "+tc.code, func(t *testing.T) {
				r := newRig(t)
				r.pool.Refuse = func(req *envelope.Request) *envelope.Failure {
					f := envelope.Fail(tc.status, tc.code, "the Pool's reason")
					receipt, err := poolfake.Receipt(req, "pool.refusal.recorded")
					if err != nil {
						t.Fatal(err)
					}
					f.Receipts = []envelope.Receipt{receipt}
					return f
				}
				resp, exit := r.send(operation, r.request(operation, mint(t, ids.Request), r.supervisor(), r.body(operation, mint(t, ids.Question))))
				if exit != tc.exit || resp.Status != tc.status || resp.Error == nil || resp.Error.Code != tc.code {
					t.Fatalf("exit %d, %s, %+v", exit, resp.Status, resp.Error)
				}
				if len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "pool.refusal.recorded" || resp.Body != nil {
					t.Errorf("receipts %d, body %v", len(resp.Receipts), resp.Body)
				}
			})
		}
	}
}

// An outcome word cannot learn is unknown: a Pool that gives no answer to the
// read leaves nothing sent, and one that gives none to the handoff leaves the
// request unknown, to be sent again, which reads first.
func TestAnOutcomeThatCannotBeLearnedIsUnknown(t *testing.T) {
	for _, operation := range operations {
		t.Run(operation, func(t *testing.T) {
			r := newRig(t)
			req := r.request(operation, mint(t, ids.Request), r.supervisor(), r.body(operation, mint(t, ids.Question)))

			r.pool.ReadErr = errors.New("the pipe closed")
			resp, exit := r.send(operation, req)
			if exit != 9 || resp.Status != envelope.Unknown || resp.Error == nil || resp.Error.Code != envelope.CodeOutcomeUnknown || r.pool.Count() != 0 {
				t.Fatalf("read: exit %d, %s, %+v, %d sent", exit, resp.Status, resp.Error, r.pool.Count())
			}
			r.pool.ReadErr, r.pool.SendErr = nil, errors.New("the pipe closed")
			resp, exit = r.send(operation, req)
			if exit != 9 || resp.Status != envelope.Unknown || r.pool.Count() != 1 {
				t.Fatalf("send: exit %d, %s, %+v, %d sent", exit, resp.Status, resp.Error, r.pool.Count())
			}
			r.pool.SendErr = nil
			if resp, _ = r.send(operation, req); resp.Status != envelope.Ok || r.pool.Count() != 2 || len(r.pool.Reads) != 3 {
				t.Fatalf("retry: %s %+v, %d sent, %d reads", resp.Status, resp.Error, r.pool.Count(), len(r.pool.Reads))
			}
		})
	}
}

// Two identical requests that race past the read both receive the outcome of
// the one that reached the Pool first, with its identities.
func TestARacingRetryReceivesTheFirstOutcome(t *testing.T) {
	r := newRig(t)
	req := r.request("hold", mint(t, ids.Request), r.supervisor(), r.body("hold", ""))
	first, _ := r.send("hold", req)
	r.pool.Hide = true
	second, _ := r.send("hold", req)
	if second.Status != envelope.Ok || !second.Replayed || second.Body["question_id"] != first.Body["question_id"] {
		t.Fatalf("%s replayed %v %+v", second.Status, second.Replayed, second.Error)
	}
	if r.pool.Count() != 2 {
		t.Errorf("%d handoffs were sent", r.pool.Count())
	}
}
