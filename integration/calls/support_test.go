package calls

import (
	"bytes"
	"crypto/sha256"
	"errors"
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
// start the seven executables, each under every command name of its package,
// and the test-only callee of integration/testcmd/. The executables are the
// ones `make build` leaves in dist/ and the callee the one `make
// test-integration` builds into dist/callee/, unless the run names other
// directories (see callDirs); every start goes through the one process-execution path of contract/exec,
// and every program is found by name on the search path the tests were
// started with, by its lookup rules. They expect the same on macOS, Windows and
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
		err error
	}
	workRoot string
)

// TestMain makes the one directory the tests keep their scratch files in,
// and removes it when the tests end.
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

// stage checks, once for all tests, that the programs the tests start are the
// ones the run set up, with every executable under each of its command names
// in the directory of the executables, beside it, as the delivered bin/
// directory has them. In the repository's own dist/ it makes the names that
// are absent and those that no longer lead to their executable; a directory
// the run names is only read. The seven executables are not built here: they
// are the ones in that directory, and a test that cannot find one fails and
// says so. It returns the directory of the executables.
func stage(t testing.TB) string {
	t.Helper()
	staged.Do(func() {
		// A failure in the first test ends it where it stands; the others
		// then see this, not a pass.
		staged.err = errors.New("the programs could not be staged: see the first failure")
		staged.err = stageAll(t)
	})
	if staged.err != nil {
		t.Fatal(staged.err)
	}
	dist, _ := callDirs(t)
	return dist
}

func stageAll(t testing.TB) error {
	dist, calleeDir := callDirs(t)
	if err := requireOnPath(dist, calleeDir); err != nil {
		return err
	}
	for _, d := range declarations(t) {
		exe := filepath.Join(dist, d.Executable+suffix())
		if fi, err := os.Stat(exe); err != nil || !fi.Mode().IsRegular() {
			return fmt.Errorf("%s is not built: run `make build` first (%s)", d.Executable, exe)
		}
		for _, c := range d.Commands {
			if err := link(exe, filepath.Join(dist, c.Name+suffix()), ownDist()); err != nil {
				return err
			}
			if err := requireFoundIn(c.Name, exe); err != nil {
				return err
			}
		}
	}
	for _, name := range []string{calleeExe, calleeSmallName} {
		exe := filepath.Join(calleeDir, name+suffix())
		if fi, err := os.Stat(exe); err != nil || !fi.Mode().IsRegular() {
			return fmt.Errorf("the test callee %s is not built: run `make test-integration`, which builds it into dist/callee/, or build it into %s (%s)", name, calleeDir, exe)
		}
		if err := requireFoundIn(name, exe); err != nil {
			return err
		}
	}
	return nil
}

// link holds exe under another name: the executable itself under its own
// name, and every other command name a link to it, as the delivered bin/
// directory does. A name that is exe, leads to it or, on Windows, is a
// byte-identical copy of it is kept as it is. Any other is made again where
// write says the directory is the repository's own dist/: a name that is
// absent, and one that no longer leads to exe, as the hard link of an earlier
// build does on Windows. Without write nothing is written, and such a name is
// an error, so that a file left by an earlier build, or a different program of
// that name, is never what runs.
func link(exe, name string, write bool) error {
	_, err := os.Lstat(name)
	switch {
	case err == nil && isTheExecutable(name, exe):
		return nil
	case err == nil && !write:
		return fmt.Errorf("%s is present but is not the executable %s or a link to it", name, exe)
	case err == nil:
		if err := os.Remove(name); err != nil {
			return err
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	case !write:
		return fmt.Errorf("%s is absent: %s, the directory the run names in %s, must hold every command name and is not written to", name, filepath.Dir(name), executablesEnv)
	}
	if runtime.GOOS == "windows" {
		return os.Link(exe, name)
	}
	return os.Symlink(exe, name)
}

// input is what a run reads on its standard input.
type input struct {
	// bytes are written to a pipe and the pipe is closed; nil is the null
	// device.
	bytes []byte
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
	spec := exec.Spec{
		Program: exec.Program{Kind: exec.Hive, Name: program},
		Args:    args,
		Dir:     t.TempDir(),
		Stdout:  exec.Pipe(),
		Stderr:  exec.Pipe(),
	}
	spec.Stdin = exec.Null()
	if in.bytes != nil {
		spec.Stdin = exec.Pipe()
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
	// requestPath names an existing file that holds the request, which the
	// call then sends without writing its own; req supplies the identity.
	requestPath string
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
	switch {
	case c.requestPath != "":
		args = append(args, "--request-file", c.requestPath)
	case c.stdin:
		in.bytes = c.req.data
	default:
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

// snapshot lists every file under dir with a digest of its content, so that
// two snapshots differ when a record, a receipt, a recorded outcome or an event
// was written, replaced or removed. The lock files under locks/ and the
// directories a store makes when a mutating command first opens it hold no
// record, and are not listed: a command that ends with anything but ok has
// changed nothing when no file of data differs (spec/commands.md, Exit and
// error states).
func snapshot(t testing.TB, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() || rel == "locks" || strings.HasPrefix(rel, "locks/") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = fmt.Sprintf("%x", sha256.Sum256(data))
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

// parseObject reads a document as a JSON object.
func parseObject(t testing.TB, data []byte) map[string]any {
	t.Helper()
	v, err := canon.Parse(data)
	obj, ok := v.(map[string]any)
	if err != nil || !ok {
		t.Fatalf("not an object: %v", err)
	}
	return obj
}

// encodeObject writes an object as a document: canonical, one line feed.
func encodeObject(t testing.TB, obj map[string]any) []byte {
	t.Helper()
	out, err := canon.Encode(obj)
	if err != nil {
		t.Fatal(err)
	}
	return append(out, '\n')
}

// wantFailure fails the test unless the call ended with a complete response of
// the exit code, status and code given, that echoes the request, carries no
// body and no partial result.
func wantFailure(t testing.TB, r reply, requestID string, exit int, status, code string) {
	t.Helper()
	resp := r.must(t)
	if r.result.Code != int64(exit) || string(resp.Status) != status {
		t.Fatalf("exit code %d, status %q; want %d, %s", r.result.Code, resp.Status, exit, status)
	}
	if resp.Error == nil || resp.Error.Code != code {
		t.Fatalf("error = %+v, want code %s", resp.Error, code)
	}
	if resp.RequestID != requestID {
		t.Errorf("request_id = %q, want %q", resp.RequestID, requestID)
	}
	if resp.Body != nil {
		t.Errorf("a %s response carries a body: %v", status, resp.Body)
	}
}

var oversize struct {
	sync.Mutex
	files map[int64]string
}

// oversizeFile is a file of exactly size bytes that is no request, made once
// for all tests that send a request over a command's limit.
func oversizeFile(t testing.TB, size int64) string {
	t.Helper()
	oversize.Lock()
	defer oversize.Unlock()
	if path, ok := oversize.files[size]; ok {
		return path
	}
	path := filepath.Join(workRoot, fmt.Sprintf("oversize-%d", size))
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	chunk := bytes.Repeat([]byte("x"), 1<<20)
	for left := size; left > 0; {
		n := int64(len(chunk))
		if left < n {
			n = left
		}
		if _, err := f.Write(chunk[:n]); err != nil {
			t.Fatal(err)
		}
		left -= n
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if oversize.files == nil {
		oversize.files = map[int64]string{}
	}
	oversize.files[size] = path
	return path
}
