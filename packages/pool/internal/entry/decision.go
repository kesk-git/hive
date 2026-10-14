package entry

import "github.com/kesk-git/hive/packages/pool/internal/read"

// RegisterDecisionReads builds the readers of the kinds question,
// decision-round and request-outcome into the registry, which declares
// hive-pool read supported as the first reader does. The package that keeps
// the decision records registers them into the default registry from its init
// function.
func RegisterDecisionReads(r *Registry) {
	read.RegisterDecisionReaders(r.Reads)
	read.RegisterOutcomeReader(r.Reads)
}
