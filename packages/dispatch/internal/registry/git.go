package registry

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/exec"
)

// gitName is the program the operation runs, to read a clone. It writes
// nothing into a clone or its Git directory: it asks Git for the common
// directory, the object format, the working trees and one remote's url, and
// no more.
const gitName = "git"

// gitConfig are the names of Git's own configuration variables that pass from
// Hive's environment to the Git it runs, where they are set. Nothing else of
// the environment does.
var gitConfig = []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM", "XDG_CONFIG_HOME"}

// maxGitOutput bounds what the operation reads of Git's answer.
const maxGitOutput = 1 << 20

// The oldest Git that knows every option a clone is read with: the last of
// them to come, -z for git worktree list, came with Git 2.36.
const (
	minGitMajor = 2
	minGitMinor = 36
)

// What is read of a clone: where its Git common directory and its main working
// tree are, as physical paths with forward slashes, its object format, and the
// url of the named remote where one was named. The main working tree is "" where
// Git cannot name it: from a linked working tree of a repository whose Git
// directory is kept apart from its main working tree.
type cloneFacts struct {
	commonDir    string
	worktreeRoot string
	objectFormat string
	remoteURL    string
	hasRemote    bool
}

// readClone resolves the clone that the path lies in, and reads the remote
// named, when one is. The path is the one the request gave for the field
// named at; a failure names the field and never the path.
func readClone(path, remote, at string) (*cloneFacts, error) {
	dir := filepath.FromSlash(path)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, envelope.Common(envelope.CodeSourceUnreadable, at+" is not a directory that can be read")
	}
	notAClone := envelope.Common(envelope.CodeSourceUnreadable, at+" is not inside a Git repository that Git can read")

	if err := checkGit(dir); err != nil {
		return nil, err
	}

	// The object format and the common directory, in the order asked. The
	// object format is one fixed line, so that what follows it is the path
	// whole, less the one newline Git ends its answer with.
	out, code, err := git(dir, "rev-parse", "--show-object-format", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, notAClone
	}
	format, rest, found := strings.Cut(string(out), "\n")
	if !found || (format != "sha1" && format != "sha256") {
		return nil, envelope.Common(envelope.CodeSourceUnreadable, "git named an object format for "+at+" that is neither sha1 nor sha256")
	}
	facts := &cloneFacts{objectFormat: format}
	commonDir, ok := strings.CutSuffix(rest, "\n")
	if !ok {
		return nil, envelope.Common(envelope.CodeSourceUnreadable, "git gave an answer for "+at+" that cannot be read")
	}
	if facts.commonDir, err = physicalPath(commonDir, at); err != nil {
		return nil, err
	}

	// The top level of the main working tree. A repository with no working tree
	// is bare, and a bare repository is no clone. The first working tree Git
	// lists is the main one, but Git names the common directory there when the
	// Git directory is kept apart from the working tree, as it is for a
	// submodule; such a candidate is no working tree, and then the tree the path
	// lies in is the main one if its Git directory is the common directory, and
	// otherwise Git cannot name it.
	out, code, err = git(dir, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, notAClone
	}
	root, bare := mainWorktree(out)
	if root == "" || bare {
		return nil, envelope.Common(envelope.CodeSourceUnreadable, at+" is in a repository with no working tree")
	}
	for _, candidate := range []string{root, dir} {
		top, found, err := topLevelOf(candidate, facts.commonDir)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		if facts.worktreeRoot, err = physicalPath(top, at); err != nil {
			return nil, err
		}
		break
	}

	if remote == "" {
		return facts, nil
	}
	out, code, err = git(dir, "config", "--get", "-z", "remote."+remote+".url")
	if err != nil {
		return nil, err
	}
	switch code {
	case 0:
		facts.remoteURL = strings.TrimSuffix(string(out), "\x00")
		facts.hasRemote = facts.remoteURL != ""
	case 1:
	default:
		return nil, notAClone
	}
	if !facts.hasRemote {
		return nil, envelope.Common(envelope.CodeSourceUnreadable, "the remote named for "+at+" is not one the clone has")
	}
	return facts, nil
}

// mainWorktree reads the first working tree of the answer of git worktree list
// --porcelain -z: the fields of a tree end in NUL, and a tree ends in one more.
func mainWorktree(out []byte) (root string, bare bool) {
	for _, field := range bytes.Split(out, []byte{0}) {
		if len(field) == 0 {
			break
		}
		switch f := string(field); {
		case strings.HasPrefix(f, "worktree "):
			root = strings.TrimPrefix(f, "worktree ")
		case f == "bare":
			bare = true
		}
	}
	return root, bare
}

// checkGit says whether the Git found is one that knows the options a clone is
// read with, by the version it reports: a Git that does not is unavailable.
func checkGit(dir string) error {
	tooOld := envelope.Fail(envelope.Unavailable, envelope.CodeDependencyUnusable, fmt.Sprintf("the git found is older than %d.%d, which provisioning needs to read a clone", minGitMajor, minGitMinor))
	out, code, err := git(dir, "version")
	if err != nil {
		return err
	}
	var major, minor int
	if _, err := fmt.Sscanf(string(out), "git version %d.%d", &major, &minor); code != 0 || err != nil {
		return tooOld
	}
	if major < minGitMajor || (major == minGitMajor && minor < minGitMinor) {
		return tooOld
	}
	return nil
}

// topLevelOf is the top level of the working tree that dir lies in, when that
// tree's Git directory is the common directory: the main working tree.
func topLevelOf(dir, commonDir string) (string, bool, error) {
	top, found, err := gitPath(dir, "--show-toplevel")
	if err != nil || !found {
		return "", false, err
	}
	gitDir, found, err := gitPath(dir, "--absolute-git-dir")
	if err != nil || !found || !sameDirectory(gitDir, commonDir) {
		return "", false, err
	}
	return top, true, nil
}

// gitPath is the one path that rev-parse prints for the option, less the
// newline Git ends its answer with. It is not found where Git has none to give.
func gitPath(dir, option string) (string, bool, error) {
	out, code, err := git(dir, "rev-parse", option)
	if err != nil || code != 0 {
		return "", false, err
	}
	path, ok := strings.CutSuffix(string(out), "\n")
	return path, ok, nil
}

// git runs Git in dir through the one process-execution path and returns its
// standard output and its exit code. A run that gives no result, or none that
// can be read, is the failure the path fixes for it: Git changes nothing here, so
// none of it is an unknown outcome.
func git(dir string, args ...string) ([]byte, int64, error) {
	program := exec.Program{Kind: exec.Core, Name: gitName}
	run, err := exec.Start(exec.Spec{
		Program: program, Args: args, Dir: dir, Config: gitConfig,
		Stdin: exec.Null(), Stdout: exec.Pipe(), Stderr: exec.Pipe(),
	})
	if err != nil {
		var se *exec.StartError
		if errors.As(err, &se) {
			return nil, 0, se.Failure()
		}
		return nil, 0, envelope.Common(envelope.CodeInternal, "git cannot be started").Because(err)
	}
	defer run.Close()
	// Standard error is drained at once, so that a full pipe never holds Git.
	drained := make(chan struct{})
	go func() {
		io.Copy(io.Discard, run.Stderr())
		close(drained)
	}()
	out, rerr := io.ReadAll(io.LimitReader(run.Stdout(), maxGitOutput+1))
	if len(out) > maxGitOutput {
		// Git has more to say than this operation reads: discard the rest so
		// that it can end.
		io.Copy(io.Discard, run.Stdout())
		rerr = fmt.Errorf("the answer is more than %d bytes", maxGitOutput)
	}
	<-drained
	res := run.Wait()
	code, failure := res.External(program, false)
	if failure != nil {
		return nil, 0, failure
	}
	if rerr != nil {
		return nil, 0, envelope.Common(envelope.CodeSourceUnreadable, "git's answer cannot be read").Because(rerr)
	}
	return out, code, nil
}

// physicalPath is the path with every symbolic link and, on Windows, every
// junction resolved, written with forward slashes in the form the platform's
// file system reports: no long-path prefix and no network share
// (spec/records.md, Encoding).
func physicalPath(path, at string) (string, error) {
	unreadable := func(what string) error {
		return envelope.Common(envelope.CodeSourceUnreadable, at+": "+what)
	}
	real, err := filepath.EvalSymlinks(filepath.FromSlash(path))
	if err != nil {
		return "", unreadable("a path of the clone cannot be resolved")
	}
	if real, err = filepath.Abs(real); err != nil {
		return "", unreadable("a path of the clone cannot be made absolute")
	}
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(real, `\\?\UNC\`) {
			return "", unreadable("a clone on a network share has no physical path to record")
		}
		real = strings.TrimPrefix(real, `\\?\`)
		if strings.HasPrefix(real, `\\`) {
			return "", unreadable("a clone on a network share has no physical path to record")
		}
	}
	return filepath.ToSlash(real), nil
}

// sameDirectory says whether two directories are the same one, by file
// identity, the device and inode on macOS and Linux and the volume and file
// index on Windows, and never by the spelling of the path: two spellings that
// differ only in case, or by a link, name one directory. A directory that
// cannot be examined is the same as no other.
func sameDirectory(a, b string) bool {
	if a == b {
		return true
	}
	ia, err := os.Stat(filepath.FromSlash(a))
	if err != nil {
		return false
	}
	ib, err := os.Stat(filepath.FromSlash(b))
	if err != nil {
		return false
	}
	return os.SameFile(ia, ib)
}
