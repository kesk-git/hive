package distribution

import (
	"testing"

	"github.com/kesk-git/hive/integration/distribution/proof"
)

func repo(t *testing.T) string {
	t.Helper()
	root, err := proof.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func expectNone(t *testing.T, findings []string, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// Item 3: no Go package outside contract/exec imports os/exec, calls
// os.StartProcess or a process-creation function of syscall, or reaches a
// process-creation call through a system library, and no macOS executable
// holds go-keyring or any third-party module whose record does not name its
// starts. The fixture modules beside the checks show that each break is found.
func TestOnlyTheOnePathStartsAProcess(t *testing.T) {
	findings, err := proof.Starts(repo(t))
	expectNone(t, findings, err)
	findings, err = proof.Libraries(repo(t))
	expectNone(t, findings, err)
}

// Item 4: every program a package runs through contract/exec is one its
// declaration lists, by the kind it is passed with.
func TestEveryProgramRunIsDeclared(t *testing.T) {
	findings, err := proof.Programs(repo(t))
	expectNone(t, findings, err)
}

// Item 5: no platform list is non-empty without a reason or names one of the
// five.
func TestPlatformUtilitiesAreReasonedAndNotOneOfTheFive(t *testing.T) {
	findings, err := proof.PlatformUtilities(repo(t))
	expectNone(t, findings, err)
}

// Item 7: the repository holds no build file of another toolchain.
func TestNoOtherToolchain(t *testing.T) {
	findings, err := proof.OtherToolchains(repo(t))
	expectNone(t, findings, err)
}
