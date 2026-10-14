package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/store"
)

// The fake callee adapters of the storeless tests. Neither knows any package:
// one runs a real callee executable of the harness in the process, the other
// answers what a test scripts. Both count what the command with no store asks
// of them.

// inProcess is a callee adapter over a real callee executable: its read is
// the recorded outcome of the callee's store, after the recovery taking the
// callee's lock gives, and its send is the callee's whole harness.
type inProcess struct {
	exe *Executable
	// hide makes the read find nothing, as the read of a racer that looked
	// before the other request reached the callee.
	hide bool

	reads []string
	sent  [][]byte
}

func (a *inProcess) ReadOutcome(c *Call, requestID string) ([]byte, bool, error) {
	a.reads = append(a.reads, requestID)
	if a.hide {
		return nil, false, nil
	}
	st, w, err := a.exe.open(c.Home, requestID, a.exe.clock())
	if err != nil {
		return nil, false, err
	}
	defer w.Release()
	data, err := st.Read(store.OutcomePath(a.exe.Package, requestID))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return data, err == nil, err
}

func (a *inProcess) Send(c *Call, request []byte) ([]byte, int, error) {
	a.sent = append(a.sent, request)
	req, err := envelope.DecodeRequest(request)
	if err != nil {
		return nil, 0, err
	}
	command := a.exe.command(req.Command)
	if command == nil {
		return nil, 0, errors.New("the callee owns no such command")
	}
	out, err := a.exe.Handle(Invocation{Command: command, Operation: req.Operation, Home: c.Home.Dir, Request: request})
	if err != nil {
		return nil, 0, err
	}
	return out.Encoded, out.Response.ExitCode(), nil
}

// scripted is a callee adapter that answers what the test sets: the recorded
// outcome it reads and what it replies to a handoff.
type scripted struct {
	outcome []byte
	found   bool
	readErr error
	// reply answers a handoff; nil answers every one with an ok response.
	reply func(request []byte) ([]byte, int, error)

	reads int
	sent  [][]byte
}

func (a *scripted) ReadOutcome(c *Call, requestID string) ([]byte, bool, error) {
	a.reads++
	return a.outcome, a.found, a.readErr
}

func (a *scripted) Send(c *Call, request []byte) ([]byte, int, error) {
	a.sent = append(a.sent, request)
	if a.reply != nil {
		return a.reply(request)
	}
	return calleeAnswer(request, func(r *envelope.Response, _ *envelope.Request) { r.Body = map[string]any{"wrote": "scripted"} })
}

// calleeAnswer is the callee's response to the handoff in hand: ok, echoing
// the request, from the responder of the callee, changed by edit.
func calleeAnswer(request []byte, edit func(*envelope.Response, *envelope.Request)) ([]byte, int, error) {
	req, err := envelope.DecodeRequest(request)
	if err != nil {
		return nil, 0, err
	}
	resp := &envelope.Response{
		RequestID: req.RequestID, Command: req.Command, Operation: req.Operation, Status: envelope.Ok,
		Responder:   envelope.Responder{Package: "dispatch", Command: req.Command, ImplementationVersion: "0.1.0-test", ContractVersion: envelope.ContractVersion},
		CompletedAt: "2026-10-11T08:00:00.000Z", Receipts: []envelope.Receipt{},
	}
	edit(resp, req)
	data, err := resp.Encode()
	if err != nil {
		return nil, 0, err
	}
	return data, resp.ExitCode(), nil
}

// layout is the state of a directory with its directories: every entry by
// slash path, a file with the digest of its bytes and a directory marked, so
// that a directory made and left empty is a change.
func layout(t testing.TB, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for path, sum := range tree(t, dir) {
		out[path] = sum
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out[filepath.ToSlash(rel)+"/"] = "dir"
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// absent asserts that nothing exists at the path.
func absent(t testing.TB, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s exists (%v)", path, err)
	}
}
