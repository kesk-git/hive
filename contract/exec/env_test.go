package exec

import (
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// The child's environment is built from nothing: the platform's base
// variables by name, the program's configuration variables, TERM for a
// terminal start on macOS and Linux; nothing of the rest of Hive's
// environment passes, whole or by pattern.
func TestEnvironmentOnUnix(t *testing.T) {
	environ := []string{
		"PATH=/bin:/usr/bin", "HOME=/home/h", "USER=u", "LOGNAME=u", "TMPDIR=/tmp", "TZ=UTC", "LANG=en_US.UTF-8",
		"LC_ALL=C", "LC_CTYPE=C.UTF-8", "LCX=not-locale", "LC=short",
		"ANTHROPIC_API_KEY=sk-secret", "GH_TOKEN=ghp_secret", "SHELL=/bin/zsh", "TERM=dumb", "EDITOR=vi", "path=lower",
		"BROKEN", "=odd", "HIVE_CONFIG=conf", "PATH=/second/ignored",
	}
	got, err := buildEnv(envInput{goos: "linux", environ: environ})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"HOME=/home/h", "LANG=en_US.UTF-8", "LC_ALL=C", "LC_CTYPE=C.UTF-8", "LOGNAME=u", "PATH=/bin:/usr/bin", "TMPDIR=/tmp", "TZ=UTC", "USER=u"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}

	// A configuration variable is copied by name where set, and never invented.
	got, err = buildEnv(envInput{goos: "linux", environ: environ, config: []string{"HIVE_CONFIG", "NOT_SET", "PATH"}})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(got, "HIVE_CONFIG=conf") || containsPrefix(got, "NOT_SET") || countPrefix(got, "PATH=") != 1 {
		t.Errorf("%q", got)
	}
	// TERM is the terminal's, on a terminal start only, and the process's own
	// TERM never passes.
	term, err := buildEnv(envInput{goos: "linux", environ: environ, terminal: true})
	if err != nil || !contains(term, "TERM=xterm-256color") || countPrefix(term, "TERM=") != 1 {
		t.Errorf("%q, %v", term, err)
	}
	if containsPrefix(got, "TERM=") {
		t.Errorf("a plain start has TERM: %q", got)
	}
	// Refusals: a name that is not one, TERM as configuration.
	for name, in := range map[string]envInput{
		"a name with =": {goos: "linux", environ: environ, config: []string{"A=B"}},
		"an empty name": {goos: "linux", environ: environ, config: []string{""}},
		"TERM twice":    {goos: "linux", environ: environ, config: []string{"TERM"}, terminal: true},
	} {
		if _, err := buildEnv(in); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// On Windows the base variables are matched without regard to case, the
// hidden drive variables never pass, TERM is not set, and the block is
// sorted by name without regard to case.
func TestEnvironmentOnWindows(t *testing.T) {
	environ := []string{
		"Path=C:\\Windows", "SYSTEMROOT=C:\\Windows", "windir=C:\\Windows", "ComSpec=C:\\Windows\\system32\\cmd.exe",
		"temp=C:\\T", "TMP=C:\\T", "ProgramFiles(x86)=C:\\PF86", "OS=Windows_NT", "=C:=C:\\Users\\u", "=ExitCode=00000000",
		"GH_TOKEN=secret", "LANG=en", "HOME=C:\\h", "LC_ALL=C", "TERM=dumb", "Zed=1", "USERNAME=u",
	}
	got, err := buildEnv(envInput{goos: "windows", environ: environ, terminal: true, config: []string{"zed"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ComSpec=C:\\Windows\\system32\\cmd.exe", "OS=Windows_NT", "Path=C:\\Windows", "ProgramFiles(x86)=C:\\PF86",
		"SYSTEMROOT=C:\\Windows", "temp=C:\\T", "TMP=C:\\T", "USERNAME=u", "windir=C:\\Windows", "Zed=1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	// The order is by name without regard to case.
	if !sort.SliceIsSorted(got, func(i, j int) bool {
		return strings.ToUpper(strings.SplitN(got[i], "=", 2)[0]) < strings.ToUpper(strings.SplitN(got[j], "=", 2)[0])
	}) {
		t.Errorf("not sorted: %q", got)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func containsPrefix(list []string, p string) bool { return countPrefix(list, p) > 0 }

func countPrefix(list []string, p string) int {
	n := 0
	for _, x := range list {
		if strings.HasPrefix(x, p) {
			n++
		}
	}
	return n
}

// The environment a real child sees holds the platform's base variables and
// nothing else of the test's own environment.
func TestChildEnvironment(t *testing.T) {
	fakeDir(t, "fakeprog")
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-secret")
	t.Setenv("GH_TOKEN", "ghp_test_secret")
	t.Setenv("HIVE_TEST_CONFIG", "configured")
	t.Setenv("HIVE_TEST_OTHER", "other")
	t.Setenv("LC_ALL", "C")
	t.Setenv("TERM", "dumb")
	t.Setenv("HOME", t.TempDir())
	spec := withFake(t, Spec{Config: []string{"HIVE_TEST_CONFIG", "HIVE_TEST_UNSET"}}, "env")
	out, res := runOutput(t, spec)
	if res.Outcome != Exited || res.Code != 0 {
		t.Fatalf("%v", res)
	}
	want, err := buildEnv(envInput{goos: runtime.GOOS, environ: ownEnviron(), config: spec.Config})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, line := range strings.Split(strings.TrimRight(out, "\r\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		// The Go runtime of the fake may add variables of its own on Windows.
		if runtime.GOOS == "windows" && (strings.HasPrefix(line, "=") || strings.HasPrefix(strings.ToUpper(line), "SYSTEMROOT=") && !containsFold(want, "SystemRoot")) {
			continue
		}
		got = append(got, line)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the child saw\n%q\nwant\n%q", got, want)
	}
	for _, kv := range got {
		for _, secret := range []string{"sk-test-secret", "ghp_test_secret", "HIVE_TEST_OTHER", "TERM=dumb"} {
			if strings.Contains(kv, secret) {
				t.Errorf("the child saw %q", kv)
			}
		}
	}
	if !contains(got, "HIVE_TEST_CONFIG=configured") {
		t.Errorf("%q", got)
	}
}

func containsFold(list []string, name string) bool {
	for _, x := range list {
		if strings.EqualFold(strings.SplitN(x, "=", 2)[0], name) {
			return true
		}
	}
	return false
}
