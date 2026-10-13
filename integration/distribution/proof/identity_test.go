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

// The stated identity is the source's: every package's version, and the
// contract version the library declares.
func TestSourceIdentity(t *testing.T) {
	versions := map[string]string{"pool": "0.1.0", "word": "0.1.0"}
	if got := proof.CheckSource(stated, versions, "1.5", 2); len(got) != 0 {
		t.Errorf("a manifest that agrees with its source: %q", got)
	}
	cases := []struct {
		name, expect string
		st           proof.Stated
		versions     map[string]string
		contract     string
		packages     int
	}{
		{"a version the source does not declare", "the source of word declares 0.1.1", stated, map[string]string{"pool": "0.1.0", "word": "0.1.1"}, "1.5", 2},
		{"a contract version the library does not declare", "declares that built code meets 1.6", stated, versions, "1.6", 2},
		{"a package with no version", "for 2 packages", stated, versions, "1.5", 3},
		{"an unknown revision", "states no source revision", proof.Stated{HiveVersion: "0.1.0", ContractVersion: "1.5"}, versions, "1.5", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := proof.CheckSource(c.st, c.versions, c.contract, c.packages)
			if !strings.Contains(strings.Join(got, "\n"), c.expect) {
				t.Errorf("no problem says %q: %q", c.expect, got)
			}
		})
	}
}
