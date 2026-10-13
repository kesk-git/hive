package proof_test

import (
	"os"
	"path/filepath"
	"runtime"
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
	// goos limits the case to the platforms whose rules it breaks; empty is all three.
	goos   []string
	change func(t *testing.T, dir, goos string)
}

var unix = []string{"linux", "darwin"}

var breakages = []breakage{
	{"a link that is gone", "lacks hive-admit", unix, func(t *testing.T, dir, goos string) { os.Remove(filepath.Join(dir, "bin", "hive-admit")) }},
	{"a link that is gone on Windows", "lacks hive-admit.exe", []string{"windows"}, func(t *testing.T, dir, goos string) {
		os.Remove(filepath.Join(dir, "bin", "hive-admit.exe"))
	}},
	{"an executable that is gone", "lacks hive-word", unix, func(t *testing.T, dir, goos string) { os.Remove(filepath.Join(dir, "bin", "hive-word")) }},
	{"a file in bin that is not part of the artefact", "which the artefact does not", nil, func(t *testing.T, dir, goos string) {
		file(t, filepath.Join(dir, "bin", "tmux"+fixture.Suffix(goos)), "x")
	}},
	{"a file beside bin that is not part of the artefact", "the artefact holds README", nil, func(t *testing.T, dir, goos string) {
		file(t, filepath.Join(dir, "README"), "x")
	}},
	{"a link that is absolute", "absolute link", unix, func(t *testing.T, dir, goos string) {
		replaceLink(t, dir, "hive-admit", func(p string) error { return os.Symlink(filepath.Join(dir, "bin", "hive-pool"), p) })
	}},
	{"a link that is dangling", "dangling", unix, func(t *testing.T, dir, goos string) {
		replaceLink(t, dir, "hive-admit", func(p string) error { return os.Symlink("hive-nowhere", p) })
	}},
	{"a link to the executable of another package", "links to", unix, func(t *testing.T, dir, goos string) {
		replaceLink(t, dir, "hive-admit", func(p string) error { return os.Symlink("hive-word", p) })
	}},
	{"a link that reaches its executable through another directory", "links to", unix, func(t *testing.T, dir, goos string) {
		replaceLink(t, dir, "hive-admit", func(p string) error { return os.Symlink("../bin/hive-pool", p) })
	}},
	{"a link that is a copy on macOS and Linux", "not a symbolic link", unix, func(t *testing.T, dir, goos string) {
		data, _ := os.ReadFile(filepath.Join(dir, "bin", "hive-pool"))
		replaceLink(t, dir, "hive-admit", func(p string) error { return os.WriteFile(p, data, 0o755) })
	}},
	{"an executable without an execute bit", "no execute bit", unix, func(t *testing.T, dir, goos string) {
		os.Chmod(filepath.Join(dir, "bin", "hive-pool"), 0o644)
	}},
	{"a name on Windows without the .exe suffix", "lacks the .exe suffix", []string{"windows"}, func(t *testing.T, dir, goos string) {
		os.Rename(filepath.Join(dir, "bin", "hive-admit.exe"), filepath.Join(dir, "bin", "hive-admit"))
	}},
	{"a link on Windows that is a copy with other bytes", "neither a hard link", []string{"windows"}, func(t *testing.T, dir, goos string) {
		replaceLink(t, dir, "hive-admit.exe", func(p string) error { return os.WriteFile(p, []byte("other bytes"), 0o755) })
	}},
	{"a link on Windows that is a symbolic link", "not a hard link or a copy", []string{"windows"}, func(t *testing.T, dir, goos string) {
		if runtime.GOOS == "windows" {
			t.Skip("a symbolic link needs rights a Windows host may not give; the case is shown on the hosts that make one")
		}
		replaceLink(t, dir, "hive-admit.exe", func(p string) error { return os.Symlink("hive-pool.exe", p) })
	}},
	{"a LICENSE that is not the committed one", "LICENSE of the artefact differs", nil, func(t *testing.T, dir, goos string) {
		file(t, filepath.Join(dir, "LICENSE"), "another licence\n")
	}},
	{"a NOTICE that is not the committed one", "NOTICE of the artefact differs", nil, func(t *testing.T, dir, goos string) {
		file(t, filepath.Join(dir, "NOTICE"), "another notice\n")
	}},
	{"a manifest that is gone", "lacks manifest.json", nil, func(t *testing.T, dir, goos string) { os.Remove(filepath.Join(dir, "manifest.json")) }},
	{"a manifest that is not canonical", "canonical form", nil, func(t *testing.T, dir, goos string) {
		p := filepath.Join(dir, "manifest.json")
		data, _ := os.ReadFile(p)
		os.WriteFile(p, []byte(strings.Replace(string(data), ",\"os\"", ", \"os\"", 1)), 0o644)
	}},
	{"a manifest with a member missing", "lacks go_release", nil, func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) { delete(m, "go_release") })
	}},
	{"a manifest with a member it does not define", "which the artefact does not", nil, func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) { m["built_by"] = "someone" })
	}},
	{"a manifest that names another version than the directory", "the directory is named", nil, func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) { m["hive_version"] = "7.8.8" })
	}},
	{"a manifest that names another platform", "names the platform", nil, func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) { m["os"] = "plan9" })
	}},
	{"a manifest with a wrong digest", "the digest", nil, func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) {
			m["files"].([]any)[0].(map[string]any)["digest"] = canon.FileDigest([]byte("other"))
		})
	}},
	{"a manifest with a wrong size", "a size of", nil, func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) { m["files"].([]any)[0].(map[string]any)["size"] = int64(1) })
	}},
	{"a manifest that omits a file", "does not list bin/hive-word", nil, func(t *testing.T, dir, goos string) {
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
	{"a manifest that lists a file the artefact does not hold", "which the artefact does not hold", nil, func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) {
			m["files"] = append(m["files"].([]any), map[string]any{"path": "zzz", "size": int64(1), "digest": canon.FileDigest([]byte("x"))})
		})
	}},
	{"a manifest that omits a link", "does not list the link bin/hive-admit", nil, func(t *testing.T, dir, goos string) {
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
	{"a manifest that gives a link another target", "not hive-pool", nil, func(t *testing.T, dir, goos string) {
		editManifest(t, dir, func(m map[string]any) {
			for _, l := range m["links"].([]any) {
				if strings.HasPrefix(l.(map[string]any)["path"].(string), "bin/hive-admit") {
					l.(map[string]any)["target"] = "hive-word"
				}
			}
		})
	}},
}

// A sound artefact of each platform passes the layout checks by that
// platform's rules, on any host.
func TestLayoutPassesASoundArtefact(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			if goos != "windows" && runtime.GOOS == "windows" {
				t.Skip("a symbolic-link fixture needs a host that makes one; the Windows artefact is checked by its own rules")
			}
			dir, want, _, _ := fixture.Artefact(t, goos)
			for _, p := range proof.Layout(dir, want) {
				t.Error(p)
			}
		})
	}
}

// Each break of the artefact is found, by the rules of the platform it is a
// break on. Every case changes one thing of a sound artefact and says which.
func TestLayoutCatchesEachBreak(t *testing.T) {
	for _, b := range breakages {
		goses := b.goos
		if goses == nil {
			goses = []string{"linux", "darwin", "windows"}
		}
		for _, goos := range goses {
			t.Run(b.name+"/"+goos, func(t *testing.T) {
				if goos != "windows" && runtime.GOOS == "windows" {
					t.Skip("a symbolic-link fixture needs a host that makes one; the Windows artefact is checked by its own rules")
				}
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
