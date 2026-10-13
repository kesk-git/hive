// Command hive-callee is the test-only callee of the cross-package call proof
// (spec/test-targets.md, test-integration). It is no part of Hive: it is
// built by the tests under integration/calls/, from this source, and started
// by them through the one process-execution path, so that the transport of
// the command contract (the request and response envelopes, every exit
// state, replay and the size limits) is proved end to end where no real
// operation exists yet.
//
// It is an executable of the contract library's own harness, as every
// package's is, and acts as the command name it was started under:
//
//	hive-callee        the default limits of the contract
//	hive-callee-small  a request and a response limit of 1,048,576 bytes,
//	                   below the record the size proof sends
//
// Both own the same five operations, whose bodies are open objects:
//
//	echo    read-only; answers the string `record` it was sent
//	expand  read-only; answers a record of `bytes` bytes, from a request that
//	        is small
//	state   read-only; ends with the exit state `status` names, to prove each
//	        state the harness does not itself reach
//	put     mutating; writes the record `name` with the `record` it was sent
//	        and one receipt, and answers the record, to prove replay
//	grow    mutating; writes the record `name` and one receipt, and answers a
//	        record of `bytes` bytes, from a request that is small, to prove
//	        that a response too large to return leaves nothing behind
//
// It claims the package name of a store the home table gives, shadow, because
// a store opens for no other name; it writes into the home it is pointed at
// and nowhere else, and the tests give it a home of their own.
package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/store"
)

const (
	version = "0.0.0-test"
	// pkg is the package name the callee's store opens under.
	pkg = "shadow"
	// bodySchema admits any object: the callee's bodies are open.
	bodySchema = "urn:hive:schema:common:v1"
	// smallLimit is the request and response limit of hive-callee-small.
	smallLimit = 1048576
	// maxExpand is the largest record expand makes.
	maxExpand = 8 << 20
)

func main() { os.Exit(cli.Run(executable())) }

// executable describes the callee.
func executable() *cli.Executable {
	return &cli.Executable{
		Package: pkg,
		Name:    "hive-callee",
		Version: version,
		Commands: []*cli.Command{
			{Name: "hive-callee", Operations: operations()},
			{Name: "hive-callee-small", Limits: envelope.Limits{MaxRequestBytes: smallLimit, MaxResponseBytes: smallLimit}, Operations: operations()},
		},
	}
}

func declared(name string, mutates bool) envelope.OperationDecl {
	d := envelope.OperationDecl{
		Name: name, State: envelope.Supported, Mode: envelope.Unary, Mutates: mutates,
		Versions: []string{"1.0"}, RequestSchema: bodySchema, ResponseSchema: bodySchema,
		ReceiptKinds: []string{}, Executes: []string{}, Calls: []string{},
	}
	if mutates {
		d.ReceiptKinds = []string{"shadow.callee.put"}
	}
	return d
}

func operations() []*cli.Operation {
	return []*cli.Operation{
		{OperationDecl: declared("echo", false), Handle: echo},
		{OperationDecl: declared("expand", false), Handle: expand},
		{OperationDecl: declared("state", false), Handle: state},
		{OperationDecl: declared("put", true), Handle: put},
		{OperationDecl: declared("grow", true), Handle: grow},
	}
}

func invalid(format string, args ...any) error {
	return envelope.Common(envelope.CodeSchemaViolation, fmt.Sprintf(format, args...))
}

// echo answers the record it was sent.
func echo(c *cli.Call) (*cli.Result, error) {
	rec, ok := c.Request.Body["record"].(string)
	if !ok {
		return nil, invalid("body.record is a string")
	}
	return &cli.Result{Body: map[string]any{"record": rec}}, nil
}

// expand answers a record of body.bytes bytes: pattern(n).
func expand(c *cli.Call) (*cli.Result, error) {
	n, ok := c.Request.Body["bytes"].(int64)
	if !ok || n < 0 || n > maxExpand {
		return nil, invalid("body.bytes is a whole number from 0 to %d", maxExpand)
	}
	return &cli.Result{Body: map[string]any{"record": pattern(int(n))}}, nil
}

// pattern is n bytes of ASCII in which every sixteen bytes carry their own
// position.
func pattern(n int) string {
	var b strings.Builder
	b.Grow(n + 16)
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, "%08x-record!", i)
	}
	return b.String()[:n]
}

// state ends with the exit state body.status names. The states the harness
// reaches by itself (usage, invalid, too_large) are reached by the tests
// the way a caller reaches them, and are not made here.
func state(c *cli.Call) (*cli.Result, error) {
	status, _ := c.Request.Body["status"].(string)
	const msg = "the callee was asked to end with this state"
	switch status {
	case "ok":
		return &cli.Result{Body: map[string]any{"status": status}}, nil
	case "failed":
		return nil, envelope.Common(envelope.CodeInternal, msg)
	case "refused":
		return nil, envelope.Common(envelope.CodeAuthorityMissing, msg)
	case "conflict":
		return nil, envelope.Common(envelope.CodeRevisionConflict, msg)
	case "not_found":
		return nil, envelope.Common(envelope.CodeNotFound, msg)
	case "unavailable":
		return nil, envelope.Common(envelope.CodeDependencyMissing, msg)
	case "unknown":
		return nil, envelope.Common(envelope.CodeOutcomeUnknown, msg)
	case "held":
		return nil, envelope.Hold(envelope.RecordRef{Schema: "shadow.hold", ID: "hold-1"}, msg)
	}
	return nil, invalid("body.status names a state the callee makes: ok, failed, refused, conflict, not_found, unavailable, unknown or held")
}

var nameForm = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// put writes the record body.name, which must not exist, holding body.record,
// with one receipt, and answers the record.
func put(c *cli.Call) (*cli.Result, error) {
	rec, _ := c.Request.Body["record"].(string)
	return write(c, rec, rec)
}

// grow writes the record body.name, which must not exist, with one receipt,
// and answers a record of body.bytes bytes: pattern(n).
func grow(c *cli.Call) (*cli.Result, error) {
	n, ok := c.Request.Body["bytes"].(int64)
	if !ok || n < 0 || n > maxExpand {
		return nil, invalid("body.bytes is a whole number from 0 to %d", maxExpand)
	}
	return write(c, "", pattern(int(n)))
}

// write is the one write of put and grow: the record named by body.name,
// holding stored, one receipt, and the answer record.
func write(c *cli.Call, stored, answer string) (*cli.Result, error) {
	name, ok := c.Request.Body["name"].(string)
	if !ok || !nameForm.MatchString(name) || len(name) > 64 {
		return nil, invalid("body.name is a lowercase word of letters, digits and hyphens")
	}
	content, err := canon.Encode(map[string]any{"schema": "shadow.callee", "schema_version": "1.0", "name": name, "record": stored})
	if err != nil {
		return nil, err
	}
	receipt, err := c.NewReceipt("shadow.callee.put", "1.0", &envelope.RecordRef{Schema: "shadow.callee", ID: name}, nil, nil)
	if err != nil {
		return nil, err
	}
	return &cli.Result{
		Body:     map[string]any{"wrote": name, "record": answer},
		Records:  []store.RecordWrite{{Path: store.RecordPath(pkg, "callee", name), Content: content, Expected: 0}},
		Receipts: []envelope.Receipt{receipt},
	}, nil
}
