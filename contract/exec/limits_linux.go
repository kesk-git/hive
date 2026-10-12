package exec

import (
	"os"
	"syscall"
)

// limits are the platform's limits on arguments and environment, in bytes:
// the total, and the largest one string, which is 32 memory pages.
func limits() (total, perString int64) {
	const (
		argMax    = 131072     // the least the kernel allows
		stackFrac = 6 << 20    // three quarters of the kernel's stack limit of 8 MiB
		maxPages  = 32         // MAX_ARG_PAGES
		infinity  = ^uint64(0) // RLIM_INFINITY
	)
	total = stackFrac
	var r syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_STACK, &r); err == nil && r.Cur != infinity {
		total = min(total, int64(r.Cur/4))
	}
	total = max(total, argMax)
	return total, int64(os.Getpagesize()) * maxPages
}
