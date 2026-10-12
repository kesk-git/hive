package exec

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

// splitCommandLine splits a command line by the rules CommandLineToArgvW and
// the Microsoft C runtime use for every word after the first: backslashes
// are literal unless they precede a quote, 2n backslashes and a quote are n
// backslashes and a quote that toggles quoting, 2n+1 are n and a literal
// quote. The first word, the program, is split by the simpler rule that a
// quote toggles quoting and nothing is an escape.
func splitCommandLine(line string) []string {
	var words []string
	i := 0
	// The first word.
	var first strings.Builder
	in := false
	for ; i < len(line); i++ {
		c := line[i]
		if c == '"' {
			in = !in
			continue
		}
		if (c == ' ' || c == '\t') && !in {
			break
		}
		first.WriteByte(c)
	}
	words = append(words, first.String())
	for i < len(line) {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i >= len(line) {
			break
		}
		var w strings.Builder
		in := false
		for i < len(line) {
			c := line[i]
			if (c == ' ' || c == '\t') && !in {
				break
			}
			if c == '\\' {
				n := 0
				for i < len(line) && line[i] == '\\' {
					n++
					i++
				}
				if i < len(line) && line[i] == '"' {
					w.WriteString(strings.Repeat(`\`, n/2))
					if n%2 == 1 {
						w.WriteByte('"')
						i++
					}
				} else {
					w.WriteString(strings.Repeat(`\`, n))
				}
				continue
			}
			if c == '"' {
				in = !in
				i++
				continue
			}
			w.WriteByte(c)
			i++
		}
		words = append(words, w.String())
	}
	return words
}

var awkwardArguments = []string{
	"plain", "two words", "tab\there", `quote"inside`, `"quoted"`, `back\slash`, `ends with backslash\`,
	`ends with two\\`, `before quote\"after`, `two before\\"after`, `three before\\\"after`,
	``, ` `, `\`, `\\`, `"`, `""`, `\"`, `a b\ "c d"\\`, `--flag=value with space`, `-x`, `é ünï 日本語 🎉`,
	`C:\Program Files\Hive\bin\hive-pool.exe`, `\\server\share\dir with space\`, `100%`, `^&|<>`, `a=b`,
}

// The command line of Windows is built from the argument list by the rules
// that split it again, so that a program that splits by those rules receives
// the list unchanged: spaces, tabs, quotes, backslashes before a quote and an
// empty argument among them. The first word is the resolved path.
func TestWindowsCommandLineRoundTrips(t *testing.T) {
	path := `C:\Program Files\Hive\bin\hive-pool.exe`
	line := windowsCommandLine(path, awkwardArguments)
	got := splitCommandLine(line)
	want := append([]string{path}, awkwardArguments...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the command line\n%s\nsplits into\n%q\nwant\n%q", line, got, want)
	}
	// Each argument alone, and in pairs, round-trips.
	for _, a := range awkwardArguments {
		for _, b := range []string{"", "x", a} {
			if got := splitCommandLine(windowsCommandLine(`C:\x.exe`, []string{a, b})); !reflect.DeepEqual(got, []string{`C:\x.exe`, a, b}) {
				t.Errorf("%q %q: %q", a, b, got)
			}
		}
	}
	// A path without a space is not quoted, and one with a space is.
	if got := windowsCommandLine(`C:\bin\git.exe`, []string{"status"}); got != `C:\bin\git.exe status` {
		t.Errorf("%q", got)
	}
	if got := windowsCommandLine(`C:\a b\git.exe`, nil); got != `"C:\a b\git.exe"` {
		t.Errorf("%q", got)
	}
}

// An argument is checked before anything starts: none holds a NUL, and on
// Windows every one is valid UTF-8.
func TestArgumentChecks(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		if err := checkArguments(goos, []string{"fine", "é", ""}); err != nil {
			t.Errorf("%s: %v", goos, err)
		}
		if err := checkArguments(goos, []string{"a", "b\x00"}); err == nil || err.Reason != Refused {
			t.Errorf("%s: a NUL passed: %v", goos, err)
		}
	}
	bad := []string{"ok", "\xff\xfe"}
	if err := checkArguments("windows", bad); err == nil || err.Reason != Refused {
		t.Errorf("windows: invalid UTF-8 passed: %v", err)
	}
	if err := checkArguments("linux", bad); err != nil {
		t.Errorf("linux passes arguments as they are: %v", err)
	}
}

// A Windows command line is over its limit at 32,766 UTF-16 code units, after
// quoting, so that with its NUL it fits 32,767; units are counted as code
// units: a character outside the basic plane counts two.
func TestWindowsLimit(t *testing.T) {
	line := func(n int) string { return strings.Repeat("a", n) }
	if err := checkWindowsLimit(line(32766)); err != nil {
		t.Errorf("32766 units: %v", err)
	}
	err := checkWindowsLimit(line(32767))
	if err == nil || err.Reason != TooLarge || err.Limit != 2*32766 || err.Actual != 2*32767 {
		t.Errorf("32767 units: %+v", err)
	}
	// Quoting counts: 16,400 quotes become 32,800 units once escaped.
	quoted := windowsCommandLine(`C:\x.exe`, []string{strings.Repeat(`"`, 16400)})
	if err := checkWindowsLimit(quoted); err == nil {
		t.Error("an argument that quoting makes too long passed")
	}
	// 16,384 characters of the supplementary plane are 32,768 units.
	if n := utf16Units(strings.Repeat("🎉", 16384)); n != 32768 {
		t.Errorf("units = %d", n)
	}
	if got, want := utf16Units("a🎉é"), len(utf16.Encode([]rune("a🎉é"))); got != want {
		t.Errorf("units = %d, want %d", got, want)
	}
	if f := err.Failure(); f.Status != "too_large" || f.Err.Code != "record_too_large" || *f.Err.LimitBytes != 2*32766 || *f.Err.ActualBytes != 2*32767 {
		t.Errorf("%+v", f)
	}
}

// The limits of macOS and Linux are the total of arguments and environment,
// and on Linux one string of at most 32 pages.
func TestUnixLimits(t *testing.T) {
	if err := checkUnixLimits([]string{"/bin/x", "a"}, []string{"PATH=/bin"}, 1000, 0); err != nil {
		t.Error(err)
	}
	if err := checkUnixLimits([]string{"/bin/x", strings.Repeat("a", 1000)}, nil, 1000, 0); err == nil || err.Reason != TooLarge {
		t.Errorf("%+v", err)
	}
	if err := checkUnixLimits([]string{"/bin/x"}, []string{strings.Repeat("a", 50)}, 100, 40); err == nil || err.Limit != 40 {
		t.Errorf("one string over its limit: %+v", err)
	}
	// The program's pathname is copied once more than its first word: a path
	// that fills the total alone is over it.
	long := "/" + strings.Repeat("p", 300)
	if err := checkUnixLimits([]string{long}, nil, 2*(int64(len(long))+1+8), 0); err == nil || err.Actual <= err.Limit {
		t.Errorf("the pathname was not counted twice: %+v", err)
	}
	// The environment counts toward the total.
	if err := checkUnixLimits([]string{"/bin/x"}, []string{strings.Repeat("a", 990)}, 1000, 0); err == nil {
		t.Error("the environment was not counted")
	}
}

// A run over the platform's limit is refused before anything starts, as
// too_large and never with a truncated argument.
func TestRunOverTheLimitIsRefused(t *testing.T) {
	fakeDir(t, "fakeprog")
	args := oversizeArguments()
	spec := withFake(t, Spec{}, "args")
	spec.Args = append(spec.Args, args...)
	run, err := Start(spec)
	if run != nil {
		run.Stop(Grace{}, nil)
	}
	serr, ok := err.(*StartError)
	if !ok || serr.Reason != TooLarge {
		t.Fatalf("%v", err)
	}
	if f := serr.Failure(); f.Status != "too_large" {
		t.Errorf("%+v", f)
	}
	// Under the limit the same list is run, whole.
	spec = withFake(t, Spec{Stdout: Pipe()}, "args", awkwardArguments...)
	got, res := runOutput(t, spec)
	if res.Outcome != Exited || res.Code != 0 {
		t.Fatalf("%v", res)
	}
	var echoed []string
	if err := json.Unmarshal([]byte(got), &echoed); err != nil {
		t.Fatalf("%q: %v", got, err)
	}
	if !reflect.DeepEqual(echoed, awkwardArguments) {
		t.Errorf("the fake received\n%q\nwant\n%q", echoed, awkwardArguments)
	}
}

// runOutput starts a ready spec, reads its output whole and waits.
func runOutput(t testing.TB, spec Spec) (string, Result) {
	t.Helper()
	if spec.Stdout.kind != streamPipe {
		spec.Stdout = Pipe()
	}
	run, err := Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { run.Stop(Grace{}, nil); run.Close() })
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := run.Stdout().Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String(), run.Wait()
}
