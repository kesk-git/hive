package legacy

import "testing"

// Translated tests. Upstream: tasks-axi 0.2.6 at revision
// 9401ff899c0d1d8ae6b4fd8727b9025abda2032c; see imports/tasks-axi.json.
//
// test/id.test.ts, describe "id", the five cases, re-expressed against the one
// thing of src/id.ts that is taken, the legacy identifier the parser accepts
// (ValidID); minting an identifier is not taken:
//   - "validateId accepts slug-shaped ids": TestValidIDAcceptsSlugs.
//   - "validateId rejects ids with spaces or punctuation": TestValidIDRejects.
//   - "mintId mints a recognizable slug-xx id from a title", "namespaces with a
//     prefix" and "falls back to a base when the title has no slug characters":
//     TestValidIDAcceptsTheShapesMintingMade. Each upstream case checks that
//     the identifier it minted is one the grammar accepts; here the three
//     identifiers are written out, and nothing is minted.

func TestValidIDAcceptsSlugs(t *testing.T) {
	for _, id := range []string{"homemux-h7", "nm-release-validation", "a.b_c-1"} {
		if !ValidID(id) {
			t.Errorf("ValidID(%q) = false, want true", id)
		}
		if err := CheckID(id); err != nil {
			t.Errorf("CheckID(%q) = %v, want nil", id, err)
		}
	}
}

func TestValidIDRejects(t *testing.T) {
	for _, id := range []string{"Bad Id!", "has space", ""} {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true, want false", id)
		}
		if err := CheckID(id); err == nil {
			t.Errorf("CheckID(%q) = nil, want an error", id)
		}
	}
	// Beyond the upstream's three: the first character is a letter or digit,
	// and a line end is not allowed to hide behind the end of the pattern.
	for _, id := range []string{".a", "-a", "_a", "a b", "a\n", "a/b", "é"} {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true, want false", id)
		}
	}
}

func TestValidIDAcceptsTheShapesMintingMade(t *testing.T) {
	for _, id := range []string{"fix-the-summary-toggle-0a", "lavish-do-a-thing-9f", "task-3c"} {
		if !ValidID(id) {
			t.Errorf("ValidID(%q) = false, want true", id)
		}
	}
}
