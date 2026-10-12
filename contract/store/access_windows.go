//go:build windows

package store

import (
	"encoding/binary"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// The access-control calls are in advapi32, which the standard library's
// syscall package does not wrap; they are reached through its lazy DLL
// loader.
var (
	advapi32                         = syscall.NewLazyDLL("advapi32.dll")
	procGetNamedSecurityInfo         = advapi32.NewProc("GetNamedSecurityInfoW")
	procSetNamedSecurityInfo         = advapi32.NewProc("SetNamedSecurityInfoW")
	procSetSecurityInfo              = advapi32.NewProc("SetSecurityInfo")
	procConvertStringSecurityDescr   = advapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
	procGetSecurityDescriptorOwner   = advapi32.NewProc("GetSecurityDescriptorOwner")
	procGetSecurityDescriptorDacl    = advapi32.NewProc("GetSecurityDescriptorDacl")
	procGetSecurityDescriptorControl = advapi32.NewProc("GetSecurityDescriptorControl")
	procGetVolumePathName            = kernel32.NewProc("GetVolumePathNameW")
	procGetDriveType                 = kernel32.NewProc("GetDriveTypeW")
	procGetVolumeInformation         = kernel32.NewProc("GetVolumeInformationW")
)

const (
	seFileObject                     = 1
	ownerSecurityInformation         = 0x1
	daclSecurityInformation          = 0x4
	protectedDaclSecurityInformation = 0x80000000

	sddlRevision1 = 1

	// seDaclProtected is the control flag of a descriptor whose list inherits
	// nothing from its parent.
	seDaclProtected = 0x1000

	// The access rights that change who owns an entry and who may reach it.
	writeDAC   = 0x00040000
	writeOwner = 0x00080000

	// What an entry made in a directory inherits from it, and the two flags
	// that narrow an entry's reach.
	objectInheritACE    = 0x1
	containerInheritACE = 0x2
	noPropagateACE      = 0x4
	inheritOnlyACE      = 0x8

	// The types of an access-control entry that grant access.
	accessAllowedACE               = 0
	accessAllowedCompoundACE       = 4
	accessAllowedObjectACE         = 5
	accessAllowedCallbackACE       = 9
	accessAllowedCallbackObjectACE = 11

	aclHeaderSize = 8
	aceHeaderSize = 4

	driveRemote         = 4
	filePersistentACLs  = 0x8
	localFileSystemName = "NTFS"
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

// The security descriptors Hive gives what it makes, in the descriptor
// language, %[1]s standing for the user's own security identifier. Each is
// owned by the user Hive runs as. An entry made in a home has no
// access-control list of its own, so it carries only the owner-only list it
// inherits from the home; a private directory, the home itself, and a private
// file, the response file, have a protected list that allows the user alone,
// the directory's inheritable by every file and directory made in it
// (spec/records.md, The home).
const (
	sddlEntry       = "O:%[1]s"
	sddlPrivateDir  = "O:%[1]sD:P(A;OICI;FA;;;%[1]s)"
	sddlPrivateFile = "O:%[1]sD:P(A;;FA;;;%[1]s)"
)

// descriptor is one of the security descriptors above, with its owner and
// its access-control list, which is nil for an entry's.
type descriptor struct {
	descriptor, owner, dacl unsafe.Pointer
}

func newDescriptor(form string) (*descriptor, error) {
	user, err := userSID()
	if err != nil {
		return nil, err
	}
	sid, err := user.String()
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	sddl, err := syscall.UTF16PtrFromString(fmt.Sprintf(form, sid))
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	p := &descriptor{}
	if r, _, err := procConvertStringSecurityDescr.Call(uintptr(unsafe.Pointer(sddl)), sddlRevision1, uintptr(unsafe.Pointer(&p.descriptor)), 0); r == 0 {
		return nil, fmt.Errorf("store: building the security descriptor: %w", err)
	}
	var present, defaulted uint32
	if r, _, err := procGetSecurityDescriptorOwner.Call(uintptr(p.descriptor), uintptr(unsafe.Pointer(&p.owner)), uintptr(unsafe.Pointer(&defaulted))); r == 0 {
		p.free()
		return nil, fmt.Errorf("store: building the security descriptor: %w", err)
	}
	if r, _, err := procGetSecurityDescriptorDacl.Call(uintptr(p.descriptor), uintptr(unsafe.Pointer(&present)), uintptr(unsafe.Pointer(&p.dacl)), uintptr(unsafe.Pointer(&defaulted))); r == 0 {
		p.free()
		return nil, fmt.Errorf("store: building the security descriptor: %w", err)
	}
	return p, nil
}

func (p *descriptor) free() { syscall.LocalFree(syscall.Handle(uintptr(p.descriptor))) }

// attributes are the security attributes that create a file or directory
// with the descriptor already applied.
func (p *descriptor) attributes() *syscall.SecurityAttributes {
	return &syscall.SecurityAttributes{Length: uint32(unsafe.Sizeof(syscall.SecurityAttributes{})), SecurityDescriptor: uintptr(p.descriptor)}
}

const privateInformation = ownerSecurityInformation | daclSecurityInformation | protectedDaclSecurityInformation

// ace is one entry that allows access, with its inheritance flags.
type ace struct {
	sid   string
	flags byte
}

// access is what the platform reports of an entry's security descriptor.
type access struct {
	owner     string
	protected bool
	granted   []ace
}

// ownerOnly refuses an entry that is a reparse point, which a home never holds,
// and one that grants access to anyone but its owner: one that is owned by
// anyone but the user, whose access-control list has an entry that allows a
// security identifier other than the user's own, or no list at all, which
// gives everyone full access, or, for a directory, whose entries do not reach
// the files and directories made in it. what names the entry in the error;
// path is where it is.
func ownerOnly(path string, info fs.FileInfo, what string) error {
	if _, err := attributes(path, what); err != nil {
		return err
	}
	user, err := userSID()
	if err != nil {
		return err
	}
	own, err := user.String()
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	a, err := readAccess(path)
	if err != nil {
		return fmt.Errorf("store: reading the access of %s: %w", what, err)
	}
	if a.owner != own {
		return fmt.Errorf("%w: %s is open beyond its owner (it is owned by %s)", ErrLayout, what, a.owner)
	}
	for _, e := range a.granted {
		if e.sid != own {
			return fmt.Errorf("%w: %s is open beyond its owner (its access-control list grants access to %s)", ErrLayout, what, e.sid)
		}
		if info.IsDir() && e.flags&(objectInheritACE|containerInheritACE|noPropagateACE|inheritOnlyACE) != objectInheritACE|containerInheritACE {
			return fmt.Errorf("%w: %s is open beyond its owner (its access-control entry for %s does not reach what is made in it)", ErrLayout, what, e.sid)
		}
	}
	return nil
}

// attributes reads the attributes of the entry at path itself, never of what a
// reparse point there leads to, and refuses, as ErrLayout, a reparse point:
// a symbolic link, a junction or any other, which a home never holds
// (spec/records.md, The home). what names the entry in the error.
func attributes(path, what string) (uint32, error) {
	name, err := syscall.UTF16PtrFromString(extendedPath(path))
	if err != nil {
		return 0, fmt.Errorf("store: %w", err)
	}
	attrs, err := syscall.GetFileAttributes(name)
	if err != nil {
		return 0, fmt.Errorf("store: reading the attributes of %s: %w", what, err)
	}
	if attrs&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return 0, fmt.Errorf("%w: %s is a reparse point, which a home does not hold", ErrLayout, what)
	}
	return attrs, nil
}

// checkEntry refuses the entry at path, one component of a path in a home,
// when it is a reparse point, and reports whether it is a directory, below
// which the path goes on. An entry that is not there is not a directory.
func checkEntry(path string) (bool, error) {
	attrs, err := attributes(path, path)
	if errNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return attrs&syscall.FILE_ATTRIBUTE_DIRECTORY != 0, nil
}

// MakeDir creates the directory at path in a home, and every directory above
// it that is not there, each owned by the user Hive runs as and carrying only
// the owner-only access it inherits from the home.
func MakeDir(path string) error {
	var missing []string
	for p := path; ; {
		info, err := os.Stat(p)
		if err == nil {
			if !info.IsDir() {
				return &os.PathError{Op: "mkdir", Path: p, Err: syscall.ENOTDIR}
			}
			break
		}
		if !errNotExist(err) {
			return err
		}
		missing = append(missing, p)
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		p = parent
	}
	if len(missing) == 0 {
		return nil
	}
	d, err := newDescriptor(sddlEntry)
	if err != nil {
		return err
	}
	defer d.free()
	for i := len(missing) - 1; i >= 0; i-- {
		name, err := syscall.UTF16PtrFromString(extendedPath(missing[i]))
		if err != nil {
			return &os.PathError{Op: "mkdir", Path: missing[i], Err: err}
		}
		if err := syscall.CreateDirectory(name, d.attributes()); err != nil && err != syscall.ERROR_ALREADY_EXISTS {
			return &os.PathError{Op: "mkdir", Path: missing[i], Err: err}
		}
	}
	return nil
}

// CreateFile creates the file at path in a home, or opens it when flag lets a
// file that is there be opened, owned by the user Hive runs as and carrying
// only the owner-only access it inherits from the home. flag is os.O_CREATE
// with the access, os.O_RDONLY, os.O_WRONLY or os.O_RDWR, and os.O_EXCL to
// refuse a file that is there or os.O_TRUNC to empty it. The handle shares
// the file with every other handle and may also delete it, which a rename
// through it needs.
func CreateFile(path string, flag int) (*os.File, error) {
	access := uint32(syscall.GENERIC_READ)
	switch flag & (os.O_WRONLY | os.O_RDWR) {
	case os.O_WRONLY:
		access = syscall.GENERIC_WRITE
	case os.O_RDWR:
		access = syscall.GENERIC_READ | syscall.GENERIC_WRITE
	}
	create := uint32(syscall.OPEN_ALWAYS)
	switch {
	case flag&os.O_EXCL != 0:
		create = syscall.CREATE_NEW
	case flag&os.O_TRUNC != 0:
		create = syscall.CREATE_ALWAYS
	}
	d, err := newDescriptor(sddlEntry)
	if err != nil {
		return nil, err
	}
	defer d.free()
	return openHandle(path, access|deleteAccess, shareAll, create, d.attributes())
}

// checkHomeDir refuses a home directory that spec/records.md does not allow
// on Windows: one on a network share or on a volume that is not NTFS, whose
// access cannot be held to its owner, and one whose access-control list
// inherits from its parent.
func checkHomeDir(path string) error {
	if err := localNTFS(path); err != nil {
		return err
	}
	a, err := readAccess(path)
	if err != nil {
		return fmt.Errorf("store: reading the access of the home: %w", err)
	}
	if !a.protected {
		return fmt.Errorf("%w: the home is open beyond its owner (its access-control list inherits from its parent)", ErrLayout)
	}
	return nil
}

// localNTFS refuses, as ErrLayout, a path on a volume that is remote or whose
// file system is not NTFS with persistent access-control lists.
func localNTFS(path string) error {
	name, err := syscall.UTF16PtrFromString(extendedPath(path))
	if err != nil {
		return fmt.Errorf("store: home: %w", err)
	}
	root := make([]uint16, len(path)+syscall.MAX_PATH)
	if r, _, err := procGetVolumePathName.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&root[0])), uintptr(len(root))); r == 0 {
		return fmt.Errorf("store: home: the volume of %s: %w", path, err)
	}
	if t, _, _ := procGetDriveType.Call(uintptr(unsafe.Pointer(&root[0]))); t == driveRemote {
		return fmt.Errorf("%w: the home is on a network share, which cannot be open to its owner only", ErrLayout)
	}
	var flags uint32
	fsName := make([]uint16, syscall.MAX_PATH+1)
	if r, _, err := procGetVolumeInformation.Call(uintptr(unsafe.Pointer(&root[0])), 0, 0, 0, 0, uintptr(unsafe.Pointer(&flags)), uintptr(unsafe.Pointer(&fsName[0])), uintptr(len(fsName))); r == 0 {
		return fmt.Errorf("store: home: the file system of %s: %w", path, err)
	}
	if fsType := syscall.UTF16ToString(fsName); fsType != localFileSystemName || flags&filePersistentACLs == 0 {
		return fmt.Errorf("%w: the home is on a %s volume, and only %s keeps access for its owner only", ErrLayout, fsType, localFileSystemName)
	}
	return nil
}

// readAccess reads the owner, the protection and the discretionary
// access-control list of the entry at path. An entry with no list allows
// everyone.
func readAccess(path string) (*access, error) {
	name, err := syscall.UTF16PtrFromString(extendedPath(path))
	if err != nil {
		return nil, err
	}
	var owner, dacl, descriptor unsafe.Pointer
	r, _, _ := procGetNamedSecurityInfo.Call(
		uintptr(unsafe.Pointer(name)), seFileObject, ownerSecurityInformation|daclSecurityInformation,
		uintptr(unsafe.Pointer(&owner)), 0, uintptr(unsafe.Pointer(&dacl)), 0, uintptr(unsafe.Pointer(&descriptor)))
	if r != 0 {
		return nil, syscall.Errno(r)
	}
	defer syscall.LocalFree(syscall.Handle(uintptr(descriptor)))
	a := &access{}
	if owner == nil {
		return nil, fmt.Errorf("the entry has no owner")
	}
	if a.owner, err = (*syscall.SID)(owner).String(); err != nil {
		return nil, err
	}
	var control uint16
	var revision uint32
	if r, _, err := procGetSecurityDescriptorControl.Call(uintptr(descriptor), uintptr(unsafe.Pointer(&control)), uintptr(unsafe.Pointer(&revision))); r == 0 {
		return nil, err
	}
	a.protected = control&seDaclProtected != 0
	if dacl == nil {
		a.granted = []ace{{sid: "everyone (the entry has no access-control list)"}}
		return a, nil
	}
	size := int(binary.LittleEndian.Uint16(unsafe.Slice((*byte)(unsafe.Add(dacl, 2)), 2)))
	acl := unsafe.Slice((*byte)(dacl), size)
	count := int(binary.LittleEndian.Uint16(acl[4:6]))
	for off, i := aclHeaderSize, 0; i < count; i++ {
		if off+aceHeaderSize > len(acl) {
			return nil, fmt.Errorf("the access-control list is cut short")
		}
		entry := acl[off:]
		entrySize := int(binary.LittleEndian.Uint16(entry[2:4]))
		if entrySize < aceHeaderSize || entrySize > len(entry) {
			return nil, fmt.Errorf("an access-control entry is cut short")
		}
		entry = entry[:entrySize]
		off += entrySize
		sidAt := 0
		switch entry[0] {
		case accessAllowedACE, accessAllowedCallbackACE:
			sidAt = aceHeaderSize + 4 // the mask
		case accessAllowedObjectACE, accessAllowedCallbackObjectACE:
			// The mask and the flags, then the two object identifiers the
			// flags say are present, then the identifier.
			if len(entry) < aceHeaderSize+8 {
				return nil, fmt.Errorf("an access-control entry is cut short")
			}
			sidAt = aceHeaderSize + 8
			flags := binary.LittleEndian.Uint32(entry[aceHeaderSize+4:])
			if flags&1 != 0 {
				sidAt += 16
			}
			if flags&2 != 0 {
				sidAt += 16
			}
		case accessAllowedCompoundACE:
			a.granted = append(a.granted, ace{sid: "a compound access-control entry", flags: entry[1]})
			continue
		default:
			// A denial, an audit or a label grants nothing.
			continue
		}
		if sidAt+8 > len(entry) {
			return nil, fmt.Errorf("an access-control entry is cut short")
		}
		s, err := (*syscall.SID)(unsafe.Pointer(&entry[sidAt])).String()
		if err != nil {
			return nil, err
		}
		a.granted = append(a.granted, ace{sid: s, flags: entry[1]})
	}
	return a, nil
}

// SecureDir makes the directory at dir open to its owner only, as
// spec/records.md gives a home on Windows: owned by the user, with a protected
// access-control list, which inherits nothing from the parent and allows the
// user's own security identifier alone, inheritable by every file and
// directory made in it.
func SecureDir(dir string) error {
	p, err := newDescriptor(sddlPrivateDir)
	if err != nil {
		return err
	}
	defer p.free()
	name, err := syscall.UTF16PtrFromString(extendedPath(dir))
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	r, _, _ := procSetNamedSecurityInfo.Call(
		uintptr(unsafe.Pointer(name)), seFileObject, privateInformation,
		uintptr(p.owner), 0, uintptr(p.dacl), 0)
	if r != 0 {
		return fmt.Errorf("store: setting the access of %s: %w", dir, syscall.Errno(r))
	}
	return nil
}

// OpenPrivate opens the file at path for writing, creating it when it is not
// there, open to its owner only: a new file is created with its private
// descriptor already applied, so that no other account holds a handle to it,
// and the descriptor is applied to the opened handle itself, never to a second
// lookup of the name, before anything is written. The file is opened without
// sharing: a file another handle has open for reading, writing or deleting is
// refused, and none can be opened while it is held.
func OpenPrivate(path string) (*os.File, error) {
	p, err := newDescriptor(sddlPrivateFile)
	if err != nil {
		return nil, err
	}
	defer p.free()
	f, err := openHandle(path, syscall.GENERIC_WRITE|writeDAC|writeOwner, 0, syscall.OPEN_ALWAYS, p.attributes())
	if err != nil {
		return nil, err
	}
	r, _, _ := procSetSecurityInfo.Call(f.Fd(), seFileObject, privateInformation, uintptr(p.owner), 0, uintptr(p.dacl), 0)
	if r != 0 {
		f.Close()
		return nil, fmt.Errorf("store: setting the access of %s: %w", path, syscall.Errno(r))
	}
	return f, nil
}
