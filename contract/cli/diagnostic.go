package cli

import (
	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
	"github.com/kesk-git/hive/contract/store"
)

// The diagnostic event (spec/events.md, Kinds): the one kind common to every
// package, each package writing its own instance.
const (
	diagnosticFact        = "diagnostic.recorded"
	diagnosticKindVersion = "1.0"
	// invocationSchema is the schema of the basis of a diagnostic event whose
	// request has no request_id that could be read: the invocation itself,
	// observed.
	invocationSchema = "hive.invocation"
	requestSchema    = "hive.request"
)

// valueChecker is a journal that says whether its own validation applies the
// no-value check of the design's Secrets section to a body.
type valueChecker interface{ ChecksValues() bool }

// diagnose records, in the journal of the home at dir, the detail of a failed
// invocation that holds some beyond its message: a lower-layer error chain or a
// panic (spec/commands.md, Invocation). req is the request where it was read.
//
// No free text is kept before the no-value check is in force: the event is
// appended only through a journal that says its validation applies that check.
// Nothing is written for an ok response, a replayed one or a failure whose
// message says everything, and nothing where the executable gives no journal,
// dir is not a directory whose home record reads, the journal does not open or
// does not apply the check. The append is made after the response is decided
// and before it is written, and a failure of it, a body the check refuses
// included, changes nothing: the detail is dropped whole, and the response, its
// status, its exit code and standard error stand.
func (e *Executable) diagnose(dir string, req *envelope.Request, resp *envelope.Response) {
	if e.Journal == nil || dir == "" || resp.Status == envelope.Ok || resp.Error == nil || resp.Replayed || resp.Diagnostic.Empty() || resp.Diagnostic.Shaped {
		return
	}
	home, err := store.CheckHome(dir)
	if err != nil || home.HomeID == "" {
		return
	}
	j, err := e.Journal(home.Dir)
	if err != nil {
		return
	}
	if c, ok := j.(valueChecker); !ok || !c.ChecksValues() {
		return
	}
	event, err := e.diagnosticEvent(home.HomeID, req, resp)
	if err != nil {
		return
	}
	_ = j.Append([]store.Event{event})
}

// diagnosticEvent makes the diagnostic event of a failed response. The
// request_id the response carries is the event's, and that request causes the
// event and is its basis. A request that was read gives the event its scope and
// correlation_id; one that was not gives it a correlation_id of its own and the
// hive scope of the home, and where not even its request_id was read, the
// invocation, observed, is the event's basis.
func (e *Executable) diagnosticEvent(homeID string, req *envelope.Request, resp *envelope.Response) (store.Event, error) {
	eventID, err := ids.Mint(e.clock(), ids.Event)
	if err != nil {
		return store.Event{}, err
	}
	at, err := clock.Stamp(e.clock())
	if err != nil {
		return store.Event{}, err
	}
	body := map[string]any{
		"command": resp.Command,
		"status":  string(resp.Status),
		"code":    resp.Error.Code,
	}
	if resp.Operation != "" {
		body["operation"] = resp.Operation
	}
	d := resp.Diagnostic
	if len(d.Chain) > 0 {
		detail := make([]any, len(d.Chain))
		for i, s := range d.Chain {
			detail[i] = s
		}
		body["detail"] = detail
	}
	if d.Stack != "" {
		body["stack"] = d.Stack
	}

	m := map[string]any{
		"schema":         "hive.event",
		"schema_version": "1.0",
		"event_id":       eventID,
		"kind":           e.Package + "." + diagnosticFact,
		"kind_version":   diagnosticKindVersion,
		"source":         map[string]any{"package": e.Package, "source_id": e.Package},
		"scope_level":    envelope.ScopeHive,
		"home_id":        homeID,
		"occurred_at":    at,
		"observed_at":    at,
		"body":           body,
	}
	if resp.RequestID != "" {
		body["request_id"] = resp.RequestID
		ref := map[string]any{"schema": requestSchema, "id": resp.RequestID}
		m["causation"] = ref
		m["basis"] = map[string]any{"kind": "observation", "ref": ref}
	}
	if req != nil {
		m["correlation_id"] = req.CorrelationID
		m["scope_level"] = req.Scope.Level
		for k, v := range map[string]string{"workspace_id": req.WorkspaceID, "project_id": req.ProjectID, "clone_id": req.CloneID, "task_id": req.TaskID} {
			if v != "" {
				m[k] = v
			}
		}
		if req.Generation != 0 {
			m["generation"] = req.Generation
		}
	} else {
		corr, err := ids.Mint(e.clock(), ids.Correlation)
		if err != nil {
			return store.Event{}, err
		}
		m["correlation_id"] = corr
		if resp.RequestID == "" {
			m["basis"] = map[string]any{"kind": "observation", "ref": map[string]any{"schema": invocationSchema, "id": corr}}
		}
	}
	content, err := canon.Encode(m)
	if err != nil {
		return store.Event{}, err
	}
	return store.Event{ID: eventID, Content: content}, nil
}
