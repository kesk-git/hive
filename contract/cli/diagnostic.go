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
	// request could not be read: the invocation itself, observed.
	invocationSchema = "hive.invocation"
	requestSchema    = "hive.request"
)

// diagnose records, in the journal of the home at dir, the detail of a failed
// invocation that holds some beyond its message: a lower-layer error chain or a
// panic (spec/commands.md, Invocation). req is the request where it was read.
//
// Nothing is written for an ok response, a replayed one or a failure whose
// message says everything, and nothing where the executable configures no
// journal, dir is not a directory whose home record reads, or the journal does
// not open. The append is made after the response is decided and before it is
// written, and a failure of it changes nothing: the detail is dropped, and the
// response, its status, its exit code and standard error stand.
func (e *Executable) diagnose(dir string, req *envelope.Request, resp *envelope.Response) {
	if e.Journal == nil || dir == "" || resp.Status == envelope.Ok || resp.Error == nil || resp.Replayed || resp.Diagnostic.Empty() {
		return
	}
	home, err := store.CheckHome(dir)
	if err != nil || home.HomeID == "" {
		return
	}
	event, err := e.diagnosticEvent(home.HomeID, req, resp)
	if err != nil {
		return
	}
	j, err := e.Journal(home.Dir)
	if err != nil {
		return
	}
	_ = j.Append([]store.Event{event})
}

// diagnosticEvent makes the diagnostic event of a failed response. A request
// that was read gives the event its scope and correlation_id, causes it, and
// is its basis; one that was not gives it a correlation_id of its own, the
// hive scope of the home, and the invocation, observed, as its basis.
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
	if d.PanicType != "" {
		body["panic_type"] = d.PanicType
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
	if req != nil {
		body["request_id"] = req.RequestID
		ref := map[string]any{"schema": requestSchema, "id": req.RequestID}
		m["correlation_id"] = req.CorrelationID
		m["causation"] = ref
		m["basis"] = map[string]any{"kind": "observation", "ref": ref}
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
		m["basis"] = map[string]any{"kind": "observation", "ref": map[string]any{"schema": invocationSchema, "id": corr}}
	}
	content, err := canon.Encode(m)
	if err != nil {
		return store.Event{}, err
	}
	return store.Event{ID: eventID, Content: content}, nil
}
