package exec

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Var is the one variable Tendril, the launch primitive of the design's
// Secrets section, sets in a child from a resolved value. It is the only way
// a value enters a child, never an argument.
type Var struct {
	Name  string
	Value string
}

// The base variables a platform needs for any program to run, each copied by
// name from Hive's own environment where it is set.
var (
	unixBase    = []string{"PATH", "HOME", "USER", "LOGNAME", "TMPDIR", "TZ", "LANG"}
	windowsBase = []string{
		"PATH", "PATHEXT", "SystemRoot", "SystemDrive", "windir", "ComSpec", "TEMP", "TMP",
		"USERPROFILE", "HOMEDRIVE", "HOMEPATH", "APPDATA", "LOCALAPPDATA", "ProgramData",
		"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "CommonProgramFiles", "USERNAME",
		"USERDOMAIN", "COMPUTERNAME", "NUMBER_OF_PROCESSORS", "PROCESSOR_ARCHITECTURE", "OS",
	}
)

// Term is the value of TERM for a terminal start on macOS and Linux, which
// describes the pseudo-terminal Hive gives.
const Term = "xterm-256color"

var varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_()]*$`)

// envInput is what a child's environment is built from.
type envInput struct {
	goos string
	// environ is Hive's own environment, as os.Environ gives it.
	environ []string
	// config are the names of the program's own configuration variables that
	// the stage which first runs it names, copied by name where set.
	config []string
	// terminal says the start is a terminal start.
	terminal bool
	tendril  *Var
}

// buildEnv builds the child's environment from nothing, for one run: the
// platform's base variables, the program's configuration variables, TERM for
// a terminal start on macOS and Linux, and Tendril's one variable. Nothing
// else of Hive's environment passes, neither whole nor by pattern. The
// variables are in a fixed order, which on Windows is the order the platform
// requires: by name, without regard to case.
func buildEnv(in envInput) ([]string, error) {
	windows := in.goos == "windows"
	same := func(a, b string) bool {
		if windows {
			return strings.EqualFold(a, b)
		}
		return a == b
	}
	own := map[string]string{}
	spelled := map[string]string{} // the name as Hive's own environment spells it
	var ownNames []string
	for _, kv := range in.environ {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			// No name, or a name starting with =, the hidden variables of
			// Windows that hold a drive's directory; none is passed.
			continue
		}
		name := kv[:i]
		key := name
		if windows {
			key = strings.ToUpper(name)
		}
		if _, dup := own[key]; !dup {
			own[key] = kv[i+1:]
			spelled[key] = name
			ownNames = append(ownNames, name)
		}
	}
	// get returns the value of a variable and its name as Hive's environment
	// spells it, which on Windows may differ from name in case.
	get := func(name string) (value, spelling string, ok bool) {
		key := name
		if windows {
			key = strings.ToUpper(name)
		}
		value, ok = own[key]
		return value, spelled[key], ok
	}
	var out []string
	add := func(name, value string) { out = append(out, name+"="+value) }
	have := func(name string) bool {
		for _, kv := range out {
			if n, _, _ := strings.Cut(kv, "="); same(n, name) {
				return true
			}
		}
		return false
	}
	base := unixBase
	if windows {
		base = windowsBase
	}
	for _, name := range base {
		if v, spelling, ok := get(name); ok {
			add(spelling, v)
		}
	}
	if !windows {
		var lc []string
		for _, name := range ownNames {
			if strings.HasPrefix(name, "LC_") {
				lc = append(lc, name)
			}
		}
		for _, name := range lc {
			v, _, _ := get(name)
			add(name, v)
		}
	}
	for _, name := range in.config {
		if err := checkVarName(name); err != nil {
			return nil, err
		}
		if have(name) {
			continue
		}
		if v, spelling, ok := get(name); ok {
			add(spelling, v)
		}
	}
	if in.terminal && !windows {
		if have("TERM") {
			return nil, fmt.Errorf("exec: TERM is set by a terminal start and is no configuration variable")
		}
		add("TERM", Term)
	}
	if in.tendril != nil {
		if err := checkVarName(in.tendril.Name); err != nil {
			return nil, err
		}
		if strings.ContainsRune(in.tendril.Value, 0) {
			return nil, fmt.Errorf("exec: the value of %s holds a NUL character", in.tendril.Name)
		}
		if have(in.tendril.Name) {
			return nil, fmt.Errorf("exec: %s is already in the child's environment and is no variable of Tendril's", in.tendril.Name)
		}
		add(in.tendril.Name, in.tendril.Value)
	}
	key := func(kv string) string {
		name, _, _ := strings.Cut(kv, "=")
		if windows {
			return strings.ToUpper(name)
		}
		return name
	}
	sort.SliceStable(out, func(i, j int) bool { return key(out[i]) < key(out[j]) })
	return out, nil
}

// checkVarName refuses a name that cannot be a variable of a child.
func checkVarName(name string) error {
	if !varName.MatchString(name) {
		return fmt.Errorf("exec: %q is not the name of an environment variable", name)
	}
	return nil
}

// ownEnviron is Hive's own environment.
var ownEnviron = os.Environ
