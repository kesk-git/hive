package acceptance

import (
	"bytes"
	"fmt"
	"io"
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
	"github.com/kesk-git/hive/packages/word/internal/poolfake"
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

// TestMain removes the delivered directory, built once for all the tests. Started
// under the name hive-pool, the test binary is the fake hive-pool the
// delivered hive-word finds on its search path.
func TestMain(m *testing.M) {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), suffix()) == "hive-pool" {
		os.Exit(servePool())
	}
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
	out, err := canon.Encode(map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": command, "operation": operation, "operation_version": "1.0",
		"request_id": requestID, "correlation_id": correlationID,
		"issued_at": "2026-10-11T08:00:00.000Z", "caller": map[string]any{"kind": "test"},
		"scope_level": "hive", "home_id": homeID,
		"authority": map[string]any{"kind": "none"},
		"body":      map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(out, "\n"...)
}

// notBuilt are the operations declared and not built yet; the others are
// proved by the tests of the intake. Every operation the declaration lists is
// built now.
var notBuilt []string

// Every command name of the package answers capabilities: exit 0, one
// complete response with a null request_id, whose body declares the
// package's command, its executable and exactly the operations the
// declaration lists, those not built yet unsupported.
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
				if slices.Contains(notBuilt, op.Name) != (op.State == envelope.Unsupported) {
					t.Errorf("%s is %s; only %v are not built", op.Name, op.State, notBuilt)
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
// 2, and every operation it declares and has not built as refused, exit 4,
// with unsupported_operation.
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
		for _, operation := range notBuilt {
			t.Run(c.Name+" "+operation, func(t *testing.T) {
				req := request(t, c.Name, operation)
				stdout, _, exit := run(t, program, req, operation, "--home", home)
				resp, err := envelope.AcceptResponse([]byte(stdout), exit, requestID(t, req))
				if err != nil {
					t.Fatal(err)
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

// requestID reads the request_id of an encoded request.
func requestID(t testing.TB, req []byte) string {
	t.Helper()
	id, err := envelope.LooseRequestID(req)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// servePool is the fake hive-pool as a process: the operation it is started
// with, the request on its standard input, the response on its standard
// output. The examples of the declared schemas are named in the file beside
// it.
func servePool() int {
	fixtures, err := os.ReadFile(filepath.Join(filepath.Dir(os.Args[0]), "fixtures"))
	if err != nil || len(os.Args) < 2 {
		return 1
	}
	poolfake.Fixtures = string(fixtures)
	request, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 1
	}
	out, exit, err := (&poolfake.Pool{}).Serve(os.Args[1], request)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	os.Stdout.Write(out)
	return exit
}

// pool is a directory that holds the fake hive-pool, to be the whole search
// path of a hive-word.
func pool(t testing.TB) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	fixtures, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "contract", "testdata", "schemas"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixtures"), []byte(fixtures), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "hive-pool"+suffix())
	if runtime.GOOS == "windows" {
		err = os.Link(self, link)
	} else {
		err = os.Symlink(self, link)
	}
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// homeWith is a home that holds only its identity record.
func homeWith(t testing.TB) (home, homeID string) {
	t.Helper()
	home = filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	homeID, err := ids.Mint(clock.System{}, ids.Home)
	if err != nil {
		t.Fatal(err)
	}
	stamp := "2026-10-11T08:00:00.000Z"
	record, err := canon.Encode(map[string]any{
		"schema": "hive.home", "schema_version": "1.0", "record_revision": int64(1),
		"home_id": homeID, "role": "hive", "name": "A test home", "parent": map[string]any{"kind": "none"},
		"created_at": stamp, "updated_at": stamp,
		"writer": map[string]any{"package": "dispatch", "command": "hive-fleet", "operation": "provision", "implementation_version": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "home.json"), append(record, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return home, homeID
}

// listing is every path under the home with its size.
func listing(t testing.TB, home string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, fmt.Sprintf("%s %d", path, info.Size()))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// holdRequest is a valid hold of a supervisor at the scope of the home.
func holdRequest(t testing.TB, homeID string) []byte {
	t.Helper()
	mint := func(p ids.Prefix) string {
		id, err := ids.Mint(clock.System{}, p)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	out, err := canon.Encode(map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": "hive-word", "operation": "hold", "operation_version": "1.0",
		"request_id": mint(ids.Request), "correlation_id": mint(ids.Correlation),
		"issued_at":   "2026-10-11T08:00:00.123Z",
		"caller":      map[string]any{"kind": "supervisor", "session_id": mint(ids.Session), "session_generation": 1},
		"scope_level": "hive", "home_id": homeID,
		"authority": map[string]any{"kind": "none"},
		"body": map[string]any{
			"held_task_id": mint(ids.Task), "origin": map[string]any{"kind": "supervisor"},
			"text": "Keep this", "consequence": "Nothing else",
			"options":        []any{map[string]any{"option_id": "yes", "label": "Yes", "consequence": "Yes"}},
			"recommendation": "yes", "round": map[string]any{"complete": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(out, '\n')
}

// hiveWord runs the delivered hive-word with only the search path given.
func hiveWord(t testing.TB, path string, stdin []byte, args ...string) (stdout string, exit int) {
	t.Helper()
	p := declaration(t)
	cmd := exec.Command(filepath.Join(delivered(t, p), "hive-word"+suffix()), args...)
	cmd.Env = []string{"PATH=" + path}
	cmd.Stdin = bytes.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		exit = ee.ExitCode()
	}
	return out.String(), exit
}

// The delivered hive-word reads the Pool's recorded outcome and sends its
// handoff by running hive-pool, found on the search path, and returns the
// Pool's receipt; it writes nothing in the home.
func TestHoldReachesHivePool(t *testing.T) {
	home, homeID := homeWith(t)
	before := listing(t, home)
	req := holdRequest(t, homeID)
	stdout, exit := hiveWord(t, pool(t), req, "hold", "--home", home)
	resp, err := envelope.AcceptResponse([]byte(stdout), exit, requestID(t, req))
	if err != nil {
		t.Fatalf("%v\n%s", err, stdout)
	}
	if exit != 0 || resp.Status != envelope.Ok || resp.Replayed || len(resp.Receipts) != 1 || resp.Receipts[0].Kind != "pool.question.held" {
		t.Fatalf("exit %d, %s, replayed %v, %d receipts, error %+v", exit, resp.Status, resp.Replayed, len(resp.Receipts), resp.Error)
	}
	if after := listing(t, home); !slices.Equal(before, after) {
		t.Errorf("the home held %v and holds %v", before, after)
	}
}

// With no hive-pool on the search path nothing can be read or sent: the answer
// is unavailable with dependency_missing, and nothing is written in the home.
func TestHoldWithNoHivePoolIsDependencyMissing(t *testing.T) {
	home, homeID := homeWith(t)
	before := listing(t, home)
	req := holdRequest(t, homeID)
	stdout, exit := hiveWord(t, t.TempDir(), req, "hold", "--home", home)
	resp, err := envelope.AcceptResponse([]byte(stdout), exit, requestID(t, req))
	if err != nil {
		t.Fatalf("%v\n%s", err, stdout)
	}
	if exit != envelope.Unavailable.ExitCode() || resp.Status != envelope.Unavailable || resp.Error == nil || resp.Error.Code != envelope.CodeDependencyMissing {
		t.Fatalf("exit %d, %s, error %+v", exit, resp.Status, resp.Error)
	}
	if after := listing(t, home); !slices.Equal(before, after) {
		t.Errorf("the home held %v and holds %v", before, after)
	}
}
