// Package proof holds the checks of test-distribution, each a function of a
// directory or a source tree and the platform whose rules apply, so that a
// fixture of any platform's artefact can be checked on any host and the break
// each check catches can be shown.
package proof

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

// RepoRoot is the root of the module that holds the directory the process
// runs in: the nearest parent with a go.mod.
func RepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

// FindArtefact is the one artefact `make assemble` left for this platform,
// dist/hive-<version>-<os>-<arch>. It never builds one.
func FindArtefact(root string) (string, error) {
	pattern := filepath.Join(root, "dist", "hive-*-"+runtime.GOOS+"-"+runtime.GOARCH)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.IsDir() {
			dirs = append(dirs, m)
		}
	}
	switch len(dirs) {
	case 0:
		return "", fmt.Errorf("no artefact for %s-%s under dist/: run `make build` and `make assemble` first; this target does not build one", runtime.GOOS, runtime.GOARCH)
	case 1:
		return dirs[0], nil
	}
	return "", fmt.Errorf("more than one artefact for %s-%s under dist/ (%v): remove the stale ones", runtime.GOOS, runtime.GOARCH, dirs)
}

// Declared reads, from the package declarations alone, the seven executables
// and the twenty-one links of spec/commands.md, Executables and command
// names: each package's executable, and every other command name it owns as a
// link to that executable. It reads the declarations as plain JSON, so that
// what it expects does not come from any code of the artefact.
func Declared(root string) (executables []string, links map[string]string, err error) {
	files, err := filepath.Glob(filepath.Join(root, "packages", "*", "hive-package.json"))
	if err != nil {
		return nil, nil, err
	}
	links = map[string]string{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, nil, err
		}
		var d struct {
			Executable string `json:"executable"`
			Commands   []struct {
				Name string `json:"name"`
			} `json:"commands"`
		}
		if err := json.Unmarshal(data, &d); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", f, err)
		}
		executables = append(executables, d.Executable)
		for _, c := range d.Commands {
			if c.Name != d.Executable {
				links[c.Name] = d.Executable
			}
		}
	}
	sort.Strings(executables)
	return executables, links, nil
}
