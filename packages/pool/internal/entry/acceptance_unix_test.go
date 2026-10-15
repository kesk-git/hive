//go:build unix

package entry

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/store"
	poolstore "github.com/kesk-git/hive/packages/pool/internal/store"
)

// within runs f and fails the test if it has not returned in time, as a
// request that opened a FIFO would not.
func within(t testing.TB, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the request is held by what stands at a record path")
	}
}

// A FIFO at a record path is never opened: a read by identity and a list name
// it in omitted as record_malformed and are incomplete, and a write that
// expects a revision of it fails as record_malformed with no intent written.
func TestAFIFOAtARecordPathIsMalformedAndNeverOpened(t *testing.T) {
	p := newPool(t)
	id, body := p.hold()
	path := filepath.Join(p.dir, filepath.FromSlash(holdPath(id)))
	if err := store.MakeDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	within(t, func() {
		for name, selector := range map[string]map[string]any{"by identity": {"id": id}, "a list": {"active": true}} {
			resp := p.do("read", map[string]any{"kind": "hold", "selector": selector})
			if resp.Status != envelope.Ok {
				t.Errorf("%s: %s %+v", name, resp.Status, resp.Error)
				continue
			}
			header, _ := resp.Body["projection"].(map[string]any)
			omitted, _ := header["omitted"].([]any)
			records, _ := resp.Body["records"].([]any)
			if header["complete"] != false || len(records) != 0 || len(omitted) != 1 ||
				omitted[0].(map[string]any)["id"] != id || omitted[0].(map[string]any)["reason"] != poolstore.ReasonMalformed {
				t.Errorf("%s: header %v, records %v", name, header, records)
			}
		}
		resp := p.write(poolstore.Transaction{Records: []poolstore.Write{{Kind: "hold", ID: id, Body: body, Expected: 1}}})
		if resp.Status == envelope.Ok || resp.Error == nil || resp.Error.Code != envelope.CodeRecordMalformed {
			t.Errorf("a write over it: %s %+v", resp.Status, resp.Error)
		}
	})
	if files := p.files("pool/tx"); len(files) != 0 {
		t.Errorf("the refused write left an intent: %v", files)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Errorf("the FIFO is not as it was: %v, %v", info, err)
	}
}
