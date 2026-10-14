// Command fake is the recorded fake of a program Hive runs and does not
// bundle: gh, claude, codex, no-mistakes, and on macOS security. The tests
// build it from this source and place a copy of the executable under the name
// of each program, so that the lookup rules find an executable and never a
// script. It does nothing a program does: it appends the arguments it was
// given, as one JSON line, to <name>.record beside itself, and exits 0.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	self, err := os.Executable()
	if err != nil {
		os.Exit(70)
	}
	name := strings.TrimSuffix(filepath.Base(self), ".exe")
	line, _ := json.Marshal(os.Args[1:])
	f, err := os.OpenFile(filepath.Join(filepath.Dir(self), name+".record"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		os.Exit(71)
	}
	defer f.Close()
	f.Write(append(line, '\n'))
}
