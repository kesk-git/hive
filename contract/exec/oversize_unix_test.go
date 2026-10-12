//go:build unix

package exec

import (
	"runtime"
	"strings"
	"testing"
)

// oversizeArguments is an argument list over the platform's limit, in
// strings each under any limit of one string.
func oversizeArguments() []string {
	total, _ := limits()
	piece := strings.Repeat("a", 100000)
	var args []string
	for n := int64(0); n < total+int64(len(piece)); n += int64(len(piece)) {
		args = append(args, piece)
	}
	return args
}

// The platform's limits are real: at least what the kernel guarantees, and on
// Linux one string of 32 pages.
func TestPlatformLimits(t *testing.T) {
	total, per := limits()
	if total < 131072 {
		t.Errorf("the platform's limit is %d", total)
	}
	if runtime.GOOS == "linux" && per < 131072 {
		t.Errorf("one string may be %d", per)
	}
	if runtime.GOOS == "darwin" && per != 0 {
		t.Errorf("macOS has no limit on one string: %d", per)
	}
}
