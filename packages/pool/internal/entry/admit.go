package entry

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
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
