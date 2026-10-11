// Package envelope carries the request and response envelopes of the command
// harness (spec/commands.md), the receipt envelope they carry
// (spec/receipts.md) and the capability declaration a command answers with.
//
// Every document is read strictly: the bytes are decoded as a Hive record,
// held to the embedded schema and then taken into a typed value, so a field
// the schema does not name, a number that is not whole or a version newer
// than this build never reaches a command. Every document is written in
// canonical form with one final line feed, and is checked against its schema
// before it is written, so a command never answers with a response it would
// not itself accept.
//
// The eleven exit states are one table here: each Status has one exit code,
// each exit code one Status, and each common error code one Status. A
// Failure is the Go error a command returns to end an invocation with a
// status other than ok; the harness turns it into the response's error.
package envelope
