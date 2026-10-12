package declare

import (
	"fmt"
	"strings"
	"unicode"
)

// patternChars are the characters with which a build file, a glob or a filter
// selects by pattern.
const patternChars = "*?%["

// CheckBuildFile applies rule 5 of spec/naming.md to the text of a build
// file: no pattern, glob or filter selects by the suffix pool. Targets,
// directories and commands are listed by their full names, because a pattern
// that matches hive-pool matches hive-spawning-pool, and the Spawning Pool
// is not the Pool. name is the file's name, for the problems.
func CheckBuildFile(name string, text []byte) Problems {
	var ps Problems
	for n, line := range strings.Split(string(text), "\n") {
		for _, word := range strings.FieldsFunc(line, wordBreak) {
			if selectsBySuffix(word) {
				ps = append(ps, Problem{"names", fmt.Sprintf("%s:%d", name, n+1), fmt.Sprintf("%s selects by the suffix pool; targets, directories and commands are listed by their full names", word)})
			}
		}
	}
	return ps
}

// wordBreak separates the words of a build file: a path is read a segment at
// a time, and an argument list or a function call an item at a time.
func wordBreak(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("/,();:|='\"`", r)
}

// selectsBySuffix reports whether a word of a build file is a pattern whose
// literal part ends in pool, or an expression anchored after pool.
func selectsBySuffix(word string) bool {
	if strings.HasSuffix(word, "pool$") {
		return true
	}
	if !strings.HasSuffix(word, "pool") {
		return false
	}
	return strings.ContainsAny(strings.TrimSuffix(word, "pool"), patternChars)
}
