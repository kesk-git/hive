package exec

import (
	"syscall"
	"unsafe"
)

// The system calls of the one process-execution path that the standard
// library's syscall package does not name, reached through
// syscall.NewLazyDLL: job objects, the attribute list of an extended
// startup block, the pseudoconsole, IsProcessInJob and ResumeThread.
var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procCreateJobObject           = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject   = kernel32.NewProc("SetInformationJobObject")
	procQueryInformationJobObject = kernel32.NewProc("QueryInformationJobObject")
	procTerminateJobObject        = kernel32.NewProc("TerminateJobObject")
	procIsProcessInJob            = kernel32.NewProc("IsProcessInJob")
	procResumeThread              = kernel32.NewProc("ResumeThread")

	procInitializeProcThreadAttributeList = kernel32.NewProc("InitializeProcThreadAttributeList")
	procUpdateProcThreadAttribute         = kernel32.NewProc("UpdateProcThreadAttribute")
	procDeleteProcThreadAttributeList     = kernel32.NewProc("DeleteProcThreadAttributeList")

	procCreatePseudoConsole = kernel32.NewProc("CreatePseudoConsole")
	procResizePseudoConsole = kernel32.NewProc("ResizePseudoConsole")
	procClosePseudoConsole  = kernel32.NewProc("ClosePseudoConsole")
)

// The constants of the calls.
const (
	jobObjectBasicAccountingInformation = 1
	jobObjectExtendedLimitInformation   = 9

	jobObjectLimitBreakawayOK    = 0x00000800
	jobObjectLimitKillOnJobClose = 0x00002000

	// terminatedByInterrupt is the status Windows itself gives a console
	// program ended by Control-C, which no Hive status uses.
	terminatedByInterrupt = 0xC000013A

	procThreadAttributeHandleList    = 0x00020002
	procThreadAttributeJobList       = 0x0002000D
	procThreadAttributePseudoConsole = 0x00020016

	extendedStartupInfoPresent = 0x00080000
	createSuspended            = 0x00000004
	detachedProcess            = 0x00000008
	createBreakawayFromJob     = 0x01000000
	createNoWindow             = 0x08000000

	infinite = 0xFFFFFFFF
	// exceptionStatus is the least status of an exception: a process that
	// ends with one was ended by the platform.
	exceptionStatus = 0xC0000000
)

// jobBasicLimit is JOBOBJECT_BASIC_LIMIT_INFORMATION.
type jobBasicLimit struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

// ioCounters is IO_COUNTERS.
type ioCounters struct {
	ReadOperationCount, WriteOperationCount, OtherOperationCount uint64
	ReadTransferCount, WriteTransferCount, OtherTransferCount    uint64
}

// jobExtendedLimit is JOBOBJECT_EXTENDED_LIMIT_INFORMATION.
type jobExtendedLimit struct {
	Basic                 jobBasicLimit
	IO                    ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

// jobAccounting is JOBOBJECT_BASIC_ACCOUNTING_INFORMATION.
type jobAccounting struct {
	TotalUserTime, TotalKernelTime, ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
	TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses     uint32
}

// startupInfoEx is STARTUPINFOEXW: the startup block followed by the
// attribute list.
type startupInfoEx struct {
	syscall.StartupInfo
	AttributeList uintptr
}

// call checks the result of a call that returns a BOOL and sets the last
// error.
func call(p *syscall.LazyProc, args ...uintptr) error {
	r, _, err := p.Call(args...)
	if r == 0 {
		if err == nil || err == syscall.Errno(0) {
			err = syscall.EINVAL
		}
		return err
	}
	return nil
}

// newJob creates the job object of a tree, with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, so that closing Hive's last handle ends
// what is in it, and JOB_OBJECT_LIMIT_BREAKAWAY_OK, so that a descendant that
// asks to leave with CREATE_BREAKAWAY_FROM_JOB leaves the tree.
func newJob() (syscall.Handle, error) {
	h, _, err := procCreateJobObject.Call(0, 0)
	if h == 0 {
		return 0, err
	}
	var info jobExtendedLimit
	info.Basic.LimitFlags = jobObjectLimitKillOnJobClose | jobObjectLimitBreakawayOK
	if err := call(procSetInformationJobObject, h, jobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info)); err != nil {
		syscall.CloseHandle(syscall.Handle(h))
		return 0, err
	}
	return syscall.Handle(h), nil
}

// activeProcesses is the number of processes in the job.
func activeProcesses(job syscall.Handle) (uint32, error) {
	var info jobAccounting
	if err := call(procQueryInformationJobObject, uintptr(job), jobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), 0); err != nil {
		return 0, err
	}
	return info.ActiveProcesses, nil
}

// attributeList is the attribute list of an extended startup block.
type attributeList struct {
	buf []byte
}

// newAttributeList makes a list with room for count attributes.
func newAttributeList(count int) (*attributeList, error) {
	var size uintptr
	// The first call fails with ERROR_INSUFFICIENT_BUFFER and sets the size.
	procInitializeProcThreadAttributeList.Call(0, uintptr(count), 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return nil, syscall.EINVAL
	}
	l := &attributeList{buf: make([]byte, size)}
	if err := call(procInitializeProcThreadAttributeList, uintptr(unsafe.Pointer(&l.buf[0])), uintptr(count), 0, uintptr(unsafe.Pointer(&size))); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *attributeList) pointer() uintptr { return uintptr(unsafe.Pointer(&l.buf[0])) }

// update sets an attribute to the value of size bytes at the address value,
// which must stay valid until the process is created; for the pseudoconsole
// the value is the handle itself.
func (l *attributeList) update(attribute, value, size uintptr) error {
	return call(procUpdateProcThreadAttribute, l.pointer(), 0, attribute, value, size, 0, 0)
}

func (l *attributeList) delete() {
	procDeleteProcThreadAttributeList.Call(l.pointer())
}

// coord packs a terminal's size as a COORD, which is passed by value.
func coord(w Window) uintptr {
	return uintptr(uint32(w.Cols) | uint32(w.Rows)<<16)
}

// processStart reads the creation time GetProcessTimes reports, as the start
// time of a process.
func processStart(h syscall.Handle) (uint64, error) {
	var creation, exit, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return 0, err
	}
	return uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime), nil
}
