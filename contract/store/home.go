package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/schema"
)

// The top level of a home (spec/records.md, The home): one file and the
// directories, in the order of the table.
const (
	homeFile   = "home.json"
	journalDir = "journal"
	locksDir   = "locks"
)

var knownDirs = []string{journalDir, locksDir, "pool", "spawning-pool", "dispatch", "shadow", "synapse", "ui", "reviews"}

// storePackages are the packages with a directory of their own in a home, in
// the order of the table. word persists through pool and has none.
var storePackages = []string{"pool", "spawning-pool", "dispatch", "shadow", "synapse", "ui"}

// homeSchema is the schema of home.json.
const homeSchema = "urn:hive:schema:home:v1"

// ownerOnly is the access of every directory and file of a home.
const (
	dirMode  = 0o700
	fileMode = 0o600
)

// Home is a private home whose layout has been checked.
type Home struct {
	// Dir is the home's directory: absolute, with symbolic links resolved.
	Dir string
	// HomeID is the home_id of home.json, or "" while the home has no
	// identity record yet.
	HomeID string
	// Unknown lists the top-level entries this specification does not name,
	// sorted. Hive leaves them alone, and a command says so in its response.
	Unknown []string
}

// CheckHome checks the layout of the home at dir: it is a directory, it is
// not inside a Git working tree, it and every known entry are open to their
// owner only, every known entry is of the kind the table fixes, and home.json,
// when present, is a valid home record. An entry the table does not name is
// listed, not refused. A directory with nothing in it yet is a valid home.
func CheckHome(dir string) (*Home, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("store: home: %w", err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, fmt.Errorf("store: home: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a directory", ErrLayout, dir)
	}
	if err := ownerOnly(info, "the home"); err != nil {
		return nil, err
	}
	if err := insideRepository(real); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(real)
	if err != nil {
		return nil, fmt.Errorf("store: home: %w", err)
	}
	h := &Home{Dir: real}
	for _, e := range entries {
		name := e.Name()
		info, err := os.Lstat(filepath.Join(real, name))
		if err != nil {
			return nil, fmt.Errorf("store: home: %w", err)
		}
		switch {
		case name == homeFile:
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("%w: %s is not a regular file", ErrLayout, name)
			}
			if err := ownerOnly(info, name); err != nil {
				return nil, err
			}
			id, err := readHomeRecord(filepath.Join(real, name))
			if err != nil {
				return nil, err
			}
			h.HomeID = id
		case slices.Contains(knownDirs, name):
			if !info.IsDir() {
				return nil, fmt.Errorf("%w: %s is not a directory", ErrLayout, name)
			}
			if err := ownerOnly(info, name); err != nil {
				return nil, err
			}
		default:
			h.Unknown = append(h.Unknown, name)
		}
	}
	slices.Sort(h.Unknown)
	return h, nil
}

// ownerOnly refuses an entry that grants access to anyone but its owner.
func ownerOnly(info fs.FileInfo, what string) error {
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("%w: %s is open beyond its owner (mode %04o)", ErrLayout, what, perm)
	}
	return nil
}

// insideRepository refuses a directory that is, or is under, a Git working
// tree: one with a .git entry, a directory in a main working tree and a file
// in a linked one.
func insideRepository(dir string) error {
	for p := dir; ; {
		if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
			return fmt.Errorf("%w: %s is under %s", ErrInsideRepository, dir, p)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return nil
		}
		p = parent
	}
}

// readHomeRecord reads home.json through the embedded schema and returns its
// home_id. A file that is not a valid home record is malformed, and a cached
// or guessed identity never stands in for it.
func readHomeRecord(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrRecordMalformed, homeFile, err)
	}
	if err := canon.CheckFile(data); err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrRecordMalformed, homeFile, err)
	}
	reg, err := schema.Embedded()
	if err != nil {
		return "", err
	}
	if err := reg.Validate(homeSchema, data); err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrRecordMalformed, homeFile, err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrRecordMalformed, homeFile, err)
	}
	id, _ := v.(map[string]any)["home_id"].(string)
	return id, nil
}

// errNotExist reports whether err says a file is missing.
func errNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
