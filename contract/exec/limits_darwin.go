package exec

import "syscall"

// limits are the platform's limits on arguments and environment, in bytes:
// the total, and the largest one string, where the platform has one.
func limits() (total, perString int64) {
	n, err := syscall.SysctlUint32("kern.argmax")
	if err != nil || n == 0 {
		return 262144, 0
	}
	return int64(n), 0
}
