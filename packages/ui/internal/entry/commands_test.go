package entry

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/kesk-git/hive/contract/declare"
	"github.com/kesk-git/hive/contract/envelope"
)

// The package, its executable and the path of its main package, relative to
// the repository root.
const (
	packageName = "ui"
	executable  = "hive-ui"
	mainPackage = "./packages/ui/cmd/hive-ui"
)

// A name no command of this package has, and the name of a command another
// package owns.
var foreign = []string{"hive-nothing", "hive-state"}

var (
	buildOnce sync.Once
	buildDir  string
	buildErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if buildDir != "" {
		os.RemoveAll(buildDir)
	}
	os.Exit(code)
}

// program is the file name a command name is started under.
func program(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// goTool finds the go command that runs this test.
func goTool() (string, error) {
	if path, err := exec.LookPath("go"); err == nil {
		return path, nil
	}
	path := filepath.Join(runtime.GOROOT(), "bin", program("go"))
	if _, err := os.Stat(path); err != nil {
		return "", errors.New("the go command is not found")
	}
	return path, nil
}

// declared is the package's declaration: each command it owns with the
// operations it lists.
func declared(t testing.TB) []declare.Command {
	t.Helper()
	set := ownSet(t)
	p := set.Lookup(packageName)
	if p == nil {
		t.Fatalf("%s is not a declared package", packageName)
	}
	return p.Commands
}

// bin builds the executable once into a temporary directory and links every
// command name of the package to it, and returns the directory. The links
// are what spec/distribution.md fixes: on macOS and Linux a relative
// symbolic link in the same directory; on Windows a hard link named
// <name>.exe, or a copy where the file system cannot link.
func bin(t testing.TB) string {
	t.Helper()
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", executable+"-test")
		if buildErr != nil {
			return
		}
		var goBin string
		if goBin, buildErr = goTool(); buildErr != nil {
			return
		}
		cmd := exec.Command(goBin, "build", "-o", filepath.Join(buildDir, program(executable)), mainPackage)
		cmd.Dir = "../../../.."
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("building %s: %v\n%s", mainPackage, err, out)
			return
		}
		for _, name := range append(commandNames(t), foreign...) {
			if name == executable {
				continue
			}
			if buildErr = link(buildDir, name); buildErr != nil {
				return
			}
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return buildDir
}

func commandNames(t testing.TB) []string {
	var names []string
	for _, c := range declared(t) {
		names = append(names, c.Name)
	}
	return names
}

// link makes name a link to the executable in dir.
func link(dir, name string) error {
	target := filepath.Join(dir, program(executable))
	path := filepath.Join(dir, program(name))
	if runtime.GOOS != "windows" {
		return os.Symlink(program(executable), path)
	}
	if err := os.Link(target, path); err == nil {
		return nil
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o755)
}

// invoke starts the program under the command name, with the arguments and
// the standard input, and returns its exit code and standard output.
func invoke(t testing.TB, name string, stdin []byte, args ...string) (int, []byte) {
	t.Helper()
	cmd := exec.Command(filepath.Join(bin(t), program(name)), args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatalf("%s %v: %v", name, args, err)
	}
	return cmd.ProcessState.ExitCode(), stdout.Bytes()
}

// Every command name the package owns answers capabilities, as the command
// it was started under, with the operations its declaration lists and
// the package's executable.
func TestEveryCommandNameAnswersCapabilities(t *testing.T) {
	for _, c := range declared(t) {
		t.Run(c.Name, func(t *testing.T) {
			code, out := invoke(t, c.Name, nil, "capabilities")
			resp, err := envelope.AcceptCapabilities(out, code)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Status != envelope.Ok || code != 0 {
				t.Fatalf("status %s, exit %d", resp.Status, code)
			}
			if resp.Command != c.Name || resp.Responder.Command != c.Name || resp.Responder.Package != packageName {
				t.Errorf("answered as %s by %s of %s", resp.Command, resp.Responder.Command, resp.Responder.Package)
			}
			caps, err := envelope.DecodeCapabilities(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if caps.Command != c.Name || caps.Package != packageName || caps.Executable != executable {
				t.Errorf("declares %s of %s in %s", caps.Command, caps.Package, caps.Executable)
			}
			var ops []string
			for _, op := range caps.Operations {
				ops = append(ops, op.Name)
				if op.State != envelope.Unsupported || op.Reason == "" {
					t.Errorf("%s is %s (%q), and no operation is built yet", op.Name, op.State, op.Reason)
				}
			}
			if want := slices.Sorted(slices.Values(c.Operations)); !slices.Equal(slices.Sorted(slices.Values(ops)), want) {
				t.Errorf("operations %v; the declaration lists %v", ops, want)
			}
		})
	}
}

// An operation a command does not declare is a usage error, exit 2.
func TestAnUnknownOperationIsRefused(t *testing.T) {
	for _, c := range declared(t) {
		t.Run(c.Name, func(t *testing.T) {
			code, out := invoke(t, c.Name, nil, "burn", "--home", t.TempDir())
			if code != envelope.Usage.ExitCode() {
				t.Fatalf("exit %d, want %d", code, envelope.Usage.ExitCode())
			}
			resp, err := envelope.AcceptResponse(out, code, "req_00000000000000000000000000")
			if err != nil {
				t.Fatal(err)
			}
			if resp.Status != envelope.Usage || resp.Error.Code != envelope.CodeUnknownOperation || resp.Command != c.Name {
				t.Errorf("%s %s by %s", resp.Status, resp.Error.Code, resp.Command)
			}
		})
	}
}

// A name the executable does not own exits 2, whether it is another
// package's command or none at all.
func TestANameNotOwnedExitsTwo(t *testing.T) {
	for _, name := range foreign {
		t.Run(name, func(t *testing.T) {
			code, out := invoke(t, name, nil, "capabilities")
			if code != envelope.Usage.ExitCode() {
				t.Fatalf("exit %d, want %d", code, envelope.Usage.ExitCode())
			}
			resp, err := envelope.AcceptResponse(out, code, "req_00000000000000000000000000")
			if err != nil {
				t.Fatal(err)
			}
			if resp.Status != envelope.Usage || resp.Error.Code != envelope.CodeUsage {
				t.Errorf("%s %s", resp.Status, resp.Error.Code)
			}
		})
	}
}

// --version prints the implementation version as one line, under every
// name, and exits 0.
func TestVersionPrintsOneLine(t *testing.T) {
	var first string
	for _, name := range commandNames(t) {
		code, out := invoke(t, name, nil, "--version")
		line := string(out)
		if code != 0 || !strings.HasSuffix(line, "\n") || strings.TrimSuffix(line, "\n") == "" || strings.Count(line, "\n") != 1 {
			t.Errorf("%s --version: exit %d, output %q", name, code, line)
		}
		if first == "" {
			first = line
		} else if line != first {
			t.Errorf("%s reports %q, not %q", name, line, first)
		}
	}
}
