package distribution

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/integration/distribution/harness"
	"github.com/kesk-git/hive/integration/distribution/proof"
)

// restricted makes the world of an integration run for the artefact of this
// platform: its bin/, the recorded fakes and the real Git's directory.
func restricted(t *testing.T) (harness.Restricted, string) {
	t.Helper()
	root := repo(t)
	dir, err := proof.FindArtefact(root)
	if err != nil {
		t.Fatal(err)
	}
	return harness.NewRestricted(t, dir), root
}

// listed says which of the directories, in order, holds a file of the name,
// read from the directory's own listing and not by trying to open the file.
func listed(t *testing.T, dirs []string, name string) string {
	t.Helper()
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Name() == name+harness.Suffix() && !e.IsDir() {
				return dir
			}
		}
	}
	return ""
}

// What the restricted search path reaches is its three directories and
// nothing else: the probe, started under that path by the same launcher the
// integration run uses, finds each of the programs in the first of the
// artefact's bin/, the fakes and Git's directory that holds it, and the path
// it reports is exactly those three. It catches a path that keeps the
// toolchain's directory or any of the test's own, a program that is found
// where the listing does not hold it, and one the listing holds that is not
// found. The probe starts no program.
func TestRestrictedSearchPathReachesOnlyItsThreeDirectories(t *testing.T) {
	r, root := restricted(t)
	harness.Build(t, "integration/distribution/testdata/probe", filepath.Join(r.Fakes, "integration-probe"+harness.Suffix()), runtime.GOOS, runtime.GOARCH, true)

	executables, links, err := proof.Declared(root)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"git", "gh", "claude", "codex", "no-mistakes", "security", "hive-nonexistent"}
	names = append(names, executables...)
	for l := range links {
		names = append(names, l)
	}
	// Programs of a development machine that no delivered Hive reaches.
	names = append(names, "go", "make", "gcc", "sh", "bash", "cmd", "powershell", "node", "npm", "python3", "tmux", "curl")

	out, code, err := r.Run(t, root, "integration-probe", names...)
	if err != nil || code != 0 {
		t.Fatalf("the probe: code %d, %v\n%s", code, err, out)
	}
	var report struct {
		Path  string            `json:"path"`
		Found map[string]string `json:"found"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &report); err != nil {
		t.Fatalf("the probe's report: %v\n%s", err, out)
	}
	if report.Path != r.Path() {
		t.Errorf("the run's search path is %q, want exactly %q", report.Path, r.Path())
	}
	dirs := []string{r.Bin, r.Fakes, r.Git}
	found := 0
	for _, name := range names {
		want := listed(t, dirs, name)
		if got := report.Found[name]; got != want {
			t.Errorf("%s is found in %q, the listing of the three directories says %q", name, got, want)
		}
		if want != "" {
			found++
		}
	}
	if found < 1+4+7+21 || report.Found["hive-nonexistent"] != "" {
		t.Errorf("the probe found %d names: git, the four fakes, the seven executables and the twenty-one links are %d", found, 1+4+7+21)
	}
	// What each of the five is, and where, on every platform.
	for _, name := range []string{"gh", "claude", "codex", "no-mistakes"} {
		if report.Found[name] != r.Fakes {
			t.Errorf("%s is found in %q, not in the fakes %q", name, report.Found[name], r.Fakes)
		}
	}
	if report.Found["git"] != r.Git {
		t.Errorf("git is found in %q, not in %q", report.Found["git"], r.Git)
	}
	if report.Found["hive-pool"] != r.Bin || report.Found["hive-admit"] != r.Bin {
		t.Errorf("the artefact's executables are found in %q and %q, not in %q", report.Found["hive-pool"], report.Found["hive-admit"], r.Bin)
	}
	switch runtime.GOOS {
	case "darwin":
		if report.Found["security"] != r.Fakes {
			t.Errorf("on macOS security is the recorded fake, found in %q", report.Found["security"])
		}
	case "linux", "windows":
		if report.Found["security"] == r.Fakes {
			t.Errorf("on %s no fake of security stands in: the Linux and Windows store adapters run in process", runtime.GOOS)
		}
	}
}

// executableMagic says whether data begins with the header of an executable of
// the platform: ELF, Mach-O or PE, and never a script's #! line.
func executableMagic(goos string, data []byte) bool {
	switch goos {
	case "linux":
		return bytes.HasPrefix(data, []byte("\x7fELF"))
	case "windows":
		return bytes.HasPrefix(data, []byte("MZ"))
	case "darwin":
		for _, m := range [][]byte{{0xcf, 0xfa, 0xed, 0xfe}, {0xce, 0xfa, 0xed, 0xfe}, {0xca, 0xfe, 0xba, 0xbe}} {
			if bytes.HasPrefix(data, m) {
				return true
			}
		}
	}
	return false
}

// Each recorded fake is an executable the test built from Go, found by the
// lookup rules and started through the one path; it records the arguments it
// was given and exits 0. It catches a fake that is a script, one the lookup
// rules do not find, and one that records nothing.
func TestFakesAreRecordedExecutables(t *testing.T) {
	r, root := restricted(t)
	for _, name := range harness.FakeNames(runtime.GOOS) {
		file := filepath.Join(r.Fakes, name+harness.Suffix())
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !executableMagic(runtime.GOOS, data) {
			t.Errorf("%s does not begin with the header of a %s executable: %q", name, runtime.GOOS, data[:min(len(data), 4)])
		}
		out, code, err := r.Run(t, root, name, "one", "--two")
		if err != nil || code != 0 {
			t.Errorf("%s: code %d, %v\n%s", name, code, err, out)
			continue
		}
		rec, err := os.ReadFile(filepath.Join(r.Fakes, name+".record"))
		if err != nil || strings.TrimSpace(string(rec)) != `["one","--two"]` {
			t.Errorf("%s recorded %q, %v", name, rec, err)
		}
	}
	// On Linux and Windows no fake of the platform utility exists.
	if !slices.Contains(harness.FakeNames(runtime.GOOS), "security") {
		if _, err := os.Stat(filepath.Join(r.Fakes, "security"+harness.Suffix())); err == nil {
			t.Errorf("a fake of security stands in on %s", runtime.GOOS)
		}
	}
}

// The integration tests pass when run, compiled, with a search path that holds
// only the artefact's bin/, the recorded fakes and the real Git, in a home of
// their own. The tests are in integration/calls, which is another lane's: while
// it holds none this case fails and says it has nothing to run.
func TestIntegrationTestsPassUnderTheRestrictedSearchPath(t *testing.T) {
	r, _ := restricted(t)
	out, err := r.Integration(t)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
