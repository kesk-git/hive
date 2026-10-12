package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// SecureDir makes a directory what a check of the home accepts, and a check
// refuses it again once someone else is given access.
func TestSecureDirAndCheckOwnerOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SecureDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := CheckOwnerOnly(dir); err != nil {
		t.Fatalf("a secured directory: %v", err)
	}
	grantOthers(t, dir, 0o755)
	if err := CheckOwnerOnly(dir); !errors.Is(err, ErrLayout) {
		t.Fatalf("a directory open to others: %v", err)
	}
	if err := SecureDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := CheckOwnerOnly(dir); err != nil {
		t.Fatalf("a directory secured again: %v", err)
	}
	if err := CheckOwnerOnly(filepath.Join(dir, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a missing entry: %v", err)
	}
}

// A file made in a secured directory is owner-only too, and OpenPrivate closes
// a file that was open to others before anything is written to it.
func TestOpenPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	secureHome(t, dir)
	path := filepath.Join(dir, "file")
	f, err := OpenPrivate(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("first"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := CheckOwnerOnly(path); err != nil {
		t.Fatalf("a file made private: %v", err)
	}
	grantOthers(t, path, 0o644)
	if err := CheckOwnerOnly(path); !errors.Is(err, ErrLayout) {
		t.Fatalf("a file open to others: %v", err)
	}
	f, err = OpenPrivate(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := CheckOwnerOnly(path); err != nil {
		t.Fatalf("a file opened private again: %v", err)
	}
	if data, err := ReadFile(path); err != nil || string(data) != "first" {
		t.Errorf("opening for writing must not truncate: %q, %v", data, err)
	}
}

// OpenRead and ReadFile read what is there and say what is missing with
// fs.ErrNotExist.
func TestReadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("content\n"), fileMode); err != nil {
		t.Fatal(err)
	}
	if data, err := ReadFile(path); err != nil || string(data) != "content\n" {
		t.Errorf("ReadFile = %q, %v", data, err)
	}
	f, err := OpenRead(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := ReadFile(path + ".missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReadFile of a missing file: %v", err)
	}
	if _, err := OpenRead(path + ".missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("OpenRead of a missing file: %v", err)
	}
}

// A link inside a package's own directory is refused before anything is read,
// written or removed through it, and what it leads to is left alone: here
// tmp/, which a writer empties of what a crash left, leads out of the home.
func TestLinkInAPackageIsRefused(t *testing.T) {
	home := newHome(t)
	s, _, _ := open(t, home, "pool")
	outside := t.TempDir()
	kept := filepath.Join(outside, "kept.txt")
	if err := os.WriteFile(kept, []byte("kept\n"), fileMode); err != nil {
		t.Fatal(err)
	}
	symlink(t, outside, filepath.Join(home, "pool", tmpDir))
	if w, err := s.Lock(); !errors.Is(err, ErrLayout) {
		if err == nil {
			w.Release()
		}
		t.Fatalf("Lock through a linked tmp/: %v", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("what the link leads to was not left alone: %v", err)
	}
}
