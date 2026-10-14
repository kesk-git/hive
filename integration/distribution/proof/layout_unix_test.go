//go:build unix

package proof_test

import (
	"os"
	"path/filepath"
	"testing"
)

// unixPlatforms are the platforms whose artefact links by relative symbolic
// links. A fixture of one needs a host that makes a symbolic link without a
// right the platform may withhold, so their cases are built for those hosts
// only; the rules of the Windows artefact have their own cases, which every
// host runs.
var unixPlatforms = []string{"linux", "darwin"}

// unixBreakages are the breaks of the rules of the macOS and Linux artefacts:
// a link is a relative symbolic link to its executable in the same directory,
// an executable carries an execute bit, and no member of the artefact is a
// link that leads out of it.
var unixBreakages = []breakage{
	{"a link that is absolute", "absolute link", func(t *testing.T, dir, goos string) {
		replaceLink(t, dir, "hive-admit", func(p string) error { return os.Symlink(filepath.Join(dir, "bin", "hive-pool"), p) })
	}},
	{"a link that is dangling", "dangling", func(t *testing.T, dir, goos string) {
		replaceLink(t, dir, "hive-admit", func(p string) error { return os.Symlink("hive-nowhere", p) })
	}},
	{"a link to the executable of another package", "links to", func(t *testing.T, dir, goos string) {
		replaceLink(t, dir, "hive-admit", func(p string) error { return os.Symlink("hive-word", p) })
	}},
	{"a link that reaches its executable through another directory", "links to", func(t *testing.T, dir, goos string) {
		replaceLink(t, dir, "hive-admit", func(p string) error { return os.Symlink("../bin/hive-pool", p) })
	}},
	{"a link that is a copy on macOS and Linux", "not a symbolic link", func(t *testing.T, dir, goos string) {
		data, _ := os.ReadFile(filepath.Join(dir, "bin", "hive-pool"))
		replaceLink(t, dir, "hive-admit", func(p string) error { return os.WriteFile(p, data, 0o755) })
	}},
	{"an executable without an execute bit", "no execute bit", func(t *testing.T, dir, goos string) {
		os.Chmod(filepath.Join(dir, "bin", "hive-pool"), 0o644)
	}},
	{"a NOTICE that is a link to a file outside the artefact", "NOTICE of the artefact is not a regular file", func(t *testing.T, dir, goos string) {
		outside := filepath.Join(t.TempDir(), "NOTICE")
		data, _ := os.ReadFile(filepath.Join(dir, "NOTICE"))
		file(t, outside, string(data))
		os.Remove(filepath.Join(dir, "NOTICE"))
		if err := os.Symlink(outside, filepath.Join(dir, "NOTICE")); err != nil {
			t.Fatal(err)
		}
	}},
}

// A sound artefact of macOS and of Linux passes the layout checks by that
// platform's rules.
func TestLayoutPassesASoundArtefactOfMacOSAndLinux(t *testing.T) {
	for _, goos := range unixPlatforms {
		t.Run(goos, func(t *testing.T) { passes(t, goos) })
	}
}

// Each break of the artefact of macOS and of Linux is found by that
// platform's rules.
func TestLayoutCatchesEachBreakOfAnArtefactOfMacOSAndLinux(t *testing.T) {
	for _, goos := range unixPlatforms {
		t.Run(goos, func(t *testing.T) { catches(t, goos, breakages, unixBreakages) })
	}
}
