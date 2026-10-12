package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

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

// The schema of home.json: its $id, the name its records carry and the
// newest version this build reads.
const (
	homeSchema  = "urn:hive:schema:home:v1"
	homeRecord  = "hive.home"
	homeVersion = "1.0"
)

var versionForm = regexp.MustCompile(`^([1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// checkVersion refuses, as ErrUnsupportedVersion, a record of the schema
// name whose schema_version is newer than known, in its major or its minor
// part. A record that is of another schema or has no version of the form
// MAJOR.MINOR is left to its schema to refuse as malformed.
func checkVersion(obj map[string]any, name, known string) error {
	if obj["schema"] != name {
		return nil
	}
	v, _ := obj["schema_version"].(string)
	got, want := versionForm.FindStringSubmatch(v), versionForm.FindStringSubmatch(known)
	if got == nil || want == nil {
		return nil
	}
	if c := cmpNumbers(got[1], want[1]); c > 0 || c == 0 && cmpNumbers(got[2], want[2]) > 0 {
		return fmt.Errorf("%w: %s is at version %s, this build reads up to %s", ErrUnsupportedVersion, name, v, known)
	}
	return nil
}

// cmpNumbers compares two decimal numbers written without leading zeros.
func cmpNumbers(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

// dirMode and fileMode are the access of every directory and file of a home
// on macOS and Linux. On Windows the access-control list of the home gives
// the same access, and the modes mean nothing.
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

// CheckHome checks the layout of the home at dir: it is a directory, on
// Windows on a local NTFS volume, it is not inside a Git working tree, it and
// every known entry are open to their owner only, every known entry is of the
// kind the table fixes, and home.json, when present, is a valid home record.
// An entry the table does not name is listed, not refused. A directory with
// nothing in it yet is a valid home.
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
	if err := checkHomeDir(real); err != nil {
		return nil, err
	}
	if err := ownerOnly(real, info, "the home"); err != nil {
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
		path, err := homePath(real, name)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("store: home: %w", err)
		}
		switch {
		case name == homeFile:
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("%w: %s is not a regular file", ErrLayout, name)
			}
			if err := ownerOnly(path, info, name); err != nil {
				return nil, err
			}
			id, err := readHomeRecord(path)
			if err != nil {
				return nil, err
			}
			h.HomeID = id
		case slices.Contains(knownDirs, name):
			if !info.IsDir() {
				return nil, fmt.Errorf("%w: %s is not a directory", ErrLayout, name)
			}
			if err := ownerOnly(path, info, name); err != nil {
				return nil, err
			}
		default:
			h.Unknown = append(h.Unknown, name)
		}
	}
	slices.Sort(h.Unknown)
	return h, nil
}

// Path is the absolute path of rel, a slash path below the home, once every
// component of it that is there has been checked, from the home down: none
// is a symbolic link, a Windows junction or any other reparse point
// (spec/records.md, The home). The check ends below the last directory, where
// nothing more is there to follow. Every path the library reads, writes,
// creates, lists or removes in a home is resolved here first.
func (h *Home) Path(rel string) (string, error) { return homePath(h.Dir, rel) }

// homePath is Path for the home at dir.
func homePath(dir, rel string) (string, error) {
	p := dir
	for _, seg := range strings.Split(rel, "/") {
		p = filepath.Join(p, seg)
		isDir, err := checkEntry(p)
		if err != nil {
			return "", err
		}
		if !isDir {
			break
		}
	}
	return filepath.Join(dir, filepath.FromSlash(rel)), nil
}

// insideRepository refuses a directory that is, or is under, a Git working
// tree: one with a .git entry, a directory in a main working tree and a file
// in a linked one. The home's own .git is looked for through its Path.
func insideRepository(dir string) error {
	git, err := homePath(dir, ".git")
	if err != nil {
		return err
	}
	for p := dir; ; {
		if _, err := os.Lstat(git); err == nil {
			return fmt.Errorf("%w: %s is under %s", ErrInsideRepository, dir, p)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return nil
		}
		p = parent
		git = filepath.Join(p, ".git")
	}
}

// readHomeRecord reads home.json through the embedded schema and returns its
// home_id. A file that is not a valid home record is malformed, and a cached
// or guessed identity never stands in for it. A home record of a newer
// version is refused as unsupported.
func readHomeRecord(path string) (string, error) {
	data, err := ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrRecordMalformed, homeFile, err)
	}
	if err := canon.CheckFile(data); err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrRecordMalformed, homeFile, err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrRecordMalformed, homeFile, err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return "", fmt.Errorf("%w: %s: a record is a JSON object", ErrRecordMalformed, homeFile)
	}
	if err := checkVersion(obj, homeRecord, homeVersion); err != nil {
		return "", fmt.Errorf("%w: %s", err, homeFile)
	}
	reg, err := schema.Embedded()
	if err != nil {
		return "", err
	}
	if err := reg.Validate(homeSchema, data); err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrRecordMalformed, homeFile, err)
	}
	id, _ := obj["home_id"].(string)
	return id, nil
}

// errNotExist reports whether err says a file is missing.
func errNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
