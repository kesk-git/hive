//go:build windows

package store

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// secureHome makes the home at dir what spec/records.md says a home is on
// Windows: a protected access-control list of its owner alone, which what is
// made in it inherits.
func secureHome(t testing.TB, dir string) {
	t.Helper()
	if err := SecureDir(dir); err != nil {
		t.Fatal(err)
	}
}

// hasMode reports whether the entry at path is open to its owner only. There
// are no mode bits to compare on Windows, so the access-control list is what
// is read.
func hasMode(info fs.FileInfo, path string, want os.FileMode) bool {
	return CheckOwnerOnly(path) == nil
}

// grantTo adds an entry that gives the security identifier sid read access to
// path, which is what a check of the home refuses.
func grantTo(t testing.TB, path, sid string) {
	t.Helper()
	if out, err := exec.Command("icacls", path, "/grant", "*"+sid+":(R)").CombinedOutput(); err != nil {
		t.Fatalf("icacls: %v: %s", err, out)
	}
}

// grantOthers gives Everyone read access to path; mode is for macOS and Linux.
func grantOthers(t testing.TB, path string, mode os.FileMode) {
	t.Helper()
	grantTo(t, path, "S-1-1-0")
}

// grantGroup gives the group Users read access to path; mode is for macOS and
// Linux.
func grantGroup(t testing.TB, path string, mode os.FileMode) {
	t.Helper()
	grantTo(t, path, "S-1-5-32-545")
}

// symlink makes link a symbolic link to target, which Windows allows only
// to a user with the privilege; a test that cannot make one is skipped.
func symlink(t testing.TB, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
}

// junction makes link a junction to the directory target, which needs no
// privilege.
func junction(t testing.TB, target, link string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v: %s", err, out)
	}
}

// platformLayoutCases are the homes only this platform refuses: a known
// directory and an unknown entry that are junctions, which are reparse points
// that are not symbolic links.
var platformLayoutCases = []layoutCase{
	{"a known directory that is a junction", func(t *testing.T, home string) string {
		os.Remove(filepath.Join(home, "pool"))
		junction(t, filepath.Join(home, "ui"), filepath.Join(home, "pool"))
		return home
	}, ErrLayout},
	{"an unknown entry that is a junction", func(t *testing.T, home string) string {
		junction(t, filepath.Join(home, "ui"), filepath.Join(home, "Backup"))
		return home
	}, ErrLayout},
}

// refuseReplace makes the replacement of the record at rel fail the way a
// handle outside Hive does, an indexer's or a scanner's: the record is held
// open without delete sharing, so the rename is refused until the handle is
// closed, and the write gives up after a short wait.
func refuseReplace(t testing.TB, s *Store, rel string) (restore func(), ok bool) {
	t.Helper()
	f, err := os.Open(s.abs(rel))
	if err != nil {
		t.Fatal(err)
	}
	wait := renameWait
	renameWait = 50 * time.Millisecond
	return func() {
		renameWait = wait
		f.Close()
	}, true
}

// OpenPrivate refuses a file another handle has open, which could read what
// is written after its access is closed, and opens it once that handle is
// gone.
func TestOpenPrivateRefusesAnOpenFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	secureHome(t, dir)
	path := filepath.Join(dir, "response.json")
	if err := os.WriteFile(path, nil, fileMode); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenRead(path)
	if err != nil {
		t.Fatal(err)
	}
	if f, err := OpenPrivate(path); !errors.Is(err, errorSharingViolation) {
		if err == nil {
			f.Close()
		}
		t.Fatalf("a file open elsewhere: %v", err)
	}
	reader.Close()
	f, err := OpenPrivate(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
}
