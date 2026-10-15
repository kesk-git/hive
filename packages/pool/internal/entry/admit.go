package entry

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/read"
)

// RegisterApprove builds approve of hive-admit into the registry, with the
// handler of the package that keeps the admission records. The package
// registers it into the default registry from its init function, as the package
// that keeps the legacy backlog registers its two operations.
func RegisterApprove(r *Registry, approve Handler) {
	r.RegisterOperation("hive-admit", &cli.Operation{
		OperationDecl: envelope.OperationDecl{
			Name:           "approve",
			State:          envelope.Supported,
			Mode:           envelope.Unary,
			Mutates:        true,
			Versions:       []string{"1.0"},
			RequestSchema:  "urn:hive:schema:ops:hive-admit.approve.request:v1",
			ResponseSchema: "urn:hive:schema:ops:hive-admit.approve.response:v1",
			ReceiptKinds:   []string{"pool.admission.approved"},
			Executes:       []string{},
			Calls:          []string{},
		},
		Handle: approve,
	})
}

// RegisterAdmit builds create and check of hive-admit into the registry, with
// the handlers of the package that keeps the admission records, and the reader
// of the kind admission, which declares hive-pool read supported as the first
// reader does. Both are declared at 1.0, their v1 schemas. check writes one of
// three receipts, whichever its result is, so it declares all three. The
// package registers them from its init function, beside approve.
func RegisterAdmit(r *Registry, create, check Handler) {
	for _, op := range []struct {
		name     string
		handle   Handler
		receipts []string
	}{
		{"create", create, []string{"pool.admission.created"}},
		{"check", check, []string{"pool.admission.admitted", "pool.admission.refused", "pool.admission.held"}},
	} {
		r.RegisterOperation("hive-admit", &cli.Operation{
			OperationDecl: envelope.OperationDecl{
				Name:           op.name,
				State:          envelope.Supported,
				Mode:           envelope.Unary,
				Mutates:        true,
				Versions:       []string{"1.0"},
				RequestSchema:  "urn:hive:schema:ops:hive-admit." + op.name + ".request:v1",
				ResponseSchema: "urn:hive:schema:ops:hive-admit." + op.name + ".response:v1",
				ReceiptKinds:   op.receipts,
				Executes:       []string{},
				Calls:          []string{},
			},
			Handle: op.handle,
		})
	}
	read.RegisterAdmissionReader(r.Reads)
}
