package registry_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/dispatch/internal/entry"
)

// isolateGit keeps the machine's own Git configuration out of every test: the
// operation passes these variables on to the Git it runs, and so do the tests
// that make a repository.
func isolateGit(t testing.TB) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// mint makes an identifier of the prefix.
func mint(t testing.TB, p ids.Prefix) string {
	t.Helper()
	id, err := ids.Mint(clock.System{}, p)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// A provisioning request: the home it names, the body it carries and the
// request_id it is sent under.
type provisioning struct {
	homeID    string
	requestID string
	data      []byte
}

// request builds a request of hive-fleet provision that proposes homeID for the
// home the command is pointed at, with the body.
func request(t testing.TB, homeID string, body map[string]any) provisioning {
	t.Helper()
	return requestAs(t, mint(t, ids.Request), homeID, body)
}

// requestAs is request under the request_id given.
func requestAs(t testing.TB, requestID, homeID string, body map[string]any) provisioning {
	t.Helper()
	issued, err := clock.Stamp(clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := canon.Encode(map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": "hive-fleet", "operation": "provision", "operation_version": "1.0",
		"request_id": requestID, "correlation_id": mint(t, ids.Correlation),
		"issued_at": issued, "caller": map[string]any{"kind": "test"},
		"scope_level": "hive", "home_id": homeID,
		"authority": map[string]any{"kind": "none"},
		"body":      body,
	})
	if err != nil {
		t.Fatal(err)
	}
	return provisioning{homeID, requestID, append(data, '\n')}
}

// send runs the request through the executable's own harness against the
// home, and returns the response as the command would have written it.
func send(t testing.TB, home string, p provisioning) *envelope.Response {
	t.Helper()
	return sendWith(t, entry.Executable(), home, p)
}

// sendWith is send through the executable given, which a test may have set a
// fault on.
func sendWith(t testing.TB, e *cli.Executable, home string, p provisioning) *envelope.Response {
	t.Helper()
	var fleet *cli.Command
	for _, c := range e.Commands {
		if c.Name == "hive-fleet" {
			fleet = c
		}
	}
	if fleet == nil {
		t.Fatal("the executable owns no hive-fleet")
	}
	out, err := e.Handle(cli.Invocation{Command: fleet, Operation: "provision", Home: home, Request: p.data})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := envelope.AcceptResponse(out.Encoded, out.Response.ExitCode(), p.requestID)
	if err != nil {
		t.Fatalf("the response is not accepted: %v\n%s", err, out.Encoded)
	}
	return resp
}

// newHome makes the empty directory a home is provisioned in.
func newHome(t testing.TB) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// tryGit runs Git in dir for a test, away from the machine's configuration.
func tryGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.test",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.test")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %v in %s: %v\n%s", args, dir, err, out.String())
	}
	return nil
}

// git is tryGit for a command that must succeed.
func git(t testing.TB, dir string, args ...string) {
	t.Helper()
	if err := tryGit(dir, args...); err != nil {
		t.Fatal(err)
	}
}

// newClone makes a Git repository with one commit and returns its top level, as
// the platform names it. Where url is not empty it is the url of the remote
// origin.
func newClone(t testing.TB, url string) string {
	t.Helper()
	dir := physical(t, t.TempDir())
	git(t, dir, "init", "--quiet")
	git(t, dir, "commit", "--quiet", "--allow-empty", "-m", "start")
	if url != "" {
		git(t, dir, "remote", "add", "origin", url)
	}
	return dir
}

// physical is the path with every symbolic link resolved, with forward
// slashes: the form a clone record keeps.
func physical(t testing.TB, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(real)
}

// snapshot is every entry under dir, a directory by its name with a trailing
// slash and a file by its name with its bytes.
func snapshot(t testing.TB, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			out[rel+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// object reads a JSON object from a tree: the member of m at the path of keys.
func object(t testing.TB, m map[string]any, keys ...string) map[string]any {
	t.Helper()
	for _, k := range keys {
		next, ok := m[k].(map[string]any)
		if !ok {
			t.Fatalf("%s is not an object in %v", k, m)
		}
		m = next
	}
	return m
}

// list reads the array of objects at m[key].
func list(t testing.TB, m map[string]any, key string) []map[string]any {
	t.Helper()
	items, ok := m[key].([]any)
	if !ok {
		t.Fatalf("%s is not an array in %v", key, m)
	}
	out := make([]map[string]any, len(items))
	for i, item := range items {
		if out[i], ok = item.(map[string]any); !ok {
			t.Fatalf("%s[%d] is not an object", key, i)
		}
	}
	return out
}
