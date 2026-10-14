package legacy_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/packages/pool/internal/entry"
	_ "github.com/kesk-git/hive/packages/pool/internal/item"
	_ "github.com/kesk-git/hive/packages/pool/internal/legacy"
)

// The acceptance test of the dry-run import and of the export of the legacy
// backlog (spec/pool.md, Legacy import and export). It drives the executable
// the default registry describes, in process, over a home of its own: the
// operations import-legacy with dry_run true and export-legacy, with the
// refusals the section states. A dry run writes nothing, in the home or at
// the source path.

var start = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

// home is a home with the Pool's executable over it.
type home struct {
	t      testing.TB
	dir    string
	homeID string
	exe    *cli.Executable
}

func mint(t testing.TB, p ids.Prefix) string {
	t.Helper()
	id, err := ids.Mint(clock.System{}, p)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func writer(operation string) map[string]any {
	return map[string]any{"package": "pool", "command": "hive-pool", "operation": operation, "implementation_version": "test"}
}

// writeHome writes the home record of the home at dir.
func writeHome(t testing.TB, dir, homeID string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := canon.Encode(map[string]any{
		"schema": "hive.home", "schema_version": "1.0", "record_revision": int64(1),
		"home_id": homeID, "role": "hive", "name": "A test home", "parent": map[string]any{"kind": "none"},
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z", "writer": writer("provision"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "home.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newHome(t testing.TB) *home {
	t.Helper()
	h := &home{t: t, dir: filepath.Join(t.TempDir(), "home"), homeID: mint(t, ids.Home)}
	writeHome(t, h.dir, h.homeID)
	h.exe = entry.Executable()
	h.exe.Clock = clock.NewFake(start)
	return h
}

// do sends one request to hive-pool and returns the response as it was
// written.
func (h *home) do(operation string, body map[string]any) *envelope.Response {
	h.t.Helper()
	req := map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": "hive-pool", "operation": operation, "operation_version": "1.0",
		"request_id": mint(h.t, ids.Request), "correlation_id": mint(h.t, ids.Correlation),
		"issued_at": "2026-10-14T09:00:00.000Z", "caller": map[string]any{"kind": "test"},
		"scope_level": "hive", "home_id": h.homeID,
		"authority": map[string]any{"kind": "none"}, "body": body,
	}
	data, err := canon.Encode(req)
	if err != nil {
		h.t.Fatal(err)
	}
	var cmd *cli.Command
	for _, c := range h.exe.Commands {
		if c.Name == "hive-pool" {
			cmd = c
		}
	}
	out, err := h.exe.Handle(cli.Invocation{Command: cmd, Operation: operation, Home: h.dir, Request: append(data, '\n')})
	if err != nil {
		h.t.Fatal(err)
	}
	resp, err := envelope.DecodeResponse(out.Encoded)
	if err != nil {
		h.t.Fatal(err)
	}
	return resp
}

// ok fails the test unless the response is ok.
func ok(t testing.TB, resp *envelope.Response) map[string]any {
	t.Helper()
	if resp.Status != envelope.Ok {
		t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
	}
	return resp.Body
}

// refused checks the status and the code of a response that must not be ok.
func refused(t testing.TB, resp *envelope.Response, status envelope.Status, code string) {
	t.Helper()
	if resp.Status != status || resp.Error == nil || resp.Error.Code != code {
		t.Fatalf("want %s %s; got status %s, error %+v", status, code, resp.Status, resp.Error)
	}
	if resp.Body != nil {
		t.Errorf("a refusal carries a body: %v", resp.Body)
	}
}

// snapshot is every file under dir, the Pool's lock excepted, with the digest
// of its bytes.
func snapshot(t testing.TB, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		// The Pool's lock file is made by any operation that reads the Pool,
		// hive-pool read as much as an export.
		if rel == "locks" || strings.HasPrefix(rel, "locks"+string(filepath.Separator)) {
			return nil
		}
		// A directory is made by opening the Pool, as hive-pool read opens it;
		// what an operation writes is a file.
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		out[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// source copies the fixture into a directory of its own, sets its
// modification time to a fixed past one, and returns its path, its bytes and
// that time: a write of the file by the import would change one of them.
func source(t testing.TB, fixture string) (string, []byte, time.Time) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "legacy", fixture))
	if err != nil {
		t.Fatal(err)
	}
	return sourceBytes(t, data)
}

func sourceBytes(t testing.TB, data []byte) (string, []byte, time.Time) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "backlog.md")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	return path, data, when
}

// unchanged fails the test unless the source file holds the same bytes and
// has the same modification time.
func unchanged(t testing.TB, path string, data []byte, when time.Time) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) || !info.ModTime().Equal(when) {
		t.Errorf("the source file was written: %d bytes, modified %s", len(got), info.ModTime())
	}
}

// dryRun imports the file as a dry run and returns the report, after checking
// that the home and the source file are as they were.
func (h *home) dryRun(path string, data []byte, when time.Time) map[string]any {
	h.t.Helper()
	before := snapshot(h.t, h.dir)
	body := ok(h.t, h.do("import-legacy", map[string]any{"path": path, "zone": "UTC", "dry_run": true}))
	if after := snapshot(h.t, h.dir); !equal(before, after) {
		h.t.Errorf("a dry run wrote in the home: before %v, after %v", before, after)
	}
	unchanged(h.t, path, data, when)
	report, _ := body["report"].(map[string]any)
	if report == nil {
		h.t.Fatalf("no report in %v", body)
	}
	if report["digest"] != canon.FileDigest(data) || report["bytes"] != int64(len(data)) {
		h.t.Errorf("the report names digest %v and %v bytes; the file has %s and %d", report["digest"], report["bytes"], canon.FileDigest(data), len(data))
	}
	if report["round_trip_exact"] != true {
		h.t.Errorf("round_trip_exact is %v", report["round_trip_exact"])
	}
	for _, k := range []string{"migration", "items", "holds"} {
		if _, has := body[k]; has {
			h.t.Errorf("a dry run returns %s", k)
		}
	}
	return report
}

func equal(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// entryOf is the report's entry for the legacy identifier.
func entryOf(t testing.TB, report map[string]any, legacyID string) map[string]any {
	t.Helper()
	list, _ := report["entries"].([]any)
	for _, v := range list {
		if e := v.(map[string]any); e["legacy_id"] == legacyID {
			return e
		}
	}
	t.Fatalf("the report holds no entry %s: %v", legacyID, list)
	return nil
}

func entryIDs(report map[string]any) []string {
	var out []string
	for _, v := range report["entries"].([]any) {
		out = append(out, v.(map[string]any)["legacy_id"].(string))
	}
	return out
}

// want checks the section, the state and the disposition of a report entry.
func want(t testing.TB, report map[string]any, legacyID, section, state, disposition string) map[string]any {
	t.Helper()
	e := entryOf(t, report, legacyID)
	if got := strings.TrimSuffix(e["section"].(string), "\r"); got != section || e["state"] != state || e["disposition"] != disposition {
		t.Errorf("%s: section %q, state %v, disposition %v; want %q, %s, %s", legacyID, got, e["state"], e["disposition"], section, state, disposition)
	}
	if _, has := e["task_id"]; has {
		t.Errorf("%s: a dry run mints no identity, and the report names %v", legacyID, e["task_id"])
	}
	return e
}

// The two upstream fixtures, re-expressed as Hive fixtures, are read in full
// by a dry run, which reports every entry and renders the file back byte for
// byte; the persistent supervisor entry is excluded and no item is made of it.
func TestDryRunOfTheTwoUpstreamFixtures(t *testing.T) {
	h := newHome(t)
	path, data, when := source(t, "backlog.md")
	report := h.dryRun(path, data, when)
	wantIDs := []string{"keeps-gadget-h7", "cache-adopt-l5", "cache-adopt", "promote-check", "key-cleanup", "survey-scout-d4", "cache-core-t4", "route-fix-k4", "notes-multi-w8"}
	if got := entryIDs(report); !slices.Equal(got, wantIDs) {
		t.Errorf("the report lists %v; want %v", got, wantIDs)
	}
	e := want(t, report, "keeps-gadget-h7", "## In flight", "in-flight", "excluded")
	if reason, _ := e["reason"].(string); reason == "" {
		t.Error("an exclusion gives its reason")
	}
	want(t, report, "cache-adopt-l5", "## In flight", "in-flight", "item")
	want(t, report, "cache-adopt", "## Queued", "queued", "item")
	want(t, report, "survey-scout-d4", "## Done (10 most recent)", "done", "item")
	want(t, report, "notes-multi-w8", "## Done (10 most recent)", "done", "item")
	if n := report["free_lines"].(int64); n < 1 {
		t.Errorf("free_lines is %d", n)
	}

	path, data, when = source(t, "checkbox-in-flight.md")
	report = h.dryRun(path, data, when)
	want(t, report, "fix-signin-k3", "## In flight", "in-flight", "item")
	want(t, report, "add-checks-q7", "## Queued", "queued", "item")
	want(t, report, "old-done-z1", "## Done", "done", "item")
	if len(report["entries"].([]any)) != 3 {
		t.Errorf("the report lists %v", entryIDs(report))
	}
}

// A file with every tag the table names is read, and a section the grammar does
// not recognize passes through as free lines.
func TestDryRunOfAFileWithEveryTag(t *testing.T) {
	h := newHome(t)
	path, data, when := source(t, "every-tag.md")
	report := h.dryRun(path, data, when)
	for id, state := range map[string]string{
		"tagged-a1": "in-flight", "tagged-b2": "queued", "tagged-c3": "queued", "tagged-e5": "queued",
		"tagged-d4": "done", "tagged-f6": "done", "tagged-g7": "done", "tagged-h8": "done",
	} {
		if e := entryOf(t, report, id); e["state"] != state || e["disposition"] != "item" {
			t.Errorf("%s: %v", id, e)
		}
	}
	if len(report["entries"].([]any)) != 8 {
		t.Errorf("the report lists %v: an entry of an unrecognized section is a free line", entryIDs(report))
	}
	// The entry of the section that is not one of the three is not addressed.
	for _, id := range entryIDs(report) {
		if id == "looks-like-an-entry" {
			t.Error("an entry of an unrecognized section has an identity")
		}
	}
}

// A file with carriage returns, a header suffix, an unrecognized section and
// no final newline is read and renders back exactly.
func TestDryRunOfCarriageReturnsASuffixAndAnUnrecognizedSection(t *testing.T) {
	h := newHome(t)
	path, data, when := source(t, "crlf-suffix-unrecognized.md")
	report := h.dryRun(path, data, when)
	want(t, report, "trail-a1", "## In flight", "in-flight", "item")
	want(t, report, "trail-b2", "## Queued", "queued", "item")
	want(t, report, "trail-d4", "## Done (10 most recent)", "done", "item")
	if ids := entryIDs(report); len(ids) != 3 {
		t.Errorf("the report lists %v", ids)
	}
	// Four lines of the preamble, the loose note and its blank line, and the
	// three lines of the section that is not recognized.
	if report["free_lines"] != int64(9) {
		t.Errorf("free_lines is %v", report["free_lines"])
	}
}

// A second entry with an identity already seen is a duplicate, an edge to an
// identifier the file does not hold is a warning, and the file is still read
// whole.
func TestDryRunReportsDuplicatesAndDanglingEdges(t *testing.T) {
	h := newHome(t)
	text := "# Backlog\n\n## Queued\n- [ ] same-a1 - first (repo: x)\n- [ ] edge-b2 - second blocked-by: gone-z9 - kept\n- [ ] same-a1 - again\n\n## Done\n"
	path, data, when := sourceBytes(t, []byte(text))
	report := h.dryRun(path, data, when)
	list := report["entries"].([]any)
	if len(list) != 3 {
		t.Fatalf("the report lists %v", list)
	}
	if first, again := list[0].(map[string]any), list[2].(map[string]any); first["disposition"] != "item" || again["disposition"] != "duplicate" || again["reason"] == "" {
		t.Errorf("first %v, second %v", first, again)
	}
	if ws, _ := report["warnings"].([]any); len(ws) == 0 {
		t.Error("a dangling edge and a duplicate are warned")
	}
}

// A PERSISTENT SECONDMATE entry is excluded by its leading words whatever kind
// tag it carries, and a duplicate identity does not make an item of it.
func TestDryRunExcludesAPersistentSupervisorWhateverItsKindTag(t *testing.T) {
	h := newHome(t)
	text := "# Backlog\n\n## Queued\n- [ ] boss-a1 - PERSISTENT SECONDMATE owns the project (kind: scout)\n- [ ] boss-a1 - PERSISTENT SECONDMATE owns the project again (kind: scout)\n- [ ] plain-b2 - SCOUT looks around\n\n## Done\n"
	path, data, when := sourceBytes(t, []byte(text))
	report := h.dryRun(path, data, when)
	list := report["entries"].([]any)
	if len(list) != 3 {
		t.Fatalf("the report lists %v", list)
	}
	for i, wantDisposition := range []string{"excluded", "excluded", "item"} {
		if got := list[i].(map[string]any)["disposition"]; got != wantDisposition {
			t.Errorf("entry %d is %v; want %s", i, got, wantDisposition)
		}
	}
}

// unrepresentable checks that a dry run of the text is refused as malformed,
// naming the line and the field, and writes nothing.
func (h *home) unrepresentable(text string, line int64, field string) {
	h.t.Helper()
	path, data, when := sourceBytes(h.t, []byte(text))
	before := snapshot(h.t, h.dir)
	resp := h.do("import-legacy", map[string]any{"path": path, "zone": "UTC", "dry_run": true})
	refused(h.t, resp, envelope.Invalid, "pool.legacy_malformed")
	if resp.Error.Detail["line"] != line || resp.Error.Detail["field"] != field {
		h.t.Errorf("the refusal names %v; want line %d and field %s", resp.Error.Detail, line, field)
	}
	if after := snapshot(h.t, h.dir); !equal(before, after) {
		h.t.Errorf("a refused dry run wrote in the home: before %v, after %v", before, after)
	}
	unchanged(h.t, path, data, when)
}

// An identifier longer than the report can carry is refused, naming its line.
func TestDryRunRefusesAnIdentifierTooLongToReport(t *testing.T) {
	newHome(t).unrepresentable("# Backlog\n\n## Queued\n- [ ] short-a1 - fine\n- [ ] "+strings.Repeat("a", 201)+" - too long\n\n## Done\n", 5, "legacy_id")
}

// A section header that is not UTF-8 is refused, naming its line.
func TestDryRunRefusesASectionHeaderThatIsNotUTF8(t *testing.T) {
	newHome(t).unrepresentable("# Backlog\n\n## Queued\n\n## Done\xff\n- [x] enc-a1 - closed (done 2026-10-01)\n", 5, "section")
}

// expected_digest makes the caller import what it inspected.
func TestExpectedDigest(t *testing.T) {
	h := newHome(t)
	path, data, when := source(t, "checkbox-in-flight.md")
	body := map[string]any{"path": path, "zone": "UTC", "dry_run": true, "expected_digest": canon.FileDigest(data)}
	ok(t, h.do("import-legacy", body))
	body["expected_digest"] = canon.FileDigest([]byte("another file"))
	resp := h.do("import-legacy", body)
	if resp.Status != envelope.Conflict || resp.Error == nil {
		t.Fatalf("status %s, error %+v", resp.Status, resp.Error)
	}
	unchanged(t, path, data, when)
}

// A file that cannot be read, and a zone that is not one, end the request.
func TestImportRefusesWhatItCannotRead(t *testing.T) {
	h := newHome(t)
	resp := h.do("import-legacy", map[string]any{"path": filepath.Join(t.TempDir(), "absent.md"), "zone": "UTC", "dry_run": true})
	refused(t, resp, envelope.Unavailable, "source_unreadable")
	path, _, _ := source(t, "backlog.md")
	resp = h.do("import-legacy", map[string]any{"path": path, "zone": "Not/AZone", "dry_run": true})
	refused(t, resp, envelope.Invalid, "schema_violation")
}

// The live import is a later lane's: with dry_run false the request is refused
// as an operation the build has not reached, and nothing is written, in the
// home or at the source.
func TestImportThatWritesIsUnsupported(t *testing.T) {
	h := newHome(t)
	path, data, when := source(t, "backlog.md")
	before := snapshot(t, h.dir)
	resp := h.do("import-legacy", map[string]any{"path": path, "zone": "UTC", "dry_run": false})
	refused(t, resp, envelope.Refused, envelope.CodeUnsupportedOperation)
	if len(resp.Receipts) != 0 {
		t.Errorf("receipts %v", resp.Receipts)
	}
	if after := snapshot(t, h.dir); !equal(before, after) {
		t.Errorf("the home changed: before %v, after %v", before, after)
	}
	unchanged(t, path, data, when)
}

// A home inside a repository is refused before the file is read.
func TestImportIntoAHomeInsideARepositoryIsRefused(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "repo"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "repo", ".git"), []byte("gitdir: elsewhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &home{t: t, dir: filepath.Join(root, "repo", "home"), homeID: mint(t, ids.Home)}
	writeHome(t, h.dir, h.homeID)
	h.exe = entry.Executable()
	h.exe.Clock = clock.NewFake(start)
	path, data, when := source(t, "backlog.md")
	for _, dry := range []bool{true, false} {
		before := snapshot(t, h.dir)
		resp := h.do("import-legacy", map[string]any{"path": path, "zone": "UTC", "dry_run": dry})
		refused(t, resp, envelope.Refused, envelope.CodeHomeInsideRepository)
		if after := snapshot(t, h.dir); !equal(before, after) {
			t.Errorf("dry_run %v: the home changed", dry)
		}
		unchanged(t, path, data, when)
	}
}
