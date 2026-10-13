package artefact

import (
	"debug/buildinfo"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/exec"
)

// Process reads an executable by running it through contract/exec, the one
// process-execution path, and by reading the build information the Go
// toolchain embedded in it, without running go. The executable is found on
// the search path the process was started with, so the directory that holds
// it must be on that path (`make assemble` puts dist/ first); an executable
// found anywhere else is refused.
type Process struct{}

// Inspect implements Inspector.
func (Process) Inspect(name, path string) (Executable, error) {
	e := Executable{Name: name}
	out, err := runHive(name, path, "--version")
	if err != nil {
		return e, err
	}
	e.Version = strings.TrimSuffix(strings.TrimSuffix(string(out), "\n"), "\r")
	out, err = runHive(name, path, "capabilities")
	if err != nil {
		return e, err
	}
	resp, err := envelope.AcceptCapabilities(out, 0)
	if err != nil {
		return e, fmt.Errorf("its capabilities are not a complete response: %w", err)
	}
	caps, err := envelope.DecodeCapabilities(resp.Body)
	if err != nil {
		return e, fmt.Errorf("its capabilities cannot be read: %w", err)
	}
	e.CapabilitiesVersion = caps.ImplementationVersion
	e.ContractVersion = caps.ContractVersion
	e.SourceRevision = caps.SourceRevision

	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return e, fmt.Errorf("its build information cannot be read: %w", err)
	}
	e.GoRelease = info.GoVersion
	for _, s := range info.Settings {
		switch s.Key {
		case "GOOS":
			e.OS = s.Value
		case "GOARCH":
			e.Arch = s.Value
		case "vcs.revision":
			e.BuildRevision = s.Value
		case "vcs.modified":
			e.Modified = s.Value == "true"
		}
	}
	return e, nil
}

// runHive runs the executable under its own name with the arguments, through
// contract/exec, and returns what it printed. It refuses a run whose program
// is not the file at path.
func runHive(name, path string, args ...string) ([]byte, error) {
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	run, err := exec.Start(exec.Spec{
		Program: exec.Program{Kind: exec.Hive, Name: name},
		Args:    args,
		Dir:     dir,
		Stdin:   exec.Null(), Stdout: exec.Pipe(), Stderr: exec.Pipe(),
	})
	if err != nil {
		return nil, fmt.Errorf("%v (the directory of the executables must be first on the search path: use `make assemble`)", err)
	}
	defer run.Close()
	if !sameFile(run.Path, path) {
		return nil, fmt.Errorf("the search path finds %s, not %s: the executable of the artefact is the one in dist/", run.Path, path)
	}
	errs := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(io.LimitReader(run.Stderr(), 1<<16)); errs <- b }()
	out, rerr := io.ReadAll(io.LimitReader(run.Stdout(), 1<<24))
	diag := <-errs
	res := run.Wait()
	if rerr != nil || !res.Passed() {
		return nil, fmt.Errorf("%s %s: %s %s", name, strings.Join(args, " "), res, strings.TrimSpace(string(diag)))
	}
	return out, nil
}

func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	return err == nil && os.SameFile(fa, fb)
}
