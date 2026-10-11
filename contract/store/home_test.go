package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fullHome builds a home with every entry of the table in spec/records.md.
func fullHome(t *testing.T) string {
	t.Helper()
	home := newHome(t)
	for _, d := range knownDirs {
		if err := os.Mkdir(filepath.Join(home, d), dirMode); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile("testdata/home/home.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, homeFile), data, fileMode); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestCheckHomeValidLayouts(t *testing.T) {
	t.Run("every entry of the table", func(t *testing.T) {
		home := fullHome(t)
		h, err := CheckHome(home)
		if err != nil {
			t.Fatal(err)
		}
		if h.HomeID != "home_01arz3ndektsv4rrffq69g5fav" {
			t.Errorf("HomeID = %q", h.HomeID)
		}
		if len(h.Unknown) != 0 {
			t.Errorf("Unknown = %v", h.Unknown)
		}
		real, _ := filepath.EvalSymlinks(home)
		if h.Dir != real {
			t.Errorf("Dir = %q, want %q", h.Dir, real)
		}
	})
	t.Run("a directory with nothing in it yet", func(t *testing.T) {
		h, err := CheckHome(newHome(t))
		if err != nil {
			t.Fatal(err)
		}
		if h.HomeID != "" || len(h.Unknown) != 0 {
			t.Errorf("Home = %+v", h)
		}
	})
	t.Run("a relative path", func(t *testing.T) {
		home := fullHome(t)
		wd, _ := os.Getwd()
		rel, err := filepath.Rel(wd, home)
		if err != nil {
			t.Skip(err)
		}
		h, err := CheckHome(rel)
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsAbs(h.Dir) {
			t.Errorf("Dir = %q is not absolute", h.Dir)
		}
	})
	t.Run("a home reached through a symbolic link", func(t *testing.T) {
		home := fullHome(t)
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(home, link); err != nil {
			t.Skip(err)
		}
		h, err := CheckHome(link)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(h.Dir, "link") {
			t.Errorf("Dir = %q keeps the link", h.Dir)
		}
	})
}

// An entry the table does not name is listed and left alone, never refused.
func TestCheckHomeListsUnknownEntries(t *testing.T) {
	home := fullHome(t)
	for _, name := range []string{"notes.txt", "Backup", ".DS_Store"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(home, "zzz"), 0o755); err != nil {
		t.Fatal(err)
	}
	h, err := CheckHome(home)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".DS_Store", "Backup", "notes.txt", "zzz"}; !slices.Equal(h.Unknown, want) {
		t.Errorf("Unknown = %v, want %v", h.Unknown, want)
	}
	if _, err := os.Stat(filepath.Join(home, "notes.txt")); err != nil {
		t.Error("the unknown entry was not left alone")
	}
}

func TestCheckHomeRefusesInvalidLayouts(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, home string) string // returns the path to check
		want  error
	}{
		{"a directory that is not there", func(t *testing.T, home string) string {
			return filepath.Join(home, "missing")
		}, fs.ErrNotExist},
		{"a file", func(t *testing.T, home string) string {
			p := filepath.Join(home, "file")
			os.WriteFile(p, []byte("x"), fileMode)
			return p
		}, ErrLayout},
		{"a home open to others", func(t *testing.T, home string) string {
			os.Chmod(home, 0o755)
			return home
		}, ErrLayout},
		{"a home open to its group", func(t *testing.T, home string) string {
			os.Chmod(home, 0o750)
			return home
		}, ErrLayout},
		{"a known directory that is a file", func(t *testing.T, home string) string {
			os.Remove(filepath.Join(home, journalDir))
			os.WriteFile(filepath.Join(home, journalDir), []byte("x"), fileMode)
			return home
		}, ErrLayout},
		{"a known directory that is a symbolic link", func(t *testing.T, home string) string {
			os.Remove(filepath.Join(home, "pool"))
			os.Symlink(filepath.Join(home, "ui"), filepath.Join(home, "pool"))
			return home
		}, ErrLayout},
		{"a known directory open to others", func(t *testing.T, home string) string {
			os.Chmod(filepath.Join(home, locksDir), 0o755)
			return home
		}, ErrLayout},
		{"home.json that is a directory", func(t *testing.T, home string) string {
			os.Remove(filepath.Join(home, homeFile))
			os.Mkdir(filepath.Join(home, homeFile), dirMode)
			return home
		}, ErrLayout},
		{"home.json open to others", func(t *testing.T, home string) string {
			os.Chmod(filepath.Join(home, homeFile), 0o644)
			return home
		}, ErrLayout},
		{"home.json without its line feed", func(t *testing.T, home string) string {
			p := filepath.Join(home, homeFile)
			data, _ := os.ReadFile(p)
			os.WriteFile(p, data[:len(data)-1], fileMode)
			return home
		}, ErrRecordMalformed},
		{"home.json torn", func(t *testing.T, home string) string {
			p := filepath.Join(home, homeFile)
			data, _ := os.ReadFile(p)
			os.WriteFile(p, data[:len(data)/2], fileMode)
			return home
		}, ErrRecordMalformed},
		{"home.json that fails its schema", func(t *testing.T, home string) string {
			p := filepath.Join(home, homeFile)
			data, _ := os.ReadFile(p)
			os.WriteFile(p, []byte(strings.Replace(string(data), `"role":"hive"`, `"role":"other"`, 1)), fileMode)
			return home
		}, ErrRecordMalformed},
		{"home.json of a fleet without its workspace", func(t *testing.T, home string) string {
			p := filepath.Join(home, homeFile)
			data, _ := os.ReadFile(p)
			os.WriteFile(p, []byte(strings.Replace(string(data), `"role":"hive"`, `"role":"fleet"`, 1)), fileMode)
			return home
		}, ErrRecordMalformed},
		{"home.json of a newer minor version", func(t *testing.T, home string) string {
			newerHome(t, home, "1.1")
			return home
		}, ErrUnsupportedVersion},
		{"home.json of a newer major version", func(t *testing.T, home string) string {
			newerHome(t, home, "2.0")
			return home
		}, ErrUnsupportedVersion},
		{"home.json with a version that is not one", func(t *testing.T, home string) string {
			newerHome(t, home, "one")
			return home
		}, ErrRecordMalformed},
		{"home.json of an older major version", func(t *testing.T, home string) string {
			newerHome(t, home, "0.9")
			return home
		}, ErrRecordMalformed},
		{"home.json empty", func(t *testing.T, home string) string {
			os.WriteFile(filepath.Join(home, homeFile), nil, fileMode)
			return home
		}, ErrRecordMalformed},
		{"a .git directory in the home", func(t *testing.T, home string) string {
			os.Mkdir(filepath.Join(home, ".git"), dirMode)
			return home
		}, ErrInsideRepository},
		{"a .git file in the home, as a linked worktree has", func(t *testing.T, home string) string {
			os.WriteFile(filepath.Join(home, ".git"), []byte("gitdir: elsewhere\n"), fileMode)
			return home
		}, ErrInsideRepository},
		{"a .git directory above the home", func(t *testing.T, home string) string {
			os.Mkdir(filepath.Join(filepath.Dir(home), ".git"), dirMode)
			return home
		}, ErrInsideRepository},
		{"a .git directory two levels above the home", func(t *testing.T, home string) string {
			os.Mkdir(filepath.Join(filepath.Dir(filepath.Dir(home)), ".git"), dirMode)
			return home
		}, ErrInsideRepository},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := fullHome(t)
			path := tc.setup(t, home)
			h, err := CheckHome(path)
			if !errors.Is(err, tc.want) {
				t.Fatalf("CheckHome = %+v, %v; want %v", h, err, tc.want)
			}
			if h != nil {
				t.Errorf("a refused home was returned: %+v", h)
			}
		})
	}
}

// newerHome gives home.json the schema version given.
func newerHome(t *testing.T, home, version string) {
	t.Helper()
	p := filepath.Join(home, homeFile)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"schema_version":"1.0"`, `"schema_version":"`+version+`"`, 1))
	if err := os.WriteFile(p, data, fileMode); err != nil {
		t.Fatal(err)
	}
}

// A newer home record is refused as unsupported_version and never as a
// malformed one, by CheckHome and by every Open.
func TestNewerHomeIsUnsupported(t *testing.T) {
	home := fullHome(t)
	newerHome(t, home, "1.1")
	before := tree(t, home)
	_, err := CheckHome(home)
	if !errors.Is(err, ErrUnsupportedVersion) || errors.Is(err, ErrRecordMalformed) || Code(err) != "unsupported_version" {
		t.Fatalf("CheckHome = %v, code %q", err, Code(err))
	}
	if _, err := Open(home, "dispatch", Options{}); !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("Open = %v", err)
	}
	sameTree(t, "after a refused home", tree(t, home), before)
}

func TestCheckHomeNamesWhatIsWrong(t *testing.T) {
	home := fullHome(t)
	os.WriteFile(filepath.Join(home, journalDir, "x"), nil, fileMode)
	os.Remove(filepath.Join(home, "shadow"))
	os.WriteFile(filepath.Join(home, "shadow"), []byte("x"), fileMode)
	_, err := CheckHome(home)
	if err == nil || !strings.Contains(err.Error(), "shadow") {
		t.Fatalf("error %v does not name the entry", err)
	}
}

func TestOpenCreatesTheStoreAndTheLocks(t *testing.T) {
	for _, pkg := range storePackages {
		t.Run(pkg, func(t *testing.T) {
			home := newHome(t)
			s, _, _ := open(t, home, pkg)
			if s.Package() != pkg {
				t.Errorf("Package = %q", s.Package())
			}
			for _, d := range []string{pkg, locksDir} {
				info, err := os.Stat(filepath.Join(home, d))
				if err != nil {
					t.Fatal(err)
				}
				if !info.IsDir() || info.Mode().Perm() != dirMode {
					t.Errorf("%s: mode %v", d, info.Mode())
				}
			}
			// The home it made is still a valid home, and opening again changes nothing.
			if _, err := CheckHome(home); err != nil {
				t.Fatal(err)
			}
			before := files(t, home)
			open(t, home, pkg)
			if after := files(t, home); !slices.Equal(before, after) {
				t.Errorf("a second Open changed the home: %v -> %v", before, after)
			}
		})
	}
}

func TestOpenRefuses(t *testing.T) {
	home := newHome(t)
	for _, pkg := range []string{"word", "contract", "journal", "locks", "Pool", "", "../pool"} {
		if _, err := Open(home, pkg, Options{}); err == nil {
			t.Errorf("Open(%q) succeeded", pkg)
		}
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("a refused Open created %v", entries)
	}
	os.Mkdir(filepath.Join(home, ".git"), dirMode)
	_, err := Open(home, "pool", Options{})
	if !errors.Is(err, ErrInsideRepository) || Code(err) != "home_inside_repository" {
		t.Errorf("Open inside a repository: %v, code %q", err, Code(err))
	}
}

func TestCode(t *testing.T) {
	tests := map[error]string{
		ErrRecordMalformed:           "record_malformed",
		ErrUnsupportedVersion:        "unsupported_version",
		ErrRevisionConflict:          "revision_conflict",
		ErrAlreadyExists:             "already_exists",
		ErrLockHeld:                  "lock_held",
		ErrInsideRepository:          "home_inside_repository",
		ErrLayout:                    "",
		ErrNoJournal:                 "",
		errors.New("something else"): "",
		fs.ErrNotExist:               "",
		errors.Join(ErrLayout, ErrRecordMalformed): "record_malformed",
	}
	for err, want := range tests {
		if got := Code(err); got != want {
			t.Errorf("Code(%v) = %q, want %q", err, got, want)
		}
	}
}
