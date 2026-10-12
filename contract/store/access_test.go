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

// A file made in a secured directory is owner-only too, and SecureFile closes
// a file that was open to others.
func TestSecureFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	secureHome(t, dir)
	path := filepath.Join(dir, "file")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, fileMode)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := CheckOwnerOnly(path); err != nil {
		t.Fatalf("a file made in a secured directory: %v", err)
	}
	grantOthers(t, path, 0o644)
	if err := CheckOwnerOnly(path); !errors.Is(err, ErrLayout) {
		t.Fatalf("a file open to others: %v", err)
	}
	if err := SecureFile(f); err != nil {
		t.Fatal(err)
	}
	if err := CheckOwnerOnly(path); err != nil {
		t.Fatalf("a file secured: %v", err)
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
