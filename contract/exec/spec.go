package exec

import (
	"fmt"
	"io"
	"path/filepath"
	"time"
)

// StartKind is one of the three ways to start a run.
type StartKind string

// The three starts (spec/distribution.md, The one process-execution path).
const (
	// PlainStart: standard streams as pipes, files or the null device, in a
	// tree of its own.
	PlainStart StartKind = "plain"
	// TerminalStart: a pseudo-terminal, for a program that needs an
	// interactive terminal.
	TerminalStart StartKind = "terminal"
	// ResidentStart: a process that outlives the command that starts it,
	// not waited for.
	ResidentStart StartKind = "resident"
)

type streamKind int

const (
	streamNull streamKind = iota
	streamPipe
	streamFile
	streamAppend
)

// Stream is what a standard stream of a plain or resident start is: a pipe
// to Hive, a file the operation names, or the null device. The zero value is
// the null device. Nothing else Hive holds open reaches the child.
type Stream struct {
	kind streamKind
	path string
}

// Null is the null device.
func Null() Stream { return Stream{} }

// Pipe is a pipe to Hive; a resident start has none.
func Pipe() Stream { return Stream{kind: streamPipe} }

// File is the file the operation names: read for standard input, and for an
// output created if absent and truncated.
func File(path string) Stream { return Stream{kind: streamFile, path: path} }

// AppendFile is an output file appended to.
func AppendFile(path string) Stream { return Stream{kind: streamAppend, path: path} }

// Window is the size of a terminal in rows and columns.
type Window struct{ Rows, Cols uint16 }

// DefaultWindow is the size of a terminal whose start names none.
var DefaultWindow = Window{Rows: 24, Cols: 80}

// Spec is one run.
type Spec struct {
	// Program is the program, by kind and bare name.
	Program Program
	// Args is the argument list after the program; the program's own name,
	// the resolved path, is the first word and is not listed.
	Args []string
	// Dir is the absolute working directory the operation names. A run never
	// takes the caller's current directory by default.
	Dir string
	// Config are the names of the program's own configuration variables that
	// the stage which first runs it names, copied by name from Hive's own
	// environment where they are set.
	Config []string
	// Tendril is the one variable Tendril sets from a resolved value, or nil.
	Tendril *Var
	// Start is the kind of start; plain when empty.
	Start StartKind
	// Stdin, Stdout and Stderr are the streams of a plain or resident start.
	Stdin, Stdout, Stderr Stream
	// Window is the size of a terminal start's terminal; DefaultWindow when
	// zero.
	Window Window
}

// check applies the rules of the run that need no platform.
func (s *Spec) check() *StartError {
	refuse := func(format string, args ...any) *StartError {
		return &StartError{Reason: Refused, Program: s.Program, Err: fmt.Errorf(format, args...)}
	}
	if err := s.Program.Check(); err != nil {
		return refuse("%v", err)
	}
	switch s.Start {
	case "":
		s.Start = PlainStart
	case PlainStart, TerminalStart, ResidentStart:
	default:
		return refuse("%q is not a start", string(s.Start))
	}
	if s.Dir == "" || !filepath.IsAbs(s.Dir) {
		return refuse("the working directory %q is not absolute", s.Dir)
	}
	for _, st := range []struct {
		name string
		s    Stream
	}{{"standard input", s.Stdin}, {"standard output", s.Stdout}, {"standard error", s.Stderr}} {
		if st.s.kind >= streamFile && !filepath.IsAbs(st.s.path) {
			return refuse("the file of %s, %q, is not absolute", st.name, st.s.path)
		}
		if s.Start == ResidentStart && st.s.kind == streamPipe {
			return refuse("a resident start has no pipe for %s: it outlives Hive's hold on it", st.name)
		}
		if s.Start == TerminalStart && st.s.kind != streamNull {
			return refuse("a terminal start gives the program its terminal for %s", st.name)
		}
	}
	if s.Start == TerminalStart && s.Window == (Window{}) {
		s.Window = DefaultWindow
	}
	return nil
}

// Grace are the grace periods of stopping a run: how long each step has to
// work before the next is taken (spec/distribution.md, Process trees, and how
// a run is stopped). Interrupt is the time after the interrupt of a terminal
// start, Request the time after the request.
type Grace struct {
	Interrupt time.Duration
	Request   time.Duration
}

// ProcessID identifies a started process by its id together with its start
// time, never by its id alone, since every platform reuses ids. Start is the
// time the platform reports, in its own unit and comparable only with
// another read of the same process: on Linux the start time /proc reports,
// on macOS the one sysctl reports, on Windows the creation time
// GetProcessTimes reports.
type ProcessID struct {
	PID   int
	Start uint64
}

// Terminal is the controller side of a terminal start: Hive writes input to
// it and reads output from it, as bytes. What a terminal shows is its
// rendering and never a record.
type Terminal interface {
	io.ReadWriteCloser
	// Resize sets the terminal's size, so that the program redraws itself.
	Resize(Window) error
}
