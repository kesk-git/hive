package calls

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/declare"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/exec"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/store"
)

// What the tests of this package are, on each of the three platforms. They
// start the seven executables that `make build` leaves in dist/, each under
// every command name of its package, and the test-only callee of
// integration/testcmd/, which they build; every start goes through the one
// process-execution path of contract/exec, and every program is found on the
// search path by its lookup rules. They expect the same on macOS, Windows and
// Linux: the same exit codes, the same responses, the same sizes. Only the
// suffix of an executable (.exe on Windows) and the kind of link that gives
// an executable its other command names (a symbolic link on macOS and Linux,
// a hard link on Windows, as the delivered bin/ directory has them) differ,
// and neither is skipped anywhere.

// recordBytes is the size of the record every call carries across in the
// size proof: 2,097,152 bytes, above the platform's limit on arguments on
// each supported platform.
const recordBytes = 2097152

// callee is the executable of integration/testcmd/, and its commands.
const (
	calleeExe       = "hive-callee"
	calleeSmallName = "hive-callee-small"
	// smallLimit is the request and response limit hive-callee-small
	// declares: below the record.
	smallLimit = 1048576
)

var (
	staged struct {
		sync.Once
		bin string
		err error
	}
	callee struct {
		sync.Once
		err error
	}
	workRoot string
)

// TestMain makes the one directory the tests build their executables and
// links in, puts it first on the search path of the one process-execution
// path, and removes it when the tests end.
func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	dir, err := os.MkdirTemp("", "hive-calls-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	workRoot = dir
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	staged.bin = bin
	restore := placeFirst(bin)
	defer restore()
	return m.Run()
}

// suffix is the suffix of an executable file on this platform.
func suffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// repoRoot is the root of the repository: the nearest directory above the
// package that holds go.mod.
func repoRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}

// declarations reads the seven packages' declarations, by package name.
func declarations(t testing.TB) map[string]*declare.Package {
	t.Helper()
	root := repoRoot(t)
	paths, err := filepath.Glob(filepath.Join(root, "packages", "*", declare.FileName))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no package declaration under packages/: %v", err)
	}
	out := map[string]*declare.Package{}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		d, err := declare.Parse(data)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		out[d.Name] = d
	}
	return out
}

// stage gives every executable its command names, once for all tests, in the
// directory TestMain placed first on the search path. The seven executables
// are not built here: they are the ones `make build` left in dist/, and a
// test that cannot find one fails and says so.
func stage(t testing.TB) string {
	t.Helper()
	staged.Do(func() { staged.err = stageAll(t) })
	if staged.err != nil {
		t.Fatal(staged.err)
	}
	requireOnPath(t, staged.bin)
	return staged.bin
}

func stageAll(t testing.TB) error {
	root := repoRoot(t)
	for _, d := range declarations(t) {
		exe := filepath.Join(root, "dist", d.Executable+suffix())
		if fi, err := os.Stat(exe); err != nil || !fi.Mode().IsRegular() {
			return fmt.Errorf("%s is not built: run `make build` first (%s)", d.Executable, exe)
		}
		for _, c := range d.Commands {
			if err := link(exe, filepath.Join(staged.bin, c.Name+suffix())); err != nil {
				return err
			}
		}
	}
	return nil
}

// stageCallee builds the test-only callee, once for all tests that start it,
// and gives it its second command name.
func stageCallee(t testing.TB) {
	t.Helper()
	stage(t)
	callee.Do(func() {
		out := filepath.Join(staged.bin, calleeExe+suffix())
		if err := buildCallee(repoRoot(t), out); err != nil {
			callee.err = err
			return
		}
		callee.err = link(out, filepath.Join(staged.bin, calleeSmallName+suffix()))
	})
	if callee.err != nil {
		t.Fatal(callee.err)
	}
}

// link gives target a second name: the executable itself under its own name,
// and every other command name a link to it, as the delivered bin/ directory
// does.
func link(exe, name string) error {
	if exe == name {
		return nil
	}
	if _, err := os.Lstat(name); err == nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		return os.Link(exe, name)
	}
	return os.Symlink(exe, name)
}

// goConfig are the variables of the Go tool itself that its build reads, and
// that a run of it is given by name from the environment: the tool's own
// configuration and nothing of the test's.
var goConfig = []string{
	"GOCACHE", "GOPATH", "GOMODCACHE", "GOROOT", "GOFLAGS", "GOTOOLCHAIN", "GOENV",
	"GOPROXY", "GOSUMDB", "GONOSUMDB", "GONOPROXY", "GOPRIVATE", "GOINSECURE", "GOVCS", "GOWORK",
}

// buildCallee builds the test-only callee of integration/testcmd/ into out,
// with the Go tool, started through the one process-execution path.
func buildCallee(root, out string) error {
	log := filepath.Join(workRoot, "build.log")
	run, err := exec.Start(exec.Spec{
		Program: exec.Program{Kind: exec.Project, Name: "go"},
		Args:    []string{"build", "-trimpath", "-o", out, "./integration/testcmd"},
		Dir:     root,
		Config:  goConfig,
		Stdin:   exec.Null(),
		Stdout:  exec.File(log),
		Stderr:  exec.AppendFile(log),
	})
	if err != nil {
		return fmt.Errorf("the callee cannot be built: %w", err)
	}
	defer run.Close()
	res := run.Wait()
	if !res.Passed() {
		text, _ := os.ReadFile(log)
		return fmt.Errorf("the callee cannot be built: go build ./integration/testcmd %s:\n%s", res, text)
	}
	return nil
}

// input is what a run reads on its standard input.
type input struct {
	// bytes are written to a pipe and the pipe is closed.
	bytes []byte
	// file names a file the run reads instead.
	file string
}

// ran is what one run of a program left: how it ended and its two outputs.
type ran struct {
	result exec.Result
	stdout []byte
	stderr []byte
}

// start runs program with the arguments, to its end, through contract/exec.
func start(t testing.TB, program string, args []string, in input) ran {
	t.Helper()
	stage(t)
	if strings.HasPrefix(program, calleeExe) {
		stageCallee(t)
	}
	spec := exec.Spec{
		Program: exec.Program{Kind: exec.Hive, Name: program},
		Args:    args,
		Dir:     t.TempDir(),
		Stdout:  exec.Pipe(),
		Stderr:  exec.Pipe(),
	}
	switch {
	case in.file != "":
		spec.Stdin = exec.File(in.file)
	case in.bytes != nil:
		spec.Stdin = exec.Pipe()
	default:
		spec.Stdin = exec.Null()
	}
	run, err := exec.Start(spec)
	if err != nil {
		t.Fatalf("%s %v: not started: %v", program, args, err)
	}
	defer run.Close()
	var wg sync.WaitGroup
	var out, errOut []byte
	wg.Add(2)
	go func() { defer wg.Done(); out, _ = io.ReadAll(run.Stdout()) }()
	go func() { defer wg.Done(); errOut, _ = io.ReadAll(run.Stderr()) }()
	if in.bytes != nil {
		go func() {
			// A command that answers before it has read everything closes its
			// end, and a write to it then fails; the answer is what is judged.
			run.Stdin().Write(in.bytes)
			run.Stdin().Close()
		}()
	}
	wg.Wait()
	return ran{result: run.Wait(), stdout: out, stderr: errOut}
}

// reply is what a caller makes of one call.
type reply struct {
	ran
	// resp is the response the caller accepts under the contract's rule, or
	// nil.
	resp *envelope.Response
	// failure is why the caller does not accept the output, when it does not.
	failure *envelope.Failure
	// raw is the response document as written, from standard output or from
	// the response file.
	raw []byte
}

// call is one invocation of an operation as a caller makes it: the request
// goes by file or by standard input, the response comes back on standard
// output or in a file.
type call struct {
	program   string
	operation string
	home      string
	req       request
	// stdin sends the request on standard input instead of in a file.
	stdin bool
	// responseFile asks for the response in a file instead of on standard
	// output.
	responseFile bool
}

// run makes the call and reads the response as a caller must: from the whole
// output, against the request's identity and the exit code.
func (c call) run(t testing.TB) reply {
	t.Helper()
	dir := t.TempDir()
	args := []string{c.operation}
	if c.home != "" {
		args = append(args, "--home", c.home)
	}
	var in input
	if c.stdin {
		in.bytes = c.req.data
	} else {
		path := filepath.Join(dir, "request.json")
		if err := os.WriteFile(path, c.req.data, 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, "--request-file", path)
	}
	respPath := ""
	if c.responseFile {
		respPath = filepath.Join(dir, "response.json")
		args = append(args, "--response-file", respPath)
	}
	r := reply{ran: start(t, c.program, args, in)}
	r.raw = r.stdout
	if c.responseFile {
		data, err := os.ReadFile(respPath)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		r.raw = data
	}
	r.resp, r.failure = r.result.HiveResult(r.raw, c.req.id)
	return r
}

// must fails the test unless the output was accepted as a response.
func (r reply) must(t testing.TB) *envelope.Response {
	t.Helper()
	if r.failure != nil {
		t.Fatalf("no response was accepted: %v (%s); stderr: %s", r.failure, r.result, r.stderr)
	}
	return r.resp
}

// request is a request document and its identity.
type request struct {
	id   string
	data []byte
}

const alphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// ident invents the n-th identifier of the prefix, in the form
// spec/identities.md gives.
func ident(p ids.Prefix, n int) string {
	s := string(p) + "_0" + strings.Repeat("0", 23) + string(alphabet[n/32%32]) + string(alphabet[n%32])
	if err := ids.Check(s, p); err != nil {
		panic(err)
	}
	return s
}

// testHome is the identity of the home every test makes.
var testHome = ident(ids.Home, 1)

// asPackage is the caller of a call: the command of the package that declares
// the call.
type asPackage struct{ pkg, command string }

// newRequest builds the request document of a call: a caller, the command and
// operation addressed, the body, in the hive scope of the test home. n makes
// the request's identity.
func newRequest(from asPackage, command, operation string, n int, body map[string]any) request {
	r := envelope.Request{
		Command:          command,
		Operation:        operation,
		OperationVersion: "1.0",
		RequestID:        ident(ids.Request, n),
		CorrelationID:    ident(ids.Correlation, 1),
		IssuedAt:         "2026-10-11T08:00:00.000Z",
		Caller:           envelope.Caller{Kind: envelope.CallerPackage, Package: from.pkg, Command: from.command},
		Scope:            envelope.Scope{Level: envelope.ScopeHive, HomeID: testHome},
		Authority:        envelope.Authority{Kind: envelope.AuthorityNone},
		Body:             body,
	}
	data, err := r.Encode()
	if err != nil {
		panic(err)
	}
	return request{id: r.RequestID, data: data}
}

// newHome makes a private home with an identity record in a temporary
// directory, and removes it when the test ends.
func newHome(t testing.TB) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.SecureDir(dir); err != nil {
		t.Fatal(err)
	}
	const at = "2026-10-11T08:00:00.000Z"
	rec, err := canon.Encode(map[string]any{
		"schema": "hive.home", "schema_version": "1.0", "record_revision": int64(1),
		"home_id": testHome, "role": "hive", "name": "A test home", "parent": map[string]any{"kind": "none"},
		"created_at": at, "updated_at": at,
		"writer": map[string]any{"package": "dispatch", "command": "hive-fleet", "operation": "provision", "implementation_version": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "home.json"), append(rec, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// snapshot lists every entry under dir with a digest of its content, so that
// two snapshots differ when anything was written, replaced or removed.
func snapshot(t testing.TB, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if d.IsDir() {
			out[filepath.ToSlash(rel)+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// sameSnapshot fails the test unless the two snapshots are the same.
func sameSnapshot(t testing.TB, what string, before, after map[string]string) {
	t.Helper()
	var diff []string
	for k, v := range after {
		if w, ok := before[k]; !ok {
			diff = append(diff, "added "+k)
		} else if w != v {
			diff = append(diff, "changed "+k)
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			diff = append(diff, "removed "+k)
		}
	}
	sort.Strings(diff)
	if len(diff) > 0 {
		t.Fatalf("%s changed the home: %s", what, strings.Join(diff, ", "))
	}
}

// pattern is the record the size proof sends: exactly n bytes of ASCII in
// which every sixteen bytes carry their own position, so that a record cut
// short, repeated, shifted or altered anywhere is not the record sent.
func pattern(n int) string {
	var b strings.Builder
	b.Grow(n)
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, "%08x-record!", i)
	}
	return b.String()[:n]
}

// sameRecord says whether got is the record pattern(n) exactly.
func sameRecord(got any, n int) bool {
	s, ok := got.(string)
	return ok && len(s) == n && bytes.Equal([]byte(s), []byte(pattern(n)))
}
