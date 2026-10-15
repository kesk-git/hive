//go:build windows

package store

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/kesk-git/hive/contract/exec"
	"github.com/kesk-git/hive/contract/internal/testsupport"
)

// On Windows the package access imports the store, so the store's own tests
// cannot import it without an import cycle Go refuses. newHome, secureHome and
// hasMode stand in for its helpers here as short wrappers over the store's own
// SecureDir and CheckOwnerOnly, so the logic is still in one place; they are
// not copies of it.
//
// newHome makes an empty home under a temporary directory, which the test
// removes.
func newHome(t testing.TB) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	secureHome(t, dir)
	return dir
}

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

// grantGroup gives the group Users read access to path; mode is for macOS and
// Linux.
func grantGroup(t testing.TB, path string, mode os.FileMode) {
	t.Helper()
	testsupport.GrantReadTo(t, path, "S-1-5-32-545")
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
// privilege. The platform's own utility makes it, run through the one
// process-execution path.
func junction(t testing.TB, target, link string) {
	t.Helper()
	run, err := exec.Start(exec.Spec{
		Program: exec.Program{Kind: exec.Platform, Name: "cmd"},
		Args:    []string{"/c", "mklink", "/J", link, target},
		Dir:     os.TempDir(),
		Stdout:  exec.Pipe(),
	})
	if err != nil {
		t.Skipf("mklink /J: %v", err)
	}
	defer run.Close()
	out, _ := io.ReadAll(run.Stdout())
	if res := run.Wait(); !res.Passed() {
		t.Skipf("mklink /J: %v: %s", res, out)
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

// The security descriptors this test reads are read through calls of its own,
// not through the store's checker, so that the store is not asked to approve
// what it made.
var (
	readerAdvapi32         = syscall.NewLazyDLL("advapi32.dll")
	readerGetNamedSecurity = readerAdvapi32.NewProc("GetNamedSecurityInfoW")
	readerSeFileObject     = uintptr(1)
	readerOwnerInformation = uintptr(0x1)
)

// ownerOf reads the owner of the entry at path from its security descriptor
// and returns the owner's security identifier in its string form.
func ownerOf(t testing.TB, path string) string {
	t.Helper()
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	var owner, descriptor unsafe.Pointer
	r, _, _ := readerGetNamedSecurity.Call(
		uintptr(unsafe.Pointer(name)), readerSeFileObject, readerOwnerInformation,
		uintptr(unsafe.Pointer(&owner)), 0, 0, 0, uintptr(unsafe.Pointer(&descriptor)))
	if r != 0 {
		t.Fatalf("reading the owner of %s: %v", path, syscall.Errno(r))
	}
	defer syscall.LocalFree(syscall.Handle(uintptr(descriptor)))
	if owner == nil {
		t.Fatalf("%s has no owner", path)
	}
	sid, err := (*syscall.SID)(owner).String()
	if err != nil {
		t.Fatal(err)
	}
	return sid
}

// tokenUser is the security identifier of the user in the token of this
// process, the user Hive runs as.
func tokenUser(t testing.TB) string {
	t.Helper()
	token, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid, err := user.User.Sid.String()
	if err != nil {
		t.Fatal(err)
	}
	return sid
}

// The user Hive runs as owns what the store makes: the directories MakeDir
// creates, every one it had to make on the way, and the file CreateFile
// creates, even where the process's token would own a new entry through its
// default owner, as an elevated token's does (spec/records.md, The home: "The
// owner is the user Hive runs as"). The owners are read from the security
// descriptors, not from the store's own check.
func TestMakeDirAndCreateFileAreOwnedByTheUser(t *testing.T) {
	home := newHome(t)
	dir := filepath.Join(home, "pool", "records", "thing")
	if err := MakeDir(dir); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "first.json")
	f, err := CreateFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	user := tokenUser(t)
	for _, path := range []string{
		home,
		filepath.Join(home, "pool"),
		filepath.Join(home, "pool", "records"),
		dir,
		file,
	} {
		if got := ownerOf(t, path); got != user {
			t.Errorf("%s is owned by %s, not by the user %s", path, got, user)
		}
	}
}
