package exec

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Kind is what a program is to the package that runs it. It is one of the
// kinds of spec/distribution.md, What a Hive executable may run, and decides
// which names are admitted and which failure a program that is missing or
// unusable is.
type Kind string

// The kinds of program.
const (
	// Core is one of the four programs of the five that are not
	// no-mistakes: git, gh, claude and codex.
	Core Kind = "core"
	// Transitional is no-mistakes, which a declaration lists under the key
	// named transitional.
	Transitional Kind = "transitional"
	// Optional is the one named exception, chrome-devtools-axi.
	Optional Kind = "optional"
	// Project is a project's own declared validation command: the program
	// the project declares, found by the same rules.
	Project Kind = "project"
	// Hive is another Hive command, such as hive-pool.
	Hive Kind = "hive"
	// Platform is a platform utility the package declares with its reason.
	Platform Kind = "platform"
)

// Program is the program of a run: its kind and a bare name. A name is one
// word with no path separator, never a path and never a line of text.
type Program struct {
	Kind Kind
	Name string
}

var (
	coreNames         = []string{"git", "gh", "claude", "codex"}
	transitionalNames = []string{"no-mistakes"}
	optionalNames     = []string{"chrome-devtools-axi"}

	hiveForm     = regexp.MustCompile(`^hive-[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	platformForm = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

// Check says whether the program is one the contract admits for its kind.
func (p Program) Check() error {
	if err := checkBare(p.Name); err != nil {
		return err
	}
	bad := func(what string) error { return fmt.Errorf("exec: %q is not %s", p.Name, what) }
	switch p.Kind {
	case Core:
		if !slices.Contains(coreNames, p.Name) {
			return bad("one of git, gh, claude and codex")
		}
	case Transitional:
		if !slices.Contains(transitionalNames, p.Name) {
			return bad("no-mistakes")
		}
	case Optional:
		if !slices.Contains(optionalNames, p.Name) {
			return bad("chrome-devtools-axi")
		}
	case Hive:
		if !hiveForm.MatchString(p.Name) {
			return bad("the name of a Hive command")
		}
	case Platform:
		if !platformForm.MatchString(p.Name) {
			return bad("the name of a platform utility")
		}
	case Project:
	default:
		return fmt.Errorf("exec: %q is not a kind of program", string(p.Kind))
	}
	return nil
}

// checkBare refuses a name that is not one bare word.
func checkBare(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("exec: a program has a name")
	case name == "." || name == "..":
		return fmt.Errorf("exec: %q is not a program name", name)
	case strings.EqualFold(filepath.Ext(name), ".exe"):
		return fmt.Errorf("exec: %q ends in .exe; a program's name is a bare word with no extension on every platform", name)
	case strings.ContainsAny(name, `/\:`):
		return fmt.Errorf("exec: %q is a path, and a run names its program by a bare name", name)
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("exec: %q is a line of text, and a run names its program by a bare name", name)
		}
	}
	return nil
}

func (p Program) String() string { return string(p.Kind) + " " + p.Name }
