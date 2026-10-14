//go:build windows

package testsupport

import (
	"fmt"
	"os"
	"syscall"
	"testing"
	"unsafe"
)

var (
	advapi32                      = syscall.NewLazyDLL("advapi32.dll")
	procConvertStringSecurityDesc = advapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
	procGetSecurityDescriptorDacl = advapi32.NewProc("GetSecurityDescriptorDacl")
	procSetNamedSecurityInfo      = advapi32.NewProc("SetNamedSecurityInfoW")
)

const (
	sddlRevision1                    = 1
	seFileObject                     = 1
	daclSecurityInformation          = 0x4
	protectedDaclSecurityInformation = 0x80000000
)

// GrantReadTo gives the security identifier sid read access to path, besides
// the user's own full access, and nothing else: the entry's list becomes
// protected and holds those two entries, which is what a check of the home
// refuses. A directory's entries are inheritable. It sets the list through the
// platform's security calls, as the store does for a private entry, and starts
// no process.
func GrantReadTo(t testing.TB, path, sid string) {
	t.Helper()
	if err := grantReadTo(path, sid); err != nil {
		t.Fatal(err)
	}
}

// GrantOthers gives Everyone read access to path; mode is for macOS and Linux.
func GrantOthers(t testing.TB, path string, mode os.FileMode) {
	t.Helper()
	GrantReadTo(t, path, "S-1-1-0")
}

func grantReadTo(path, sid string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	token, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return fmt.Errorf("the process token: %w", err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return fmt.Errorf("the user of the process: %w", err)
	}
	owner, err := user.User.Sid.String()
	if err != nil {
		return err
	}
	inherit := ""
	if info.IsDir() {
		inherit = "OICI"
	}
	sddl, err := syscall.UTF16PtrFromString(fmt.Sprintf("D:P(A;%[1]s;FA;;;%[2]s)(A;%[1]s;FR;;;%[3]s)", inherit, owner, sid))
	if err != nil {
		return err
	}
	var descriptor, dacl unsafe.Pointer
	if r, _, err := procConvertStringSecurityDesc.Call(uintptr(unsafe.Pointer(sddl)), sddlRevision1, uintptr(unsafe.Pointer(&descriptor)), 0); r == 0 {
		return fmt.Errorf("building the security descriptor: %w", err)
	}
	defer syscall.LocalFree(syscall.Handle(uintptr(descriptor)))
	var present, defaulted uint32
	if r, _, err := procGetSecurityDescriptorDacl.Call(uintptr(descriptor), uintptr(unsafe.Pointer(&present)), uintptr(unsafe.Pointer(&dacl)), uintptr(unsafe.Pointer(&defaulted))); r == 0 {
		return fmt.Errorf("reading the security descriptor: %w", err)
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r, _, _ := procSetNamedSecurityInfo.Call(
		uintptr(unsafe.Pointer(name)), seFileObject, daclSecurityInformation|protectedDaclSecurityInformation,
		0, 0, uintptr(dacl), 0)
	if r != 0 {
		return fmt.Errorf("setting the access of %s: %w", path, syscall.Errno(r))
	}
	return nil
}
