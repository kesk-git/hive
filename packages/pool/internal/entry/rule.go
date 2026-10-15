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

// RegisterRuleWriters builds record, amend and expire of hive-rule into the
// registry, with the handlers of the package that writes the rule records. record
// is declared at 2.0, its v2 request; amend and expire at 1.0 like resolve, their
// unchanged v1 request bodies. Each writes the rule and re-renders the projections
// of its scope in the same transaction, so each declares the receipt of the
// rendering beside its own.
func RegisterRuleWriters(r *Registry, record, amend, expire Handler) {
	for _, op := range []struct {
		name, version, request, response string
		handle                           Handler
		receipts                         []string
	}{
		{"record", "2.0", "record.request:v2", "record.response:v2", record, []string{"pool.rule.recorded", "pool.projection.rendered"}},
		{"amend", "1.0", "amend.request:v1", "amend.response:v2", amend, []string{"pool.rule.amended", "pool.rule.lifted", "pool.projection.rendered"}},
		{"expire", "1.0", "expire.request:v1", "expire.response:v2", expire, []string{"pool.rule.expired", "pool.projection.rendered"}},
	} {
		r.RegisterOperation("hive-rule", &cli.Operation{
			OperationDecl: envelope.OperationDecl{
				Name:           op.name,
				State:          envelope.Supported,
				Mode:           envelope.Unary,
				Mutates:        true,
				Versions:       []string{op.version},
				RequestSchema:  "urn:hive:schema:ops:hive-rule." + op.request,
				ResponseSchema: "urn:hive:schema:ops:hive-rule." + op.response,
				ReceiptKinds:   op.receipts,
				Executes:       []string{},
				Calls:          []string{},
			},
			Handle: op.handle,
		})
	}
}
