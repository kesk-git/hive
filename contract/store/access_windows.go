//go:build windows

package store

import (
	"encoding/binary"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"unsafe"
)

// The access-control calls are in advapi32, which the standard library's
// syscall package does not wrap; they are reached through its lazy DLL
// loader.
var (
	advapi32                 = syscall.NewLazyDLL("advapi32.dll")
	procGetNamedSecurityInfo = advapi32.NewProc("GetNamedSecurityInfoW")
	procSetNamedSecurityInfo = advapi32.NewProc("SetNamedSecurityInfoW")
	procInitializeAcl        = advapi32.NewProc("InitializeAcl")
	procAddAccessAllowedAce  = advapi32.NewProc("AddAccessAllowedAceEx")
)

const (
	seFileObject                     = 1
	daclSecurityInformation          = 0x4
	protectedDaclSecurityInformation = 0x80000000

	aclRevision = 2

	// fileAllAccess is every access right on a file or directory.
	fileAllAccess = 0x001F01FF

	// What an entry made in a directory inherits from it.
	objectInheritACE    = 0x1
	containerInheritACE = 0x2

	// The types of an access-control entry that grant access.
	accessAllowedACE               = 0
	accessAllowedCompoundACE       = 4
	accessAllowedObjectACE         = 5
	accessAllowedCallbackACE       = 9
	accessAllowedCallbackObjectACE = 11

	aclHeaderSize = 8
	aceHeaderSize = 4
)

// userSID is the security identifier of the user Hive runs as.
func userSID() (*syscall.SID, error) {
	token, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return nil, fmt.Errorf("store: the process token: %w", err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("store: the user of the process: %w", err)
	}
	return user.User.Sid, nil
}

// ownerOnly refuses an entry that grants access to anyone but its owner: one
// whose access-control list has an entry that allows a security identifier
// other than the user's own, or no list at all, which gives everyone full
// access. what names the entry in the error; path is where it is.
func ownerOnly(path string, info fs.FileInfo, what string) error {
	user, err := userSID()
	if err != nil {
		return err
	}
	own, err := user.String()
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	granted, err := grantedSIDs(path)
	if err != nil {
		return fmt.Errorf("store: reading the access of %s: %w", what, err)
	}
	for _, sid := range granted {
		if sid != own {
			return fmt.Errorf("%w: %s is open beyond its owner (its access-control list grants access to %s)", ErrLayout, what, sid)
		}
	}
	return nil
}

// grantedSIDs lists the security identifiers, as strings, that the
// discretionary access-control list of the entry at path allows. An entry with
// no list allows everyone.
func grantedSIDs(path string) ([]string, error) {
	name, err := syscall.UTF16PtrFromString(extendedPath(path))
	if err != nil {
		return nil, err
	}
	var dacl, descriptor unsafe.Pointer
	r, _, _ := procGetNamedSecurityInfo.Call(
		uintptr(unsafe.Pointer(name)), seFileObject, daclSecurityInformation, 0, 0,
		uintptr(unsafe.Pointer(&dacl)), 0, uintptr(unsafe.Pointer(&descriptor)))
	if r != 0 {
		return nil, syscall.Errno(r)
	}
	defer syscall.LocalFree(syscall.Handle(uintptr(descriptor)))
	if dacl == nil {
		return []string{"everyone (the entry has no access-control list)"}, nil
	}
	size := int(binary.LittleEndian.Uint16(unsafe.Slice((*byte)(unsafe.Add(dacl, 2)), 2)))
	acl := unsafe.Slice((*byte)(dacl), size)
	count := int(binary.LittleEndian.Uint16(acl[4:6]))
	var granted []string
	for off, i := aclHeaderSize, 0; i < count; i++ {
		if off+aceHeaderSize > len(acl) {
			return nil, fmt.Errorf("the access-control list is cut short")
		}
		ace := acl[off:]
		aceSize := int(binary.LittleEndian.Uint16(ace[2:4]))
		if aceSize < aceHeaderSize || aceSize > len(ace) {
			return nil, fmt.Errorf("an access-control entry is cut short")
		}
		ace = ace[:aceSize]
		off += aceSize
		sidAt := 0
		switch ace[0] {
		case accessAllowedACE, accessAllowedCallbackACE:
			sidAt = aceHeaderSize + 4 // the mask
		case accessAllowedObjectACE, accessAllowedCallbackObjectACE:
			// The mask and the flags, then the two object identifiers the
			// flags say are present, then the identifier.
			if len(ace) < aceHeaderSize+8 {
				return nil, fmt.Errorf("an access-control entry is cut short")
			}
			sidAt = aceHeaderSize + 8
			flags := binary.LittleEndian.Uint32(ace[aceHeaderSize+4:])
			if flags&1 != 0 {
				sidAt += 16
			}
			if flags&2 != 0 {
				sidAt += 16
			}
		case accessAllowedCompoundACE:
			granted = append(granted, "a compound access-control entry")
			continue
		default:
			// A denial, an audit or a label grants nothing.
			continue
		}
		if sidAt+8 > len(ace) {
			return nil, fmt.Errorf("an access-control entry is cut short")
		}
		sid := (*syscall.SID)(unsafe.Pointer(&ace[sidAt]))
		s, err := sid.String()
		if err != nil {
			return nil, err
		}
		granted = append(granted, s)
	}
	return granted, nil
}

// setOwnerOnly replaces the access-control list of the entry at path with one
// that inherits nothing from its parent and allows the user's own security
// identifier alone, with the entry inheriting flags carries: none for a file,
// the two that reach a directory's files and directories for a directory.
func setOwnerOnly(path string, inherit uint32) error {
	user, err := userSID()
	if err != nil {
		return err
	}
	name, err := syscall.UTF16PtrFromString(extendedPath(path))
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	// One entry: the list's header, the entry's header and mask, the
	// identifier, in whole four-byte words.
	size := (aclHeaderSize + aceHeaderSize + 4 + user.Len() + 3) &^ 3
	acl := make([]uint32, size/4)
	if r, _, err := procInitializeAcl.Call(uintptr(unsafe.Pointer(&acl[0])), uintptr(size), aclRevision); r == 0 {
		return fmt.Errorf("store: building the access-control list: %w", err)
	}
	if r, _, err := procAddAccessAllowedAce.Call(uintptr(unsafe.Pointer(&acl[0])), aclRevision, uintptr(inherit), fileAllAccess, uintptr(unsafe.Pointer(user))); r == 0 {
		return fmt.Errorf("store: building the access-control list: %w", err)
	}
	r, _, _ := procSetNamedSecurityInfo.Call(
		uintptr(unsafe.Pointer(name)), seFileObject, daclSecurityInformation|protectedDaclSecurityInformation,
		0, 0, uintptr(unsafe.Pointer(&acl[0])), 0)
	if r != 0 {
		return fmt.Errorf("store: setting the access of %s: %w", path, syscall.Errno(r))
	}
	return nil
}

// SecureDir makes the directory at dir open to its owner only, as
// spec/records.md gives a home on Windows: a protected access-control list,
// which inherits nothing from the parent and allows the user's own security
// identifier alone, inheritable by every file and directory made in it.
func SecureDir(dir string) error {
	return setOwnerOnly(dir, objectInheritACE|containerInheritACE)
}

// SecureFile makes the open file f open to its owner only: a protected
// access-control list that allows the user's own security identifier alone.
func SecureFile(f *os.File) error { return setOwnerOnly(f.Name(), 0) }
