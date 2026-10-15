package memory_test

import (
	"testing"

	"github.com/kesk-git/hive/contract/envelope"
)

// render, check and propagate are declared at 2.0 and accept no other
// operation version: a request at 1.0 is refused as unsupported_version
// before its body is read, and one at 2.0 is read against the v2 body.
func TestRenderCheckAndPropagateAcceptOnlyVersionTwo(t *testing.T) {
	h := newHome(t)
	for _, operation := range []string{"render", "check", "propagate"} {
		t.Run(operation, func(t *testing.T) {
			h := &home{t: t, dir: h.dir, clk: h.clk, exe: h.exe}
			resp := h.send("hive-memory", operation, map[string]any{}, request{version: "1.0"})
			if resp.Status != envelope.Invalid || resp.Error == nil || resp.Error.Code != envelope.CodeUnsupportedVersion {
				t.Fatalf("at 1.0: status %s, error %+v; want unsupported_version", resp.Status, resp.Error)
			}
			resp = h.send("hive-memory", operation, map[string]any{}, request{version: "2.0"})
			if resp.Error != nil && resp.Error.Code == envelope.CodeUnsupportedVersion {
				t.Fatalf("at 2.0: refused as unsupported_version")
			}
		})
	}
}
