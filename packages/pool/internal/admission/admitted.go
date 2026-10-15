package admission

// IsAdmitted reports whether the admission is in state admitted, the result of
// its last check.
func (r *Record) IsAdmitted() bool {
	state, _ := r.Data["state"].(string)
	return state == "admitted"
}

// LastCheckRead is the revision of the item and the revision of the Imperative
// that the last check of the admission read. It reports false where the
// admission has no check, or where its last check does not carry both.
func (r *Record) LastCheckRead() (item, imperative int64, ok bool) {
	checks, _ := r.Data["checks"].([]any)
	if len(checks) == 0 {
		return 0, 0, false
	}
	last, _ := checks[len(checks)-1].(map[string]any)
	item, itemOK := last["item_revision"].(int64)
	imperative, impOK := last["imperative_revision"].(int64)
	return item, imperative, itemOK && impOK
}
