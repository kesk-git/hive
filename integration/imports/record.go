package imports

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/kesk-git/hive/contract/envelope"
)

// RecordSchema is the schema every import record follows.
const RecordSchema = "urn:hive:schema:import-record:v1"

// Dir is where the records sit, relative to the repository root.
const Dir = "imports"

// FileRef is one retained upstream licence or notice text: its path, size and
// digest.
type FileRef struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
	Bytes  int64  `json:"bytes"`
}

// Portion is one file of this repository that holds imported material, or one
// retained test.
type Portion struct {
	HivePath string `json:"hive_path"`
}

// Record is the part of an import record the checks and the notice read.
type Record struct {
	ImportID string `json:"import_id"`
	Kind     string `json:"kind"`
	Upstream struct {
		Name       string `json:"name"`
		Repository string `json:"repository"`
		Module     *struct {
			Path    string `json:"path"`
			Version string `json:"version"`
		} `json:"module"`
	} `json:"upstream"`
	Licence struct {
		SPDX      string    `json:"spdx"`
		Copyright []string  `json:"copyright"`
		Files     []FileRef `json:"files"`
	} `json:"licence"`
	Portions      []Portion `json:"portions"`
	RetainedTests []Portion `json:"retained_tests"`
}

// Problem is one failure of a check: the rule of spec/imports.md, What
// check-imports proves, that it breaks, where, and what is wrong.
type Problem struct {
	Rule    int
	Where   string
	Message string
}

func (p Problem) String() string {
	return fmt.Sprintf("check %d: %s: %s", p.Rule, p.Where, p.Message)
}

// LoadRecords reads every record under imports/ of the tree, in import_id
// order. A file that is not a valid record whose name matches its import_id
// is a problem of the first check and is left out of the records.
func LoadRecords(fsys fs.FS) (records []Record, problems []Problem) {
	entries, err := fs.ReadDir(fsys, Dir)
	if err != nil {
		return nil, []Problem{{1, Dir, "the directory cannot be read: " + err.Error()}}
	}
	for _, e := range entries {
		name := e.Name()
		if name == "README.md" {
			continue
		}
		where := path.Join(Dir, name)
		bad := func(format string, args ...any) {
			problems = append(problems, Problem{1, where, fmt.Sprintf(format, args...)})
		}
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			bad("is not a record: a record is a file <import_id>.json")
			continue
		}
		data, err := fs.ReadFile(fsys, where)
		if err != nil {
			bad("cannot be read: %v", err)
			continue
		}
		if err := envelope.Validate(RecordSchema, data); err != nil {
			bad("is not a valid record: %v", err)
			continue
		}
		var r Record
		if err := json.Unmarshal(data, &r); err != nil {
			bad("cannot be read as a record: %v", err)
			continue
		}
		if want := strings.TrimSuffix(name, ".json"); r.ImportID != want {
			bad("is named for %q but its import_id is %q", want, r.ImportID)
			continue
		}
		records = append(records, r)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ImportID < records[j].ImportID })
	return records, problems
}
