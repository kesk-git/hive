package rule

import "github.com/kesk-git/hive/contract/canon"

// Digest is the resolved-rule digest: the digest of the canonical form of the
// rules array, as the response carries it.
func Digest(rules []Resolved) (string, error) {
	data, err := canon.Encode(Trees(rules))
	if err != nil {
		return "", err
	}
	return canon.FileDigest(data), nil
}
