package distribution

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/integration/distribution/harness"
	"github.com/kesk-git/hive/integration/distribution/proof"
)

// restricted makes the world of an integration run for the artefact of this
// platform: its bin/, the recorded fakes and, alone in a directory, the git
// that starts the real Git.
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
// artefact's bin/, the fakes and the directory of git alone that holds it, and
// the path it reports is exactly those three. It catches a path that keeps the
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
	for _, name := range []string{"go", "make", "gcc", "sh", "bash", "cmd", "powershell", "node", "npm", "python3", "tmux", "curl", "hive-nonexistent"} {
		if got := report.Found[name]; got != "" {
			t.Errorf("%s, a program beyond the listed ones, is reachable in %q", name, got)
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

// Each test-only callee the integration tests start is an executable the test
// built from Go and placed in the directory of the fakes, under both names,
// where the run's search path finds it. It catches a callee that is missing, a
// script, or one that is not in the directory the run names for it.
func TestCalleesAreExecutablesInTheFakesDirectory(t *testing.T) {
	r, _ := restricted(t)
	for _, name := range harness.Callees {
		file := filepath.Join(r.Fakes, name+harness.Suffix())
		fi, err := os.Stat(file)
		if err != nil || !fi.Mode().IsRegular() {
			t.Errorf("%s is not a file in the fakes directory: %v", name, err)
			continue
		}
		if runtime.GOOS != "windows" && fi.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s is not executable: %v", name, fi.Mode())
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !executableMagic(runtime.GOOS, data) {
			t.Errorf("%s does not begin with the header of a %s executable: %q", name, runtime.GOOS, data[:min(len(data), 4)])
		}
	}
	if dir := listed(t, []string{r.Bin, r.Fakes, r.Git}, "hive-callee"); dir != r.Fakes {
		t.Errorf("hive-callee is found in %q, not in the fakes %q", dir, r.Fakes)
	}
}

// The two variables that name the directories of the executables and of the
// callees reach the program the launcher starts, with the artefact's bin/ and
// the fakes directory as their values, and nothing else of the test's
// environment does. The probe prints the environment it was started with.
func TestIntegrationVariablesReachTheChild(t *testing.T) {
	r, root := restricted(t)
	harness.Build(t, "integration/distribution/testdata/probe", filepath.Join(r.Fakes, "integration-probe"+harness.Suffix()), runtime.GOOS, runtime.GOARCH, true)
	t.Setenv("HIVE_TEST_LEAK", "must-not-reach-the-child")

	out, code, err := r.RunWithDirectories(t, root, "integration-probe")
	if err != nil || code != 0 {
		t.Fatalf("the probe: code %d, %v\n%s", code, err, out)
	}
	var report struct {
		Environ []string `json:"environ"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &report); err != nil {
		t.Fatalf("the probe's report: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, kv := range report.Environ {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	if got["HIVE_TEST_EXECUTABLES_DIR"] != r.Bin {
		t.Errorf("HIVE_TEST_EXECUTABLES_DIR is %q, want the artefact's bin/ %q", got["HIVE_TEST_EXECUTABLES_DIR"], r.Bin)
	}
	if got["HIVE_TEST_CALLEES_DIR"] != r.Fakes {
		t.Errorf("HIVE_TEST_CALLEES_DIR is %q, want the fakes directory %q", got["HIVE_TEST_CALLEES_DIR"], r.Fakes)
	}
	if v, ok := got["HIVE_TEST_LEAK"]; ok {
		t.Errorf("a variable the launcher was not told to pass reached the child: HIVE_TEST_LEAK=%q", v)
	}
	if got["PATH"] != r.Path() {
		t.Errorf("the child's search path is %q, want exactly %q", got["PATH"], r.Path())
	}

	// A launch with no variable gives the child none of them.
	out, code, err = r.Run(t, root, "integration-probe")
	if err != nil || code != 0 {
		t.Fatalf("the probe: code %d, %v\n%s", code, err, out)
	}
	if strings.Contains(out, "HIVE_TEST_") {
		t.Errorf("a plain run gives the child a test variable:\n%s", out)
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

// The git of the restricted search path is one executable the test built,
// alone in its directory, and what it runs is the real Git where that is
// installed: neither a link to the real Git nor a copy of it, which an
// installation that finds its own parts beside it does not survive. Started
// by its bare name under the restricted search path it makes a repository and
// a commit and names the commit, which no fake does; what the real Git prints
// on both of its output streams and the code it exits with pass through
// unchanged, and so does a standard input that holds nothing. It catches a
// directory that holds anything else, and a relay that loses a stream, the
// working directory, the home or the exit code.
func TestGitOfTheRestrictedPathStartsTheRealGit(t *testing.T) {
	realDir, err := harness.GitDir()
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(realDir, "git"+harness.Suffix())
	r, _ := restricted(t)
	entries, err := os.ReadDir(r.Git)
	if err != nil || len(entries) != 1 || entries[0].Name() != "git"+harness.Suffix() {
		t.Fatalf("the directory of git alone holds %d entries, want git and nothing else: %v", len(entries), err)
	}
	file := filepath.Join(r.Git, entries[0].Name())
	fi, err := os.Lstat(file)
	if err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("git is not a regular file of its directory: %v", err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !executableMagic(runtime.GOOS, data) {
		t.Errorf("git does not begin with the header of a %s executable: %q", runtime.GOOS, data[:min(len(data), 4)])
	}
	rfi, err := os.Stat(real)
	if err != nil {
		t.Fatal(err)
	}
	realData, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(fi, rfi) || bytes.Equal(data, realData) {
		t.Errorf("git is a link to %s or a copy of it, not the program that starts it", real)
	}

	repo := t.TempDir()
	git := func(args ...string) (string, int) {
		t.Helper()
		out, code, err := r.Run(t, repo, "git", args...)
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return out, code
	}
	if out, code := git("--version"); code != 0 || !strings.HasPrefix(out, "git version ") {
		t.Fatalf("git --version: code %d\n%s", code, out)
	}
	for _, args := range [][]string{
		{"init", "-q", "--object-format=sha1"},
		{"-c", "user.name=hive", "-c", "user.email=hive@example.org", "commit", "-q", "--allow-empty", "-m", "x"},
	} {
		if out, code := git(args...); code != 0 {
			t.Fatalf("git %v: code %d\n%s", args, code, out)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "HEAD")); err != nil {
		t.Errorf("git init made no repository in the directory it was started from: %v", err)
	}
	if out, code := git("rev-parse", "--verify", "HEAD"); code != 0 || !regexp.MustCompile(`^[0-9a-f]{40}\r?\n$`).MatchString(out) {
		t.Errorf("git rev-parse --verify HEAD: code %d, and what it printed is not the name of the commit: %q", code, out)
	}
	if out, code := git("rev-parse", "--verify", "refs/heads/no-such-branch"); code != 128 || !strings.Contains(out, "fatal:") {
		t.Errorf("a failure of the real Git: code %d, want its own 128 with its message: %q", code, out)
	}
	// The name of the blob that holds nothing.
	if out, code := git("hash-object", "--stdin"); code != 0 || strings.TrimSpace(out) != "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391" {
		t.Errorf("git hash-object --stdin with nothing on standard input: code %d, %q", code, out)
	}
}
