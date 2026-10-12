package exec

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// startPath is the search path Hive's own process was started with. It is
// read once, when the package is loaded: a later change to the environment of
// the process does not change where programs are found.
var startPath = os.Getenv("PATH")

// statFunc reads the file at a path, following links.
type statFunc func(path string) (fs.FileInfo, error)

// find looks for the program on the search path by the lookup rules of
// spec/distribution.md, Finding the program, for the platform goos. The
// search path is split into directories; an empty or relative entry is
// skipped, so the current directory is never searched by accident. The
// result is an absolute path.
//
// On macOS and Linux the first regular file of the name with an execute bit
// is the program. On Windows the program is found only as <name>.exe,
// compared without regard to case as Windows compares names, and PATHEXT is
// not consulted; where no <name>.exe exists but a batch file of that name
// does, the run is refused and the file is named.
func find(goos, searchPath, name string, stat statFunc) (string, *StartError) {
	dirs := searchDirs(searchPath)
	if goos == "windows" {
		return findWindows(dirs, name, stat)
	}
	notExecutable := ""
	for _, dir := range dirs {
		path := filepath.Join(dir, name)
		fi, err := stat(path)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if fi.Mode().Perm()&0o111 != 0 {
			return path, nil
		}
		if notExecutable == "" {
			notExecutable = path
		}
	}
	if notExecutable != "" {
		return "", &StartError{Reason: NotExecutable, File: notExecutable}
	}
	return "", &StartError{Reason: Missing}
}

// searchDirs are the usable entries of a search path, in order.
func searchDirs(searchPath string) []string {
	var dirs []string
	for _, dir := range filepath.SplitList(searchPath) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		dirs = append(dirs, dir)
	}
	return dirs
}

// The files Windows runs only under cmd.exe, which is a shell.
var batchSuffixes = []string{".cmd", ".bat"}

func findWindows(dirs []string, name string, stat statFunc) (string, *StartError) {
	exe := name + ".exe"
	for _, dir := range dirs {
		path := filepath.Join(dir, exe)
		if fi, err := stat(path); err == nil && !fi.IsDir() {
			return path, nil
		}
	}
	for _, dir := range dirs {
		for _, suffix := range batchSuffixes {
			path := filepath.Join(dir, name+suffix)
			if fi, err := stat(path); err == nil && !fi.IsDir() {
				return "", &StartError{Reason: BatchFile, File: path}
			}
		}
	}
	return "", &StartError{Reason: Missing}
}

// lookup finds the program of the run on this platform's search path.
func lookup(p Program) (string, *StartError) {
	path, err := find(runtime.GOOS, startPath, p.Name, os.Stat)
	if err != nil {
		err.Program = p
	}
	return path, err
}
