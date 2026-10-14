package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/store"
)

// Run is Main over the process: its arguments, standard streams and exit.
// A package's main function is one line: os.Exit(cli.Run(exe)).
func Run(e *Executable) int {
	return e.Main(os.Args, os.Stdin, os.Stdout, os.Stderr)
}

// commandName is the command name a program started as argv0 acts as: its
// base name, and on Windows that name without its .exe suffix and in lower
// case, as Windows compares names (spec/commands.md, Executables and command
// names).
func commandName(argv0 string, windows bool) string {
	name := filepath.Base(argv0)
	if windows {
		name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	}
	return name
}

// Main runs one invocation of the executable as the command named by argv[0]
// and returns the process's exit code. It writes the one response document
// to standard output or the response file, and diagnostics for a person to
// stderr. Only --version answers without a response document; an
// invocation that fails before a response can be written ends with exit
// code 1 and what went wrong on stderr.
func (e *Executable) Main(argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if err := e.Check(); err != nil {
		fmt.Fprintln(stderr, err)
		return envelope.Failed.ExitCode()
	}
	if len(argv) == 0 {
		fmt.Fprintln(stderr, "cli: no command name")
		return envelope.Failed.ExitCode()
	}
	name := commandName(argv[0], runtime.GOOS == "windows")
	args := argv[1:]
	c := e.command(name)
	if c == nil {
		return e.answer(nil, "", "", stdout, stderr, e.failure("", e.Name, "", envelope.Common(envelope.CodeUsage, fmt.Sprintf("%s does not own the command name it was started under, of %d bytes", e.Name, len(name)))))
	}

	// No argument is longer than the limit; a longer one exits 2 before
	// anything is read.
	limit := c.Limits.Resolved().MaxArgumentBytes
	for _, a := range args {
		if int64(len(a)) > limit {
			f := envelope.Common(envelope.CodeArgumentTooLarge, fmt.Sprintf("an argument is %d bytes; the limit is %d", len(a), limit))
			f.With("limit_bytes", limit).With("actual_bytes", int64(len(a)))
			return e.answer(c, "", "", stdout, stderr, e.failure("", c.Name, "", f))
		}
	}

	usage := func(operation, msg string) int {
		return e.answer(c, "", "", stdout, stderr, e.failure("", c.Name, operation, envelope.Common(envelope.CodeUsage, msg)))
	}
	if len(args) == 0 {
		return usage("", "usage: "+c.Name+" <operation> --home <directory> [--request-file <path>] [--response-file <path>] | capabilities | --version")
	}
	switch args[0] {
	case VersionFlag:
		if len(args) != 1 {
			return usage("", VersionFlag+" takes no other argument")
		}
		fmt.Fprintln(stdout, e.Version)
		return envelope.Ok.ExitCode()
	case CapabilitiesOperation:
		if len(args) != 1 {
			return usage(CapabilitiesOperation, "capabilities takes no request, no home and no other argument")
		}
		resp := e.capabilities(c)
		if err := e.stamp(resp); err != nil {
			fmt.Fprintln(stderr, err)
			return envelope.Failed.ExitCode()
		}
		return e.answer(c, "", "", stdout, stderr, resp)
	}

	// The operation is known before anything is read; the options read
	// before an error still say where the response goes.
	operation := args[0]
	opts, err := parseOptions(args[1:])
	if !operationForm.MatchString(operation) {
		return e.answer(c, opts.responseFile, "", stdout, stderr, e.failure("", c.Name, "", envelope.Common(envelope.CodeUnknownOperation, fmt.Sprintf("argument 1, of %d bytes, is not an operation name", len(operation)))))
	}
	if c.operation(operation) == nil {
		return e.answer(c, opts.responseFile, "", stdout, stderr, e.failure("", c.Name, operation, envelope.Common(envelope.CodeUnknownOperation, fmt.Sprintf("%s has no operation of the name in argument 1, of %d bytes", c.Name, len(operation)))))
	}
	if err != nil {
		return e.answer(c, opts.responseFile, "", stdout, stderr, e.failure("", c.Name, operation, envelope.Common(envelope.CodeUsage, err.Error())))
	}
	if opts.home == "" {
		return e.answer(c, opts.responseFile, "", stdout, stderr, e.failure("", c.Name, operation, envelope.Common(envelope.CodeUsage, HomeOption+" is required: there is no default home")))
	}

	// The request: from the file or standard input, refused over the limit
	// before it is parsed.
	request, f := readRequest(opts.requestFile, stdin, c.Limits.Resolved().MaxRequestBytes)
	if f != nil {
		return e.answer(c, opts.responseFile, opts.home, stdout, stderr, e.failure("", c.Name, operation, f))
	}
	out, err := e.Handle(Invocation{Command: c, Operation: operation, Home: opts.home, Request: request, Stderr: stderr})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return envelope.Failed.ExitCode()
	}
	code := e.write(c, out.Response, out.Encoded, opts.responseFile, stdout, stderr)
	if out.After != nil && code == envelope.Ok.ExitCode() {
		if err := out.After(); err != nil {
			fmt.Fprintf(stderr, "%s %s: %v\n", c.Name, operation, err)
			return envelope.Failed.ExitCode()
		}
	}
	return code
}

// options are the parsed options of an operation invocation.
type options struct {
	home         string
	requestFile  string
	responseFile string
}

// parseOptions reads the options after the operation name. Each is given at
// most once, as --name value; anything else is a usage error. The options
// read before the error are returned with it, so that the response can still
// go where the caller asked.
func parseOptions(args []string) (options, error) {
	var o options
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		name := args[i]
		if !strings.HasPrefix(name, "--") {
			return o, fmt.Errorf("argument %d is not an option; the request is never an argument, it is read from %s or standard input", i+2, RequestFileOption)
		}
		var dst *string
		switch name {
		case HomeOption:
			dst = &o.home
		case RequestFileOption:
			dst = &o.requestFile
		case ResponseFileOption:
			dst = &o.responseFile
		default:
			return o, fmt.Errorf("argument %d, of %d bytes, is not an option this command takes", i+2, len(name))
		}
		if seen[name] {
			return o, fmt.Errorf("%s is given twice", name)
		}
		seen[name] = true
		if i+1 >= len(args) {
			return o, fmt.Errorf("%s needs a value", name)
		}
		i++
		value := args[i]
		if value == "" {
			return o, fmt.Errorf("%s needs a value", name)
		}
		*dst = value
	}
	return o, nil
}

// readRequest reads the request from the file, or from standard input when
// the file is "", up to the limit. A request over the limit is too_large
// with its actual size, counted to the end without being kept.
func readRequest(file string, stdin io.Reader, limit int64) ([]byte, *envelope.Failure) {
	var r io.Reader = stdin
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return nil, envelope.Common(envelope.CodeSourceUnreadable, "the request file cannot be read").Because(err)
		}
		defer f.Close()
		r = f
	}
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, envelope.Common(envelope.CodeSourceUnreadable, "the request cannot be read").Because(err)
	}
	if int64(len(data)) > limit {
		rest, err := io.Copy(io.Discard, r)
		if err != nil {
			return nil, envelope.Common(envelope.CodeSourceUnreadable, "the request cannot be read").Because(err)
		}
		return nil, envelope.Oversize("request", limit, int64(len(data))+rest)
	}
	return data, nil
}

// answer stamps, encodes and writes a response made before Handle ran. home
// is the directory the invocation named, where it named one: the failure's
// detail goes to that home's journal before the response is written.
func (e *Executable) answer(c *Command, responseFile, home string, stdout, stderr io.Writer, resp *envelope.Response) int {
	if err := e.stamp(resp); err != nil {
		fmt.Fprintln(stderr, err)
		return envelope.Failed.ExitCode()
	}
	var data []byte
	var err error
	if c != nil {
		data, err = e.finish(c, resp)
	} else {
		data, err = resp.Encode()
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return envelope.Failed.ExitCode()
	}
	e.diagnose(home, nil, resp)
	return e.write(c, resp, data, responseFile, stdout, stderr)
}

// write writes the encoded response to the response file, or to standard
// output when there is none, and one line to stderr for every status but ok:
// the command, the operation, the status, the code and the request_id, and no
// message (spec/commands.md, Invocation). It returns the exit code the response fixes, or 1 when
// the response could not be written: the command then gave no response.
func (e *Executable) write(c *Command, resp *envelope.Response, data []byte, responseFile string, stdout, stderr io.Writer) int {
	if resp.Status != envelope.Ok && resp.Error != nil {
		fmt.Fprintf(stderr, "%s %s: %s %s %s\n", resp.Command, orDash(resp.Operation), resp.Status, resp.Error.Code, orDash(resp.RequestID))
	}
	var err error
	if responseFile != "" {
		err = writeFile(responseFile, data)
	} else {
		_, err = io.Copy(stdout, bytes.NewReader(data))
	}
	if err != nil {
		fmt.Fprintf(stderr, "%s: the response cannot be written: %v\n", resp.Command, err)
		return envelope.Failed.ExitCode()
	}
	return resp.ExitCode()
}

// writeFile writes the response file whole, for its owner only, and flushes
// it: a caller that reads it after the process ends sees all of it.
func writeFile(path string, data []byte) error {
	f, err := store.OpenPrivate(path)
	if err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return errors.Join(err, f.Close())
	}
	_, werr := f.Write(data)
	serr := f.Sync()
	cerr := f.Close()
	return errors.Join(werr, serr, cerr)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
