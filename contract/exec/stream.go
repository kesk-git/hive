package exec

import (
	"errors"
	"os"
)

// openStream opens the child's end of a stream of a plain or resident start,
// on every platform, and returns the parent's end of a pipe, nil for the
// others.
func openStream(s Stream, input bool) (child *os.File, parent *os.File, err error) {
	switch s.kind {
	case streamPipe:
		r, w, err := os.Pipe()
		if err != nil {
			return nil, nil, err
		}
		if input {
			return r, w, nil
		}
		return w, r, nil
	case streamFile:
		if input {
			f, err := os.Open(s.path)
			return f, nil, err
		}
		f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		return f, nil, err
	case streamAppend:
		if input {
			return nil, nil, errors.New("exec: standard input is not appended to")
		}
		f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
		return f, nil, err
	}
	f, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	return f, nil, err
}
