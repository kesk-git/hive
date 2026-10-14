package poolfake

import "fmt"

// Round is a review round's feedback file as the fake holds it for the tests
// of a review submission (spec/review.md, round-NNNN.feedback.json): a
// workspace-scope file of one review round, with the events given.
type Round struct {
	HomeID, WorkspaceID string
	TaskID              string
	Generation          int64
	ReviewID            string
	RoundID             int64
	Events              []map[string]any
}

// Path is the file's path below the home.
func (r Round) Path() string {
	return fmt.Sprintf("reviews/%s/%d/%s/round-%04d.feedback.json", r.TaskID, r.Generation, r.ReviewID, r.RoundID)
}

// Tree is the file, valid against the feedback schema.
func (r Round) Tree() map[string]any {
	events := make([]any, len(r.Events))
	for i, e := range r.Events {
		events[i] = e
	}
	return map[string]any{
		"schema": "hive.review-feedback", "schema_version": "1.0", "record_revision": int64(1),
		"review_id": r.ReviewID, "round_id": r.RoundID,
		"scope_level": "workspace", "home_id": r.HomeID, "workspace_id": r.WorkspaceID,
		"task_id": r.TaskID, "generation": r.Generation,
		"owner":  map[string]any{"role": "fleet", "home_id": r.HomeID, "workspace_id": r.WorkspaceID},
		"lictor": map[string]any{"task_id": r.TaskID, "generation": r.Generation},
		"reviewed_revision": map[string]any{
			"revision": int64(1), "path": "revision-0001.html",
			"digest": "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		},
		"events": events, "updated_at": "2026-10-11T08:02:00.250Z",
	}
}

// Event is the event of the kind, at the sequence, with the member the kind
// holds.
func Event(id string, sequence int64, kind string, member map[string]any) map[string]any {
	e := map[string]any{
		"event_id": id, "sequence": sequence,
		"captured_at": "2026-10-11T08:01:00.000Z", "received_at": "2026-10-11T08:01:00.250Z",
		"operator": map[string]any{"login": "local-session", "ref": "ui-session-0001"},
		"kind":     kind,
	}
	switch kind {
	case "submit":
		e["control_label"], e["submission"] = "Submit feedback", member
	case "card-draft":
		e["card_draft"] = member
	}
	return e
}

// Submission is the submission member of a submit event carrying the answers.
func Submission(requestID string, answers ...map[string]any) map[string]any {
	list := make([]any, len(answers))
	for i, a := range answers {
		list[i] = a
	}
	return map[string]any{
		"request_id": requestID, "expected_round_revision": int64(1), "last_draft_sequence": int64(0),
		"answers": list, "notes": []any{}, "attachments": []any{},
	}
}
