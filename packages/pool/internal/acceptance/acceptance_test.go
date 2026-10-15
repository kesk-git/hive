package acceptance

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/declare"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// The acceptance test of the package's command entry point (spec/commands.md,
// Executables and command names, Capabilities): the package's one executable
// is built, started under each command name of its declaration, and every
// name answers capabilities and refuses everything else.

// declaration is the package's own declaration.
func declaration(t testing.TB) *declare.Package {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "hive-package.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := declare.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// suffix is the executable suffix of the platform.
func suffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// delivered builds the package's executable and gives it every command name
// of the declaration as the delivered bin/ directory does: a relative
// symbolic link, or on Windows a hard link named with the .exe suffix. It
// returns the directory.
func delivered(t testing.TB, p *declare.Package) string {
	t.Helper()
	once.Do(func() { dir, failure = deliver(p) })
	if failure != nil {
		t.Fatal(failure)
	}
	return dir
}

var (
	once    sync.Once
	dir     string
	failure error
)

// TestMain removes the delivered directory, built once for all the tests.
func TestMain(m *testing.M) {
	code := m.Run()
	if dir != "" {
		os.RemoveAll(dir)
	}
	os.Exit(code)
}

// deliver builds the executable and its links into a new directory.
func deliver(p *declare.Package) (string, error) {
	dir, err := os.MkdirTemp("", "hive-delivered-")
	if err != nil {
		return "", err
	}
	build := exec.Command("go", "build", "-o", filepath.Join(dir, p.Executable+suffix()), "./cmd/"+p.Executable)
	build.Dir = filepath.Join("..", "..")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		return dir, fmt.Errorf("building %s: %v\n%s", p.Executable, err, out)
	}
	for _, c := range p.Commands {
		if c.Name == p.Executable {
			continue
		}
		link := filepath.Join(dir, c.Name+suffix())
		if runtime.GOOS == "windows" {
			if err := os.Link(filepath.Join(dir, p.Executable+suffix()), link); err != nil {
				return dir, err
			}
			continue
		}
		if err := os.Symlink(p.Executable, link); err != nil {
			return dir, err
		}
	}
	return dir, nil
}

// run starts the program under the name it is called by and returns its
// standard output, standard error and exit code.
func run(t testing.TB, program string, stdin []byte, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	cmd := exec.Command(program, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("%s: %v", program, err)
		}
		exit = ee.ExitCode()
	}
	return out.String(), errOut.String(), exit
}

// request is a well-formed request for the operation of the command.
func request(t testing.TB, command, operation string) []byte {
	t.Helper()
	requestID, err := ids.Mint(clock.System{}, ids.Request)
	if err != nil {
		t.Fatal(err)
	}
	correlationID, err := ids.Mint(clock.System{}, ids.Correlation)
	if err != nil {
		t.Fatal(err)
	}
	homeID, err := ids.Mint(clock.System{}, ids.Home)
	if err != nil {
		t.Fatal(err)
	}
	// An empty body breaks the schema of every built operation but check, whose
	// members are all optional: an identity that is not one breaks its.
	body := map[string]any{}
	if command == "hive-memory" && operation == "check" {
		body["projection_id"] = "not-a-projection"
	}
	// Each built operation is sent the version it is declared at.
	version := "1.0"
	switch {
	case command == "hive-memory" && (operation == "render" || operation == "check" || operation == "propagate"):
		version = "2.0"
	case command == "hive-spec" && operation == "define":
		version = "1.2"
	}
	out, err := canon.Encode(map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": command, "operation": operation, "operation_version": version,
		"request_id": requestID, "correlation_id": correlationID,
		"issued_at": "2026-10-11T08:00:00.000Z", "caller": map[string]any{"kind": "test"},
		"scope_level": "hive", "home_id": homeID,
		"authority": map[string]any{"kind": "none"},
		"body":      body,
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(out, "\n"...)
}

// built are the operations built so far, by command: the items and the holds
// that are not the decision intake's, with the read of their kinds, and the
// operations hold-question, revise-question, answer-question, release-question,
// complete-question and close-question, and the dry-run import and export of
// the legacy backlog, hive-admit approve, and the operations define, accept and
// close of hive-spec, record, amend, expire and resolve of hive-rule, and
// render, check, propagate, budget and subject of hive-memory. Every other operation of the declaration is declared
// and refused.
var built = map[string][]string{
	"hive-pool":   {"add", "read", "update", "transition", "hold", "release", "hold-question", "revise-question", "answer-question", "release-question", "complete-question", "close-question", "import-legacy", "export-legacy"},
	"hive-admit":  {"approve"},
	"hive-spec":   {"define", "accept", "close"},
	"hive-rule":   {"record", "amend", "expire", "resolve"},
	"hive-memory": {"render", "check", "propagate", "budget", "subject"},
}

// Every command name of the package answers capabilities: exit 0, one
// complete response with a null request_id, whose body declares the
// package's command, its executable and exactly the operations the
// declaration lists, supported where it is built and otherwise not callable.
func TestEveryCommandNameAnswersCapabilities(t *testing.T) {
	p := declaration(t)
	dir := delivered(t, p)
	for _, c := range p.Commands {
		t.Run(c.Name, func(t *testing.T) {
			stdout, stderr, exit := run(t, filepath.Join(dir, c.Name+suffix()), nil, "capabilities")
			if exit != 0 {
				t.Fatalf("exit %d, stdout %q, stderr %q", exit, stdout, stderr)
			}
			resp, err := envelope.AcceptCapabilities([]byte(stdout), exit)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Status != envelope.Ok || resp.Command != c.Name {
				t.Fatalf("status %s, command %s", resp.Status, resp.Command)
			}
			caps, err := envelope.DecodeCapabilities(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if caps.Command != c.Name || caps.Package != p.Name || caps.Executable != p.Executable {
				t.Errorf("declares command %s of package %s with executable %s", caps.Command, caps.Package, caps.Executable)
			}
			var got []string
			for _, op := range caps.Operations {
				got = append(got, op.Name)
				if want := builtOperation(c.Name, op.Name); (op.State == envelope.Supported) != want {
					t.Errorf("%s is %s; built is %v", op.Name, op.State, want)
				}
			}
			slices.Sort(got)
			if want := slices.Sorted(slices.Values(c.Operations)); !slices.Equal(got, want) {
				t.Errorf("declares the operations %v; the declaration lists %v", got, want)
			}
		})
	}
}

// Every command name refuses an operation it does not declare as usage, exit
// 2, and every operation it declares and has not built as refused, exit 4, with
// unsupported_operation; one that is built answers its own schema, and an empty
// body is invalid, exit 3.
func TestEveryCommandNameRefusesEverythingElse(t *testing.T) {
	p := declaration(t)
	dir := delivered(t, p)
	home := t.TempDir()
	for _, c := range p.Commands {
		program := filepath.Join(dir, c.Name+suffix())
		t.Run(c.Name+" unknown operation", func(t *testing.T) {
			req := request(t, c.Name, "no-such-operation")
			stdout, _, exit := run(t, program, req, "no-such-operation", "--home", home)
			resp, err := envelope.AcceptResponse([]byte(stdout), exit, requestID(t, req))
			if err != nil {
				t.Fatal(err)
			}
			if exit != 2 || resp.Status != envelope.Usage || resp.Error == nil || resp.Error.Code != envelope.CodeUnknownOperation {
				t.Fatalf("exit %d, status %s, error %+v", exit, resp.Status, resp.Error)
			}
		})
		for _, operation := range c.Operations {
			t.Run(c.Name+" "+operation, func(t *testing.T) {
				req := request(t, c.Name, operation)
				stdout, _, exit := run(t, program, req, operation, "--home", home)
				resp, err := envelope.AcceptResponse([]byte(stdout), exit, requestID(t, req))
				if err != nil {
					t.Fatal(err)
				}
				if builtOperation(c.Name, operation) {
					if exit != 3 || resp.Status != envelope.Invalid || resp.Error == nil || resp.Error.Code != envelope.CodeSchemaViolation {
						t.Fatalf("exit %d, status %s, error %+v", exit, resp.Status, resp.Error)
					}
					return
				}
				if exit != 4 || resp.Status != envelope.Refused || resp.Error == nil || resp.Error.Code != envelope.CodeUnsupportedOperation {
					t.Fatalf("exit %d, status %s, error %+v", exit, resp.Status, resp.Error)
				}
				if resp.Body != nil || len(resp.Receipts) != 0 {
					t.Errorf("a refusal of a build with no operation leaves a body %v or receipts %v", resp.Body, resp.Receipts)
				}
			})
		}
	}
}

// A name the executable does not own exits 2 and writes nothing in the home.
func TestAForeignCommandNameExitsTwo(t *testing.T) {
	p := declaration(t)
	dir := delivered(t, p)
	foreign := filepath.Join(dir, "hive-not-owned"+suffix())
	if runtime.GOOS == "windows" {
		if err := os.Link(filepath.Join(dir, p.Executable+suffix()), foreign); err != nil {
			t.Fatal(err)
		}
	} else if err := os.Symlink(p.Executable, foreign); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"capabilities"}, {"--version"}} {
		_, stderr, exit := run(t, foreign, nil, args...)
		if exit != 2 {
			t.Errorf("%v: exit %d, stderr %q", args, exit, stderr)
		}
	}
}

// --version prints one line and exits 0, under every command name.
func TestEveryCommandNameAnswersVersion(t *testing.T) {
	p := declaration(t)
	dir := delivered(t, p)
	for _, c := range p.Commands {
		stdout, stderr, exit := run(t, filepath.Join(dir, c.Name+suffix()), nil, "--version")
		if exit != 0 || strings.Count(stdout, "\n") != 1 || strings.TrimSpace(stdout) == "" {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", c.Name, exit, stdout, stderr)
		}
	}
}

// builtOperation reports whether the operation of the command is built.
func builtOperation(command, operation string) bool {
	return slices.Contains(built[command], operation)
}

// requestID reads the request_id of an encoded request.
func requestID(t testing.TB, req []byte) string {
	t.Helper()
	id, err := envelope.LooseRequestID(req)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
