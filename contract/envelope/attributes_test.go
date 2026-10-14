package envelope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The repository's attributes keep the bytes of every committed file as they
// were committed: no end-of-line translation and no setting of a checkout
// changes them.
func TestRepositoryAttributesKeepCommittedBytes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	var rules []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			rules = append(rules, strings.Join(strings.Fields(line), " "))
		}
	}
	if len(rules) != 1 || rules[0] != "* -text" {
		t.Errorf("the attributes are %q, want the one rule %q", rules, "* -text")
	}
}
