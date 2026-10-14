package legacy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The acceptance test of the legacy grammar (spec/pool.md, Legacy import and
// export): a backlog whose lines end in CRLF and whose last line has no line
// end parses and renders back byte for byte, and the import record names the
// translation with the licence it keeps.

// TestRoundTripKeepsCRLFAndNoFinalNewline parses the fixture and renders it
// again; the bytes must be the ones read, on every platform.
func TestRoundTripKeepsCRLFAndNoFinalNewline(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "testdata", "legacy", "crlf-no-final-newline.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(src, []byte("\r\n")) || bytes.HasSuffix(src, []byte("\n")) {
		t.Fatal("the fixture must hold CRLF line ends and no final newline")
	}
	b, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Render(); !bytes.Equal(got, src) {
		t.Fatalf("the rendering differs from the source\n got %q\nwant %q", got, src)
	}
}

// TestImportRecordNamesTheTranslation reads the record of the import and the
// licence text it keeps.
func TestImportRecordNamesTheTranslation(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	data, err := os.ReadFile(filepath.Join(root, "imports", "tasks-axi.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		ImportID string `json:"import_id"`
		Kind     string `json:"kind"`
		Owner    string `json:"owner"`
		Upstream struct {
			Revision string `json:"revision"`
			Tag      string `json:"tag"`
		} `json:"upstream"`
		Licence struct {
			SPDX      string   `json:"spdx"`
			Copyright []string `json:"copyright"`
			Files     []struct {
				Path   string `json:"path"`
				Digest string `json:"digest"`
				Bytes  int    `json:"bytes"`
			} `json:"files"`
		} `json:"licence"`
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.ImportID != "tasks-axi" || rec.Kind != "translation" || rec.Owner != "pool" {
		t.Fatalf("the record is %q, %q, %q; want tasks-axi, translation, pool", rec.ImportID, rec.Kind, rec.Owner)
	}
	if rec.Upstream.Revision != "9401ff899c0d1d8ae6b4fd8727b9025abda2032c" || rec.Upstream.Tag != "tasks-axi-v0.2.6" {
		t.Fatalf("the record names upstream %q at %q", rec.Upstream.Revision, rec.Upstream.Tag)
	}
	if rec.Licence.SPDX != "MIT" || len(rec.Licence.Copyright) != 1 || rec.Licence.Copyright[0] != "Copyright (c) 2026 Kun Chen" {
		t.Fatalf("the record's licence is %q with notices %q", rec.Licence.SPDX, rec.Licence.Copyright)
	}
	if len(rec.Licence.Files) != 1 || rec.Licence.Files[0].Path != "packages/pool/upstream/tasks-axi/LICENSE" {
		t.Fatalf("the record keeps %+v", rec.Licence.Files)
	}
	text, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rec.Licence.Files[0].Path)))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(text)
	if len(text) != 1065 || rec.Licence.Files[0].Bytes != 1065 ||
		rec.Licence.Files[0].Digest != "sha256:"+hex.EncodeToString(sum[:]) ||
		rec.Licence.Files[0].Digest != "sha256:945016bd37e1ba7211622ef60ee1d23ab727896ba7710edd21e8fbe983863969" {
		t.Fatalf("the kept licence is %d bytes with digest %x", len(text), sum)
	}
}
