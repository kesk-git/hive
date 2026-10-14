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
//
// A fourth part is the owner's no-value check of the design's Secrets section:
// the fixed list of fifteen credential shapes, six vendor tokens of an exact
// prefix, length and alphabet and nine private-key opening lines, each named
// (credential_shapes.go, spec/records.md "Encoding"). ValidateOwned refuses a
// document that holds a string of a listed shape, in a value or a member name,
// before it applies the schema; the error names the nearest declared field and
// the shape and never the text. The list changes only as a contract change. It
// recognizes nothing by entropy, by prefix alone or by field name, redacts and
// rewrites nothing, and makes no validity or network check.
package schema
