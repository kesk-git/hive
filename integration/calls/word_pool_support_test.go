package calls

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/exec"
	"github.com/kesk-git/hive/contract/ids"
)

// The support of word_pool_test.go: the callers, requests and fixtures of the
// call from hive-word to hive-pool, made through the built executables only.
// A home is a fixture the test builds in a temporary directory with newHome;
// the items a question holds are added to it through the real hive-pool, and
// every record the tests read is read back through hive-pool read.

// Callers, as a request names them.
var (
	wpSupervisor = envelope.Caller{Kind: envelope.CallerSupervisor, SessionID: ident(ids.Session, 1), SessionGeneration: 1}
	wpOperator   = envelope.Caller{Kind: envelope.CallerOperator}
	wpWorker     = envelope.Caller{Kind: envelope.CallerWorker, TaskID: ident(ids.Task, 900), Generation: 1}
	wpTest       = envelope.Caller{Kind: envelope.CallerTest}
)

// wpWords are words kept exactly: spacing, quotes and a dash included.
const wpWords = "  Keep   this\ttext exactly — 'quoted', \"escaped\"  "

// wpOptions are the options of a question, and the recommendation.
func wpOptions() []any {
	return []any{
		map[string]any{"option_id": "keep", "label": "Keep", "consequence": "The vendor stays."},
		map[string]any{"option_id": "switch", "label": "Switch", "consequence": "The vendor changes."},
	}
}

// wpCall is one request to a built executable: the command, the operation,
// the caller and body, and the revisions the caller read.
type wpCall struct {
	command, operation string
	n                  int
	caller             envelope.Caller
	body               map[string]any
	expected           []envelope.ExpectedRevision
	// causation is the request a handoff stands on, or nil.
	causation *envelope.RecordRef
}

// wpRequest builds the request of the call, in the hive scope of the test
// home, at the version its command declares for the operation. n makes its
// identity; the issue time is fixed, so the same call is the same bytes.
func wpRequest(t testing.TB, c wpCall) request {
	t.Helper()
	r := envelope.Request{
		Command:           c.command,
		Operation:         c.operation,
		OperationVersion:  declaredVersion(t, c.command, c.operation),
		RequestID:         ident(ids.Request, c.n),
		CorrelationID:     ident(ids.Correlation, 1),
		IssuedAt:          "2026-10-11T08:00:00.000Z",
		Caller:            c.caller,
		Scope:             envelope.Scope{Level: envelope.ScopeHive, HomeID: testHome},
		Authority:         envelope.Authority{Kind: envelope.AuthorityNone},
		ExpectedRevisions: c.expected,
		Causation:         c.causation,
		Body:              c.body,
	}
	data, err := r.Encode()
	if err != nil {
		t.Fatalf("%s %s: %v", c.command, c.operation, err)
	}
	return request{id: r.RequestID, data: data}
}

// word sends the call to the built hive-word.
func (c wpCall) word(t testing.TB, home string) reply {
	t.Helper()
	c.command = "hive-word"
	return call{program: "hive-word", operation: c.operation, home: home, req: wpRequest(t, c)}.run(t)
}

// pool sends the call to the built hive-pool.
func (c wpCall) pool(t testing.TB, home string) reply {
	t.Helper()
	c.command = "hive-pool"
	return call{program: "hive-pool", operation: c.operation, home: home, req: wpRequest(t, c)}.run(t)
}

// wpOK fails the test unless the reply is an ok response, and returns it.
func wpOK(t testing.TB, what string, r reply) *envelope.Response {
	t.Helper()
	resp := r.must(t)
	if r.result.Code != 0 || string(resp.Status) != "ok" {
		t.Fatalf("%s: exit code %d, status %q, error %+v; want 0, ok", what, r.result.Code, resp.Status, resp.Error)
	}
	return resp
}

// wpSerial numbers the requests of one fixture, so that no two requests of a
// test share an identity.
type wpSerial struct{ next int }

func (s *wpSerial) take() int {
	s.next++
	return s.next
}

// wpFixture is a home with the requests a test makes in it.
type wpFixture struct {
	t    testing.TB
	home string
	ids  wpSerial
}

func newWpFixture(t testing.TB) *wpFixture {
	t.Helper()
	// Request numbers start above those the callers' identities use.
	return &wpFixture{t: t, home: newHome(t), ids: wpSerial{next: 100}}
}

// addItem adds a leaf item to the home through hive-pool and returns its
// identity.
func (f *wpFixture) addItem() string {
	f.t.Helper()
	resp := wpOK(f.t, "add", wpCall{operation: "add", n: f.ids.take(), caller: wpTest, body: map[string]any{
		"title": "A leaf", "level": "prey", "deliverable": "swarm",
	}}.pool(f.t, f.home))
	item, _ := resp.Body["item"].(map[string]any)
	id, _ := item["task_id"].(string)
	if id == "" {
		f.t.Fatalf("add returned no item: %v", resp.Body)
	}
	return id
}

// archive archives the item through hive-pool.
func (f *wpFixture) archive(task string) {
	f.t.Helper()
	wpOK(f.t, "transition", wpCall{operation: "transition", n: f.ids.take(), caller: wpTest, body: map[string]any{
		"task_id": task, "to": "archived",
	}}.pool(f.t, f.home))
}

// wpHoldBody is the body of a hive-word hold on the item, with the question
// text given.
func wpHoldBody(task, text string) map[string]any {
	return map[string]any{
		"held_task_id": task, "origin": map[string]any{"kind": "supervisor"},
		"text": text, "consequence": wpWords,
		"options": wpOptions(), "recommendation": "keep",
		"round": map[string]any{"complete": true},
	}
}

// wpHeld is a question raised through hive-word, with what its response names.
type wpHeld struct {
	item, question, round, card, hold string
	resp                              *envelope.Response
}

// hold raises a question on a new item through hive-word.
func (f *wpFixture) hold() wpHeld {
	f.t.Helper()
	return f.holdOn(f.addItem(), wpWords)
}

// holdOn raises a question with the text on the item through hive-word.
func (f *wpFixture) holdOn(item, text string) wpHeld {
	f.t.Helper()
	resp := wpOK(f.t, "hold", wpCall{operation: "hold", n: f.ids.take(), caller: wpSupervisor, body: wpHoldBody(item, text)}.word(f.t, f.home))
	return wpHeldFrom(f.t, item, resp)
}

func wpHeldFrom(t testing.TB, item string, resp *envelope.Response) wpHeld {
	t.Helper()
	h := wpHeld{item: item, resp: resp}
	h.question, _ = resp.Body["question_id"].(string)
	h.round, _ = resp.Body["decision_round_id"].(string)
	h.card, _ = resp.Body["card_id"].(string)
	h.hold, _ = resp.Body["hold_id"].(string)
	if h.question == "" || h.round == "" || h.card == "" || h.hold == "" {
		t.Fatalf("hold names question %q, round %q, card %q, hold %q", h.question, h.round, h.card, h.hold)
	}
	return h
}

// read reads one record of the kind by its identity through hive-pool read,
// and returns it.
func (f *wpFixture) read(kind, id string) map[string]any {
	f.t.Helper()
	resp := wpOK(f.t, "read "+kind, wpCall{operation: "read", n: f.ids.take(), caller: wpTest, body: map[string]any{
		"kind": kind, "selector": map[string]any{"id": id},
	}}.pool(f.t, f.home))
	records, _ := resp.Body["records"].([]any)
	if len(records) != 1 {
		f.t.Fatalf("read %s %s: %d records", kind, id, len(records))
	}
	rec, _ := records[0].(map[string]any)
	return rec
}

// revision is the record revision of the question as the Pool holds it.
func (f *wpFixture) revision(question string) int64 {
	f.t.Helper()
	n, _ := f.read("question", question)["record_revision"].(int64)
	return n
}

// binding is the card binding of the question as its card shows it now.
func (f *wpFixture) binding(h wpHeld) map[string]any {
	f.t.Helper()
	q := f.read("question", h.question)
	r := f.read("decision-round", h.round)
	var card map[string]any
	for _, c := range r["cards"].([]any) {
		if m := c.(map[string]any); m["card_id"] == h.card {
			card = m
		}
	}
	if card == nil {
		f.t.Fatalf("round %s has no card %s", h.round, h.card)
	}
	return map[string]any{
		"card_id": h.card, "card_revision": card["card_revision"],
		"question_id": h.question, "question_revision": card["question_revision"],
		"held_task_id": h.item, "held_task_revision": card["held_task_revision"],
		"decision_round_id": h.round, "decision_round_revision": r["record_revision"],
		"owner_home_id": testHome, "owner_task_id": q["held_task_id"],
	}
}

// operatorAnswer is an answer typed at a terminal by the operator, under the
// request number n, which is its source.
func (f *wpFixture) operatorAnswer(h wpHeld, n int, with map[string]any) wpCall {
	f.t.Helper()
	body := map[string]any{
		"card": f.binding(h), "text": "", "answered_at": "2026-10-11T08:30:00.000Z",
		"source": map[string]any{"kind": "operator-request", "request_id": ident(ids.Request, n)},
	}
	for k, v := range with {
		body[k] = v
	}
	return wpCall{operation: "answer", n: n, caller: wpOperator, body: body}
}

// expecting names the question's revision as the caller read it.
func wpExpecting(question string, revision int64) []envelope.ExpectedRevision {
	return []envelope.ExpectedRevision{{Schema: "pool.question", ID: question, RecordRevision: revision}}
}

// wpKeep is the option an answer chooses.
var wpKeep = map[string]any{"option_id": "keep", "option_label": "Keep"}

// wpKinds are the kinds of the receipts of a response, sorted.
func wpKinds(resp *envelope.Response) []string {
	out := []string{}
	for _, r := range resp.Receipts {
		out = append(out, r.Kind)
	}
	sort.Strings(out)
	return out
}

// wpRecordFiles lists, by slash path, every file of the home.
func wpRecordFiles(t testing.TB, home string) []string {
	t.Helper()
	var out []string
	for name := range snapshot(t, home) {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// wpUnder lists the files of the home under the directory, slash paths
// relative to the home.
func wpUnder(t testing.TB, home, dir string) []string {
	t.Helper()
	var out []string
	for _, name := range wpRecordFiles(t, home) {
		if strings.HasPrefix(name, dir+"/") {
			out = append(out, name)
		}
	}
	return out
}

// wpWrite replaces the file of the home at the slash path.
func wpWrite(t testing.TB, home, rel string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, filepath.FromSlash(rel)), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// The pass-through hive-pool of the race tests is this test executable
// itself, built from Go as every fake is (spec/test-targets.md, rule 3), and
// copied under the name hive-pool into a directory the tests place first on
// the search path of one hive-word. Started under that name it runs no test:
// it forwards every call to the real hive-pool unchanged, the arguments, the
// request on standard input, the response on standard output, standard error
// and the exit code, and holds the handoff, which is every call whose
// operation is not read, as hive-word sends them. It writes the file named
// arrived into its own directory, holding the operation, and forwards the call
// once a file named release is there.
//
// It finds the real hive-pool by running itself once more: the first run,
// which has its own directory on its search path, takes that directory off the
// path it gives its child, so that the child, which contract/exec finds on the
// path it was started with, finds the real hive-pool and forwards to it.
const (
	holdPoolName = "hive-pool"
	holdArrived  = "arrived"
	holdRelease  = "release"
	// holdPatience is how long either side waits for the other.
	holdPatience = time.Minute
)

func init() {
	if self, err := os.Executable(); err == nil && filepath.Base(self) == holdPoolName+suffix() {
		os.Exit(passThrough(filepath.Dir(self)))
	}
}

// passThrough is the pass-through hive-pool, run from its own directory.
func passThrough(own string) int {
	request, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 99
	}
	if rest, first := withoutDir(own); first {
		if len(os.Args) > 1 && os.Args[1] != "read" && !holdCall(own, os.Args[1]) {
			return 99
		}
		if os.Setenv("PATH", rest) != nil {
			return 99
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		return 99
	}
	r, err := exec.Start(exec.Spec{
		Program: exec.Program{Kind: exec.Hive, Name: holdPoolName},
		Args:    os.Args[1:],
		Dir:     dir,
		Stdin:   exec.Pipe(),
		Stdout:  exec.Pipe(),
		Stderr:  exec.Pipe(),
	})
	if err != nil {
		return 99
	}
	defer r.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); io.Copy(os.Stdout, r.Stdout()) }()
	go func() { defer wg.Done(); io.Copy(os.Stderr, r.Stderr()) }()
	go func() {
		r.Stdin().Write(request)
		r.Stdin().Close()
	}()
	wg.Wait()
	res := r.Wait()
	if res.Outcome != exec.Exited {
		return 98
	}
	return int(res.Code)
}

// withoutDir is the search path this run was started with, less the
// directory, and whether the directory was on it.
func withoutDir(dir string) (string, bool) {
	own, err := os.Stat(dir)
	if err != nil {
		return "", false
	}
	var kept []string
	found := false
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if fi, err := os.Stat(entry); err == nil && os.SameFile(fi, own) {
			found = true
			continue
		}
		kept = append(kept, entry)
	}
	return strings.Join(kept, string(os.PathListSeparator)), found
}

// holdCall says the call has arrived and waits for its release.
func holdCall(dir, operation string) bool {
	pending := filepath.Join(dir, holdArrived+".tmp")
	if os.WriteFile(pending, []byte(operation), 0o600) != nil || os.Rename(pending, filepath.Join(dir, holdArrived)) != nil {
		return false
	}
	for end := time.Now().Add(holdPatience); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if _, err := os.Stat(filepath.Join(dir, holdRelease)); err == nil {
			return true
		}
	}
	return false
}

// wpHeldHandoff is a hive-word started with a pass-through hive-pool first on
// its search path alone, which holds the word's handoff to the Pool until the
// test releases it.
type wpHeldHandoff struct {
	t    testing.TB
	dir  string
	wait func() reply
	once sync.Once
}

// holdHandoff starts the call of hive-word with a pass-through hive-pool
// placed first on the search path of that one hive-word, and returns at once.
// Every call the word makes to hive-pool passes through to the real one
// unchanged, the handoff excepted, which is held.
func holdHandoff(t testing.TB, c call) *wpHeldHandoff {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "holdpool")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, holdPoolName+suffix()), exe, 0o700); err != nil {
		t.Fatal(err)
	}
	h := &wpHeldHandoff{t: t, dir: dir}
	t.Cleanup(h.release)

	// The environment of the process is read when the word starts, and not
	// again: no other word started after this one sees the directory.
	was := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+was); err != nil {
		t.Fatal(err)
	}
	defer os.Setenv("PATH", was)
	h.wait = c.begin(t)
	return h
}

// arrived waits until the held handoff has arrived, and returns the operation
// of hive-pool it names. The word has read the Pool by then, and the handoff
// is sent only after its read.
func (h *wpHeldHandoff) arrived() string {
	h.t.Helper()
	for end := time.Now().Add(holdPatience); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		data, err := os.ReadFile(filepath.Join(h.dir, holdArrived))
		if err == nil {
			return string(data)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			h.t.Fatal(err)
		}
	}
	h.t.Fatal("the word's handoff to the Pool did not arrive")
	return ""
}

// release lets the held handoff go on to the real hive-pool.
func (h *wpHeldHandoff) release() {
	h.once.Do(func() {
		if err := os.WriteFile(filepath.Join(h.dir, holdRelease), nil, 0o600); err != nil {
			h.t.Error(err)
		}
	})
}
