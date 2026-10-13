package distribution

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kesk-git/hive/integration/distribution/fixture"
	"github.com/kesk-git/hive/integration/distribution/harness"
	"github.com/kesk-git/hive/integration/distribution/proof"
)

// The assemble command, run as `make assemble` runs it, on the seven real
// executables, reads each one's version and capabilities through
// contract/exec and its build information, and refuses to assemble executables
// built without version-control information, as from a source archive: their
// identity is unknown, and an unknown identity does not qualify. It catches an
// assembler that labels such a build, and a read of the executables that does
// not reach them. The sound assembly is the one `make assemble` makes and the
// checks of this target read.
func TestAssembleCommandRefusesAnUnknownIdentity(t *testing.T) {
	root := fixture.Root(t)
	real := repo(t)
	dist := filepath.Join(root, "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	executables, _, err := proof.Declared(real)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range executables {
		matches, _ := filepath.Glob(filepath.Join(real, "packages", "*", "cmd", e))
		if len(matches) != 1 {
			t.Fatalf("%s has %d main packages", e, len(matches))
		}
		rel, _ := filepath.Rel(real, matches[0])
		harness.Build(t, rel, filepath.Join(dist, e+harness.Suffix()), runtime.GOOS, runtime.GOARCH, true)
	}
	bin := t.TempDir()
	harness.Build(t, "integration/distribution/cmd/assemble", filepath.Join(bin, "assemble"+harness.Suffix()), runtime.GOOS, runtime.GOARCH, true)

	path := dist + string(os.PathListSeparator) + bin
	out, code, err := harness.Launch(t, path, t.TempDir(), root, "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 || !strings.Contains(out, "reports no source revision") {
		t.Errorf("the assembly of executables built without version-control information: code %d\n%s", code, out)
	}
	if left, _ := filepath.Glob(filepath.Join(dist, "hive-*-*-*")); len(left) != 0 {
		t.Errorf("an artefact was written: %v", left)
	}
}
