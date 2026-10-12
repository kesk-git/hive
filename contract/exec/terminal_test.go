package exec

import (
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// pump reads a terminal for the whole test, keeping what it reads, so that no
// reader is left holding output a later wait wants.
type pump struct {
	mu   sync.Mutex
	data strings.Builder
	done bool
	err  error
}

var pumps sync.Map

// readUntil waits until the terminal's output holds s, within a bound, and
// returns all of the output so far.
func readUntil(t testing.TB, term io.Reader, s string) string {
	t.Helper()
	v, loaded := pumps.LoadOrStore(term, &pump{})
	p := v.(*pump)
	if !loaded {
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := term.Read(buf)
				p.mu.Lock()
				p.data.Write(buf[:n])
				if err != nil {
					p.done, p.err = true, err
				}
				p.mu.Unlock()
				if err != nil {
					return
				}
			}
		}()
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		got, done, err := p.data.String(), p.done, p.err
		p.mu.Unlock()
		if strings.Contains(got, s) {
			return got
		}
		if done {
			t.Fatalf("the terminal ended before %q: %v (read %q)", s, err, got)
		}
		time.Sleep(5 * time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	t.Fatalf("no %q within 20 seconds (read %q)", s, p.data.String())
	return ""
}

// A program that refuses a non-interactive terminal runs under a terminal
// start.
func TestAProgramThatNeedsATerminalRunsUnderATerminalStart(t *testing.T) {
	fakeDir(t, "fakeprog")
	run := fake(t, Spec{Stdin: Pipe(), Stdout: Null(), Stderr: Null()}, "needstty")
	if res := run.Wait(); res.Outcome != Exited || res.Code != 1 {
		t.Errorf("a plain start: %v", res)
	}
	term := fake(t, Spec{Start: TerminalStart}, "needstty")
	readUntil(t, term.Terminal(), "terminal")
	if res := term.Wait(); !res.Passed() {
		t.Errorf("a terminal start: %v", res)
	}
}

// A terminal start gives the program a terminal of the size asked for, as its
// controlling terminal in a session of its own; a resize reaches it; Control-C
// written to its input arrives as an interrupt.
func TestTerminalStart(t *testing.T) {
	fakeDir(t, "fakeprog")
	run := fake(t, Spec{Start: TerminalStart, Window: Window{Rows: 30, Cols: 100}}, "tty")
	term := run.Terminal()
	if term == nil || run.Stdin() != nil || run.Stdout() != nil {
		t.Fatal("a terminal start has a terminal and no pipes")
	}
	got := readUntil(t, term, "controlling=")
	for _, want := range []string{"tty=true", "rows=30", "cols=100", "leader=true", "controlling=true"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q does not hold %s", got, want)
		}
	}
	if err := term.Resize(Window{Rows: 40, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, term, "rows=40 cols=120")
	if _, err := term.Write([]byte{0x03}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, term, "interrupt")
	if res := run.Wait(); res.Outcome != Exited || res.Code != 3 {
		t.Errorf("%v", res)
	}
}

// Stopping a terminal start begins with the interrupt: Control-C written to
// its input, which its line discipline turns into SIGINT.
func TestStopInterruptsATerminalStart(t *testing.T) {
	fakeDir(t, "fakeprog")
	run := fake(t, Spec{Start: TerminalStart}, "tty")
	readUntil(t, run.Terminal(), "controlling=")
	recorded := false
	res, err := run.Stop(Grace{Interrupt: 10 * time.Second}, func() error { recorded = true; return nil })
	if err != nil || recorded || res.Outcome != Exited || res.Code != 3 {
		t.Errorf("%+v %v recorded=%v", res, err, recorded)
	}
	// A program that does not handle it is ended by the signal.
	run = fake(t, Spec{Start: TerminalStart}, "hold")
	readUntil(t, run.Terminal(), "ready")
	res, err = run.Stop(Grace{Interrupt: 10 * time.Second}, nil)
	wantSignal := "interrupt"
	if runtime.GOOS == "windows" {
		wantSignal = ""
	}
	if err != nil || res.Outcome != Ended || res.Signal != wantSignal {
		t.Errorf("%+v %v", res, err)
	}
	// With no grace at all the request and the end follow at once.
	run = fake(t, Spec{Start: TerminalStart}, "holdterm")
	readUntil(t, run.Terminal(), "ready")
	if res, err = run.Stop(Grace{}, nil); err != nil || res.Outcome == "" {
		t.Errorf("%+v %v", res, err)
	}
}
