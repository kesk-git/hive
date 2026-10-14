package entry

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/pool/internal/read"
)

// RegisterRuleResolve builds resolve of hive-rule into the registry, with the
// handler of the package that keeps the rule records, and the readers of the
// kinds rule and subject, which declare hive-pool read supported as the first
// reader does. resolve is read-only: it takes no lock for writing, records
// nothing and returns no receipt. The package registers them into the default
// registry from its init function, as the package that keeps the
// specification records does.
func RegisterRuleResolve(r *Registry, resolve Handler) {
	r.RegisterOperation("hive-rule", &cli.Operation{
		OperationDecl: envelope.OperationDecl{
			Name:           "resolve",
			State:          envelope.Supported,
			Mode:           envelope.Unary,
			Versions:       []string{"1.0"},
			RequestSchema:  "urn:hive:schema:ops:hive-rule.resolve.request:v1",
			ResponseSchema: "urn:hive:schema:ops:hive-rule.resolve.response:v2",
			ReceiptKinds:   []string{},
			Executes:       []string{},
			Calls:          []string{},
		},
		Handle: resolve,
	})
	read.RegisterRuleReaders(r.Reads)
}
