package rule

import (
	"time"

	"github.com/kesk-git/hive/contract/clock"
)

// EffectiveExpiry is the expiry time the clock reads for the rule: the new
// expiry of its latest extend or end amendment, by sequence, in place of the
// original, which is never changed; otherwise the original. A note or a lift
// carries no expiry and leaves the one before it. ok is false for a rule with
// none, and an error is a time that is not a calendar instant.
func (r *Rule) EffectiveExpiry() (at time.Time, ok bool, err error) {
	text := r.ExpiresAt
	var latest int64
	for _, a := range r.Amendments {
		if a.NewExpiresAt != "" && a.Sequence >= latest {
			text, latest = a.NewExpiresAt, a.Sequence
		}
	}
	if text == "" {
		return time.Time{}, false, nil
	}
	at, err = clock.Parse(text)
	return at, err == nil, err
}

// ExpiredAt reports whether the rule is past its expiry time at the instant,
// by the clock alone: from the expiry time on, whether or not an expiry
// receipt exists.
func (r *Rule) ExpiredAt(at time.Time) (bool, error) {
	expiry, ok, err := r.EffectiveExpiry()
	return ok && !at.Before(expiry), err
}

// Lifted reports whether the rule's lift is honoured. A lift is honoured only
// by its recorded receipt: a lift amendment that carries the receipt as the
// request supplied it, whole, whose kind is the kind the rule's lift
// condition names and whose subject is the record the condition names. A lift
// amendment without the receipt, or with another kind or another subject, and
// a rule with no lift condition, honour nothing.
func (r *Rule) Lifted() bool {
	if r.Lift == nil {
		return false
	}
	for _, a := range r.Amendments {
		if a.Kind == AmendmentLift && a.Receipt != nil && a.Receipt["kind"] == r.Lift.ReceiptKind && meets(r.Lift.Subject, a.Receipt["subject"]) {
			return true
		}
	}
	return false
}

// meets reports whether the subject a receipt is about is the record the lift
// condition names: the same schema and identity, and the same home, record
// revision and digest wherever the condition names one.
func meets(want map[string]any, got any) bool {
	have, ok := got.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range []string{"schema", "id", "home_id", "record_revision", "digest"} {
		if w, given := want[key]; (given || key == "schema" || key == "id") && have[key] != w {
			return false
		}
	}
	return true
}
