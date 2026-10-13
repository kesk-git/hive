package entry

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/declare"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// root is the repository, three directories above the internal package of a
// package.
func root() string { return filepath.Join("..", "..", "..", "..") }

// invoke runs the executable as the command named by argv0 and returns its
// standard output and exit code.
func invoke(t testing.TB, argv0 string, stdin []byte, args ...string) (string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	exit := Executable().Main(append([]string{argv0}, args...), bytes.NewReader(stdin), &out, &errOut)
	return out.String(), exit
}

// request is a well-formed request for the operation of the command.
func request(t testing.TB, command, operation string) []byte {
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
		"command": command, "operation": operation, "operation_version": "1.0",
		"request_id": mint(ids.Request), "correlation_id": mint(ids.Correlation),
		"issued_at": "2026-10-11T08:00:00.000Z", "caller": map[string]any{"kind": "test"},
		"scope_level": "hive", "home_id": mint(ids.Home),
		"authority": map[string]any{"kind": "none"},
		"body":      map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(out, "\n"...)
}

// The executable is well formed and agrees with the package's declaration:
// the executable, the commands, the operations of each, what they may run,
// call and write, and whether the package keeps a store.
func TestTheExecutableAgreesWithTheDeclaration(t *testing.T) {
	e := Executable()
	if err := e.Check(); err != nil {
		t.Fatal(err)
	}
	set, problems := declare.Read(os.DirFS(root()))
	if err := problems.Err(); err != nil {
		t.Fatal(err)
	}
	if ps := set.Agree(e); len(ps) != 0 {
		t.Fatal(ps)
	}
	if p := set.Lookup(e.Package); p == nil || p.Executable != e.Name {
		t.Fatalf("%s is not the executable of %s", e.Name, e.Package)
	}
}

// Every command name, as the executable is started under it, answers
// capabilities for that command and no other, with every operation
// unsupported.
func TestEachNameAnswersCapabilities(t *testing.T) {
	for _, c := range Executable().Commands {
		out, exit := invoke(t, "/some/bin/"+c.Name, nil, "capabilities")
		resp, err := envelope.AcceptCapabilities([]byte(out), exit)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		caps, err := envelope.DecodeCapabilities(resp.Body)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if exit != 0 || caps.Command != c.Name || caps.Executable != Executable().Name || caps.ImplementationVersion != Version {
			t.Errorf("%s: exit %d, declares %s of %s at %s", c.Name, exit, caps.Command, caps.Executable, caps.ImplementationVersion)
		}
		if len(caps.Operations) != len(c.Operations) {
			t.Errorf("%s: %d operations declared, %d owned", c.Name, len(caps.Operations), len(c.Operations))
		}
		for _, op := range caps.Operations {
			if op.State != envelope.Unsupported || op.Reason == "" || op.Mutates {
				t.Errorf("%s %s: state %s, reason %q, mutates %v", c.Name, op.Name, op.State, op.Reason, op.Mutates)
			}
		}
	}
}

// A name the executable does not own exits 2, whatever it is asked, and so
// does one of another package's commands.
func TestAForeignNameExitsTwo(t *testing.T) {
	for _, name := range []string{"hive-nothing", "hive-dispatch", "hive", "hive-" + Executable().Name} {
		for _, args := range [][]string{{"capabilities"}, {"--version"}, {"add", "--home", "x"}, {}} {
			if out, exit := invoke(t, name, nil, args...); exit != 2 {
				t.Errorf("%s %v: exit %d, output %q", name, args, exit, out)
			}
		}
	}
}

// An operation the command does not have exits 2 with unknown_operation, an
// operation named in the wrong form too, and so does one that belongs to
// another command of the package.
func TestAnUnknownOperationExitsTwo(t *testing.T) {
	e := Executable()
	for _, c := range e.Commands {
		for _, operation := range []string{"no-such-operation", "Add", "-", "--", strings.Repeat("a", 64)} {
			out, exit := invoke(t, c.Name, request(t, c.Name, "no-such-operation"), operation, "--home", t.TempDir())
			resp, err := envelope.AcceptResponse([]byte(out), exit, "req_00000000000000000000000000")
			if err != nil {
				t.Fatalf("%s %s: %v", c.Name, operation, err)
			}
			if exit != 2 || resp.Error == nil || resp.Error.Code != envelope.CodeUnknownOperation {
				t.Errorf("%s %s: exit %d, error %+v", c.Name, operation, exit, resp.Error)
			}
		}
		for _, other := range e.Commands {
			for _, op := range other.Operations {
				if other.Name == c.Name || slices.ContainsFunc(c.Operations, func(o *cli.Operation) bool { return o.Name == op.Name }) {
					continue
				}
				if out, exit := invoke(t, c.Name, request(t, c.Name, op.Name), op.Name, "--home", t.TempDir()); exit != 2 {
					t.Errorf("%s %s of %s: exit %d, output %q", c.Name, op.Name, other.Name, exit, out)
				}
			}
		}
	}
}

// A declared operation is refused as unsupported, writes nothing in the home
// it is pointed at, and a request addressed to another command or operation
// is invalid before the state of the operation is read.
func TestADeclaredOperationIsRefusedAndWritesNothing(t *testing.T) {
	for _, c := range Executable().Commands {
		for _, op := range c.Operations {
			home := t.TempDir()
			req := request(t, c.Name, op.Name)
			out, exit := invoke(t, c.Name, req, op.Name, "--home", home)
			id, _ := envelope.LooseRequestID(req)
			resp, err := envelope.AcceptResponse([]byte(out), exit, id)
			if err != nil {
				t.Fatalf("%s %s: %v", c.Name, op.Name, err)
			}
			if exit != 4 || resp.Error == nil || resp.Error.Code != envelope.CodeUnsupportedOperation || resp.Error.Retryable {
				t.Errorf("%s %s: exit %d, error %+v", c.Name, op.Name, exit, resp.Error)
			}
			if entries, _ := os.ReadDir(home); len(entries) != 0 {
				t.Errorf("%s %s left %d entries in the home", c.Name, op.Name, len(entries))
			}
			wrong := request(t, "hive-nothing", op.Name)
			out, exit = invoke(t, c.Name, wrong, op.Name, "--home", home)
			if resp, err := envelope.AcceptResponse([]byte(out), exit, mustID(t, wrong)); err != nil || exit != 3 || resp.Error.Code != envelope.CodeEnvelopeMismatch {
				t.Errorf("%s %s addressed to another command: exit %d, %v, %s", c.Name, op.Name, exit, err, out)
			}
		}
	}
}

// An invocation that is not one the contract describes is usage, exit 2:
// no home, a home option with no value and an argument over the limit.
func TestAMalformedInvocationIsUsage(t *testing.T) {
	for _, c := range Executable().Commands {
		op := c.Operations[0].Name
		long := strings.Repeat("x", envelope.MaxArgumentBytes+1)
		for _, args := range [][]string{{op}, {op, "--home"}, {op, "--home", long}, {"capabilities", "--home", "x"}, {"--version", "x"}, {}} {
			if out, exit := invoke(t, c.Name, request(t, c.Name, op), args...); exit != 2 {
				t.Errorf("%s %.40v: exit %d, output %q", c.Name, args, exit, out)
			}
		}
	}
}

// capabilities answers without a home and without reading standard input,
// and --version prints one line.
func TestCapabilitiesAndVersionNeedNothing(t *testing.T) {
	for _, c := range Executable().Commands {
		if _, exit := invoke(t, c.Name, nil, "capabilities"); exit != 0 {
			t.Errorf("%s capabilities: exit %d", c.Name, exit)
		}
		out, exit := invoke(t, c.Name, nil, "--version")
		if exit != 0 || out != Version+"\n" {
			t.Errorf("%s --version: exit %d, output %q", c.Name, exit, out)
		}
	}
}

// mustID reads the request_id of an encoded request.
func mustID(t testing.TB, req []byte) string {
	t.Helper()
	id, err := envelope.LooseRequestID(req)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
