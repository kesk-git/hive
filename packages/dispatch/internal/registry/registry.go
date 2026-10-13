// Package registry is the identity registry of the dispatch package: the home,
// workspace, project and clone records of spec/identities.md, and the one
// operation of stage 1 that creates them, hive-fleet provision.
//
// Provision creates identity records in the one home it is pointed at and does
// nothing else: no charter, no parent channel, no session and no write to any
// other home. Every record it writes is an entry of one transaction of the
// store, expected to be absent, with one receipt per record created, so that a
// request sent again finds what the first wrote and writes nothing more.
package registry

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
)

// The command and operation this package builds, and the body version and the
// schemas of its request and response.
const (
	Command   = "hive-fleet"
	Operation = "provision"

	BodyVersion    = "1.0"
	RequestSchema  = "urn:hive:schema:ops:hive-fleet.provision.request:v1"
	ResponseSchema = "urn:hive:schema:ops:hive-fleet.provision.response:v1"
)

// The receipt kinds the operation writes, one per record created
// (spec/receipts.md, the kinds registered at stage 1).
const (
	homeCreated         = "dispatch.home.created"
	workspaceRegistered = "dispatch.workspace.registered"
	projectRegistered   = "dispatch.project.registered"
	cloneRegistered     = "dispatch.clone.registered"
)

// Provision is the operation hive-fleet provision: supported, mutating, and
// the one operation admitted to a home directory that holds no home record
// yet. It runs Git, to read a clone, and calls no other package.
func Provision() *cli.Operation {
	return &cli.Operation{
		OperationDecl: envelope.OperationDecl{
			Name:           Operation,
			State:          envelope.Supported,
			Mode:           envelope.Unary,
			Mutates:        true,
			Versions:       []string{BodyVersion},
			RequestSchema:  RequestSchema,
			ResponseSchema: ResponseSchema,
			ReceiptKinds:   []string{homeCreated, workspaceRegistered, projectRegistered, cloneRegistered},
			Executes:       []string{gitName},
			Calls:          []string{},
		},
		Provisions: true,
		Handle:     provision,
	}
}
