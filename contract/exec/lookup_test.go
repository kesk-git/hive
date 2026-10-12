package exec

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeInfo is the file information of the fake file system.
type fakeInfo struct {
	name string
	mode fs.FileMode
}

func (i fakeInfo) Name() string       { return i.name }
func (i fakeInfo) Size() int64        { return 1 }
func (i fakeInfo) Mode() fs.FileMode  { return i.mode }
func (i fakeInfo) ModTime() time.Time { return time.Time{} }
func (i fakeInfo) IsDir() bool        { return i.mode.IsDir() }
func (i fakeInfo) Sys() any           { return nil }

// fakeFS is a file system for the lookup rules of a platform other than the
// one the tests run on: files by absolute path, with their modes. With
// nocase names are compared without regard to case, as Windows compares
// them.
type fakeFS struct {
	files  map[string]fs.FileMode
	nocase bool
}

func (f fakeFS) stat(path string) (fs.FileInfo, error) {
	for name, mode := range f.files {
		if name == path || f.nocase && strings.EqualFold(name, path) {
			return fakeInfo{filepath.Base(name), mode}, nil
		}
	}
	return nil, fs.ErrNotExist
}

// at is an absolute path of the host under a root of the fake file system,
// made by the host's own rules: the lookup splits, joins and tests paths by
// them, so the fixtures are written by them.
func at(parts ...string) string {
	root, err := filepath.Abs(string(filepath.Separator) + "search")
	if err != nil {
		panic(err)
	}
	return filepath.Join(append([]string{root}, parts...)...)
}

// searchPath joins entries by the host's list separator.
func searchPath(entries ...string) string {
	return strings.Join(entries, string(filepath.ListSeparator))
}

// The program is found on the search path by the rules of macOS and Linux:
// the first regular file of the name with an execute bit, in the order of the
// directories, and never by an empty or relative entry.
func TestLookupOnUnix(t *testing.T) {
	files := fakeFS{files: map[string]fs.FileMode{
		at("a", "git"):           0o644,              // regular, not executable
		at("b", "git"):           0o755,              // the program
		at("c", "git"):           0o755,              // later
		at("b", "gh"):            fs.ModeDir | 0o755, // a directory is no program
		at("c", "gh"):            0o755,
		at("a", "only-data"):     0o644,
		"git":                    0o755, // relative: in the current directory
		filepath.Join(".", "gh"): 0o755,
	}}
	path := searchPath("", ".", "git", filepath.Join("relative", "bin"), at("a"), at("b"), at("c"))
	for _, tc := range []struct {
		name, want string
		reason     Reason
		file       string
	}{
		{"git", at("b", "git"), "", ""},
		{"gh", at("c", "gh"), "", ""},
		{"only-data", "", NotExecutable, at("a", "only-data")},
		{"absent", "", Missing, ""},
	} {
		got, err := find("linux", path, tc.name, files.stat)
		if tc.reason == "" {
			if err != nil || got != tc.want {
				t.Errorf("%s: %q, %v; want %q", tc.name, got, err, tc.want)
			}
			continue
		}
		if err == nil || err.Reason != tc.reason || err.File != tc.file {
			t.Errorf("%s: %q, %+v; want %s %s", tc.name, got, err, tc.reason, tc.file)
		}
	}
	// A search path with nothing usable in it finds nothing, even where the
	// current directory holds the program.
	if got, err := find("linux", searchPath("", ".", "relative"), "git", files.stat); err == nil {
		t.Errorf("found %q on a search path of empty and relative entries", got)
	}
}

// On Windows a program is found only as <name>.exe, compared without regard
// to case; PATHEXT is not consulted; a batch file of the name is refused and
// named, and only where no <name>.exe is on the search path.
func TestLookupOnWindows(t *testing.T) {
	files := fakeFS{nocase: true, files: map[string]fs.FileMode{
		at("a", "tool.cmd"):   0o644,
		at("a", "tool.com"):   0o644,
		at("a", "both.bat"):   0o644,
		at("b", "BOTH.EXE"):   0o644,
		at("b", "tool.BAT"):   0o644,
		at("b", "dir.exe"):    fs.ModeDir | 0o755,
		at("c", "Git.exe"):    0o644,
		at("c", "cmd.exe"):    0o644,
		at("c", "script.ps1"): 0o644,
		at("c", "x.vbs"):      0o644,
		at("c", "y.js"):       0o644,
		at("c", "plain"):      0o644,
		at("c", "git.cmd"):    0o644,
	}}
	path := searchPath(at("a"), at("b"), at("c"), "rel", "")
	win := func(name string) (string, *StartError) { return find("windows", path, name, files.stat) }

	if got, err := win("git"); err != nil || got != at("c", "git.exe") && got != at("c", "Git.exe") {
		t.Errorf("git: %q, %v", got, err)
	}
	if got, err := win("both"); err != nil || !strings.EqualFold(got, at("b", "both.exe")) {
		t.Errorf("an executable and a batch file of one name: %q, %v", got, err)
	}
	if got, err := win("cmd"); err != nil || got != at("c", "cmd.exe") {
		t.Errorf("cmd: %q, %v", got, err)
	}
	// A name is looked for as <name>.exe and never as given.
	if got, err := win("cmd.exe"); err == nil {
		t.Errorf("cmd.exe was found as %q", got)
	}
	for _, name := range []string{"tool", "script", "x", "y", "plain", "dir"} {
		got, err := win(name)
		if err == nil {
			t.Errorf("%s: found %q", name, got)
			continue
		}
		want := Missing
		file := ""
		if name == "tool" {
			want, file = BatchFile, at("a", "tool.cmd")
		}
		if err.Reason != want || err.File != file {
			t.Errorf("%s: %+v; want %s %s", name, err, want, file)
		}
	}
	// The batch file is named whichever of the two suffixes it has.
	if _, err := find("windows", path, "git", fakeFS{nocase: true, files: map[string]fs.FileMode{at("c", "git.cmd"): 0o644}}.stat); err == nil || err.Reason != BatchFile || err.File != at("c", "git.cmd") {
		t.Errorf("%+v", err)
	}
	// The failure of a batch file is dependency_unusable, naming the file.
	_, err := win("tool")
	f := err.Failure()
	if f.Err.Code != "dependency_unusable" || !strings.Contains(f.Err.Message, "tool.cmd") {
		t.Errorf("%+v", f)
	}
}

// The lookup of this platform, on real files: a directory of the search path
// is tried in order, the current directory is never searched by accident, and
// the result is an absolute path.
func TestLookupOnThisPlatform(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	name := "hive-lookup-probe"
	exe := name
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	for _, dir := range []string{first, second} {
		if err := os.WriteFile(filepath.Join(dir, exe), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Relative to the second directory the program is also at "./name".
	t.Chdir(second)
	for _, path := range []string{
		strings.Join([]string{"", ".", first, second}, string(os.PathListSeparator)),
		strings.Join([]string{"", ".", first}, string(os.PathListSeparator)),
	} {
		got, err := find(runtime.GOOS, path, name, os.Stat)
		if err != nil || got != filepath.Join(first, exe) || !filepath.IsAbs(got) {
			t.Errorf("%q: %q, %v", path, got, err)
		}
	}
	// With the program only in the current directory, nothing is found.
	if got, err := find(runtime.GOOS, searchPath(".", ""), name, os.Stat); err == nil {
		t.Errorf("found %q through the current directory", got)
	}
	// lookup uses the search path Hive's process started with and no other.
	old := startPath
	t.Cleanup(func() { startPath = old })
	startPath = second
	t.Setenv("PATH", first)
	if got, err := lookup(Program{Kind: Project, Name: name}); err != nil || got != filepath.Join(second, exe) {
		t.Errorf("%q, %v", got, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.WriteFile(filepath.Join(first, "plain"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		startPath = first
		_, err := lookup(Program{Kind: Project, Name: "plain"})
		var serr *StartError
		if !errors.As(err, &serr) || serr.Reason != NotExecutable || serr.File != filepath.Join(first, "plain") {
			t.Errorf("%v", err)
		}
	}
}

// A name is one bare word of the kind: never a path, a line of text or a
// program of another kind.
func TestProgramNames(t *testing.T) {
	good := []Program{
		{Core, "git"}, {Core, "gh"}, {Core, "claude"}, {Core, "codex"},
		{Transitional, "no-mistakes"}, {Optional, "chrome-devtools-axi"},
		{Hive, "hive-pool"}, {Hive, "hive-spawning-pool"}, {Platform, "security"}, {Project, "make"}, {Project, "cmd"}, {Project, "python3.11"},
	}
	for _, p := range good {
		if err := p.Check(); err != nil {
			t.Errorf("%v: %v", p, err)
		}
	}
	bad := []Program{
		{Core, "svn"}, {Core, "/usr/bin/git"}, {Core, `..\git`}, {Core, "git status"}, {Core, ""}, {Core, "."}, {Core, ".."},
		{Transitional, "git"}, {Optional, "git"}, {Hive, "git"}, {Hive, "hive-"}, {Hive, "Hive-pool"}, {Platform, "Security"},
		{Platform, "../security"}, {Project, "a/b"}, {Project, "cmd.exe"}, {Core, "git.EXE"}, {Project, "tool.Exe"}, {Project, "c:x"}, {Project, "a\nb"}, {Project, "a\x00b"}, {"mystery", "git"}, {"", "git"},
	}
	for _, p := range bad {
		if err := p.Check(); err == nil {
			t.Errorf("%v was admitted", p)
		}
	}
}

// A run is refused before it starts when it breaks a rule of the path, and a
// refusal leaves no process.
func TestSpecRefusals(t *testing.T) {
	dir := t.TempDir()
	base := Spec{Program: Program{Core, "git"}, Dir: dir}
	for name, change := range map[string]func(s *Spec){
		"a relative working directory": func(s *Spec) { s.Dir = "work" },
		"no working directory":         func(s *Spec) { s.Dir = "" },
		"an unknown start":             func(s *Spec) { s.Start = "background" },
		"a pipe for a resident":        func(s *Spec) { s.Start = ResidentStart; s.Stdout = Pipe() },
		"a stream for a terminal":      func(s *Spec) { s.Start = TerminalStart; s.Stdin = Pipe() },
		"a relative output file":       func(s *Spec) { s.Stdout = File("out.txt") },
		"a path for a program":         func(s *Spec) { s.Program = Program{Core, "/usr/bin/git"} },
		"an argument holding a NUL":    func(s *Spec) { s.Args = []string{"a", "b\x00c"} },
	} {
		s := base
		change(&s)
		_, err := Start(s)
		var serr *StartError
		if !errors.As(err, &serr) || serr.Reason != Refused {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if f := serr.Failure(); f.Err.Code != "internal" {
			t.Errorf("%s: %+v", name, f)
		}
	}
}
