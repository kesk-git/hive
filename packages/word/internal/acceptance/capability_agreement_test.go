package acceptance

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/kesk-git/hive/contract/canon"
	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/ids"
)

// The agreement of what each executable of the package says it has built with
// what the specification declares (spec/README.md: "Each executable's own
// `capabilities` answer declares which of its operations are built"; spec/
// commands.md, Capabilities): every operation the package's declaration lists
// is answered in `capabilities` as built, at a version and with the schemas the
// specification gives it, or is answered as not built and refuses with
// unsupported_operation. Which operations are left unbuilt is not written in
// this file: it is derived, for each operation, from the capabilities answer of
// the built executable and, independently, from what a request to the
// operation gets back.

// capAnswer is the capabilities answer of one command name.
func capAnswer(t testing.TB, dir, command string) *envelope.Capabilities {
	t.Helper()
	stdout, stderr, exit := run(t, filepath.Join(dir, command+suffix()), nil, "capabilities")
	resp, err := envelope.AcceptCapabilities([]byte(stdout), exit)
	if err != nil {
		t.Fatalf("%s capabilities: %v; exit %d, stderr %q", command, err, exit, stderr)
	}
	caps, err := envelope.DecodeCapabilities(resp.Body)
	if err != nil {
		t.Fatalf("%s capabilities: %v", command, err)
	}
	return caps
}

// capSchemaFiles are the request or response schema files the specification
// holds for the operation: the versions of the schema, by file name.
func capSchemaFiles(t testing.TB, command, operation, side string) map[int]string {
	t.Helper()
	pattern := filepath.Join("..", "..", "..", "..", "spec", "schemas", "ops", command+"."+operation+"."+side+".v*.schema.json")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int]string{}
	name := regexp.MustCompile(`\.v([0-9]+)\.schema\.json$`)
	for _, p := range paths {
		m := name.FindStringSubmatch(filepath.Base(p))
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		out[n] = p
	}
	return out
}

// capSchemaID reads the identity a schema file gives itself.
func capSchemaID(t testing.TB, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.Parse(data)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	doc, _ := v.(map[string]any)
	id, _ := doc["$id"].(string)
	return id
}

// capDeclaredSchema is the schema the specification declares for a side of an
// operation: the latest version of the schema file the specification holds for
// it, with the identity that file gives itself.
func capDeclaredSchema(t testing.TB, command, operation, side string) (version int, id string) {
	t.Helper()
	files := capSchemaFiles(t, command, operation, side)
	if len(files) == 0 {
		t.Fatalf("the specification holds no %s schema for %s %s", side, command, operation)
	}
	for n := range files {
		version = max(version, n)
	}
	return version, capSchemaID(t, files[version])
}

// TestCapabilitiesAnswerEachDeclaredOperationAsBuilt proves that, for every
// operation the package's declaration lists, the capabilities answer of the
// command that owns it says what the operation is, and that an operation it
// declares supported is declared at body versions and with request and response
// schemas the specification gives it: the schemas are the latest the
// specification holds for the operation under the names it gives them, and the
// major number of every version the build accepts is the version of the
// request schema.
//
// It catches an operation the declaration lists and the answer omits, one the
// answer lists twice or that the declaration does not list, an operation
// declared supported with no version, with a schema that is not the
// specification's, or with a body version of another major than its request
// schema, and a state that is none of the three.
func TestCapabilitiesAnswerEachDeclaredOperationAsBuilt(t *testing.T) {
	p := declaration(t)
	dir := delivered(t, p)
	for _, c := range p.Commands {
		t.Run(c.Name, func(t *testing.T) {
			caps := capAnswer(t, dir, c.Name)
			answered := map[string]envelope.OperationDecl{}
			for _, op := range caps.Operations {
				if _, twice := answered[op.Name]; twice {
					t.Errorf("%s is answered twice", op.Name)
				}
				answered[op.Name] = op
				if !slices.Contains(c.Operations, op.Name) {
					t.Errorf("%s answers the operation %s, which the declaration does not list", c.Name, op.Name)
				}
			}
			for _, name := range c.Operations {
				op, ok := answered[name]
				if !ok {
					t.Errorf("the declaration lists %s %s and the answer omits it", c.Name, name)
					continue
				}
				switch op.State {
				case envelope.Supported:
					capAgreeBuilt(t, c.Name, op)
				case envelope.Unsupported, envelope.Parked:
					if op.Reason == "" {
						t.Errorf("%s is %s with no reason", name, op.State)
					}
				default:
					t.Errorf("%s is in the state %q, which is none of supported, unsupported and parked", name, op.State)
				}
			}
		})
	}
}

// capAgreeBuilt holds an operation answered as supported to the version and
// schemas of the specification.
func capAgreeBuilt(t *testing.T, command string, op envelope.OperationDecl) {
	t.Helper()
	if len(op.Versions) == 0 {
		t.Errorf("%s is supported and accepts no body version", op.Name)
	}
	requestVersion, requestID := capDeclaredSchema(t, command, op.Name, "request")
	if op.RequestSchema != requestID {
		t.Errorf("%s names the request schema %q; the specification's is %q", op.Name, op.RequestSchema, requestID)
	}
	if want := fmt.Sprintf("urn:hive:schema:ops:%s.%s.request:v%d", command, op.Name, requestVersion); requestID != want {
		t.Errorf("%s: the specification's request schema is %q, which is not the name %q the operation's own gives it", op.Name, requestID, want)
	}
	responseVersion, responseID := capDeclaredSchema(t, command, op.Name, "response")
	if op.ResponseSchema != responseID {
		t.Errorf("%s names the response schema %q; the specification's is %q", op.Name, op.ResponseSchema, responseID)
	}
	if want := fmt.Sprintf("urn:hive:schema:ops:%s.%s.response:v%d", command, op.Name, responseVersion); responseID != want {
		t.Errorf("%s: the specification's response schema is %q, which is not the name %q the operation's own gives it", op.Name, responseID, want)
	}
	for _, v := range op.Versions {
		major, _, ok := strings.Cut(v, ".")
		if n, err := strconv.Atoi(major); !ok || err != nil || n != requestVersion {
			t.Errorf("%s accepts the body version %q; the major number of a version is the version of the request schema, %d", op.Name, v, requestVersion)
		}
	}
}

// capProbe is what a request to an operation got back.
type capProbe struct {
	exit   int
	status envelope.Status
	code   string
	body   map[string]any
	// receipts is how many receipts the response carries.
	receipts int
}

// capRequest is a request to the operation at the version, whose body breaks
// the operation's request schema whatever the operation is: it names a member
// that no request body has. A built operation answers it invalid before it
// reads the home; an operation that is not built, or is parked, refuses it
// before it reads the body.
func capRequest(t testing.TB, command, operation, version string) []byte {
	t.Helper()
	mint := func(p ids.Prefix) string {
		id, err := ids.Mint(clock.System{}, p)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	out, err := canon.Encode(map[string]any{
		"schema": "hive.request", "schema_version": "1.0",
		"command": command, "operation": operation, "operation_version": version,
		"request_id": mint(ids.Request), "correlation_id": mint(ids.Correlation),
		"issued_at": "2026-10-11T08:00:00.000Z", "caller": map[string]any{"kind": "test"},
		"scope_level": "hive", "home_id": mint(ids.Home),
		"authority": map[string]any{"kind": "none"},
		"body":      map[string]any{"no_such_member_of_any_request": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(out, "\n"...)
}

// capProbeOperation sends the request to the operation of the command, in a
// home that holds nothing, and reads the response.
func capProbeOperation(t testing.TB, dir, command, operation, version string) capProbe {
	t.Helper()
	home := t.TempDir()
	req := capRequest(t, command, operation, version)
	stdout, stderr, exit := run(t, filepath.Join(dir, command+suffix()), req, operation, "--home", home)
	resp, err := envelope.AcceptResponse([]byte(stdout), exit, requestID(t, req))
	if err != nil {
		t.Fatalf("%s %s: %v; exit %d, stderr %q", command, operation, err, exit, stderr)
	}
	if left, _ := os.ReadDir(home); len(left) != 0 {
		t.Errorf("%s %s wrote %d entries into the home", command, operation, len(left))
	}
	probe := capProbe{exit: exit, status: resp.Status, body: resp.Body, receipts: len(resp.Receipts)}
	if resp.Error != nil {
		probe.code = resp.Error.Code
	}
	return probe
}

// capAgree says whether what a request to an operation got back agrees with
// the state its capabilities answer gives it, and why not.
func capAgree(state envelope.OperationState, p capProbe) error {
	refusedWith := func(code string) bool {
		return p.exit == 4 && p.status == envelope.Refused && p.code == code
	}
	switch state {
	case envelope.Supported:
		// A built operation reads the request against its own schema.
		if refusedWith(envelope.CodeUnsupportedOperation) {
			return fmt.Errorf("the operation is declared supported and refuses as unsupported_operation")
		}
		if p.exit != 3 || p.status != envelope.Invalid || p.code != envelope.CodeSchemaViolation {
			return fmt.Errorf("the operation is declared supported and answers exit %d, %s, %q instead of its own schema's violation", p.exit, p.status, p.code)
		}
	case envelope.Unsupported:
		if !refusedWith(envelope.CodeUnsupportedOperation) {
			return fmt.Errorf("the operation is declared unsupported and answers exit %d, %s, %q instead of the refusal unsupported_operation", p.exit, p.status, p.code)
		}
		if p.body != nil || p.receipts != 0 {
			return fmt.Errorf("the refusal of an operation that is not built leaves a body %v or %d receipts", p.body, p.receipts)
		}
	case envelope.Parked:
		if !refusedWith(envelope.CodeOperationParked) {
			return fmt.Errorf("the operation is declared parked and answers exit %d, %s, %q instead of the refusal operation_parked", p.exit, p.status, p.code)
		}
	default:
		return fmt.Errorf("the operation is declared in the state %q, which is none of supported, unsupported and parked", state)
	}
	return nil
}

// TestEveryDeclaredOperationIsBuiltOrRefusedAsUnsupported proves, for every
// operation the package's declaration lists, that it is either built or
// refused: what its capabilities answer says of it is what a request to it
// gets back. An operation answered supported reads the request against its own
// schema; one answered unsupported is refused with unsupported_operation, exit
// 4, with no body and no receipt, and writes nothing; one answered parked is
// refused with operation_parked. The operations left unbuilt are not listed
// here: they are the ones the answer does not declare supported, and the test
// logs them.
//
// It catches an operation declared and neither built nor refused, one the
// answer calls built and the executable refuses, one the answer calls unbuilt
// and the executable runs, and a refusal that leaves a body, a receipt or a
// file.
func TestEveryDeclaredOperationIsBuiltOrRefusedAsUnsupported(t *testing.T) {
	p := declaration(t)
	dir := delivered(t, p)
	var built, unbuilt []string
	for _, c := range p.Commands {
		caps := capAnswer(t, dir, c.Name)
		states := map[string]envelope.OperationDecl{}
		for _, op := range caps.Operations {
			states[op.Name] = op
		}
		for _, name := range c.Operations {
			t.Run(c.Name+" "+name, func(t *testing.T) {
				op, ok := states[name]
				if !ok {
					t.Fatalf("the declaration lists %s %s and the capabilities answer omits it", c.Name, name)
				}
				version := "1.0"
				if len(op.Versions) > 0 {
					version = op.Versions[0]
				}
				if err := capAgree(op.State, capProbeOperation(t, dir, c.Name, name, version)); err != nil {
					t.Error(err)
				}
			})
			if op, ok := states[name]; ok && op.State == envelope.Supported {
				built = append(built, c.Name+" "+name)
			} else {
				unbuilt = append(unbuilt, c.Name+" "+name)
			}
		}
	}
	t.Logf("declared operations built: %d", len(built))
	t.Logf("declared operations not built: %d %v", len(unbuilt), unbuilt)
	var declared int
	for _, c := range p.Commands {
		declared += len(c.Operations)
	}
	if len(built)+len(unbuilt) != declared {
		t.Errorf("%d built and %d not built are not the %d operations the declaration lists", len(built), len(unbuilt), declared)
	}
}

// TestACapabilitiesStateThatTheExecutableDoesNotKeepIsFound proves that the
// comparison above fails on each disagreement it exists to find: an operation
// that is neither built nor refused is not accepted, whichever state the
// answer gave it.
func TestACapabilitiesStateThatTheExecutableDoesNotKeepIsFound(t *testing.T) {
	invalid := capProbe{exit: 3, status: envelope.Invalid, code: envelope.CodeSchemaViolation}
	unsupported := capProbe{exit: 4, status: envelope.Refused, code: envelope.CodeUnsupportedOperation}
	parked := capProbe{exit: 4, status: envelope.Refused, code: envelope.CodeOperationParked}
	ran := capProbe{exit: 0, status: envelope.Ok}
	for _, c := range []struct {
		name  string
		state envelope.OperationState
		probe capProbe
		agree bool
	}{
		{"built and reading its schema", envelope.Supported, invalid, true},
		{"unsupported and refused", envelope.Unsupported, unsupported, true},
		{"parked and refused as parked", envelope.Parked, parked, true},
		{"supported and refused as unsupported", envelope.Supported, unsupported, false},
		{"supported and run on an invalid body", envelope.Supported, ran, false},
		{"unsupported and reading its schema", envelope.Unsupported, invalid, false},
		{"unsupported and run", envelope.Unsupported, ran, false},
		{"unsupported and refused with a body", envelope.Unsupported, capProbe{exit: 4, status: envelope.Refused, code: envelope.CodeUnsupportedOperation, body: map[string]any{"x": true}}, false},
		{"unsupported and refused with a receipt", envelope.Unsupported, capProbe{exit: 4, status: envelope.Refused, code: envelope.CodeUnsupportedOperation, receipts: 1}, false},
		{"parked and refused as unsupported", envelope.Parked, unsupported, false},
		{"in a state of no name", envelope.OperationState("unknown"), invalid, false},
	} {
		if err := capAgree(c.state, c.probe); (err == nil) != c.agree {
			t.Errorf("%s: agreement = %v, want %v (%v)", c.name, err == nil, c.agree, err)
		}
	}
}
