package proof

import (
	"fmt"
	"strings"

	"github.com/kesk-git/hive/contract/exec/starts"
)

// Starts is How it is proved item 3: no Go source file of the module outside
// contract/exec starts a process by any means, by the one rule of
// contract/exec/starts. Test files are not part of an executable and are not
// read, and neither are the directories no scan reads.
func Starts(root string) ([]string, error) {
	var found []string
	err := walkFiles(root, func(rel string) error {
		if !sourceFile(rel) || rel == execPackage || strings.HasPrefix(rel, execPackage+"/") {
			return nil
		}
		src, err := readFile(root, rel)
		if err != nil {
			return err
		}
		f, err := starts.Find(rel, src)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		found = append(found, f...)
		return nil
	})
	return found, err
}
