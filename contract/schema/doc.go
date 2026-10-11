// Package schema is the contract library's validator for Hive's record
// schemas: JSON Schema draft 2020-12 restricted to the profile of
// spec/records.md ("Schema notation"), implemented with the standard library
// alone.
//
// Three parts make it up. Decode reads a JSON document the way a Hive record
// is read: strictly, keeping every string whole and admitting only whole
// numbers. Load reads a directory of schema files, holds each to the profile,
// resolves every reference and refuses a reference that cannot end. A Registry
// then validates documents against the schemas it holds.
package schema
