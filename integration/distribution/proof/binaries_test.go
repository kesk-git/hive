package proof_test

import (
	"debug/macho"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/kesk-git/hive/integration/distribution/harness"
	"github.com/kesk-git/hive/integration/distribution/proof"
)

const hello = "integration/distribution/testdata/hello"

func exeName(goos string) string {
	if goos == "windows" {
		return "hello.exe"
	}
	return "hello"
}

// A Go executable built without the C toolchain and with paths trimmed passes
// in the format of each platform: the ELF, Mach-O and PE headers are read for
// real, from executables built for each. It catches a header read that fails
// on a format, and a platform's own libraries reported as foreign.
func TestBinaryOfEachPlatformPasses(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), exeName(goos))
			harness.Build(t, hello, out, goos, "amd64", true)
			if got := proof.Binary(out, "hive-pool", goos); len(got) != 0 {
				t.Error(got)
			}
			libs, err := proof.ImportedLibraries(out, goos)
			if err != nil {
				t.Fatal(err)
			}
			switch goos {
			case "linux":
				if len(libs) != 0 {
					t.Errorf("a static Go executable on Linux needs %v", libs)
				}
			case "darwin":
				if !slices.Contains(libs, "/usr/lib/libSystem.B.dylib") {
					t.Errorf("a Go executable on macOS imports the system library; it imports %v", libs)
				}
			case "windows":
				if !slices.Contains(libs, "kernel32.dll") {
					t.Errorf("a Go executable on Windows imports kernel32.dll; it imports %v", libs)
				}
			}
		})
	}
}

// An executable whose paths are not trimmed, or that is built for another
// platform than the artefact's, fails.
func TestBinaryBreaks(t *testing.T) {
	dir := t.TempDir()
	untrimmed := filepath.Join(dir, "untrimmed")
	harness.Build(t, hello, untrimmed, "linux", "amd64", false)
	if got := strings.Join(proof.Binary(untrimmed, "hive-pool", "linux"), "\n"); !strings.Contains(got, "without -trimpath") {
		t.Errorf("an executable built without -trimpath: %q", got)
	}
	trimmed := filepath.Join(dir, "trimmed")
	harness.Build(t, hello, trimmed, "linux", "amd64", true)
	if got := strings.Join(proof.Binary(trimmed, "hive-pool", "windows"), "\n"); !strings.Contains(got, "not a PE executable") {
		t.Errorf("a Linux executable in a Windows artefact: %q", got)
	}
	if got := strings.Join(proof.Binary(filepath.Join(dir, "absent"), "hive-pool", "linux"), "\n"); !strings.Contains(got, "build information cannot be read") {
		t.Errorf("a file that is not there: %q", got)
	}
}

// The build settings of an executable built with the C toolchain are refused.
func TestBuildSettings(t *testing.T) {
	sound := map[string]string{"CGO_ENABLED": "0", "-trimpath": "true", "GOOS": "linux"}
	if got := proof.BuildSettings(sound, "linux"); len(got) != 0 {
		t.Error(got)
	}
	cgo := map[string]string{"CGO_ENABLED": "1", "-trimpath": "true", "GOOS": "linux"}
	if got := strings.Join(proof.BuildSettings(cgo, "linux"), "\n"); !strings.Contains(got, "C toolchain involved") {
		t.Errorf("an executable built with cgo: %q", got)
	}
	unset := map[string]string{"-trimpath": "true", "GOOS": "linux"}
	if got := strings.Join(proof.BuildSettings(unset, "linux"), "\n"); !strings.Contains(got, "C toolchain involved") {
		t.Errorf("an executable whose build information does not say cgo was off: %q", got)
	}
}

// The libraries the platform's own are accepted and no others, for hive-ui
// with the system parts the artefact accepts, on each of the three.
func TestLibraryAllowed(t *testing.T) {
	cases := []struct {
		goos, exe, lib string
		want           bool
	}{
		{"linux", "hive-pool", "libc.so.6", false},
		{"linux", "hive-pool", "(a dynamic loader)", false},
		{"linux", "hive-ui", "libc.so.6", true},
		{"linux", "hive-ui", "libgtk-3.so.0", true},
		{"linux", "hive-ui", "libwebkit2gtk-4.1.so.0", true},
		{"linux", "hive-ui", "libssl.so.3", false},
		{"darwin", "hive-pool", "/usr/lib/libSystem.B.dylib", true},
		{"darwin", "hive-ui", "/System/Library/Frameworks/WebKit.framework/Versions/A/WebKit", true},
		{"darwin", "hive-pool", "/usr/local/lib/libssl.dylib", false},
		{"darwin", "hive-pool", "@rpath/libfoo.dylib", false},
		{"windows", "hive-pool", "kernel32.dll", true},
		{"windows", "hive-pool", "KERNEL32.DLL", true},
		{"windows", "hive-pool", "api-ms-win-core-synch-l1-2-0.dll", true},
		{"windows", "hive-pool", "libwinpthread-1.dll", false},
		{"windows", "hive-pool", "WebView2Loader.dll", false},
		{"windows", "hive-ui", "WebView2Loader.dll", true},
		{"plan9", "hive-pool", "libc", false},
	}
	for _, c := range cases {
		if got := proof.LibraryAllowed(c.goos, c.exe, c.lib); got != c.want {
			t.Errorf("%s %s needs %s: allowed is %v, want %v", c.goos, c.exe, c.lib, got, c.want)
		}
	}
}

// A program that is dynamically linked is reported with the libraries it
// needs, read from a system executable of the host: this is what makes a
// foreign library visible in a real header on each platform.
func TestImportedLibrariesOfADynamicSystemExecutable(t *testing.T) {
	var path string
	var want []string
	switch runtime.GOOS {
	case "linux":
		path, want = "/bin/sh", []string{"(a dynamic loader)"}
	case "darwin":
		path, want = "/bin/ls", []string{"/usr/lib/libSystem.B.dylib"}
	case "windows":
		path, want = filepath.Join(`C:\Windows`, "System32", "cmd.exe"), []string{"kernel32.dll"}
	default:
		t.Fatalf("no expectation for %s", runtime.GOOS)
	}
	libs, err := proof.ImportedLibraries(path, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range want {
		found := false
		for _, l := range libs {
			found = found || strings.EqualFold(l, w)
		}
		if !found {
			t.Errorf("%s: %v does not hold %s", path, libs, w)
		}
	}
	if runtime.GOOS == "linux" && proof.LibraryAllowed("linux", "hive-pool", "(a dynamic loader)") {
		t.Errorf("a dynamic loader is accepted for an executable that must be static")
	}
}

// The slice of a universal Mach-O file that is read is the one of the host's
// architecture, matched on the CPU type. It catches a universal file read as
// a thin one (an index taken without looking at the architectures), and the
// wrong slice taken (the first one, or an architecture other than the host's).
// The slice with the CPU type of arm64 is found whatever its sub-type, and no
// match is an error that names the architectures the file holds. The real
// read of /bin/ls on macOS is the test above.
func TestSelectSliceOfAUniversalFile(t *testing.T) {
	for _, c := range []struct {
		name  string
		cpus  []macho.Cpu
		arch  string
		index int
		fails []string
	}{
		{"arm64 host, amd64 slice first", []macho.Cpu{macho.CpuAmd64, macho.CpuArm64}, "arm64", 1, nil},
		{"arm64 host, arm64 slice first", []macho.Cpu{macho.CpuArm64, macho.CpuAmd64}, "arm64", 0, nil},
		{"amd64 host, arm64 slice first", []macho.Cpu{macho.CpuArm64, macho.CpuAmd64}, "amd64", 1, nil},
		{"amd64 host, amd64 slice first", []macho.Cpu{macho.CpuAmd64, macho.CpuArm64}, "amd64", 0, nil},
		{"one slice, the host's", []macho.Cpu{macho.CpuArm64}, "arm64", 0, nil},
		{"three slices, the host's last", []macho.Cpu{macho.Cpu386, macho.CpuAmd64, macho.CpuArm64}, "arm64", 2, nil},
		{"no slice for the host", []macho.Cpu{macho.CpuAmd64}, "arm64", 0, []string{"arm64", macho.CpuAmd64.String()}},
		{"no slice at all", nil, "amd64", 0, []string{"amd64", "[]"}},
		{"an architecture that has no slice read", []macho.Cpu{macho.CpuAmd64, macho.CpuArm64}, "riscv64", 0, []string{"riscv64"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			i, err := proof.SelectSlice(c.cpus, c.arch)
			if c.fails != nil {
				if err == nil {
					t.Fatalf("slice %d taken, want an error", i)
				}
				for _, w := range c.fails {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("%q does not name %q", err, w)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if i != c.index {
				t.Errorf("slice %d taken, want %d: the wrong slice is read", i, c.index)
			}
		})
	}
}
