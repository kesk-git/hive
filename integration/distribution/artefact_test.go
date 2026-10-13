package distribution

import (
	"runtime"
	"testing"

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
