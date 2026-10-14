package proof

import (
	"debug/buildinfo"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kesk-git/hive/contract/envelope"
)

// Stated is the identity manifest.json states.
type Stated struct {
	HiveVersion     string `json:"hive_version"`
	ContractVersion string `json:"contract_version"`
	SourceRevision  string `json:"source_revision"`
	GoRelease       string `json:"go_release"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
}

// Reported is what one executable says of itself when it is run: the line
// `--version` prints, and the implementation version, the contract version and
// the source revision its capabilities declare.
type Reported struct {
	Name                  string
	Version               string
	ImplementationVersion string
	ContractVersion       string
	SourceRevision        string
}

// ParseReported reads what an executable printed for `--version` and for
// `capabilities`. Output that is not a complete capabilities response is an
// error, never an empty report.
func ParseReported(name, version, capabilities string) (Reported, error) {
	r := Reported{Name: name, Version: strings.TrimRight(version, "\r\n")}
	resp, err := envelope.AcceptCapabilities([]byte(capabilities), 0)
	if err != nil {
		return r, fmt.Errorf("%s: its capabilities are not a complete response: %w", name, err)
	}
	caps, err := envelope.DecodeCapabilities(resp.Body)
	if err != nil {
		return r, fmt.Errorf("%s: its capabilities cannot be read: %w", name, err)
	}
	r.ImplementationVersion = caps.ImplementationVersion
	r.ContractVersion = caps.ContractVersion
	r.SourceRevision = caps.SourceRevision
	return r, nil
}

// CheckReported compares the identity stated with what each of the
// executables reports when it is run: its version by `--version` and in its
// capabilities, the contract version and the source revision of its
// capabilities.
func CheckReported(st Stated, reports []Reported, packages int) []string {
	var problems []string
	addf := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if st.SourceRevision == "" {
		addf("manifest.json states no source revision: an unknown identity does not qualify")
	}
	if len(reports) != packages {
		addf("%d executables were run, not %d", len(reports), packages)
	}
	for _, r := range reports {
		if r.Version != st.HiveVersion {
			addf("%s reports the version %q with --version; manifest.json states %q", r.Name, r.Version, st.HiveVersion)
		}
		if r.ImplementationVersion != st.HiveVersion {
			addf("%s reports the version %q in its capabilities; manifest.json states %q", r.Name, r.ImplementationVersion, st.HiveVersion)
		}
		if r.ContractVersion != st.ContractVersion {
			addf("%s reports the contract version %q; manifest.json states %q", r.Name, r.ContractVersion, st.ContractVersion)
		}
		if r.SourceRevision != st.SourceRevision {
			addf("%s reports the source revision %q in its capabilities; manifest.json states %q", r.Name, r.SourceRevision, st.SourceRevision)
		}
	}
	return problems
}

// CheckBuilt compares the identity stated with the build information of one
// executable: the Go release, the platform and the revision, and that the
// tree it was built from held no change.
func CheckBuilt(name string, info *buildinfo.BuildInfo, st Stated) []string {
	var problems []string
	addf := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	s := settings(info)
	if info.GoVersion != st.GoRelease {
		addf("%s was built with %s; manifest.json states %s", name, info.GoVersion, st.GoRelease)
	}
	if s["GOOS"] != st.OS || s["GOARCH"] != st.Arch {
		addf("%s was built for %s-%s; manifest.json states %s-%s", name, s["GOOS"], s["GOARCH"], st.OS, st.Arch)
	}
	if s["vcs.revision"] != st.SourceRevision {
		addf("%s carries the source revision %q; manifest.json states %q", name, s["vcs.revision"], st.SourceRevision)
	}
	if s["vcs.modified"] == "true" {
		addf("%s was built from a modified tree (vcs.modified is true), which is not a release", name)
	}
	return problems
}

// Identity checks the identity manifest.json states against what the
// executables themselves carry: the Go release, the platform and the source
// revision from the build information the toolchain embedded in each, read
// without running any of them, and the versions and the source revision each
// reports when it is run, given as reports. An executable whose build
// information marks a modified tree, or holds no revision, is not a release
// and fails (spec/distribution.md, Packaged build identity).
func Identity(dir string, w Want, reports []Reported) []string {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return []string{fmt.Sprintf("manifest.json cannot be read: %v", err)}
	}
	var st Stated
	if err := json.Unmarshal(data, &st); err != nil {
		return []string{fmt.Sprintf("manifest.json does not parse: %v", err)}
	}
	problems := CheckReported(st, reports, len(w.Executables))
	for _, e := range w.Executables {
		info, err := buildinfo.ReadFile(filepath.Join(dir, "bin", e+w.suffix()))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: its build information cannot be read: %v", e, err))
			continue
		}
		problems = append(problems, CheckBuilt(e, info, st)...)
	}
	return problems
}
