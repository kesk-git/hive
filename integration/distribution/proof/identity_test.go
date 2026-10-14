package proof_test

import (
	"debug/buildinfo"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/kesk-git/hive/integration/distribution/proof"
)

func info(goVersion string, kv ...string) *buildinfo.BuildInfo {
	b := &buildinfo.BuildInfo{GoVersion: goVersion}
	for i := 0; i+1 < len(kv); i += 2 {
		b.Settings = append(b.Settings, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
	}
	return b
}

var stated = proof.Stated{
	HiveVersion: "0.1.0", ContractVersion: "1.5", SourceRevision: "abc123", GoRelease: "go1.27.0", OS: "linux", Arch: "amd64",
}

func soundInfo() *buildinfo.BuildInfo {
	return info("go1.27.0", "GOOS", "linux", "GOARCH", "amd64", "vcs.revision", "abc123", "vcs.modified", "false")
}

// An executable built from the revision the manifest states, with the release
// and for the platform it states, from a tree without change, passes.
func TestBuiltIdentityPasses(t *testing.T) {
	if got := proof.CheckBuilt("hive-pool", soundInfo(), stated); len(got) != 0 {
		t.Error(got)
	}
}

// Each way an executable's own identity departs from the manifest is found.
func TestBuiltIdentityCatchesEachDeparture(t *testing.T) {
	cases := []struct {
		name, expect string
		info         *buildinfo.BuildInfo
	}{
		{"another Go release", "was built with go1.26.0", info("go1.26.0", "GOOS", "linux", "GOARCH", "amd64", "vcs.revision", "abc123")},
		{"another platform", "was built for darwin-arm64", info("go1.27.0", "GOOS", "darwin", "GOARCH", "arm64", "vcs.revision", "abc123")},
		{"another revision", `carries the source revision "def456"`, info("go1.27.0", "GOOS", "linux", "GOARCH", "amd64", "vcs.revision", "def456")},
		{"no revision, as from a source archive", `carries the source revision ""`, info("go1.27.0", "GOOS", "linux", "GOARCH", "amd64")},
		{"a modified tree", "vcs.modified is true", info("go1.27.0", "GOOS", "linux", "GOARCH", "amd64", "vcs.revision", "abc123", "vcs.modified", "true")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := proof.CheckBuilt("hive-pool", c.info, stated)
			if !strings.Contains(strings.Join(got, "\n"), c.expect) {
				t.Errorf("no problem says %q: %q", c.expect, got)
			}
		})
	}
}

// The stated identity is the one every executable reports when run: its
// version by --version and in its capabilities, the contract version and the
// source revision.
func TestReportedIdentity(t *testing.T) {
	sound := func(name string) proof.Reported {
		return proof.Reported{Name: name, Version: "0.1.0", ImplementationVersion: "0.1.0", ContractVersion: "1.5", SourceRevision: "abc123"}
	}
	reports := []proof.Reported{sound("hive-pool"), sound("hive-word")}
	if got := proof.CheckReported(stated, reports, 2); len(got) != 0 {
		t.Errorf("a manifest that agrees with what the executables report: %q", got)
	}
	with := func(change func(r *proof.Reported)) []proof.Reported {
		r := sound("hive-word")
		change(&r)
		return []proof.Reported{sound("hive-pool"), r}
	}
	cases := []struct {
		name, expect string
		st           proof.Stated
		reports      []proof.Reported
		packages     int
	}{
		{"a version --version does not print", `hive-word reports the version "0.1.1" with --version`, stated, with(func(r *proof.Reported) { r.Version = "0.1.1" }), 2},
		{"a version the capabilities do not declare", `reports the version "0.1.1" in its capabilities`, stated, with(func(r *proof.Reported) { r.ImplementationVersion = "0.1.1" }), 2},
		{"a contract version the executable does not report", `reports the contract version "1.6"`, stated, with(func(r *proof.Reported) { r.ContractVersion = "1.6" }), 2},
		{"another source revision", `reports the source revision "def456"`, stated, with(func(r *proof.Reported) { r.SourceRevision = "def456" }), 2},
		{"an executable that was not run", "1 executables were run, not 2", stated, reports[:1], 2},
		{"an unknown revision", "states no source revision", proof.Stated{HiveVersion: "0.1.0", ContractVersion: "1.5"}, reports, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := proof.CheckReported(c.st, c.reports, c.packages)
			if !strings.Contains(strings.Join(got, "\n"), c.expect) {
				t.Errorf("no problem says %q: %q", c.expect, got)
			}
		})
	}
}

// What an executable prints that is not a complete capabilities response is
// an error, not an empty report.
func TestParseReportedRefusesAnIncompleteResponse(t *testing.T) {
	if r, err := proof.ParseReported("hive-pool", "0.1.0\n", `{"status":"ok"`); err == nil {
		t.Errorf("a truncated document was read: %+v", r)
	}
}
