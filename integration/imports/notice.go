package imports

import (
	"sort"
	"strings"
)

// NoticeFile is the root file the notices are committed in.
const NoticeFile = "NOTICE"

const noticeHeader = `NOTICE

Hive is distributed under the licence in the file LICENSE. This file carries
the copyright notices of the material Hive took from other projects: one
block for each import record under imports/, in order of import_id, each with
the notices of its record verbatim.
`

const noNotice = "No import record exists yet.\n"

const blockRule = "--------------------------------------------------------------------------------\n"

// Notice is the root NOTICE the records produce: a fixed header, then one
// block for each record in import_id order, each with the copyright notices
// of its licence verbatim. With no record it is the header and the one
// sentence that none exists yet. check-imports regenerates it from the
// records and compares it with the committed file byte for byte, and the
// artefact's assembler refuses a committed NOTICE that differs from it.
func Notice(records []Record) []byte {
	var b strings.Builder
	b.WriteString(noticeHeader)
	b.WriteString("\n")
	if len(records) == 0 {
		b.WriteString(noNotice)
		return []byte(b.String())
	}
	sorted := append([]Record(nil), records...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ImportID < sorted[j].ImportID })
	for i, r := range sorted {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(blockRule)
		b.WriteString(r.ImportID + ": " + r.Upstream.Name + "\n")
		b.WriteString("Licence: " + r.Licence.SPDX + "\n")
		if len(r.Licence.Files) > 0 {
			var files []string
			for _, f := range r.Licence.Files {
				files = append(files, f.Path)
			}
			sort.Strings(files)
			b.WriteString("Licence texts kept in the repository: " + strings.Join(files, ", ") + "\n")
		}
		b.WriteString("\n")
		for _, c := range r.Licence.Copyright {
			b.WriteString(c)
			if !strings.HasSuffix(c, "\n") {
				b.WriteString("\n")
			}
		}
	}
	return []byte(b.String())
}
