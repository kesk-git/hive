// Package distribution holds the test-distribution target
// (spec/distribution.md, How it is proved): the checks that run against the
// artefact of the platform they run on. The artefact is assembled by
// `make assemble`, which this target never runs; the checks themselves are in
// the proof package and are proved against fixtures beside it.
package distribution
