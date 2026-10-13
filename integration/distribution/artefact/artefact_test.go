package artefact_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kesk-git/hive/integration/distribution/artefact"
	"github.com/kesk-git/hive/integration/distribution/fixture"
	"github.com/kesk-git/hive/integration/distribution/proof"
)

var platforms = []string{"linux", "darwin", "windows"}

// needsLinks skips a fixture whose rules need a symbolic link where the host
// cannot make one: on Windows the artefact of Windows is the one that
// matters and the real artefact is checked by its own rules.
func needsLinks(t *testing.T, goos string) {
	t.Helper()
	if goos != "windows" && runtime.GOOS == "windows" {
		t.Skip("a symbolic-link fixture needs a host that makes one; the Windows artefact is checked by its own rules")
	}
}

// An assembly of seven executables that agree writes an artefact that
// satisfies the layout of its platform, with the identity the executables
// report. The version the executables report is 7.8.9: an assembler that
// carries a version of its own, or any other constant, names the wrong
// directory and the wrong manifest.
func TestAssembleWritesTheArtefactOfEachPlatform(t *testing.T) {
	for _, goos := range platforms {
		t.Run(goos, func(t *testing.T) {
			needsLinks(t, goos)
			dir, want, _, _ := fixture.Artefact(t, goos)
			if got, wantName := filepath.Base(dir), "hive-7.8.9-"+goos+"-amd64"; got != wantName {
				t.Errorf("the directory is %s, want %s", got, wantName)
			}
			for _, p := range proof.Layout(dir, want) {
				t.Error(p)
			}
			data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			for k, v := range map[string]string{
				"hive_version": "7.8.9", "contract_version": "1.5", "go_release": "go1.99.9", "os": goos, "arch": "amd64",
				"source_revision": "0123456789abcdef0123456789abcdef01234567",
			} {
				if m[k] != v {
					t.Errorf("manifest %s is %v, want %s", k, m[k], v)
				}
			}
		})
	}
}

// Where a platform without hard links cannot make one, the link is a whole
// copy that satisfies the same layout.
func TestAssembleFallsBackToACopyWhereNoHardLinkCanBeMade(t *testing.T) {
	defer artefact.SetHardLinkForTest(func(string, string) error { return errors.New("no hard links here") })()
	dir, want, _, _ := fixture.Artefact(t, "windows")
	for _, p := range proof.Layout(dir, want) {
		t.Error(p)
	}
	exe, link := filepath.Join(dir, "bin", "hive-pool.exe"), filepath.Join(dir, "bin", "hive-admit.exe")
	a, _ := os.Stat(exe)
	b, _ := os.Stat(link)
	if os.SameFile(a, b) {
		t.Errorf("the link is a hard link although none could be made")
	}
}

// An assembly that finds the executables disagree, or find an identity
// unknown or modified, writes no artefact and says why. Each case changes one
// thing of an otherwise sound set of seven.
func TestAssembleRefuses(t *testing.T) {
	host := runtime.GOOS
	cases := []struct {
		name   string
		expect string
		change func(e *artefact.Executable)
	}{
		{"a version that differs", `reports the version "7.8.8"`, func(e *artefact.Executable) { e.Version, e.CapabilitiesVersion = "7.8.8", "7.8.8" }},
		{"capabilities that name another version than --version", "with --version and", func(e *artefact.Executable) { e.CapabilitiesVersion = "7.8.8" }},
		{"a contract version that differs", "contract version", func(e *artefact.Executable) { e.ContractVersion = "1.4" }},
		{"a source revision that is unknown", "reports no source revision", func(e *artefact.Executable) { e.SourceRevision, e.BuildRevision = "", "" }},
		{"a source revision that differs", "reports the source revision", func(e *artefact.Executable) {
			e.SourceRevision, e.BuildRevision = "ffffffffffffffffffffffffffffffffffffffff", "ffffffffffffffffffffffffffffffffffffffff"
		}},
		{"a revision that its build information does not name", "its build information names", func(e *artefact.Executable) { e.BuildRevision = "" }},
		{"a modified tree", "vcs.modified is true", func(e *artefact.Executable) { e.Modified = true }},
		{"a Go release that differs", "was built with go1.98.0", func(e *artefact.Executable) { e.GoRelease = "go1.98.0" }},
		{"another platform", "was built for", func(e *artefact.Executable) { e.OS = "plan9" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := fixture.Root(t)
			dist := fixture.Dist(t, root, host)
			executables, _, _ := proof.Declared(root)
			insp := fixture.Good(executables, host, runtime.GOARCH)
			e := insp["hive-synapse"]
			c.change(&e)
			insp["hive-synapse"] = e
			_, err := artefact.Assemble(artefact.Options{Root: root, Dist: dist, Inspect: insp})
			if err == nil || !strings.Contains(err.Error(), c.expect) {
				t.Errorf("error %v, want one that says %q", err, c.expect)
			}
			assertNoArtefact(t, dist)
		})
	}
}

func assertNoArtefact(t *testing.T, dist string) {
	t.Helper()
	left, _ := filepath.Glob(filepath.Join(dist, "hive-*-*-*"))
	for _, l := range left {
		if fi, err := os.Stat(l); err == nil && fi.IsDir() {
			t.Errorf("%s was written although the assembly was refused", l)
		}
	}
}

// An executable that dist/ does not hold, and a NOTICE that is not the one the
// records produce, each refuse the assembly: the artefact would otherwise
// carry a file that does not say what it must.
func TestAssembleRefusesAMissingExecutableAndAStaleNotice(t *testing.T) {
	root := fixture.Root(t)
	dist := fixture.Dist(t, root, runtime.GOOS)
	executables, _, _ := proof.Declared(root)
	insp := fixture.Good(executables, runtime.GOOS, runtime.GOARCH)

	if err := os.Remove(filepath.Join(dist, "hive-ui"+fixture.Suffix(runtime.GOOS))); err != nil {
		t.Fatal(err)
	}
	if _, err := artefact.Assemble(artefact.Options{Root: root, Dist: dist, Inspect: insp}); err == nil || !strings.Contains(err.Error(), "hive-ui") {
		t.Errorf("a missing executable: %v", err)
	}
	assertNoArtefact(t, dist)

	dist = fixture.Dist(t, root, runtime.GOOS)
	if err := os.WriteFile(filepath.Join(root, "NOTICE"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := artefact.Assemble(artefact.Options{Root: root, Dist: dist, Inspect: insp}); err == nil || !strings.Contains(err.Error(), "NOTICE") {
		t.Errorf("a stale NOTICE: %v", err)
	}
	assertNoArtefact(t, dist)
}

// Running the assembly again replaces the directory: what an earlier run or a
// person left in it is gone.
func TestAssembleReplacesTheDirectory(t *testing.T) {
	needsLinks(t, runtime.GOOS)
	dir, want, root, dist := fixture.Artefact(t, runtime.GOOS)
	stray := filepath.Join(dir, "bin", "stray")
	if err := os.WriteFile(stray, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := artefact.Assemble(artefact.Options{Root: root, Dist: dist, GOOS: runtime.GOOS, GOARCH: "amd64", Inspect: fixture.Good(want.Executables, runtime.GOOS, "amd64")})
	if err != nil {
		t.Fatal(err)
	}
	if again != dir {
		t.Fatalf("the second assembly is at %s, the first at %s", again, dir)
	}
	if _, err := os.Lstat(stray); err == nil {
		t.Errorf("the stray file survived the second assembly")
	}
	for _, p := range proof.Layout(dir, want) {
		t.Error(p)
	}
}
