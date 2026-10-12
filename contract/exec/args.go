package exec

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// windowsCommandLineUnits is the limit of a Windows command line, in UTF-16
// code units, after quoting (spec/distribution.md, Arguments): CreateProcessW
// takes 32,767 characters including the terminating NUL.
const windowsCommandLineUnits = 32766

// checkArguments applies the rules every argument obeys before anything
// starts: none holds a NUL character, and on Windows every one is valid
// UTF-8, so that its conversion to the UTF-16 Windows takes changes nothing.
func checkArguments(goos string, args []string) *StartError {
	for i, a := range args {
		if strings.IndexByte(a, 0) >= 0 {
			return &StartError{Reason: Refused, Err: fmt.Errorf("argument %d holds a NUL character", i+1)}
		}
		if goos == "windows" && !utf8.ValidString(a) {
			return &StartError{Reason: Refused, Err: fmt.Errorf("argument %d is not valid UTF-8", i+1)}
		}
	}
	return nil
}

// windowsCommandLine builds the one command line Windows takes from the
// resolved path and the argument list, by the rules CommandLineToArgvW and
// the Microsoft C runtime use to split it again, as the standard library's
// syscall.EscapeArg does: an argument that is empty or holds a space, a tab
// or a quote is quoted, and backslashes are doubled only where they precede a
// quote. The first word is the resolved path.
func windowsCommandLine(path string, args []string) string {
	var b []byte
	b = appendEscapeArg(b, path)
	for _, a := range args {
		b = append(b, ' ')
		b = appendEscapeArg(b, a)
	}
	return string(b)
}

// appendEscapeArg is the escaping of syscall.EscapeArg, which exists on
// Windows only: it quotes s when it is empty or holds a space or a tab, puts
// a backslash before each quote, and doubles the backslashes that precede a
// quote or the closing quote.
func appendEscapeArg(b []byte, s string) []byte {
	if len(s) == 0 {
		return append(b, `""`...)
	}
	needsBackslash, hasSpace := false, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\\':
			needsBackslash = true
		case ' ', '\t':
			hasSpace = true
		}
	}
	if !needsBackslash && !hasSpace {
		return append(b, s...)
	}
	if !needsBackslash {
		b = append(b, '"')
		b = append(b, s...)
		return append(b, '"')
	}
	if hasSpace {
		b = append(b, '"')
	}
	slashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		default:
			slashes = 0
		case '\\':
			slashes++
		case '"':
			for ; slashes > 0; slashes-- {
				b = append(b, '\\')
			}
			b = append(b, '\\')
		}
		b = append(b, c)
	}
	if hasSpace {
		for ; slashes > 0; slashes-- {
			b = append(b, '\\')
		}
		b = append(b, '"')
	}
	return b
}

// utf16Units is the length of s in UTF-16 code units.
func utf16Units(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// checkWindowsLimit refuses a command line over the platform's limit, which is
// far less than the other two platforms allow. The sizes it reports are in
// bytes, two to a UTF-16 code unit, as every StartError's are.
func checkWindowsLimit(commandLine string) *StartError {
	if n := utf16Units(commandLine); n > windowsCommandLineUnits {
		return &StartError{Reason: TooLarge, Limit: windowsCommandLineUnits * 2, Actual: int64(n) * 2}
	}
	return nil
}

// unixSize is what an argument list and an environment take of the
// platform's total: each string counted with its terminating NUL and with its
// pointer, and the pathname of the program once more, since the kernel
// copies it besides as the first word, which is that pathname.
func unixSize(argv, env []string) int64 {
	const pointer = 8
	var sum int64 = 2 * pointer
	if len(argv) > 0 {
		sum += int64(len(argv[0])) + 1
	}
	for _, group := range [][]string{argv, env} {
		for _, s := range group {
			sum += int64(len(s)) + 1 + pointer
		}
	}
	return sum
}

// checkUnixLimits refuses an argument list over the limits of macOS and
// Linux: the total of arguments and environment, and on Linux also 32 memory
// pages for one string, 131,072 bytes at the usual page size. The first word
// of argv is the program's pathname.
func checkUnixLimits(argv, env []string, total, perString int64) *StartError {
	for _, group := range [][]string{argv, env} {
		for _, s := range group {
			if n := int64(len(s)) + 1; perString > 0 && n > perString {
				return &StartError{Reason: TooLarge, Limit: perString, Actual: n}
			}
		}
	}
	if sum := unixSize(argv, env); total > 0 && sum > total {
		return &StartError{Reason: TooLarge, Limit: total, Actual: sum}
	}
	return nil
}
