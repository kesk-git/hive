// Package specification builds the Pool's specifications and the effective
// acceptance of an item's criteria (spec/pool.md, Specifications): the
// operations define, accept and close of hive-spec, on the transaction
// boundary of the store. The package registers them from its init function,
// so that importing it builds them into the executable.
package specification
