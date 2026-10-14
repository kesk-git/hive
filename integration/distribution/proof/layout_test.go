package proof_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/integration/distribution/fixture"
	"github.com/kesk-git/hive/integration/distribution/proof"
)

// editManifest rewrites manifest.json in canonical form after fn changed it.
func editManifest(t *testing.T, dir string, fn func(m map[string]any)) {
	t.Helper()
	p := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	fn(v.(map[string]any))
	out, err := canon.Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func replaceLink(t *testing.T, dir, name string, make func(path string) error) {
	t.Helper()
	p := filepath.Join(dir, "bin", name)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := make(p); err != nil {
		t.Fatal(err)
	}
}

func file(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

type breakage struct {
	name   string
	expect string
	change func(t *testing.T, dir, goos string)
}

// breakages are the breaks of any platform's artefact.
var breakages = []breakage{
	{"a link that is gone", "lacks hive-admit", func(t *testing.T, dir, goos string) {
		os.Remove(filepath.Join(dir, "bin", "hive-admit"+fixture.Suffix(goos)))
	}},
	{"an executable that is gone", "lacks hive-word", func(t *testing.T, dir, goos string) {
		os.Remove(filepath.Join(dir, "bin", "hive-word"+fixture.Suffix(goos)))
	}},
	{"a file in bin that is not part of the artefact", "which the artefact does not", func(t *testing.T, dir, goos string) {
		file(t, filepath.Join(dir, "bin", "tmux"+fixture.Suffix(goos)), "x")
	}},
	{"a file beside bin that is not part of the artefact", "the artefact holds README", func(t *testing.T, dir, goos string) {
		file(t, filepath.Join(dir, "README"), "x")
	}},
	{"a LICENSE that is not the committed one", "LICENSE of the artefact differs", func(t *testing.T, dir, goos string) {
		file(t, filepath.Join(dir, "LICENSE"), "another licence\n")
	}},
	{"a NOTICE that is not the committed one", "NOTICE of the artefact differs", func(t *testing.T, dir, goos string) {
		file(t, filepath.Join(dir, "NOTICE"), "another notice\n")
	}},
	{"a LICENSE that is a directory", "LICENSE of the artefact is not a regular file", func(t *testing.T, dir, goos string) {
		os.Remove(filepath.Join(dir, "LICENSE"))
		os.Mkdir(filepath.Join(dir, "LICENSE"), 0o755)
	}},
	{"a NOTICE that is a directory", "NOTICE of the artefact is not a regular file", func(t *testing.T, dir, goos string) {
		os.Remove(filepath.Join(dir, "NOTICE"))
		os.Mkdir(filepath.Join(dir, "NOTICE"), 0o755)
	}},
	{"a manifest that is a directory", "manifest.json of the artefact is not a regular file", func(t *testing.T, dir, goos string) {
		os.Remove(filepath.Join(dir, "manifest.json"))
		os.Mkdir(filepath.Join(dir, "manifest.json"), 0o755)
	}},
	{"a manifest that is gone", "lacks manifest.json", func(t *testing.T, dir, goos string) { os.Remove(filepath.Join(dir, "manifest.json")) }},
	{"a manifest that is not canonical", "canonical form", func(t *testing.T, dir, goos string) {
		p := filepath.Join(dir, "manifest.json")
		data, _ := os.ReadFile(p)
		os.WriteFile(p, []byte(strings.Replace(string(data), ",\"os\"", ", \"os\"", 1)), 0o644)
	}},
	{"a manifest with a member missing", "lacks go_release", func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) { delete(m, "go_release") })
	}},
	{"a manifest with a member it does not define", "which the artefact does not", func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) { m["built_by"] = "someone" })
	}},
	{"a manifest that names another version than the directory", "the directory is named", func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) { m["hive_version"] = "7.8.8" })
	}},
	{"a manifest that names another platform", "names the platform", func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) { m["os"] = "plan9" })
	}},
	{"a manifest with a wrong digest", "the digest", func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) {
			m["files"].([]any)[0].(map[string]any)["digest"] = canon.FileDigest([]byte("other"))
		})
	}},
	{"a manifest with a wrong size", "a size of", func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) { m["files"].([]any)[0].(map[string]any)["size"] = int64(1) })
	}},
	{"a manifest that omits a file", "does not list bin/hive-word", func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) {
			var kept []any
			for _, f := range m["files"].([]any) {
				if !strings.HasPrefix(f.(map[string]any)["path"].(string), "bin/hive-word") {
					kept = append(kept, f)
				}
			}
			m["files"] = kept
		})
	}},
	{"a manifest that lists a file the artefact does not hold", "which the artefact does not hold", func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) {
			m["files"] = append(m["files"].([]any), map[string]any{"path": "zzz", "size": int64(1), "digest": canon.FileDigest([]byte("x"))})
		})
	}},
	{"a manifest that omits a link", "does not list the link bin/hive-admit", func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) {
			var kept []any
			for _, l := range m["links"].([]any) {
				if !strings.HasPrefix(l.(map[string]any)["path"].(string), "bin/hive-admit") {
					kept = append(kept, l)
				}
			}
			m["links"] = kept
		})
	}},
	{"a manifest that gives a link another target", "not hive-pool", func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) {
			for _, l := range m["links"].([]any) {
				if strings.HasPrefix(l.(map[string]any)["path"].(string), "bin/hive-admit") {
					l.(map[string]any)["target"] = "hive-word"
				}
			}
		})
	}},
}

// windowsBreakages are the breaks of the rules of the Windows artefact: every
// name ends in .exe, and a link is a hard link to its executable or a
// byte-identical copy of it. Each is made of regular files and directories,
// which every host makes.
var windowsBreakages = []breakage{
	{"a name on Windows without the .exe suffix", "lacks the .exe suffix", func(t *testing.T, dir, goos string) {
		os.Rename(filepath.Join(dir, "bin", "hive-admit.exe"), filepath.Join(dir, "bin", "hive-admit"))
	}},
	{"a link on Windows that is a copy with other bytes", "neither a hard link", func(t *testing.T, dir, goos string) {
		replaceLink(t, dir, "hive-admit.exe", func(p string) error { return os.WriteFile(p, []byte("other bytes"), 0o755) })
	}},
	{"a link on Windows that is not a file", "not a hard link or a copy", func(t *testing.T, dir, goos string) {
		replaceLink(t, dir, "hive-admit.exe", func(p string) error { return os.Mkdir(p, 0o755) })
	}},
}

// passes checks that a sound artefact of the platform passes the layout
// checks by that platform's rules.
func passes(t *testing.T, goos string) {
	t.Helper()
	dir, want, _, _ := fixture.Artefact(t, goos)
	for _, p := range proof.Layout(dir, want) {
		t.Error(p)
	}
}

// catches checks that each break is found by the rules of the platform. Every
// case changes one thing of a sound artefact and says which.
func catches(t *testing.T, goos string, lists ...[]breakage) {
	t.Helper()
	for _, list := range lists {
		for _, b := range list {
			t.Run(b.name, func(t *testing.T) {
				dir, want, _, _ := fixture.Artefact(t, goos)
				b.change(t, dir, goos)
				got := proof.Layout(dir, want)
				for _, p := range got {
					if strings.Contains(p, b.expect) {
						return
					}
				}
				t.Errorf("no problem says %q; problems: %q", b.expect, got)
			})
		}
	}
}

// A sound artefact of Windows passes the layout checks by the rules of
// Windows. Its links are hard links, which every host makes, so the case runs
// wherever the tests do; the artefacts of macOS and Linux, whose links are
// symbolic, have their cases in the file built for those platforms.
func TestLayoutPassesASoundWindowsArtefact(t *testing.T) {
	passes(t, "windows")
}

// Each break of the Windows artefact is found by the rules of Windows.
func TestLayoutCatchesEachBreakOfAWindowsArtefact(t *testing.T) {
	catches(t, "windows", breakages, windowsBreakages)
}
