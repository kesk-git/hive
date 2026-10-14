// Package fixture builds, in a temporary directory, the inputs of an
// assembly and the artefact it produces, for the tests of the assembler and
// of the checks that read an artefact. Nothing here is built from Go: the
// seven executables are small files, so that the layout can be checked for
// any platform's rules on any host.
package fixture

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kesk-git/hive/integration/distribution/artefact"
	"github.com/kesk-git/hive/integration/distribution/proof"
)

// Suffix is the suffix of an executable's name on a platform.
func Suffix(goos string) string {
	if goos == "windows" {
		return ".exe"
	}
	return ""
}

// Fake is an Inspector that answers from a table.
type Fake map[string]artefact.Executable

// Inspect implements artefact.Inspector.
func (f Fake) Inspect(name, path string) (artefact.Executable, error) {
	return f[name], nil
}

// Good is seven executables that agree, built for the platform: a version
// that is not the one the code reports, so that a constant in the assembler
// shows.
func Good(executables []string, goos, goarch string) Fake {
	f := Fake{}
	for _, e := range executables {
		f[e] = artefact.Executable{
			Name: e, Version: "7.8.9", CapabilitiesVersion: "7.8.9", ContractVersion: "1.5",
			SourceRevision: "0123456789abcdef0123456789abcdef01234567",
			BuildRevision:  "0123456789abcdef0123456789abcdef01234567",
			GoRelease:      "go1.99.9", OS: goos, Arch: goarch,
		}
	}
	return f
}

// Root is a temporary repository root holding what an assembly reads of the
// repository: the package declarations, the LICENSE and the NOTICE, and the
// imports directory with every record the NOTICE is produced from.
func Root(t testing.TB) string {
	t.Helper()
	real, err := proof.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	copyTo := func(rel string) {
		data, err := os.ReadFile(filepath.Join(real, rel))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(real, "packages", "*", "hive-package.json"))
	for _, f := range files {
		rel, _ := filepath.Rel(real, f)
		copyTo(rel)
	}
	for _, rel := range []string{"contract/hive-package.json", "LICENSE", "NOTICE"} {
		copyTo(rel)
	}
	records, err := os.ReadDir(filepath.Join(real, "imports"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range records {
		if !e.IsDir() {
			copyTo("imports/" + e.Name())
		}
	}
	return root
}

// Dist is a temporary dist directory holding the seven executables of the
// repository under the names of the platform, each a different file.
func Dist(t testing.TB, root, goos string) string {
	t.Helper()
	executables, _, err := proof.Declared(root)
	if err != nil {
		t.Fatal(err)
	}
	dist := t.TempDir()
	for _, e := range executables {
		if err := os.WriteFile(filepath.Join(dist, e+Suffix(goos)), []byte("executable "+e+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dist
}

// Artefact assembles an artefact for the platform in a temporary directory
// and returns it with what the checks expect of it.
func Artefact(t testing.TB, goos string) (dir string, want proof.Want, root, dist string) {
	t.Helper()
	root = Root(t)
	dist = Dist(t, root, goos)
	executables, links, err := proof.Declared(root)
	if err != nil {
		t.Fatal(err)
	}
	dir, err = artefact.Assemble(artefact.Options{
		Root: root, Dist: dist, GOOS: goos, GOARCH: "amd64",
		Inspect: Good(executables, goos, "amd64"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir, proof.Want{Root: root, GOOS: goos, Executables: executables, Links: links}, root, dist
}
