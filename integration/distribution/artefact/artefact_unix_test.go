//go:build unix

package artefact_test

import "testing"

// The artefact of macOS and of Linux is assembled with each link a relative
// symbolic link to its executable. A symbolic link needs a host that makes one
// without a right the platform may withhold, so the case is built for those
// hosts only; the artefact of Windows has its own case, which every host runs.
func TestAssembleWritesTheArtefactOfMacOSAndLinux(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		t.Run(goos, func(t *testing.T) { assembles(t, goos) })
	}
}
