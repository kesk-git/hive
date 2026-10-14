package distribution

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kesk-git/hive/integration/distribution/harness"
	"github.com/kesk-git/hive/integration/distribution/proof"
)

// The assembled artefact of this platform holds exactly what
// spec/distribution.md fixes, under the names and links of this platform, and
// its manifest agrees with its files. It catches an artefact that was never
// assembled, a missing, extra or misnamed file, an absolute, dangling or
// mistargeted link, and a manifest that disagrees with the files. The names
// and links it expects come from the package declarations alone.
func TestArtefactMatchesTheContract(t *testing.T) {
	root, err := proof.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := proof.FindArtefact(root)
	if err != nil {
		t.Fatal(err)
	}
	executables, links, err := proof.Declared(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(executables) != 7 || len(links) != 21 {
		t.Fatalf("the declarations name %d executables and %d links, want 7 and 21", len(executables), len(links))
	}
	for _, p := range proof.Layout(dir, proof.Want{Root: root, GOOS: runtime.GOOS, Executables: executables, Links: links}) {
		t.Error(p)
	}
}

// The identity the manifest states is the one the executables carry: the Go
// release, the platform and the revision of their build information, and the
// version, the contract version and the source revision each reports when it
// is run, through the launcher of the integration run, under a search path of
// the artefact's bin/ alone. It catches a manifest that states what the
// assembler assumed instead of what the executables report, and an executable
// built from a modified tree.
func TestArtefactIdentityIsTheExecutables(t *testing.T) {
	root, err := proof.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := proof.FindArtefact(root)
	if err != nil {
		t.Fatal(err)
	}
	executables, links, err := proof.Declared(root)
	if err != nil {
		t.Fatal(err)
	}
	var reports []proof.Reported
	for _, e := range executables {
		run := func(arg string) string {
			out, code, err := harness.Launch(t, filepath.Join(dir, "bin"), t.TempDir(), root, e, arg)
			if err != nil || code != 0 {
				t.Fatalf("%s %s: code %d, %v\n%s", e, arg, code, err, out)
			}
			return out
		}
		r, err := proof.ParseReported(e, run("--version"), run("capabilities"))
		if err != nil {
			t.Error(err)
			continue
		}
		reports = append(reports, r)
	}
	for _, p := range proof.Identity(dir, proof.Want{Root: root, GOOS: runtime.GOOS, Executables: executables, Links: links}, reports) {
		t.Error(p)
	}
}

// No executable of the artefact needs a library beyond the platform's own or
// was built with the C toolchain involved.
func TestArtefactExecutablesAreSelfContained(t *testing.T) {
	root, err := proof.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := proof.FindArtefact(root)
	if err != nil {
		t.Fatal(err)
	}
	executables, links, err := proof.Declared(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range proof.Binaries(dir, proof.Want{Root: root, GOOS: runtime.GOOS, Executables: executables, Links: links}) {
		t.Error(p)
	}
}
