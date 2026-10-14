package proof_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/integration/distribution/proof"
)

func scanRoot(name string) string { return filepath.Join("..", "testdata", "scan", name) }

// hits says which of the findings are in a package of the fixture, by the
// package's directory name.
func hits(findings []string, pkg string) []string {
	var out []string
	for _, f := range findings {
		if strings.Contains(f, "packages/"+pkg+"/") || strings.Contains(f, "packages/"+pkg+" ") || strings.HasPrefix(f, pkg+" ") {
			out = append(out, f)
		}
	}
	return out
}

func mustFind(t *testing.T, findings []string, pkg, expect string) {
	t.Helper()
	got := hits(findings, pkg)
	if !strings.Contains(strings.Join(got, "\n"), expect) {
		t.Errorf("%s: no finding says %q; findings: %q", pkg, expect, got)
	}
}

// A module whose packages run only what they declare, through contract/exec,
// and whose words, comments and platform-specific third-party import, which
// its package declares as a library with its record, are all in order, passes
// every source check. It catches a check that rejects sound
// code, a word that is also a call name among them.
func TestSourceChecksPassSoundCode(t *testing.T) {
	root := scanRoot("good")
	starts, err := proof.Starts(root)
	if err != nil || len(starts) != 0 {
		t.Errorf("starts: %q %v", starts, err)
	}
	programs, err := proof.Programs(root)
	if err != nil || len(programs) != 0 {
		t.Errorf("programs: %q %v", programs, err)
	}
	platform, err := proof.PlatformUtilities(root)
	if err != nil || len(platform) != 0 {
		t.Errorf("platform: %q %v", platform, err)
	}
	libraries, err := proof.Libraries(root)
	if err != nil || len(libraries) != 0 {
		t.Errorf("libraries: %q %v", libraries, err)
	}
}

// Every way of starting a process outside contract/exec is found, in the
// package that does it and only there; contract/exec, which holds the
// imports and calls, is not reported.
func TestStartsFindsEveryWayOfStartingAProcess(t *testing.T) {
	findings, err := proof.Starts(scanRoot("breaks"))
	if err != nil {
		t.Fatal(err)
	}
	mustFind(t, findings, "osexec", "imports os/exec")
	mustFind(t, findings, "renamedexec", "imports os/exec")
	mustFind(t, findings, "startprocess", "uses os.StartProcess")
	mustFind(t, findings, "forkexec", "uses syscall.ForkExec")
	mustFind(t, findings, "syscallnumber", "uses syscall.SYS_FORK")
	mustFind(t, findings, "lazydll", "names the call CreateProcessW")
	mustFind(t, findings, "spliced", "names the call CreateProcessW")
	mustFind(t, findings, "loader", "looks up the call system")
	mustFind(t, findings, "loader", "names the call posix_spawn")
	mustFind(t, findings, "cgo", "imports C")
	mustFind(t, findings, "dotimport", "uses os.StartProcess through a dot import")
	for _, f := range findings {
		if strings.Contains(f, "contract/exec") {
			t.Errorf("contract/exec is reported: %s", f)
		}
	}
	for _, pkg := range []string{"undeclared", "hivecall", "platformfive", "keyringmac", "thirdparty"} {
		if got := hits(findings, pkg); len(got) != 0 {
			t.Errorf("%s starts nothing and is reported: %q", pkg, got)
		}
	}
}

// A package that starts a program its declaration does not list is found:
// another core program, a Hive command it declares no call to, a program of
// the wrong kind, and a program whose name the scan cannot read.
func TestProgramsFindsAProgramThatIsNotDeclared(t *testing.T) {
	findings, err := proof.Programs(scanRoot("breaks"))
	if err != nil {
		t.Fatal(err)
	}
	mustFind(t, findings, "undeclared", `core program "gh", which the declaration of undeclared does not list`)
	mustFind(t, findings, "hivecall", `hive program "hive-pool"`)
	mustFind(t, findings, "wrongkind", `transitional program "git"`)
	mustFind(t, findings, "unreadable", "name of this Core program cannot be read")
	mustFind(t, findings, "unreadablerun", "not a literal or a name the scan can follow")
	if len(findings) != 5 {
		t.Errorf("%d findings, want one for each of the five breaks: %q", len(findings), findings)
	}
}

// A platform list with no reason, or that names one of the five, is found.
func TestPlatformUtilitiesFindsAListThatIsNotAllowed(t *testing.T) {
	findings, err := proof.PlatformUtilities(scanRoot("breaks"))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("%q", findings)
	}
	mustFind(t, findings, "platformfive", "one of the five")
	mustFind(t, findings, "noreason", "without a reason")
}

// A third-party module is linked only where it is declared: the package that
// imports it lists it under libraries, with the import record that is the
// dependency record of that module, and the record names the starts of a
// process. A package that imports a module it does not list, one that lists it
// with the record of another module, one that lists a module with no record,
// and a file that imports one outside every package are each found, as is a
// package that builds for macOS and imports go-keyring. It catches a check
// that takes a record alone, or another package's declaration, for a
// declaration of the package that links the module.
func TestLibrariesFindsAnExceptionUsedWrongly(t *testing.T) {
	findings, err := proof.Libraries(scanRoot("breaks"))
	if err != nil {
		t.Fatal(err)
	}
	mustFind(t, findings, "keyringmac", "no macOS executable holds")
	mustFind(t, findings, "keyringmac", "does not name the starts of a process")
	mustFind(t, findings, "thirdparty", "example.org/anything, which has no dependency record")
	mustFind(t, findings, "unlisted", "which the declaration of unlisted does not list under libraries")
	mustFind(t, findings, "wrongrecord", `names the import record "other-dep", which is not the dependency record of github.com/zalando/go-keyring`)
	outside := "tools/helper/keyring_linux.go imports github.com/zalando/go-keyring, and is in no package whose declaration lists it under libraries"
	if !slices.Contains(findings, outside) {
		t.Errorf("no finding says %q; findings: %q", outside, findings)
	}
	if len(findings) != 6 {
		t.Errorf("%d findings, want two for keyringmac and one for each of the other four breaks: %q", len(findings), findings)
	}
}

// A build file of another toolchain is found wherever it sits in the tree,
// whatever the case of its name, except in the directories no scan reads; the
// repository's own Makefile and go.mod are not found.
func TestOtherToolchainsFindsABuildFile(t *testing.T) {
	findings, err := proof.OtherToolchains(filepath.Join("..", "testdata", "toolchains", "breaks"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"package.json", "sub/requirements.txt", "sub/deep/pyproject.toml", "Cargo.toml", "Pom.xml", "build.gradle", "CMakeLists.txt", "app.csproj"}
	if len(findings) != len(want) {
		t.Errorf("%d findings, want %d: %q", len(findings), len(want), findings)
	}
	for _, w := range want {
		found := false
		for _, f := range findings {
			found = found || strings.HasPrefix(f, w+" ")
		}
		if !found {
			t.Errorf("%s is not found: %q", w, findings)
		}
	}
	for _, f := range findings {
		if strings.Contains(f, "testdata") || strings.Contains(f, "dist/") {
			t.Errorf("a directory no scan reads is read: %s", f)
		}
	}
	good, err := proof.OtherToolchains(filepath.Join("..", "testdata", "toolchains", "good"))
	if err != nil || len(good) != 0 {
		t.Errorf("a tree with only the repository's own build files and words in other names: %q %v", good, err)
	}
}
