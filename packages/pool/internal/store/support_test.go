package store_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/flushtest"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/journal"
	cstore "github.com/kesk-git/hive/contract/store"
	"github.com/kesk-git/hive/packages/pool/internal/entry"

	// Every writer of the Pool registers its operations into the default
	// registry from its init function; the executable under test is the
	// delivered one.
	_ "github.com/kesk-git/hive/packages/pool/internal/admission"
	_ "github.com/kesk-git/hive/packages/pool/internal/decision"
	_ "github.com/kesk-git/hive/packages/pool/internal/imperative"
	_ "github.com/kesk-git/hive/packages/pool/internal/item"
	_ "github.com/kesk-git/hive/packages/pool/internal/legacy"
	_ "github.com/kesk-git/hive/packages/pool/internal/memory"
	_ "github.com/kesk-git/hive/packages/pool/internal/rule"
	_ "github.com/kesk-git/hive/packages/pool/internal/rulewrite"
	_ "github.com/kesk-git/hive/packages/pool/internal/specification"
)

// What test-pool proves of the Pool's store (spec/pool.md, What test-pool
// proves; spec/records.md, Writing several records): every multi-record
// transaction of every writer rolled forward from its intent, once, from a
// crash at each step the store names; two writers under the lock with no lost
// update; a record an operation needs that is malformed stopping it with
// nothing written; and a replay returning the recorded outcome with events
// and receipts that agree with it. This file is the harness the cases share:
// a home, the request that drives a writer, the executable the registry
// builds with the store's fault seam, and the readers of what a run left.

// The identities the record fixtures of contract/testdata share, so that the
// fixtures seed a home as they stand.
const (
	homeID      = "home_01jc7d8e9f0g1h2j3k4m5n6p7a"
	workspaceID = "ws_01jc7d8e9f0g1h2j3k4m5n6p7b"
	projectID   = "proj_01jc7d8e9f0g1h2j3k4m5n6p7c"
)

// start is the instant every home of a case begins at; the clock of a home
// moves only when a seed moves it.
var start = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

// The scopes a request is made at.
var (
	hiveScope    = map[string]any{"scope_level": "hive", "home_id": homeID}
	projectScope = map[string]any{"scope_level": "project", "home_id": homeID, "workspace_id": workspaceID, "project_id": projectID}
)

// scratch is the directory every home of the run is made under.
var scratch string

func TestMain(m *testing.M) {
	// The proof observes the steps the store reports, all of which stay; the
	// real flush to the medium is skipped.
	flushtest.Skip()
	dir, err := os.MkdirTemp("", "pool-store-proof-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	scratch = dir
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// seedError is the panic of a seed that cannot be set up; the case that ran it
// reports it as its own failure.
type seedError struct{ err error }

func (e seedError) Error() string { return e.err.Error() }

func must(err error) {
	if err != nil {
		panic(seedError{err})
	}
}

func mustf(format string, args ...any) { panic(seedError{fmt.Errorf(format, args...)}) }

var (
	dirMu  sync.Mutex
	dirSeq int
)

// newDir makes an empty directory of the run.
func newDir() string {
	dirMu.Lock()
	defer dirMu.Unlock()
	dirSeq++
	dir := filepath.Join(scratch, fmt.Sprintf("d%05d", dirSeq))
	must(os.MkdirAll(dir, 0o700))
	return dir
}

func mint(p ids.Prefix) string {
	id, err := ids.Mint(clock.System{}, p)
	must(err)
	return id
}

// home is a home with the Pool's executable over it, as a case seeds it.
type home struct {
	dir string
	clk *clock.Fake
	exe *cli.Executable
}

// newHome makes a home with the identity records the fixtures name: the home,
// one workspace and one project not bound to a repository.
func newHome() *home {
	h := &home{dir: filepath.Join(newDir(), "home"), clk: clock.NewFake(start)}
	must(cstore.MakeDir(h.dir))
	must(cstore.SecureDir(h.dir))
	h.write("home.json", map[string]any{
		"schema": "hive.home", "schema_version": "1.0", "record_revision": int64(1),
		"home_id": homeID, "role": "hive", "name": "A test home", "parent": map[string]any{"kind": "none"},
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z",
		"writer": map[string]any{"package": "pool", "command": "hive-pool", "operation": "provision", "implementation_version": "test"},
	})
	registry := func(extra map[string]any) map[string]any {
		out := map[string]any{"schema_version": "1.0", "record_revision": int64(1), "home_id": homeID, "state": "active",
			"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z", "slug": "registered", "name": "Registered",
			"writer": map[string]any{"package": "dispatch", "command": "hive-fleet", "operation": "provision", "implementation_version": "test"}}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	h.write("dispatch/registry/workspaces/"+workspaceID+".json", registry(map[string]any{
		"schema": "hive.workspace", "workspace_id": workspaceID, "parent_home_id": mint(ids.Home)}))
	h.write("dispatch/registry/projects/"+projectID+".json", registry(map[string]any{
		"schema": "hive.project", "project_id": projectID, "workspace_id": workspaceID, "repository": map[string]any{"kind": "local-only"}}))
	h.exe = executable(h.clk)
	return h
}

// write writes a file of the home as the canonical form of the record.
func (h *home) write(rel string, rec map[string]any) {
	data, err := canon.Encode(rec)
	must(err)
	path := filepath.Join(h.dir, filepath.FromSlash(rel))
	putFile(path, append(data, '\n'))
}

// putFile puts the file at path in a home with the content given, through the
// store's own primitives, so that the directories above it and the file are
// owner-only and owned by the user, as Hive makes them. A file that is there is
// emptied first.
func putFile(path string, data []byte) {
	must(cstore.MakeDir(filepath.Dir(path)))
	f, err := cstore.CreateFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	must(err)
	if _, err := f.Write(data); err != nil {
		f.Close()
		must(err)
	}
	must(f.Close())
}

// plant stands a record in the Pool as the writers of other lanes would.
func (h *home) plant(kind, id string, rec map[string]any) {
	h.write("pool/records/"+kind+"/"+id+".json", rec)
}

// stored reads a record file of the home.
func (h *home) stored(rel string) map[string]any {
	data, err := os.ReadFile(filepath.Join(h.dir, filepath.FromSlash(rel)))
	must(err)
	v, err := canon.Parse(data)
	must(err)
	obj, ok := v.(map[string]any)
	if !ok {
		mustf("%s is not an object", rel)
	}
	return obj
}

// fixture reads a valid record fixture of the contract's schemas.
func fixture(schema, name string) map[string]any {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contract", "testdata", "schemas", "records_"+schema, "valid", name+".json"))
	must(err)
	v, err := canon.Parse(data)
	must(err)
	return v.(map[string]any)
}

// executable is the delivered executable of the Pool over the clock.
func executable(clk clock.Clock) *cli.Executable {
	exe := entry.Executable()
	exe.Clock = clk
	return exe
}

// rev is the revision of a record a caller read, as a request names it.
type rev struct {
	schema, id string
	revision   int64
}

// req is a request to a writer: the command, the operation, the body, and
// what the envelope carries besides.
type req struct {
	command, op string
	version     string
	body        map[string]any
	scope       map[string]any
	authority   map[string]any
	expected    []rev
	caller      map[string]any
	causation   map[string]any
	// handoff makes the request the way hive-word hands one off: its caller
	// is hive-word and its causation is the request itself.
	handoff     bool
	id          string
	correlation string
	// edits are the words of the body edit the request prepares that no other
	// writer's request carries.
	edits []string
}

// bytes is the request as the command reads it. The request_id and the
// correlation_id are minted once and kept, so that the same request can be
// sent again.
func (r *req) bytes() []byte {
	if r.id == "" {
		r.id = mint(ids.Request)
	}
	if r.correlation == "" {
		r.correlation = mint(ids.Correlation)
	}
	version, scope, authority, caller := r.version, r.scope, r.authority, r.caller
	if version == "" {
		version = "1.0"
	}
	if scope == nil {
		scope = hiveScope
	}
	if authority == nil {
		authority = map[string]any{"kind": "none"}
	}
	if caller == nil {
		caller = map[string]any{"kind": "test"}
	}
	causation := r.causation
	if r.handoff {
		caller = map[string]any{"kind": "package", "package": "word", "command": "hive-word"}
		causation = map[string]any{"schema": "hive.request", "id": r.id, "digest": canon.FileDigest([]byte(r.id))}
	}
	doc := map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": r.command, "operation": r.op, "operation_version": version,
		"request_id": r.id, "correlation_id": r.correlation,
		"issued_at": "2026-10-14T09:00:00.000Z", "caller": caller,
		"authority": authority, "body": r.body,
	}
	for k, v := range scope {
		doc[k] = v
	}
	if causation != nil {
		doc["causation"] = causation
	}
	if len(r.expected) > 0 {
		list := []any{}
		for _, e := range r.expected {
			list = append(list, map[string]any{"schema": e.schema, "id": e.id, "record_revision": e.revision})
		}
		doc["expected_revisions"] = list
	}
	data, err := canon.Encode(doc)
	must(err)
	return append(data, '\n')
}

// again is the request with a request of its own: the same words from another
// caller, as a second writer makes them.
func (r *req) again() *req {
	c := *r
	c.id, c.correlation = "", ""
	return &c
}

// send hands the request bytes to the command of the executable over the home
// and returns the response as the command wrote it.
func send(exe *cli.Executable, dir, command, op string, data []byte) (*envelope.Response, error) {
	var cmd *cli.Command
	for _, c := range exe.Commands {
		if c.Name == command {
			cmd = c
		}
	}
	if cmd == nil {
		return nil, fmt.Errorf("the executable has no command %s", command)
	}
	out, err := exe.Handle(cli.Invocation{Command: cmd, Operation: op, Home: dir, Request: data})
	if err != nil {
		return nil, err
	}
	return envelope.DecodeResponse(out.Encoded)
}

// do sends the request over the home and fails the seed unless the response
// is ok.
func (h *home) do(r *req) *envelope.Response {
	resp, err := send(h.exe, h.dir, r.command, r.op, r.bytes())
	must(err)
	if resp.Status != envelope.Ok {
		mustf("seed %s %s: status %s, error %+v", r.command, r.op, resp.Status, resp.Error)
	}
	return resp
}

// writeFile replaces a file of the home, through the store's own primitives, so
// that it keeps the owner and the access of the file it replaces.
func writeFile(dir, rel string, data []byte) {
	putFile(filepath.Join(dir, filepath.FromSlash(rel)), data)
}

// clone copies a home to a directory of its own, which the test removes when
// it ends; a seed, which has no test, keeps its copy until the run ends.
func clone(t testing.TB, src string) string {
	dst := filepath.Join(newDir(), "home")
	if t != nil {
		t.Cleanup(func() { os.RemoveAll(filepath.Dir(dst)) })
	}
	must(cstore.MakeDir(dst))
	must(cstore.SecureDir(dst))
	must(filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return cstore.MakeDir(target)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		putFile(target, data)
		return nil
	}))
	return dst
}

// files is every file under the directory of the home, by slash path, with
// its bytes. The lock files are not part of what a request writes.
func files(dir string) map[string][]byte {
	out := map[string][]byte{}
	must(filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "locks/") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = data
		return nil
	}))
	return out
}

// poolFiles is the files of the Pool's store that a request leaves: everything
// under pool/ but its temporary files, which the next writer empties.
func poolFiles(all map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for k, v := range all {
		if strings.HasPrefix(k, "pool/") && !strings.HasPrefix(k, "pool/tmp/") {
			out[k] = v
		}
	}
	return out
}

// same reports the paths at which two sets of files differ.
func differences(a, b map[string][]byte) []string {
	var out []string
	for k, v := range a {
		if w, ok := b[k]; !ok {
			out = append(out, "-"+k)
		} else if string(v) != string(w) {
			out = append(out, "~"+k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			out = append(out, "+"+k)
		}
	}
	sort.Strings(out)
	return out
}

// event is one line of the home's journal.
type event struct {
	ID    string
	Kind  string
	Basis map[string]any
}

// journalEvents reads the events of the home's journal, in order.
func journalEvents(dir string) ([]event, error) {
	j, err := journal.Open(dir, journal.Options{})
	if err != nil {
		return nil, err
	}
	r, err := j.Reader(0)
	if err != nil {
		return nil, err
	}
	var out []event
	for {
		e, err := r.Next()
		if err != nil {
			return nil, err
		}
		if e == nil {
			return out, nil
		}
		basis, _ := e.Event["basis"].(map[string]any)
		out = append(out, event{e.EventID, e.Kind, basis})
	}
}

// diagnostic is the kind of the event a failed invocation may leave beside its
// transaction: a detail for a person, not a write of the Pool's records.
const diagnostic = "pool.diagnostic.recorded"

// mustEvents is journalEvents for a case that has no use for its error.
func mustEvents(dir string) []event {
	e, err := journalEvents(dir)
	must(err)
	return e
}

// poolEvents are the events of the Pool's kinds among them, but for the
// diagnostics of failed invocations.
func poolEvents(all []event) []event {
	var out []event
	for _, e := range all {
		if strings.HasPrefix(e.Kind, "pool.") && e.Kind != diagnostic {
			out = append(out, e)
		}
	}
	return out
}

// intentFile is a file an intent names.
type intentFile struct {
	Path     string
	Digest   string
	Expected int64 // the revision a record is replaced at, 0 for one created
}

// intent is the transaction a crash left in tx/.
type intent struct {
	Request  string
	Records  []intentFile
	Receipts []intentFile
	Events   []string
	Outcome  *intentFile
}

// readIntent reads the intent of the request in the files, nil where there is
// none.
func readIntent(all map[string][]byte, request string) (*intent, error) {
	data, ok := all[cstore.IntentPath("pool", request)]
	if !ok {
		return nil, nil
	}
	v, err := canon.Parse(data)
	if err != nil {
		return nil, err
	}
	doc, _ := v.(map[string]any)
	file := func(v any) intentFile {
		m, _ := v.(map[string]any)
		f := intentFile{}
		f.Path, _ = m["path"].(string)
		f.Digest, _ = m["digest"].(string)
		if m["expected"] == "revision" {
			f.Expected, _ = m["expected_revision"].(int64)
		}
		return f
	}
	in := &intent{Request: request}
	for _, v := range list(doc["records"]) {
		in.Records = append(in.Records, file(v))
	}
	for _, v := range list(doc["receipts"]) {
		in.Receipts = append(in.Receipts, file(v))
	}
	for _, v := range list(doc["events"]) {
		m, _ := v.(map[string]any)
		id, _ := m["event_id"].(string)
		in.Events = append(in.Events, id)
	}
	if raw, ok := doc["outcome"]; ok {
		f := file(raw)
		in.Outcome = &f
	}
	return in, nil
}

func list(v any) []any { l, _ := v.([]any); return l }

// revisionOf is the record_revision of the record file, 0 where it is not one.
func revisionOf(data []byte) int64 {
	v, err := canon.Parse(data)
	if err != nil {
		return 0
	}
	obj, _ := v.(map[string]any)
	n, _ := obj["record_revision"].(int64)
	return n
}

// recorded is the response a request's recorded outcome holds.
func recorded(all map[string][]byte, request string) (*envelope.Response, error) {
	data, ok := all[cstore.OutcomePath("pool", request)]
	if !ok {
		return nil, fs.ErrNotExist
	}
	v, err := canon.Parse(data)
	if err != nil {
		return nil, err
	}
	doc, _ := v.(map[string]any)
	tree, _ := doc["response"].(map[string]any)
	enc, err := canon.Encode(tree)
	if err != nil {
		return nil, err
	}
	return envelope.DecodeResponse(append(enc, '\n'))
}

// errCrash is the interruption the seam delivers: a process that stopped.
var errCrash = errors.New("crash")

// run is one execution of a request over a home.
type run struct {
	resp  *envelope.Response
	steps []string // the steps the store named, in order, until it stopped
}

// execute sends the request bytes over the dir with a fresh executable on the
// clock. fault, when it is not nil, is the store's fault seam; waiting is how
// the executable waits for the lock.
func execute(dir string, now time.Time, r *req, data []byte, fault func(step string) error, waiting cstore.Waiting) (*run, error) {
	exe := executable(clock.NewFake(now))
	out := &run{}
	exe.Waiting = waiting
	exe.Fault = func(step string) error {
		out.steps = append(out.steps, step)
		if fault != nil {
			return fault(step)
		}
		return nil
	}
	resp, err := send(exe, dir, r.command, r.op, data)
	if err != nil {
		return nil, err
	}
	out.resp = resp
	return out, nil
}

// crashAt runs the request and stops the store at the n-th step it names, as a
// process that died there would.
func crashAt(dir string, now time.Time, r *req, data []byte, n int) (*run, error) {
	count := 0
	return execute(dir, now, r, data, func(string) error {
		if count++; count-1 == n {
			return errCrash
		}
		return nil
	}, cstore.Waiting{})
}

// recoverByLock is the next command to take the store's lock that is not the
// request itself: it completes what an intent says and releases the lock.
func recoverByLock(dir string, now time.Time) error {
	jr, err := journal.Opener("pool")(dir)
	if err != nil {
		return err
	}
	st, err := cstore.Open(dir, "pool", cstore.Options{Clock: clock.NewFake(now), Journal: jr})
	if err != nil {
		return err
	}
	w, err := st.Lock()
	if err != nil {
		return err
	}
	return w.Release()
}

// shape is what a request did to a home, in terms that do not depend on the
// identities the request minted: the revision of every record that was there
// and is not as it was, the files added in each directory and the events
// added of each kind.
type shape struct {
	Revisions map[string]int64
	Added     map[string]int
	Events    map[string]int
}

func shapeOf(before, after map[string][]byte, evBefore, evAfter []event) shape {
	s := shape{Revisions: map[string]int64{}, Added: map[string]int{}, Events: map[string]int{}}
	for path, data := range after {
		if !strings.HasPrefix(path, "pool/") || strings.HasPrefix(path, "pool/tmp/") {
			continue
		}
		old, existed := before[path]
		switch {
		case !existed:
			s.Added[filepath.ToSlash(filepath.Dir(path))]++
		case string(old) != string(data) && strings.HasPrefix(path, "pool/records/"):
			s.Revisions[path] = revisionOf(data)
		case string(old) != string(data):
			s.Revisions[path] = -1
		}
	}
	seen := map[string]bool{}
	for _, e := range evBefore {
		seen[e.ID] = true
	}
	for _, e := range poolEvents(evAfter) {
		if !seen[e.ID] {
			s.Events[e.Kind]++
		}
	}
	return s
}

func (s shape) String() string {
	return fmt.Sprintf("revisions %v, added %v, events %v", s.Revisions, s.Added, s.Events)
}

func sameShape(a, b shape) bool { return reflect.DeepEqual(a, b) }

// scenario is a writer of the Pool and the home that drives it: build seeds
// the home, with the Pool's own operations or with the fixtures the packages
// test with, and returns the request of the writer.
type scenario struct {
	name    string
	command string
	op      string
	// build seeds the home and returns the request, with the paths (slash,
	// from the home) of the records the operation needs, which a case may
	// make malformed.
	build func(h *home) (*req, []string)
	// stops names, for a record the operation needs, the code it stops with
	// where that is not record_malformed: a record that is the evidence of
	// an authority, which does not stand once it is malformed.
	stops map[string]string
	// stale, when the operation replaces a record, names the record the
	// request of a second writer expects; empty takes the first the
	// operation replaced.
	stale string
	// rival, when the request carries a body edit, changes a copy of its body
	// to another edit of the same record and returns the words that edit adds,
	// as a second caller prepares it from the revision the first one read.
	rival func(body map[string]any) []string
	// repeats says the contract answers the same request from another caller
	// with what the first one wrote, writing nothing of its own: a duplicate
	// answer (spec/word.md), a second expire (spec/pool.md) and a rendering
	// equal to the current one (spec/pool.md).
	repeats bool
}

// prepared is a scenario seeded once: the home as the request finds it, the
// request, and the run of the request over a copy of it with nothing
// interrupting.
type prepared struct {
	sc     *scenario
	before string
	now    time.Time
	req    *req
	data   []byte
	needs  []string

	clean      *run
	after      map[string][]byte
	beforeEv   []event
	afterEv    []event
	cleanShape shape
}

var (
	cacheMu sync.Mutex
	cache   = map[string]*cached{}
)

type cached struct {
	once sync.Once
	p    *prepared
	err  error
}

// prepare seeds the scenario and runs its request once, clean. The result is
// shared by every case of the writer; the homes it holds are never written, a
// case copies them.
func prepare(sc *scenario) (*prepared, error) {
	cacheMu.Lock()
	c := cache[sc.name]
	if c == nil {
		c = &cached{}
		cache[sc.name] = c
	}
	cacheMu.Unlock()
	c.once.Do(func() { c.p, c.err = seed(sc) })
	return c.p, c.err
}

func seed(sc *scenario) (p *prepared, err error) {
	defer func() {
		if r := recover(); r != nil {
			se, ok := r.(seedError)
			if !ok {
				panic(r)
			}
			p, err = nil, fmt.Errorf("the home of %s cannot be seeded: %w", sc.name, se.err)
		}
	}()
	h := newHome()
	r, needs := sc.build(h)
	p = &prepared{sc: sc, before: h.dir, now: h.clk.Now(), req: r, data: r.bytes(), needs: needs}
	dir := clone(nil, p.before)
	p.clean, err = execute(dir, p.now, r, p.data, nil, cstore.Waiting{})
	if err != nil {
		return nil, err
	}
	p.after = files(dir)
	if p.beforeEv, err = journalEvents(p.before); err != nil {
		return nil, err
	}
	if p.afterEv, err = journalEvents(dir); err != nil {
		return nil, err
	}
	p.cleanShape = shapeOf(files(p.before), p.after, p.beforeEv, p.afterEv)
	return p, nil
}

// replaced are the records of the home that the request replaced, in path
// order, each with the revision it was at.
func (p *prepared) replaced() []rev {
	before := files(p.before)
	var out []rev
	for path, data := range p.after {
		old, ok := before[path]
		kind, id, isRecord := recordPath(path)
		if !ok || !isRecord || string(old) == string(data) {
			continue
		}
		out = append(out, rev{"pool." + kind, id, revisionOf(old)})
	}
	slices.SortFunc(out, func(a, b rev) int { return strings.Compare(a.schema+a.id, b.schema+b.id) })
	return out
}

// recordPath splits pool/records/<kind>/<id>.json.
func recordPath(path string) (kind, id string, ok bool) {
	rest, found := strings.CutPrefix(path, "pool/records/")
	if !found {
		return "", "", false
	}
	kind, file, found := strings.Cut(rest, "/")
	if !found || !strings.HasSuffix(file, ".json") {
		return "", "", false
	}
	return kind, strings.TrimSuffix(file, ".json"), true
}

// durable reports whether the request's transaction is the store's: its intent
// or its outcome is on disk.
func durable(all map[string][]byte, request string) bool {
	_, intent := all[cstore.IntentPath("pool", request)]
	_, outcome := all[cstore.OutcomePath("pool", request)]
	return intent || outcome
}

// completeOnce checks that the transaction of the request is complete in the
// files exactly once: nothing of its intent or its temporary files is left;
// every record is at the revision its intent expected plus one and at the
// digest it names; every receipt and the outcome are there with their
// digests; and each event it names is in the journal once.
func completeOnce(all map[string][]byte, evAfter []event, in *intent) []string {
	var bad []string
	if _, ok := all[cstore.IntentPath("pool", in.Request)]; ok {
		bad = append(bad, "the intent is still there")
	}
	for path := range all {
		if strings.HasPrefix(path, "pool/tmp/") || strings.HasPrefix(path, "pool/tx/") {
			bad = append(bad, "left behind: "+path)
		}
	}
	digest := func(data []byte) string {
		d, err := canon.RecordDigest(data)
		if err != nil {
			return err.Error()
		}
		return d
	}
	for _, f := range in.Records {
		data, ok := all[f.Path]
		switch {
		case !ok:
			bad = append(bad, "record absent: "+f.Path)
		case digest(data) != f.Digest:
			bad = append(bad, "record not at the intended digest: "+f.Path)
		case revisionOf(data) != f.Expected+1:
			bad = append(bad, fmt.Sprintf("record %s at revision %d, not %d", f.Path, revisionOf(data), f.Expected+1))
		}
	}
	once := slices.Clone(in.Receipts)
	if in.Outcome != nil {
		once = append(once, *in.Outcome)
	}
	for _, f := range once {
		data, ok := all[f.Path]
		switch {
		case !ok:
			bad = append(bad, "absent: "+f.Path)
		case digest(data) != f.Digest:
			bad = append(bad, "not at the intended digest: "+f.Path)
		}
	}
	count := map[string]int{}
	for _, e := range evAfter {
		count[e.ID]++
	}
	for _, id := range in.Events {
		if count[id] != 1 {
			bad = append(bad, fmt.Sprintf("event %s is in the journal %d times", id, count[id]))
		}
	}
	for id, n := range count {
		if n > 1 {
			bad = append(bad, fmt.Sprintf("event %s is in the journal %d times", id, n))
		}
	}
	return bad
}

// agrees checks that the receipts and events of a response are on disk and
// consistent with it: every receipt the response returns is the receipt of the
// Pool's receipts file of its identity, whole, and every event of the Pool
// that is new names, as its basis, a receipt that is there.
func agrees(all map[string][]byte, resp *envelope.Response, newEvents []event) []string {
	var bad []string
	for _, r := range resp.Receipts {
		data, ok := all[cstore.ReceiptPath("pool", r.ReceiptID)]
		if !ok {
			bad = append(bad, "the response names receipt "+r.ReceiptID+", which is not on disk")
			continue
		}
		stored, err := envelope.DecodeReceipt(data)
		if err != nil {
			bad = append(bad, "receipt "+r.ReceiptID+" on disk does not decode: "+err.Error())
			continue
		}
		have, err1 := r.Encode()
		want, err2 := stored.Encode()
		if err1 != nil || err2 != nil {
			bad = append(bad, fmt.Sprintf("receipt %s does not encode: %v, %v", r.ReceiptID, err1, err2))
		} else if string(have) != string(want) {
			bad = append(bad, "the response returns receipt "+r.ReceiptID+" as "+string(have)+", and it is on disk as "+string(want))
		}
	}
	for _, e := range newEvents {
		ref, _ := e.Basis["ref"].(map[string]any)
		id, _ := ref["id"].(string)
		if kind, _ := e.Basis["kind"].(string); kind != "receipt" {
			bad = append(bad, fmt.Sprintf("event %s has the basis %q, not a receipt", e.ID, kind))
		} else if _, ok := all[cstore.ReceiptPath("pool", id)]; !ok {
			bad = append(bad, "event "+e.ID+" has the basis receipt "+id+", which is not on disk")
		}
	}
	return bad
}

// newEvents are the events of the Pool in after that were not in before.
func newEvents(before, after []event) []event {
	seen := map[string]bool{}
	for _, e := range before {
		seen[e.ID] = true
	}
	var out []event
	for _, e := range poolEvents(after) {
		if !seen[e.ID] {
			out = append(out, e)
		}
	}
	return out
}

// check fails the test with each problem.
func check(t *testing.T, what string, bad []string) {
	t.Helper()
	for _, b := range bad {
		t.Errorf("%s: %s", what, b)
	}
}

// The scenarios are the writers of the Pool: every mutating operation the
// entry registry registers, each with a home that holds what it finds and the
// request that drives it.

// rivalWords is the edit of a request that sets one field of its body to other
// words.
func rivalWords(field, words string) func(map[string]any) []string {
	return func(body map[string]any) []string {
		body[field] = words
		return []string{words}
	}
}

func poolReq(op string, body map[string]any, expected ...rev) *req {
	return &req{command: "hive-pool", op: op, body: body, expected: expected}
}

// atHome moves a fixture record into the home, at the hive scope.
func atHome(rec map[string]any) {
	rec["scope_level"], rec["home_id"] = "hive", homeID
	delete(rec, "workspace_id")
	delete(rec, "project_id")
	delete(rec, "clone_id")
}

func itemPath(id string) string       { return "pool/records/item/" + id + ".json" }
func holdPath(id string) string       { return "pool/records/hold/" + id + ".json" }
func kindPath(kind, id string) string { return "pool/records/" + kind + "/" + id + ".json" }

// addItem adds a queued item through the Pool's own add and returns its
// identity.
func (h *home) addItem(extra map[string]any) string {
	body := map[string]any{"title": "A leaf", "level": "prey", "deliverable": "swarm"}
	for k, v := range extra {
		body[k] = v
	}
	resp := h.do(poolReq("add", body))
	return resp.Body["item"].(map[string]any)["task_id"].(string)
}

// imperativeOf plants the Imperative of the item at its revision 1, with no
// criteria, for the place and item revision the item stands at.
func (h *home) imperativeOf(id string) {
	imp := fixture("pool.imperative", "project-swarm-claude-mac")
	atHome(imp)
	imp["task_id"] = id
	r := imp["revisions"].([]any)[0].(map[string]any)
	delete(r["task"].(map[string]any)["standard"].(map[string]any), "criteria")
	item := h.stored(itemPath(id))
	place, ok := item["placement_requested"]
	if !ok {
		place = "policy"
	}
	r["setup"].(map[string]any)["placement"] = map[string]any{"requested": place, "item_revision": item["record_revision"]}
	h.plant("imperative", id, imp)
}

// admitted plants an admission of the item, admitted, whose last check read
// the item and the Imperative as they stand.
func (h *home) admitted(id string) string {
	adm := fixture("pool.admission", "admitted-mac-project")
	atHome(adm)
	adm["admission_id"] = mint(ids.Admission)
	adm["task_id"] = id
	adm["imperative_revision"] = h.stored(kindPath("imperative", id))["current_revision"]
	checks := adm["checks"].([]any)
	check := checks[len(checks)-1].(map[string]any)
	check["item_revision"] = h.stored(itemPath(id))["record_revision"]
	check["imperative_revision"] = adm["imperative_revision"]
	h.plant("admission", adm["admission_id"].(string), adm)
	return adm["admission_id"].(string)
}

// scenarios are the writers, in the order of the registry's commands.
var scenarios = slices.Concat(itemScenarios, admissionScenarios, imperativeScenarios, specificationScenarios, ruleScenarios, memoryScenarios, maintenanceScenarios, decisionScenarios, legacyScenarios)

var itemScenarios = []*scenario{
	{name: "hive-pool add", command: "hive-pool", op: "add", build: func(h *home) (*req, []string) {
		existing := h.addItem(nil)
		return poolReq("add", map[string]any{"title": "Another leaf", "level": "prey", "deliverable": "swarm"}), []string{itemPath(existing)}
	}},
	{name: "hive-pool update", command: "hive-pool", op: "update", build: func(h *home) (*req, []string) {
		id := h.addItem(nil)
		return poolReq("update", map[string]any{"task_id": id, "patch": map[string]any{"title": "Renamed", "add_body_lines": []any{"one"}}}), []string{itemPath(id)}
	}, rival: func(body map[string]any) []string {
		patch := tree(body["patch"])
		patch["title"], patch["add_body_lines"] = "Renamed by the second caller", []any{"two"}
		return []string{"Renamed by the second caller", "two"}
	}},
	{name: "hive-pool transition", command: "hive-pool", op: "transition", build: func(h *home) (*req, []string) {
		id := h.addItem(nil)
		return poolReq("transition", map[string]any{"task_id": id, "to": "in-flight"}), []string{itemPath(id)}
	}},
	{name: "hive-pool hold", command: "hive-pool", op: "hold", build: func(h *home) (*req, []string) {
		id := h.addItem(nil)
		return poolReq("hold", map[string]any{"task_id": id, "kind": "parked", "reason": "Waiting for the vendor."}), []string{itemPath(id)}
	}},
	{name: "hive-pool release", command: "hive-pool", op: "release", build: func(h *home) (*req, []string) {
		id := h.addItem(nil)
		held := h.do(poolReq("hold", map[string]any{"task_id": id, "kind": "parked", "reason": "Waiting for the vendor."}))
		hold := held.Body["hold"].(map[string]any)["hold_id"].(string)
		return poolReq("release", map[string]any{"hold_id": hold}), []string{holdPath(hold)}
	}},
	{name: "hive-pool open-generation", command: "hive-pool", op: "open-generation", build: func(h *home) (*req, []string) {
		id := h.addItem(map[string]any{"placement_requested": "mac"})
		h.imperativeOf(id)
		adm := h.admitted(id)
		return poolReq("open-generation", map[string]any{"task_id": id, "admission_id": adm}), []string{itemPath(id), kindPath("imperative", id), kindPath("admission", adm)}
	}},
	{name: "hive-pool close-generation", command: "hive-pool", op: "close-generation", build: func(h *home) (*req, []string) {
		id := h.addItem(map[string]any{"placement_requested": "mac"})
		h.imperativeOf(id)
		adm := h.admitted(id)
		h.do(poolReq("open-generation", map[string]any{"task_id": id, "admission_id": adm}))
		return poolReq("close-generation", map[string]any{"task_id": id, "generation": int64(1), "outcome": "failed",
			"closed_by": map[string]any{"schema": "hive.receipt", "id": mint(ids.Receipt)}}), []string{itemPath(id)}
	}},
}

// The identities of the item and the admission the fixtures share.
const (
	taskID      = "task_01jc7d8e9f0g1h2j3k4m5n6p7f"
	admissionID = "adm_01jc7d8e9f0g1h2j3k4m5n6p7h"
	questionID  = "q_01jc7d8e9f0g1h2j3k4m5n6p7q"
)

// github is the repository binding of a project a Harvest can be published for.
var github = map[string]any{"kind": "github", "host": "github.com", "owner": "example-org", "name": "widget-service"}

func authority(kind, schema, id string, revision int64) map[string]any {
	return map[string]any{"kind": kind, "ref": map[string]any{"schema": schema, "id": id, "home_id": homeID, "record_revision": revision}}
}

// word is the authority of kind word that names the question at a revision.
func word(revision int64) map[string]any {
	return authority("word", "pool.question", questionID, revision)
}

func copyOf(v any) any {
	data, err := canon.Encode(map[string]any{"v": v})
	must(err)
	out, err := canon.Parse(data)
	must(err)
	return out.(map[string]any)["v"]
}

func tree(v any) map[string]any { m, _ := v.(map[string]any); return m }

// bindForge binds the project of the home to a repository a Harvest can be
// published for.
func (h *home) bindForge() {
	rel := "dispatch/registry/projects/" + projectID + ".json"
	rec := h.stored(rel)
	rec["repository"] = github
	h.write(rel, rec)
}

// work plants a queued Hunt at the project that needs an accepted
// specification and has one, requested on the operator's machine, with an
// Imperative that agrees with it in everything and a guess in nothing, and the
// project bound to a forge.
func (h *home) work() {
	h.bindForge()
	item := fixture("pool.item", "hunt-in-flight-with-open-generation")
	item["state"], item["generations"], item["deps"] = "queued", []any{}, []any{}
	delete(item, "schedule")
	imp := fixture("pool.imperative", "project-swarm-claude-mac")
	r := imp["revisions"].([]any)[0].(map[string]any)
	tree(r["setup"])["placement"] = map[string]any{"requested": "mac", "item_revision": item["record_revision"]}
	tree(tree(r["task"])["standard"])["criteria"] = copyOf(item["specification"])
	h.plant("item", taskID, item)
	h.plant("imperative", taskID, imp)
}

func adm(op string, body map[string]any, r *req) *req {
	r.command, r.op, r.body, r.scope = "hive-admit", op, body, projectScope
	return r
}

var admissionScenarios = []*scenario{
	{name: "hive-admit create", command: "hive-admit", op: "create", build: func(h *home) (*req, []string) {
		h.work()
		return adm("create", map[string]any{"task_id": taskID, "imperative_revision": int64(1), "words": "Run the export retry fix now."}, &req{}),
			[]string{itemPath(taskID), kindPath("imperative", taskID)}
	}},
	{name: "hive-admit check", command: "hive-admit", op: "check", build: func(h *home) (*req, []string) {
		h.work()
		created := h.do(adm("create", map[string]any{"task_id": taskID, "imperative_revision": int64(1), "words": "Run the export retry fix now."}, &req{}))
		id := created.Body["admission"].(map[string]any)["admission_id"].(string)
		return adm("check", map[string]any{"admission_id": id, "placement": map[string]any{"kind": "requested", "value": "mac"}}, &req{}),
			[]string{kindPath("admission", id), itemPath(taskID), kindPath("imperative", taskID)}
	}},
	{name: "hive-admit approve", command: "hive-admit", op: "approve", build: func(h *home) (*req, []string) {
		h.plant("admission", admissionID, fixture("pool.admission", "created-no-checks"))
		h.plant("question", questionID, fixture("pool.question", "answered-superseding"))
		return adm("approve", map[string]any{"admission_id": admissionID}, &req{authority: word(3)}),
			[]string{kindPath("admission", admissionID), kindPath("question", questionID)}
	}, stops: map[string]string{kindPath("question", questionID): envelope.CodeAuthorityInsufficient}},
}

// The instants the rules and the clock of the cases are dated by.
const (
	d01 = "2026-10-01T00:00:00.000Z"
	d02 = "2026-10-02T00:00:00.000Z"
	d15 = "2026-10-15T00:00:00.000Z"
	d20 = "2026-10-20T00:00:00.000Z"
)

func writerOf(op string) map[string]any {
	return map[string]any{"package": "pool", "command": "hive-pool", "operation": op, "implementation_version": "test"}
}

// mem is a request to hive-memory at the operation's declared version.
func mem(op string, body map[string]any, r *req) *req {
	r.command, r.op, r.body = "hive-memory", op, body
	r.version = map[string]string{"render": "2.0", "check": "2.0", "propagate": "2.0", "budget": "1.0"}[op]
	return r
}

// rule is a request to hive-rule at the operation's declared version.
func ruleReq(op string, body map[string]any, r *req) *req {
	r.command, r.op, r.body = "hive-rule", op, body
	r.version = map[string]string{"record": "2.0"}[op]
	return r
}

// plantBudget stands the budget configuration in the home, at revision 1 with
// the entries given, and the question that is the Hive Mind's word.
func (h *home) plantBudget(entries ...map[string]any) {
	rec := fixture("pool.budget", "changed")
	rec["home_id"], rec["record_revision"] = homeID, int64(1)
	list := []any{}
	for _, e := range entries {
		list = append(list, e)
	}
	rec["projections"] = list
	h.plant("budget", "memory", rec)
	q := fixture("pool.question", "answered-superseding")
	q["scope_level"] = "hive"
	delete(q, "workspace_id")
	delete(q, "project_id")
	h.plant("question", questionID, q)
}

func factList(texts []string) []any {
	provenance := fixture("pool.subject", "class-with-fact")["facts"].([]any)[0].(map[string]any)["provenance"]
	list := []any{}
	for i, text := range texts {
		list = append(list, map[string]any{"fact_id": "fact-" + string(rune('a'+i)), "text": text, "provenance": provenance})
	}
	return list
}

// subject plants a subject at the scope and returns its identity.
func (h *home) subject(scope map[string]any, key string, facts ...string) string {
	id := "subj_" + strings.TrimPrefix(mint(ids.Task), "task_")
	rec := map[string]any{
		"schema": "pool.subject", "schema_version": "1.0", "record_revision": int64(1),
		"subject_id": id, "home_id": homeID, "key": key,
		"loads_at": []any{"dispatch"}, "facts": factList(facts),
		"created_at": "2026-10-11T08:00:00.000Z", "updated_at": "2026-10-11T08:00:00.000Z", "writer": writerOf("record"),
	}
	for k, v := range scope {
		rec[k] = v
	}
	h.plant("subject", id, rec)
	return id
}

// rule plants an operating rule of the tier, filed under the subject, in force
// since the first, that applies to every role, and returns its identity.
func (h *home) rule(tier, filedUnder string, scope map[string]any, over map[string]any) string {
	id := mint(ids.Rule)
	rec := map[string]any{
		"schema": "pool.rule", "schema_version": "2.0", "record_revision": int64(1),
		"rule_id": id, "home_id": homeID, "subject": "key." + strings.ToLower(id[len(id)-6:]),
		"words": "The Hive Mind's words.", "origin": map[string]any{"kind": "ruling"},
		"applies": []any{}, "status": "in-force", "effective_at": d01,
		"authority": map[string]any{"kind": "none"}, "amendments": []any{},
		"created_at": d01, "updated_at": d01, "writer": writerOf("record"),
		"kind": "operating", "tier": tier, "filed_under": filedUnder, "rule_line": "Line of " + id,
	}
	for k, v := range scope {
		rec[k] = v
	}
	for k, v := range over {
		rec[k] = v
	}
	h.plant("rule", id, rec)
	return id
}

// seedRules is a home with the budget configuration and the subject the rules
// of the Hive are filed under.
func (h *home) seedRules() string {
	h.plantBudget()
	return h.subject(hiveScope, "filing")
}

// opRule is the body of an operating rule of the tier, filed under the subject.
func opRule(subject, tier, filedUnder, line string) map[string]any {
	return map[string]any{
		"subject": subject, "words": "The Hive Mind's words, kept whole.", "origin": map[string]any{"kind": "ruling"},
		"applies": []any{}, "kind": "operating", "tier": tier, "filed_under": filedUnder, "rule_line": line,
	}
}

// recorded records a rule through hive-rule and returns it.
func (h *home) recorded(body map[string]any) map[string]any {
	return h.do(ruleReq("record", body, &req{})).Body["rule"].(map[string]any)
}

func revOf(schema, id string, rec map[string]any) rev {
	return rev{schema, id, rec["record_revision"].(int64)}
}

var ruleScenarios = []*scenario{
	{name: "hive-rule record", command: "hive-rule", op: "record", build: func(h *home) (*req, []string) {
		filing := h.seedRules()
		return ruleReq("record", opRule("dispatch.cap", "always", filing, "Dispatch at most four Swarms."), &req{}),
			[]string{kindPath("subject", filing), kindPath("budget", "memory")}
	}},
	{name: "hive-rule amend", command: "hive-rule", op: "amend", rival: rivalWords("words", "Extended by the second caller."), build: func(h *home) (*req, []string) {
		filing := h.seedRules()
		body := opRule("dispatch.cap", "always", filing, "Dispatch at most four Swarms.")
		body["expires_at"] = d15
		rule := h.recorded(body)
		id := rule["rule_id"].(string)
		return ruleReq("amend", map[string]any{"rule_id": id, "kind": "extend", "words": "Extended by five days.", "new_expires_at": d20},
			&req{expected: []rev{revOf("pool.rule", id, rule)}}), []string{kindPath("rule", id), kindPath("subject", filing)}
	}},
	{name: "hive-rule amend (lift)", command: "hive-rule", op: "amend", rival: rivalWords("words", "Lifted by the second caller."), build: func(h *home) (*req, []string) {
		filing := h.seedRules()
		body := opRule("dispatch.cap", "always", filing, "Dispatch at most one Swarm until the round closes.")
		body["lift"] = map[string]any{"receipt_kind": "synapse.round.closed", "subject": map[string]any{"schema": "synapse.round", "id": "round_a"}}
		rule := h.recorded(body)
		id := rule["rule_id"].(string)
		receipt := envelope.Receipt{
			ReceiptID: mint(ids.Receipt), Kind: "synapse.round.closed", KindVersion: "1.0", WrittenAt: "2026-10-14T08:00:00.000Z",
			Writer:    envelope.Writer{Package: "synapse", Command: "hive-synapse", Operation: "close", ImplementationVersion: "test"},
			RequestID: mint(ids.Request), CorrelationID: mint(ids.Correlation),
			Scope:     envelope.Scope{Level: "hive", HomeID: homeID},
			Authority: envelope.Authority{Kind: "none"},
			Subject:   &envelope.RecordRef{Schema: "synapse.round", ID: "round_a", HomeID: homeID, RecordRevision: 3},
			Evidence:  []envelope.Evidence{}, Body: map[string]any{},
		}
		data, err := receipt.Encode()
		must(err)
		carried, err := canon.Parse(data)
		must(err)
		return ruleReq("amend", map[string]any{"rule_id": id, "kind": "lift", "words": "The round closed.", "receipt": carried},
			&req{expected: []rev{revOf("pool.rule", id, rule)}}), []string{kindPath("rule", id), kindPath("subject", filing)}
	}},
	{name: "hive-rule expire", command: "hive-rule", op: "expire", repeats: true, build: func(h *home) (*req, []string) {
		filing := h.seedRules()
		body := opRule("dispatch.cap", "always", filing, "Dispatch at most four Swarms.")
		body["expires_at"] = d15
		rule := h.recorded(body)
		id := rule["rule_id"].(string)
		h.clk.Set(time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC))
		return ruleReq("expire", map[string]any{"rule_id": id, "noticed_at": "2026-10-15T01:00:00.000Z"}, &req{}),
			[]string{kindPath("rule", id), kindPath("subject", filing)}
	}},
	{name: "hive-memory subject", command: "hive-memory", op: "subject", build: func(h *home) (*req, []string) {
		filing := h.seedRules()
		h.recorded(opRule("dispatch.cap", "always", filing, "Dispatch at most four Swarms."))
		intake := map[string]any{"schema": "pool.question", "id": questionID, "home_id": homeID, "record_revision": int64(3)}
		return &req{command: "hive-memory", op: "subject", body: map[string]any{"subject_id": filing, "loads_at": []any{"dispatch", "gate"}, "intake": intake},
				authority: word(3), expected: []rev{{"pool.subject", filing, 1}}},
			[]string{kindPath("subject", filing), kindPath("question", questionID)}
	}, stops: map[string]string{kindPath("question", questionID): envelope.CodeAuthorityInsufficient}},
}

// consumerOf is a supervisor session as a consumer of a projection.
func sessionConsumer() map[string]any {
	return map[string]any{"kind": "session", "session_id": mint(ids.Session), "session_generation": int64(1)}
}

func current(p map[string]any) map[string]any { return p["current"].(map[string]any) }

// rendered renders a projection through hive-memory and returns it.
func (h *home) rendered(scope map[string]any, tier, audience, subject string) map[string]any {
	body := map[string]any{"tier": tier, "audience": audience}
	if subject != "" {
		body["subject_id"] = subject
	}
	return h.do(mem("render", body, &req{scope: scope})).Body["projection"].(map[string]any)
}

var memoryScenarios = []*scenario{
	{name: "hive-memory render", command: "hive-memory", op: "render", repeats: true, build: func(h *home) (*req, []string) {
		filing := h.seedRules()
		rule := h.rule("always", filing, hiveScope, nil)
		return mem("render", map[string]any{"tier": "always", "audience": "hive"}, &req{}),
			[]string{kindPath("rule", rule), kindPath("budget", "memory")}
	}},
	{name: "hive-memory propagate", command: "hive-memory", op: "propagate", build: func(h *home) (*req, []string) {
		filing := h.seedRules()
		h.rule("always", filing, hiveScope, nil)
		p := h.rendered(hiveScope, "always", "hive", "")
		id, c := p["projection_id"].(string), current(p)
		return mem("propagate", map[string]any{"projection_id": id, "consumer": sessionConsumer(), "step": "received",
				"rendering": c["rendering"], "digest": c["digest"], "at": "2026-10-14T09:00:00.000Z"}, &req{}),
			[]string{kindPath("projection", id)}
	}},
	{name: "hive-memory budget", command: "hive-memory", op: "budget", build: func(h *home) (*req, []string) {
		filing := h.seedRules()
		h.rule("always", filing, hiveScope, nil)
		s := h.subject(hiveScope, "class.3", "One fact.")
		h.rule("subject", s, hiveScope, nil)
		ap := h.rendered(hiveScope, "always", "hive", "")
		sp := h.rendered(hiveScope, "subject", "class-3", s)
		sid := sp["projection_id"].(string)
		now := current(sp)["bytes"].(int64)
		budget := h.stored(kindPath("budget", "memory"))
		return mem("budget", map[string]any{"projections": []any{map[string]any{"projection_id": sid, "max_bytes": now + 4000}},
				"intake": map[string]any{"schema": "word.intake", "id": "rcpt_01jc7d8e9f0g1h2j3k4m5n6p7v"}},
				&req{authority: word(3), expected: []rev{{"pool.budget", "memory", budget["record_revision"].(int64)}}}),
			[]string{kindPath("budget", "memory"), kindPath("projection", ap["projection_id"].(string)), kindPath("projection", sid), kindPath("question", questionID)}
	}, stops: map[string]string{kindPath("question", questionID): envelope.CodeAuthorityInsufficient}},
}

// plantItem plants an item of the project at the level, beneath the parent
// where one is given, and returns its identity. A Target carries none of the
// work axes.
func (h *home) plantItem(level, parent string) string {
	id := mint(ids.Task)
	deps := []any{}
	if parent != "" {
		deps = append(deps, map[string]any{"type": "parent", "task_id": parent})
	}
	rec := map[string]any{
		"schema": "pool.item", "schema_version": "1.0", "record_revision": int64(3),
		"task_id": id, "scope_level": "project", "home_id": homeID, "workspace_id": workspaceID, "project_id": projectID,
		"title": "An item", "level": level, "state": "queued", "definition": "accepted",
		"deps": deps, "links": []any{}, "generations": []any{},
		"created_at": d01, "updated_at": d02, "writer": writerOf("add"),
	}
	if level != "target" {
		rec["deliverable"], rec["class"], rec["placement_requested"] = "swarm", int64(3), "mac"
		rec["delivery"] = map[string]any{"validation": "synapse", "publication": "harvest", "assimilation": "word"}
	}
	h.plant("item", id, rec)
	return id
}

// opened puts the item in flight with generation 1 open, as open-generation
// would.
func (h *home) opened(id string) {
	rec := h.stored(itemPath(id))
	rec["state"], rec["record_revision"] = "in-flight", rec["record_revision"].(int64)+1
	rec["generations"] = []any{map[string]any{
		"generation": int64(1), "state": "open", "opened_at": d02, "placement_requested": "mac",
		"imperative_revision": int64(1), "admission_id": mint(ids.Admission),
	}}
	h.plant("item", id, rec)
}

// world is a home with a Hunt beneath a Prey beneath a Target, the playbook of
// class 3 as two rules, a subject the Hunt's specification touches with a rule
// of its own, that subject's projection rendered for class 3, and the
// specification revision the Standard names.
type world struct {
	*home
	hunt, prey, target string
	touched            string
	projection         string
	criteria           map[string]any
}

func newWorld(h *home) *world {
	h.bindForge()
	h.plantBudget()
	w := &world{home: h}
	w.target = h.plantItem("target", "")
	w.prey = h.plantItem("prey", w.target)
	w.hunt = h.plantItem("hunt", w.prey)
	class := h.subject(hiveScope, "class.3")
	w.touched = h.subject(hiveScope, "widget.exports")
	h.rule("subject", class, hiveScope, map[string]any{"subject": "class3.steps", "applies": []any{"class-3"}, "rule_line": "Work in small commits and run the declared command after each."})
	h.rule("subject", class, hiveScope, map[string]any{"subject": "class3.stop", "applies": []any{"class-3"}, "rule_line": "Stop at the stated goal and report the evidence."})
	h.rule("subject", w.touched, hiveScope, map[string]any{"subject": "exports.retry", "applies": []any{"class-3"}, "rule_line": "Exports retry at most three times."})

	spec := fixture("pool.specification", "v1-2-touched-subjects-and-evidence")
	specID := mint(ids.Spec)
	spec["specification_id"], spec["home_id"], spec["workspace_id"], spec["project_id"] = specID, homeID, workspaceID, projectID
	revisions := spec["revisions"].([]any)
	revisions[0].(map[string]any)["touched_subjects"] = []any{}
	second := revisions[1].(map[string]any)
	second["touched_subjects"] = []any{w.touched}
	delete(second, "product_rules")
	delete(second, "plan_stage")
	delete(spec, "acceptances")
	h.plant("specification", specID, spec)
	w.criteria = map[string]any{"specification_id": specID, "revision": int64(2), "digest": second["digest"]}
	w.projection = h.rendered(projectScope, "subject", "class-3", w.touched)["projection_id"].(string)
	return w
}

// body is the create request for the item: the revision of the fixture with
// what the command supplies taken out, and the Standard naming the criteria.
func (w *world) body(task string) map[string]any {
	rec := fixture("pool.imperative", "project-swarm-claude-mac")
	r := rec["revisions"].([]any)[0].(map[string]any)
	for _, k := range []string{"revision", "digest", "authored_at", "authored_by", "authority", "request_id", "supersedes_reason"} {
		delete(r, k)
	}
	setup := r["setup"].(map[string]any)
	delete(setup, "placement")
	delete(setup, "repository")
	r["task"].(map[string]any)["standard"].(map[string]any)["criteria"] = w.criteria
	body := map[string]any{"task_id": task, "class": int64(3)}
	for _, k := range []string{"task", "setup", "rules", "definition_of_done"} {
		body[k] = r[k]
	}
	return body
}

func imp(op string, body map[string]any, r *req) *req {
	r.command, r.op, r.body, r.scope = "hive-imperative", op, body, projectScope
	r.version = map[string]string{"render": "1.1", "deliver": "1.1"}[op]
	return r
}

var imperativeScenarios = []*scenario{
	{name: "hive-imperative create", command: "hive-imperative", op: "create", build: func(h *home) (*req, []string) {
		w := newWorld(h)
		return imp("create", w.body(w.hunt), &req{}), []string{itemPath(w.hunt)}
	}},
	{name: "hive-imperative revise", command: "hive-imperative", op: "revise", build: func(h *home) (*req, []string) {
		w := newWorld(h)
		h.do(imp("create", w.body(w.hunt), &req{}))
		body := w.body(w.hunt)
		body["class"] = int64(4)
		body["supersedes_reason"] = "The steer narrowed the scope to the retry loop."
		tree(tree(body["task"])["object"])["text"] = "The retry loop of the nightly export, and nothing else."
		return imp("revise", body, &req{expected: []rev{{"pool.imperative", w.hunt, 1}}}), []string{kindPath("imperative", w.hunt), itemPath(w.hunt)}
	}, rival: func(body map[string]any) []string {
		words := "The retry loop of the nightly export, as the second caller reads it."
		tree(tree(body["task"])["object"])["text"] = words
		return []string{words}
	}},
	{name: "hive-imperative deliver", command: "hive-imperative", op: "deliver", build: func(h *home) (*req, []string) {
		w := newWorld(h)
		h.do(imp("create", w.body(w.hunt), &req{}))
		h.opened(w.hunt)
		r := h.do(imp("render", map[string]any{"task_id": w.hunt}, &req{})).Body
		body := map[string]any{
			"task_id": w.hunt, "generation": int64(1), "revision": r["revision"], "renderer_version": r["renderer_version"],
			"text_digest": r["text_digest"], "text_bytes": r["text_bytes"], "transport": "launch-prompt",
			"evidence": map[string]any{"schema": "dispatch.launch", "id": mint(ids.Receipt), "home_id": homeID, "record_revision": int64(1)},
			"rules":    r["rules"], "subject_projections": r["subject_projections"], "rendered_at": r["rendered_at"], "rendered_text": r["text"],
		}
		return imp("deliver", body, &req{}), []string{kindPath("imperative", w.hunt), itemPath(w.hunt), kindPath("projection", w.projection)}
	}},
}

// The bodies of a specification and its acceptance.

func criterion(id, text, evidence string) map[string]any {
	return map[string]any{"criterion_id": id, "text": text, "evidence": evidence}
}

func criteria() []any {
	return []any{
		criterion("resets", "The counter resets after its sample is collected.", "A test reads the counter twice."),
		criterion("kept", "The collected sample is kept.", "A test reads the sample after the reset."),
	}
}

func specReq(op string, body map[string]any, expected ...rev) *req {
	version := "1.0"
	if op == "define" {
		version = "1.2"
	}
	return &req{command: "hive-spec", op: op, body: body, expected: expected, version: version}
}

// needsSpec adds a Target that needs a specification.
func (h *home) needsSpec() string {
	resp := h.do(poolReq("add", map[string]any{"title": "T", "level": "target", "specification_required": true}))
	return resp.Body["item"].(map[string]any)["task_id"].(string)
}

func (h *home) defineSpec() string {
	resp := h.do(specReq("define", map[string]any{"title": "A specification", "text": "What the change must satisfy.", "criteria": criteria(), "touched_subjects": []any{}}))
	return resp.Body["specification"].(map[string]any)["specification_id"].(string)
}

func acceptBody(spec string, revision int64, task string) map[string]any {
	return map[string]any{
		"specification_id": spec, "revision": revision, "task_id": task,
		"need_test":              map[string]any{"outcome": "The counter resets.", "coverage": "new-definition", "reason": "No accepted specification covered it."},
		"open_product_questions": []any{},
	}
}

var specificationScenarios = []*scenario{
	{name: "hive-spec define", command: "hive-spec", op: "define", build: func(h *home) (*req, []string) {
		target := h.needsSpec()
		return specReq("define", map[string]any{"title": "A specification", "text": "What the change must satisfy.", "criteria": criteria(), "touched_subjects": []any{}}),
			[]string{itemPath(target)}
	}},
	{name: "hive-spec define (revise)", command: "hive-spec", op: "define", rival: rivalWords("text", "What the second caller says the change must satisfy."), build: func(h *home) (*req, []string) {
		target := h.needsSpec()
		spec := h.defineSpec()
		return specReq("define", map[string]any{"specification_id": spec, "text": "What the change must satisfy, narrowed.", "criteria": criteria(), "touched_subjects": []any{}},
				rev{"pool.specification", spec, 1}),
			[]string{kindPath("specification", spec), itemPath(target)}
	}},
	{name: "hive-spec accept", command: "hive-spec", op: "accept", build: func(h *home) (*req, []string) {
		target := h.needsSpec()
		spec := h.defineSpec()
		return specReq("accept", acceptBody(spec, 1, target)), []string{kindPath("specification", spec), itemPath(target)}
	}},
	{name: "hive-spec close", command: "hive-spec", op: "close", build: func(h *home) (*req, []string) {
		target := h.needsSpec()
		spec := h.defineSpec()
		h.do(specReq("accept", acceptBody(spec, 1, target)))
		prey := h.do(poolReq("add", map[string]any{"title": "P", "level": "prey", "deliverable": "swarm", "deps": []any{map[string]any{"type": "parent", "task_id": target}}})).Body["item"].(map[string]any)["task_id"].(string)
		hunt := h.do(poolReq("add", map[string]any{"title": "H", "level": "hunt", "deliverable": "swarm", "deps": []any{map[string]any{"type": "parent", "task_id": prey}}})).Body["item"].(map[string]any)["task_id"].(string)
		rec := h.stored(itemPath(hunt))
		rec["generations"] = []any{map[string]any{
			"generation": int64(1), "state": "closed", "opened_at": "2026-10-14T09:00:00.000Z",
			"placement_requested": "mac", "imperative_revision": int64(1), "admission_id": mint(ids.Admission),
			"closed_at": "2026-10-14T10:00:00.000Z", "outcome": "delivered", "closed_by": map[string]any{"schema": "hive.receipt", "id": mint(ids.Receipt)},
		}}
		h.plant("item", hunt, rec)
		landing := func() map[string]any { return map[string]any{"schema": "hive.receipt", "id": mint(ids.Receipt)} }
		return specReq("close", map[string]any{
				"specification_id": spec, "revision": int64(1), "task_id": hunt, "generation": int64(1),
				"landed_revision": strings.Repeat("a", 40), "landing": landing(), "proof": []any{landing(), landing()}, "resolved_findings": []any{landing()},
			}),
			[]string{kindPath("specification", spec), itemPath(hunt)}
	}},
}

// The notes and the queue of the maintenance of the Pool's memory.

func (h *home) plantQueue() {
	rec := fixture("pool.maintenance-queue", "minimal")
	rec["home_id"], rec["record_revision"] = homeID, int64(1)
	h.plant("maintenance-queue", "maintenance", rec)
}

// foldSeed is a home with the budget, the queue, and a subject with a fact.
func (h *home) foldSeed() (subject string) {
	h.seedRules()
	h.plantQueue()
	return h.subject(hiveScope, "class.0", "The first fact.")
}

func note(text string, over map[string]any) map[string]any {
	n := map[string]any{"text": text, "provenance": fixture("pool.note", "minimal")["provenance"], "origin": "stow"}
	for k, v := range over {
		n[k] = v
	}
	return n
}

func enqueueReq(queueRevision int64, notes ...map[string]any) *req {
	list := make([]any, len(notes))
	for i, n := range notes {
		list[i] = n
	}
	return &req{command: "hive-memory", op: "enqueue", body: map[string]any{"notes": list},
		expected: []rev{{"pool.maintenance-queue", "maintenance", queueRevision}}}
}

// The accounts of an incident a miss is filed with.

func recordRef(schema, id string) map[string]any {
	return map[string]any{"schema": schema, "id": id, "home_id": homeID}
}

func accountOf(incident, independence string, derived ...map[string]any) map[string]any {
	from := []any{}
	for _, d := range derived {
		from = append(from, d)
	}
	return map[string]any{
		"incident": recordRef("pool.item", incident), "observer": map[string]any{"kind": "test"},
		"independence": independence, "derived_from": from, "evidence": []any{recordRef("pool.item", incident)},
	}
}

func sessionActor() (caller, actor map[string]any) {
	id := mint(ids.Session)
	return map[string]any{"kind": "supervisor", "session_id": id, "session_generation": int64(1)},
		map[string]any{"kind": "session", "session_id": id, "session_generation": int64(1)}
}

func workerActor() (caller, actor map[string]any) {
	id := mint(ids.Task)
	return map[string]any{"kind": "worker", "task_id": id, "generation": int64(1)},
		map[string]any{"kind": "generation", "task_id": id, "generation": int64(1)}
}

func missBody(rule string, actor, provenance map[string]any) map[string]any {
	return map[string]any{
		"rule": map[string]any{"rule_id": rule, "record_revision": int64(1)}, "actor": actor,
		"step": "dispatch", "provenance": provenance, "observed_at": "2026-10-14T09:00:00.000Z",
	}
}

var maintenanceScenarios = []*scenario{
	{name: "hive-memory enqueue", command: "hive-memory", op: "enqueue", build: func(h *home) (*req, []string) {
		subject := h.foldSeed()
		return enqueueReq(1, note("A note, kept exactly.", map[string]any{"subject_id": subject, "fact_id": "kept.exactly"}), note("A note with no subject.", nil)),
			[]string{kindPath("maintenance-queue", "maintenance")}
	}},
	{name: "hive-memory fold", command: "hive-memory", op: "fold", build: func(h *home) (*req, []string) {
		subject := h.foldSeed()
		queued := h.do(enqueueReq(1, note("Waiting.", map[string]any{"subject_id": subject, "fact_id": "waiting"}), note("A note with no subject.", nil)))
		pending := queued.Body["queue"].(map[string]any)["pending"].([]any)
		needs := []string{kindPath("maintenance-queue", "maintenance"), kindPath("subject", subject)}
		for _, id := range pending {
			needs = append(needs, kindPath("note", id.(string)))
		}
		return &req{command: "hive-memory", op: "fold", body: map[string]any{}, expected: []rev{{"pool.maintenance-queue", "maintenance", 2}}}, needs
	}},
	{name: "hive-memory miss", command: "hive-memory", op: "miss", build: func(h *home) (*req, []string) {
		subject := h.seedRules()
		rule := h.rule("subject", subject, hiveScope, nil)
		caller, actor := sessionActor()
		return &req{command: "hive-memory", op: "miss", body: missBody(rule, actor, accountOf("task_a", "independent")), caller: caller},
			[]string{kindPath("rule", rule), kindPath("subject", subject)}
	}},
	{name: "hive-memory miss (correction)", command: "hive-memory", op: "miss", build: func(h *home) (*req, []string) {
		subject := h.seedRules()
		rule := h.rule("subject", subject, hiveScope, nil)
		callerA, actorA := workerActor()
		callerB, actorB := sessionActor()
		first := h.do(&req{command: "hive-memory", op: "miss", body: missBody(rule, actorA, accountOf("incident_a", "independent")), caller: callerA}).Body["miss"].(map[string]any)
		second := h.do(&req{command: "hive-memory", op: "miss", body: missBody(rule, actorB, accountOf("incident_b", "independent")), caller: callerB}).Body["miss"].(map[string]any)
		id := second["miss_id"].(string)
		body := missBody(rule, actorB, accountOf("incident_b", "derived", recordRef("pool.miss", first["miss_id"].(string))))
		body["miss_id"] = id
		return &req{command: "hive-memory", op: "miss", body: body, caller: callerB, expected: []rev{{"pool.miss", id, 1}}},
			[]string{kindPath("miss", id)}
	}},
}

// The questions the Mind's word persists, raised the way hive-word hands them
// off.

func handoff(op string, body map[string]any, expected ...rev) *req {
	r := &req{command: "hive-pool", op: op, body: body, expected: expected, handoff: true}
	r.id = mint(ids.Request)
	return r
}

const asked = "2026-10-14T09:00:00.000Z"

func option(id string) map[string]any {
	return map[string]any{"option_id": id, "label": "Option " + id, "consequence": "Choosing " + id + " goes ahead."}
}

func ask(held string) map[string]any {
	return map[string]any{
		"question_id": mint(ids.Question), "held_task_id": held, "origin": map[string]any{"kind": "supervisor"},
		"text": "Which vendor should the integration use?", "consequence": "Without an answer the work stays held.",
		"recommendation": "keep", "options": []any{option("keep"), option("switch")}, "asked_at": asked,
	}
}

func holdQuestionBody(held string) map[string]any {
	return map[string]any{
		"question": ask(held),
		"round": map[string]any{
			"decision_round_id": mint(ids.DecisionRound), "new": true,
			"origin": map[string]any{"kind": "supervisor"}, "attachments": []any{}, "complete": true,
		},
		"card_id":      mint(ids.Card),
		"requested_by": map[string]any{"kind": "supervisor", "session_id": mint(ids.Session), "session_generation": int64(1)},
	}
}

// raisedQuestion is a question raised on a new item, with what holds it.
type raisedQuestion struct{ item, question, round, card, hold string }

func (h *home) raiseQuestion() raisedQuestion {
	item := h.addItem(nil)
	body := h.do(handoff("hold-question", holdQuestionBody(item))).Body
	q, r := tree(body["question"]), tree(body["round"])
	return raisedQuestion{item, q["question_id"].(string), r["decision_round_id"].(string), q["card_id"].(string), tree(body["hold"])["hold_id"].(string)}
}

// binding is the card binding of the question as its card shows it now.
func (h *home) binding(question string) map[string]any {
	q := h.stored(kindPath("question", question))
	r := h.stored(kindPath("decision-round", q["decision_round_id"].(string)))
	var c map[string]any
	for _, v := range r["cards"].([]any) {
		if m := v.(map[string]any); m["card_id"] == q["card_id"] {
			c = m
		}
	}
	return map[string]any{
		"card_id": c["card_id"], "card_revision": c["card_revision"],
		"question_id": question, "question_revision": c["question_revision"],
		"held_task_id": q["held_task_id"], "held_task_revision": c["held_task_revision"],
		"decision_round_id": r["decision_round_id"], "decision_round_revision": r["record_revision"],
		"owner_home_id": homeID, "owner_task_id": q["held_task_id"],
	}
}

// answerReq is an answer typed at a terminal: under a request of its own,
// which is its source.
func (h *home) answerReq(question string) *req {
	r := handoff("answer-question", nil)
	r.body = map[string]any{
		"card": h.binding(question), "text": "", "source": map[string]any{"kind": "operator-request", "request_id": r.id},
		"answered_at": "2026-10-14T09:30:00.000Z", "requested_by": map[string]any{"kind": "operator"},
		"option_id": "keep", "option_label": "Option keep",
	}
	return r
}

// answered is a question raised on a new item and answered, its answer
// applied and its hold still active.
func (h *home) answered() raisedQuestion {
	q := h.raiseQuestion()
	h.do(h.answerReq(q.question))
	return q
}

func (h *home) atRevision(question string) []rev {
	return []rev{{"pool.question", question, h.stored(kindPath("question", question))["record_revision"].(int64)}}
}

func (h *home) archive(item string) {
	h.do(poolReq("transition", map[string]any{"task_id": item, "to": "archived"}))
}

func disposeBody(question string) map[string]any {
	return map[string]any{
		"question_id": question, "note": "The owner read the Hive Mind's words and acted on them.",
		"requested_by": map[string]any{"kind": "operator"},
	}
}

var decisionScenarios = []*scenario{
	{name: "hive-pool hold-question", command: "hive-pool", op: "hold-question", build: func(h *home) (*req, []string) {
		item := h.addItem(nil)
		return handoff("hold-question", holdQuestionBody(item)), []string{itemPath(item)}
	}},
	{name: "hive-pool revise-question", command: "hive-pool", op: "revise-question", rival: rivalWords("text", "Which vendor does the second caller want?"), build: func(h *home) (*req, []string) {
		q := h.raiseQuestion()
		body := map[string]any{
			"question_id": q.question, "text": "Which vendor should the integration use after the price rise?",
			"consequence": "Without an answer the work stays held and the price rise lands.", "recommendation": "switch",
			"options": []any{option("switch"), option("keep"), option("both")}, "amount": "12000 USD",
			"asked_at": "2026-10-14T10:00:00.000Z", "requested_by": map[string]any{"kind": "operator"},
		}
		return handoff("revise-question", body, rev{"pool.question", q.question, 1}),
			[]string{kindPath("question", q.question), kindPath("decision-round", q.round)}
	}},
	{name: "hive-pool answer-question", command: "hive-pool", op: "answer-question", repeats: true, build: func(h *home) (*req, []string) {
		q := h.raiseQuestion()
		return h.answerReq(q.question), []string{kindPath("question", q.question), kindPath("decision-round", q.round), kindPath("hold", q.hold)}
	}},
	{name: "hive-pool release-question", command: "hive-pool", op: "release-question", build: func(h *home) (*req, []string) {
		q := h.answered()
		return handoff("release-question", disposeBody(q.question), h.atRevision(q.question)...),
			[]string{kindPath("question", q.question), kindPath("hold", q.hold), itemPath(q.item)}
	}},
	{name: "hive-pool complete-question", command: "hive-pool", op: "complete-question", build: func(h *home) (*req, []string) {
		q := h.answered()
		h.archive(q.item)
		body := disposeBody(q.question)
		body["evidence"] = []any{map[string]any{"schema": "pool.item", "id": q.item}}
		return handoff("complete-question", body, h.atRevision(q.question)...),
			[]string{kindPath("question", q.question), kindPath("hold", q.hold), itemPath(q.item)}
	}},
	{name: "hive-pool close-question", command: "hive-pool", op: "close-question", build: func(h *home) (*req, []string) {
		q := h.raiseQuestion()
		h.archive(q.item)
		body := map[string]any{
			"question_id": q.question, "reason": "Nothing is left for the question to gate.",
			"evidence":     []any{map[string]any{"kind": "item-archived", "ref": map[string]any{"schema": "pool.item", "id": q.item}}},
			"requested_by": map[string]any{"kind": "operator"},
		}
		return handoff("close-question", body, h.atRevision(q.question)...),
			[]string{kindPath("question", q.question), kindPath("hold", q.hold), itemPath(q.item)}
	}},
}

// The legacy backlog is read from a file outside the home and never written.

var legacyScenarios = []*scenario{
	{name: "hive-pool import-legacy", command: "hive-pool", op: "import-legacy", build: func(h *home) (*req, []string) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "legacy", "backlog.md"))
		must(err)
		path := filepath.Join(newDir(), "backlog.md")
		must(os.WriteFile(path, data, 0o600))
		// The request carries the schema's form of the path: absolute, with
		// forward slashes, the same string on every platform.
		abs, err := filepath.Abs(path)
		must(err)
		earlier := fixture("pool.migration", "empty-file-hive")
		earlier["home_id"] = homeID
		h.plant("migration", earlier["migration_id"].(string), earlier)
		return poolReq("import-legacy", map[string]any{"path": filepath.ToSlash(abs), "zone": "UTC", "dry_run": false, "expected_digest": canon.FileDigest(data)}),
			[]string{kindPath("migration", earlier["migration_id"].(string))}
	}},
}
