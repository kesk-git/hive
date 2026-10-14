package specification

import (
	"slices"

	"github.com/kesk-git/hive/contract/canon"
)

// What a revision is compared by. Invalidation is decided from the criteria of
// a revision, by identity, text and evidence, and the subjects it names
// (spec/pool.md, Specifications); the requirements a criterion answers, the
// prose, the product rules and the plan stage are not compared.

// criterionKey is the identity, text and evidence of a criterion, as one
// comparable string.
func criterionKey(c map[string]any) string {
	b, _ := canon.Encode(map[string]any{"criterion_id": c["criterion_id"], "text": c["text"], "evidence": c["evidence"]})
	return string(b)
}

// criteriaKeys are the criteria of a revision, as a sorted list of keys: a
// reordering of the same criteria is no change.
func criteriaKeys(rev map[string]any) []string {
	var keys []string
	for _, v := range listOf(rev["criteria"]) {
		keys = append(keys, criterionKey(mapOf(v)))
	}
	slices.Sort(keys)
	return keys
}

// subjectSet are the subjects a revision names, sorted and without repeats; a
// revision with no list names none.
func subjectSet(rev map[string]any) []string {
	var subjects []string
	for _, v := range listOf(rev["touched_subjects"]) {
		if s, ok := v.(string); ok {
			subjects = append(subjects, s)
		}
	}
	slices.Sort(subjects)
	return slices.Compact(subjects)
}

// sameCriteriaAndSubjects reports whether the two revisions have the same
// criteria by identity, text and evidence and name the same subjects: the
// revisions an acceptance of one survives the other.
func sameCriteriaAndSubjects(a, b map[string]any) bool {
	return slices.Equal(criteriaKeys(a), criteriaKeys(b)) && slices.Equal(subjectSet(a), subjectSet(b))
}

// changedCriteria are the identities of the criteria whose text or evidence
// differs from the revision before, or that the revision adds or drops, as
// information; invalidation does not read them. The criteria of the new
// revision come first, in its order, and then those it drops.
func changedCriteria(before, after map[string]any) []any {
	old := map[string]string{}
	for _, v := range listOf(before["criteria"]) {
		c := mapOf(v)
		old[c["criterion_id"].(string)] = criterionKey(c)
	}
	out := []any{}
	seen := map[string]bool{}
	for _, v := range listOf(after["criteria"]) {
		c := mapOf(v)
		id := c["criterion_id"].(string)
		seen[id] = true
		if key, had := old[id]; !had || key != criterionKey(c) {
			if !slices.Contains(out, any(id)) {
				out = append(out, id)
			}
		}
	}
	for _, v := range listOf(before["criteria"]) {
		id := mapOf(v)["criterion_id"].(string)
		if !seen[id] && !slices.Contains(out, any(id)) {
			out = append(out, id)
		}
	}
	return out
}

// revisionDigest is the digest of a revision: the canonical form of its text
// and criteria, with touched_subjects, product_rules and plan_stage where the
// revision has them. A member the revision lacks is left out of the form.
func revisionDigest(rev map[string]any) (string, error) {
	form := map[string]any{"text": rev["text"], "criteria": rev["criteria"]}
	for _, name := range []string{"touched_subjects", "product_rules", "plan_stage"} {
		if v, ok := rev[name]; ok {
			form[name] = v
		}
	}
	data, err := canon.Encode(form)
	if err != nil {
		return "", err
	}
	return canon.FileDigest(data), nil
}

// revisionNamed finds the revision of the specification record by its number.
func revisionNamed(rec map[string]any, n int64) map[string]any {
	for _, v := range listOf(rec["revisions"]) {
		if r := mapOf(v); r["revision"] == n {
			return r
		}
	}
	return nil
}

// bindingOf is the specification an item is bound to by its own accepted
// binding: the specification, the revision and the digest, with ok false for
// an item with none.
func bindingOf(item map[string]any) (specification string, revision int64, digest string, ok bool) {
	b := mapOf(item["specification"])
	if b == nil {
		return "", 0, "", false
	}
	specification, _ = b["specification_id"].(string)
	revision, _ = b["revision"].(int64)
	digest, _ = b["digest"].(string)
	return specification, revision, digest, true
}
