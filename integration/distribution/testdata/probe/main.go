// Command probe reports which of the program names it is given it can find on
// its own search path, and the search path itself, and starts nothing. The
// tests run it under the restricted search path of the integration run to show
// what that path reaches.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

func main() {
	path := os.Getenv("PATH")
	found := map[string]string{}
	for _, name := range os.Args[1:] {
		file := name
		if runtime.GOOS == "windows" {
			file += ".exe"
		}
		for _, dir := range filepath.SplitList(path) {
			fi, err := os.Stat(filepath.Join(dir, file))
			if err == nil && fi.Mode().IsRegular() && (runtime.GOOS == "windows" || fi.Mode().Perm()&0o111 != 0) {
				found[name] = dir
				break
			}
		}
	}
	out, _ := json.Marshal(map[string]any{"path": path, "found": found})
	os.Stdout.Write(append(out, '\n'))
}
