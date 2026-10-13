package proof

import (
	"debug/buildinfo"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// settings are the build settings the toolchain embedded in an executable.
func settings(info *buildinfo.BuildInfo) map[string]string {
	m := map[string]string{}
	for _, s := range info.Settings {
		m[s.Key] = s.Value
	}
	return m
}

// Binaries checks every executable of the artefact against How it is proved
// item 2: none needs a shared library beyond the platform's own, which for
// hive-ui include the system parts the artefact accepts, and none was built
// with the C toolchain involved. The libraries are read from the
// executable's own header, in the format of the platform: ELF on Linux,
// Mach-O on macOS, PE on Windows. That the C toolchain was not involved is
// read from the build information the Go toolchain embedded: CGO_ENABLED=0,
// and file-system paths trimmed.
func Binaries(dir string, w Want) []string {
	var problems []string
	for _, e := range w.Executables {
		path := filepath.Join(dir, "bin", e+w.suffix())
		for _, p := range Binary(path, e, w.GOOS) {
			problems = append(problems, fmt.Sprintf("%s: %s", e, p))
		}
	}
	return problems
}

// Binary checks one executable built for goos. name is its name without
// suffix, which decides whether the system parts of hive-ui are accepted.
func Binary(path, name, goos string) []string {
	var problems []string
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("its build information cannot be read: %v", err)}
	}
	problems = append(problems, BuildSettings(settings(info), goos)...)
	libs, err := ImportedLibraries(path, goos)
	if err != nil {
		return append(problems, err.Error())
	}
	for _, l := range libs {
		if !LibraryAllowed(goos, name, l) {
			problems = append(problems, fmt.Sprintf("needs the library %s, which is not one of the platform's own", l))
		}
	}
	return problems
}

// BuildSettings says whether the build settings show an executable built
// without the C toolchain, with file-system paths trimmed, for goos.
func BuildSettings(s map[string]string, goos string) []string {
	var problems []string
	if s["CGO_ENABLED"] != "0" {
		problems = append(problems, fmt.Sprintf("was built with the C toolchain involved: CGO_ENABLED is %q, not \"0\"", s["CGO_ENABLED"]))
	}
	if s["-trimpath"] != "true" {
		problems = append(problems, "was built without -trimpath: file-system paths are not trimmed")
	}
	if s["GOOS"] != goos {
		problems = append(problems, fmt.Sprintf("was built for %q, not for %s", s["GOOS"], goos))
	}
	return problems
}

// ImportedLibraries reads the libraries an executable needs from its own
// header, in the format of the platform goos names. An ELF executable that
// asks for a dynamic loader is reported as needing it: the loader is a
// library of its own.
func ImportedLibraries(path, goos string) ([]string, error) {
	switch goos {
	case "linux":
		f, err := elf.Open(path)
		if err != nil {
			return nil, fmt.Errorf("it is not an ELF executable: %v", err)
		}
		defer f.Close()
		libs, err := f.ImportedLibraries()
		if err != nil {
			return nil, fmt.Errorf("its libraries cannot be read: %v", err)
		}
		for _, p := range f.Progs {
			if p.Type == elf.PT_INTERP {
				libs = append(libs, "(a dynamic loader)")
			}
		}
		return libs, nil
	case "darwin":
		f, err := macho.Open(path)
		if err != nil {
			return nil, fmt.Errorf("it is not a Mach-O executable: %v", err)
		}
		defer f.Close()
		return f.ImportedLibraries()
	case "windows":
		f, err := pe.Open(path)
		if err != nil {
			return nil, fmt.Errorf("it is not a PE executable: %v", err)
		}
		defer f.Close()
		symbols, err := f.ImportedSymbols()
		if err != nil {
			return nil, fmt.Errorf("its imports cannot be read: %v", err)
		}
		var libs []string
		for _, s := range symbols {
			// A symbol is written name:library.
			if i := strings.LastIndex(s, ":"); i >= 0 && !slices.Contains(libs, s[i+1:]) {
				libs = append(libs, s[i+1:])
			}
		}
		return libs, nil
	}
	return nil, fmt.Errorf("%q is not one of the three platforms", goos)
}

// The libraries of each platform's own that an executable may need. Windows
// names are compared without regard to case.
var (
	windowsSystem = []string{
		"kernel32.dll", "kernelbase.dll", "ntdll.dll", "advapi32.dll", "user32.dll", "gdi32.dll", "ws2_32.dll",
		"winmm.dll", "shell32.dll", "ole32.dll", "oleaut32.dll", "crypt32.dll", "bcrypt.dll", "iphlpapi.dll",
		"netapi32.dll", "secur32.dll", "userenv.dll", "dbghelp.dll", "comctl32.dll", "comdlg32.dll", "shlwapi.dll",
		"msvcrt.dll", "rpcrt4.dll", "sechost.dll", "wtsapi32.dll", "mswsock.dll", "dwmapi.dll", "uxtheme.dll",
		"version.dll", "imm32.dll", "setupapi.dll", "cfgmgr32.dll", "powrprof.dll", "psapi.dll", "wininet.dll",
		"winhttp.dll", "dnsapi.dll", "ncrypt.dll",
	}
	// What hive-ui loads in addition: Microsoft's WebView2 loader.
	windowsUI = []string{"webview2loader.dll"}

	// The system C library on Linux, and what hive-ui reaches beside it.
	linuxC  = []string{"libc.so.", "libdl.so.", "libm.so.", "libpthread.so.", "librt.so.", "ld-linux"}
	linuxUI = []string{
		"libgtk-3.so.", "libgdk-3.so.", "libgobject-2.0.so.", "libglib-2.0.so.", "libgio-2.0.so.",
		"libwebkit2gtk-4.0.so.", "libwebkit2gtk-4.1.so.", "libjavascriptcoregtk-4.0.so.", "libjavascriptcoregtk-4.1.so.",
	}
)

// LibraryAllowed says whether an executable may need the library: one of the
// platform's own, which for hive-ui include the system parts the artefact
// accepts (spec/distribution.md, The artefact). The Go executables of the
// first stage are static on Linux and need nothing, so a Linux executable
// other than hive-ui that needs any library fails.
func LibraryAllowed(goos, executable, lib string) bool {
	ui := executable == "hive-ui"
	switch goos {
	case "linux":
		if !ui {
			return false
		}
		base := filepath.Base(lib)
		for _, prefix := range append(slices.Clone(linuxC), linuxUI...) {
			if strings.HasPrefix(base, prefix) {
				return true
			}
		}
		return false
	case "darwin":
		// The system library, and the system's own frameworks.
		return strings.HasPrefix(lib, "/usr/lib/") || strings.HasPrefix(lib, "/System/Library/")
	case "windows":
		l := strings.ToLower(lib)
		if slices.Contains(windowsSystem, l) || strings.HasPrefix(l, "api-ms-win-") {
			return true
		}
		return ui && slices.Contains(windowsUI, l)
	}
	return false
}
