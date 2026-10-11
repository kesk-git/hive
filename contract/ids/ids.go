// Package ids mints, parses and checks Hive's typed identifiers.
//
// An identifier is a registered prefix, an underscore and a ULID in
// lowercase (spec/identities.md, Identifier form):
//
//	ws_01jb2m3n4p5q6r7s8t9vwxyz0a
//
// An identifier is opaque. This package exposes no way to read the time part
// of one, and nothing here orders identifiers.
package ids

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/kesk-git/hive/contract/clock"
)

// Prefix is a registered identifier prefix.
type Prefix string

// The registered prefixes, in the order of the table in spec/identities.md.
// A later stage registers a prefix by adding a row there and a constant here.
const (
	Home          Prefix = "home"
	Workspace     Prefix = "ws"
	Project       Prefix = "proj"
	Clone         Prefix = "clone"
	Session       Prefix = "sess"
	Message       Prefix = "msg"
	Input         Prefix = "inp"
	Task          Prefix = "task"
	Question      Prefix = "q"
	DecisionRound Prefix = "dr"
	Card          Prefix = "card"
	Lease         Prefix = "lease"
	Review        Prefix = "rev"
	FeedbackEvent Prefix = "fev"
	ReviewNote    Prefix = "note"
	Response      Prefix = "resp"
	Request       Prefix = "req"
	Correlation   Prefix = "corr"
	Receipt       Prefix = "rcpt"
	Event         Prefix = "evt"
)

var registry = []Prefix{
	Home, Workspace, Project, Clone, Session, Message, Input, Task, Question,
	DecisionRound, Card, Lease, Review, FeedbackEvent, ReviewNote, Response,
	Request, Correlation, Receipt, Event,
}

// Prefixes returns the registered prefixes in registration order.
func Prefixes() []Prefix { return append([]Prefix(nil), registry...) }

// Registered reports whether p is a registered prefix.
func Registered(p Prefix) bool {
	for _, r := range registry {
		if r == p {
			return true
		}
	}
	return false
}

// ValidPrefixForm reports whether p has the form every prefix has: one to
// eight lowercase letters and digits, beginning with a letter. A registered
// prefix always does; the form is what a new registration must meet.
func ValidPrefixForm(p Prefix) bool {
	if len(p) < 1 || len(p) > 8 || p[0] < 'a' || p[0] > 'z' {
		return false
	}
	for i := 1; i < len(p); i++ {
		c := p[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// ULIDLength is the number of characters in the ULID part of an identifier.
const ULIDLength = 26

// alphabet is Crockford base 32, lowercase: no i, l, o or u.
const alphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// maxMillis is the largest value of the 48-bit time part.
const maxMillis = 1<<48 - 1

// Errors returned for an identifier that is not well formed.
var (
	// ErrForm reports text that is not a prefix, an underscore and a
	// lowercase ULID.
	ErrForm = errors.New("ids: not a typed identifier")
	// ErrPrefix reports an identifier whose prefix is not registered.
	ErrPrefix = errors.New("ids: unregistered prefix")
	// ErrWrongPrefix reports an identifier of another kind than the one asked for.
	ErrWrongPrefix = errors.New("ids: identifier of another kind")
	// ErrTime reports a clock time that cannot be the 48-bit time of a ULID.
	ErrTime = errors.New("ids: time outside the range of a ULID")
)

// Mint returns a new identifier with prefix p, taking its time from c and its
// 80 random bits from the operating system's random source.
func Mint(c clock.Clock, p Prefix) (string, error) {
	return MintFrom(c, rand.Reader, p)
}

// MintFrom is Mint with the random bits read from entropy, which a test
// supplies. It reads exactly ten bytes.
func MintFrom(c clock.Clock, entropy io.Reader, p Prefix) (string, error) {
	if !Registered(p) {
		return "", fmt.Errorf("%w: %q", ErrPrefix, p)
	}
	ms := c.Now().UnixMilli()
	if ms < 0 || ms > maxMillis {
		return "", ErrTime
	}
	var e [10]byte
	if _, err := io.ReadFull(entropy, e[:]); err != nil {
		return "", fmt.Errorf("ids: reading random bits: %w", err)
	}
	return compose(p, uint64(ms), e), nil
}

// Parse checks that id is a well-formed identifier with a registered prefix
// and returns the prefix.
func Parse(id string) (Prefix, error) {
	p, ulid, ok := strings.Cut(id, "_")
	if !ok || !validULID(ulid) || !ValidPrefixForm(Prefix(p)) {
		return "", fmt.Errorf("%w: %q", ErrForm, id)
	}
	if !Registered(Prefix(p)) {
		return "", fmt.Errorf("%w: %q", ErrPrefix, p)
	}
	return Prefix(p), nil
}

// Check checks that id is a well-formed identifier of the kind named by want.
func Check(id string, want Prefix) error {
	got, err := Parse(id)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("%w: %q is %q, not %q", ErrWrongPrefix, id, got, want)
	}
	return nil
}

// Valid reports whether id is a well-formed identifier with a registered
// prefix.
func Valid(id string) bool {
	_, err := Parse(id)
	return err == nil
}

// validULID reports whether s is 26 lowercase Crockford characters whose
// first is 0 to 7, the largest a 128-bit value allows.
func validULID(s string) bool {
	if len(s) != ULIDLength || s[0] > '7' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if decodeChar(s[i]) < 0 {
			return false
		}
	}
	return true
}

func decodeChar(c byte) int {
	i := strings.IndexByte(alphabet, c)
	return i
}

// compose writes the identifier of prefix p, a 48-bit time and 80 random bits.
func compose(p Prefix, ms uint64, entropy [10]byte) string {
	// The 128 bits are held as hi (64) and lo (64); the 130 bits written are
	// those 128 with two leading zero bits.
	hi := ms<<16 | uint64(entropy[0])<<8 | uint64(entropy[1])
	var lo uint64
	for _, b := range entropy[2:] {
		lo = lo<<8 | uint64(b)
	}
	var out [ULIDLength]byte
	for i := ULIDLength - 1; i >= 0; i-- {
		out[i] = alphabet[lo&31]
		lo = lo>>5 | hi<<59
		hi >>= 5
	}
	return string(p) + "_" + string(out[:])
}
