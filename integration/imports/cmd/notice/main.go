// Command notice writes the root NOTICE from the import records: the same
// bytes check-imports compares the committed file with. Run it from the
// repository root.
package main

import (
	"fmt"
	"os"

	"github.com/kesk-git/hive/integration/imports"
)

func main() {
	records, problems := imports.LoadRecords(os.DirFS("."))
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, p)
		}
		os.Exit(1)
	}
	if err := os.WriteFile(imports.NoticeFile, imports.Notice(records), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "notice:", err)
		os.Exit(1)
	}
}
