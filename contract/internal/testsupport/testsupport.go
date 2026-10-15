// Package testsupport holds the helpers the tests of the contract share. Only
// the tests of the contract import it: it is under contract/internal/, no
// delivered file imports it, and it starts no process of
// its own: where a test needs a platform utility it runs it through
// contract/exec.
//
// It imports no package of the contract that holds a test using it, so no test
// of the contract is in an import cycle through it. Home-access helpers live
// in the access subpackage; see its package comment for the store dependency
// and import-cycle constraint.
package testsupport

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/ids"
)

// alphabet is Crockford base 32 in lowercase, for invented identifiers.
const alphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// ID invents the n-th identifier of kind p: a valid, lowercase ULID whose
// last two characters count n.
func ID(p ids.Prefix, n int) string {
	s := string(p) + "_0" + strings.Repeat("0", 23) + string(alphabet[n/32%32]) + string(alphabet[n%32])
	if err := ids.Check(s, p); err != nil {
		panic(err)
	}
	return s
}

// LockPlaceholder is the value Tree gives every lock file in place of a
// digest. It is not 64 lowercase hexadecimal characters, so it cannot equal
// the digest of any file's bytes.
const LockPlaceholder = "lock file, not read"

// Tree is the state of a directory: every file by slash path below it, with
// the digest of its bytes.
//
// A lock file is listed without being opened, the same on every platform: a
// file directly inside the top-level locks/ directory of dir (locks/pool.lock,
// whatever its name) appears under its slash path with LockPlaceholder as its
// value, whatever its content. A lock file's content is never a record and no
// test compares it, and a lock held through a handle that shares deletion
// cannot be opened by os.ReadFile on Windows. The rule is by place alone: a
// file in a deeper directory under locks/ (locks/x/y.lock) and a file named
// like a lock file elsewhere (pool/pool.lock, a nested locks/ directory) are
// read and digested like any other file.
func Tree(t testing.TB, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if isLockFile(rel) {
			out[rel] = LockPlaceholder
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		out[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// isLockFile reports whether the slash path rel, relative to the walked
// directory, is a file directly inside its top-level locks/ directory.
func isLockFile(rel string) bool {
	dir, name, ok := strings.Cut(rel, "/")
	return ok && dir == "locks" && name != "" && !strings.Contains(name, "/")
}

// Parsed reads a JSON object.
func Parsed(t testing.TB, data []byte) map[string]any {
	t.Helper()
	v, err := canon.Parse(data)
	if err != nil {
		t.Fatalf("%v: %q", err, data)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("not an object: %q", data)
	}
	return m
}
