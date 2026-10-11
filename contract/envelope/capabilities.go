package envelope

import "fmt"

// Size limits of the command contract (spec/commands.md, Size and
// Invocation).
const (
	// MaxArgumentBytes is the longest argument a command accepts.
	MaxArgumentBytes = 4096
	// DefaultMaxBytes is the default limit on a request and on a response.
	DefaultMaxBytes = 67108864
)

// Limits are the limits a command declares. A zero value takes the default.
type Limits struct {
	MaxArgumentBytes int64
	MaxRequestBytes  int64
	MaxResponseBytes int64
}

// DefaultLimits are the limits of a command that declares none.
func DefaultLimits() Limits {
	return Limits{MaxArgumentBytes, DefaultMaxBytes, DefaultMaxBytes}
}

// Resolved fills each zero limit with its default.
func (l Limits) Resolved() Limits {
	d := DefaultLimits()
	if l.MaxArgumentBytes == 0 {
		l.MaxArgumentBytes = d.MaxArgumentBytes
	}
	if l.MaxRequestBytes == 0 {
		l.MaxRequestBytes = d.MaxRequestBytes
	}
	if l.MaxResponseBytes == 0 {
		l.MaxResponseBytes = d.MaxResponseBytes
	}
	return l
}

// OperationState is whether an operation can be called.
type OperationState string

// The states of an operation.
const (
	// Supported: callable.
	Supported OperationState = "supported"
	// Unsupported: declared by the contract, not callable in this build.
	// Calling it is refused with unsupported_operation.
	Unsupported OperationState = "unsupported"
	// Parked: built, and held closed by a recorded decision. Calling it is
	// refused with operation_parked.
	Parked OperationState = "parked"
)

// Mode is how an operation answers.
type Mode string

// The modes of an operation.
const (
	// Unary: one request, one response, then exit.
	Unary Mode = "unary"
	// Resident: the response is written once the operation has durably
	// started, and the process then runs until stopped.
	Resident Mode = "resident"
)

// OperationDecl is the declaration of one operation in a capability
// declaration: everything but capabilities itself.
type OperationDecl struct {
	Name  string
	State OperationState
	// Reason is given with unsupported and parked.
	Reason  string
	Mode    Mode
	Mutates bool
	// Versions are the body versions this build accepts; at least one for a
	// supported operation.
	Versions []string
	// RequestSchema and ResponseSchema name the schemas of the bodies; both
	// are given for a supported operation.
	RequestSchema  string
	ResponseSchema string
	ReceiptKinds   []string
	Executes       []string
	Calls          []string
}

// Capabilities is the body of every command's capabilities response
// (spec/commands.md, Capabilities).
type Capabilities struct {
	Command               string
	Package               string
	Executable            string
	ContractVersion       string
	ImplementationVersion string
	// SourceRevision is the revision of the repository the executable was
	// built from, where known.
	SourceRevision string
	Limits         Limits
	Operations     []OperationDecl
}

// Tree is the declaration as a JSON tree: the body of the capabilities
// response.
func (c *Capabilities) Tree() map[string]any {
	limits := c.Limits.Resolved()
	ops := make([]any, 0, len(c.Operations))
	for _, op := range c.Operations {
		m := map[string]any{
			"name":          op.Name,
			"state":         string(op.State),
			"mode":          string(op.Mode),
			"mutates":       op.Mutates,
			"versions":      stringsTree(op.Versions),
			"receipt_kinds": stringsTree(op.ReceiptKinds),
			"executes":      stringsTree(op.Executes),
			"calls":         stringsTree(op.Calls),
		}
		put(m, "reason", op.Reason)
		put(m, "request_schema", op.RequestSchema)
		put(m, "response_schema", op.ResponseSchema)
		ops = append(ops, m)
	}
	m := map[string]any{
		"schema":                 CapabilitiesSchema,
		"schema_version":         ContractVersion,
		"command":                c.Command,
		"package":                c.Package,
		"executable":             c.Executable,
		"contract_version":       c.ContractVersion,
		"implementation_version": c.ImplementationVersion,
		"limits": map[string]any{
			"max_argument_bytes": limits.MaxArgumentBytes,
			"max_request_bytes":  limits.MaxRequestBytes,
			"max_response_bytes": limits.MaxResponseBytes,
		},
		"operations": ops,
	}
	put(m, "source_revision", c.SourceRevision)
	return m
}

// Check checks the declaration against capabilities.v1.
func (c *Capabilities) Check() error {
	return validateTree(c.Tree(), CapabilitiesID)
}

// Encode writes the declaration in canonical form with one final line feed,
// after checking it against capabilities.v1.
func (c *Capabilities) Encode() ([]byte, error) {
	return encodeDocument(c.Tree(), CapabilitiesID)
}

// Operation returns the declaration of the named operation, or nil.
func (c *Capabilities) Operation(name string) *OperationDecl {
	for i := range c.Operations {
		if c.Operations[i].Name == name {
			return &c.Operations[i]
		}
	}
	return nil
}

// DecodeCapabilities reads a capability declaration from the body of a
// capabilities response, checking it against capabilities.v1 first.
func DecodeCapabilities(body map[string]any) (*Capabilities, error) {
	if err := validateTree(body, CapabilitiesID); err != nil {
		return nil, err
	}
	f := fields{body, "capabilities"}
	c := &Capabilities{}
	var err error
	if c.Command, err = f.str("command"); err != nil {
		return nil, err
	}
	if c.Package, err = f.str("package"); err != nil {
		return nil, err
	}
	if c.Executable, err = f.str("executable"); err != nil {
		return nil, err
	}
	if c.ContractVersion, err = f.str("contract_version"); err != nil {
		return nil, err
	}
	if c.ImplementationVersion, err = f.str("implementation_version"); err != nil {
		return nil, err
	}
	if c.SourceRevision, err = f.optStr("source_revision"); err != nil {
		return nil, err
	}
	limits, err := f.object("limits")
	if err != nil {
		return nil, err
	}
	if c.Limits.MaxArgumentBytes, err = limits.whole("max_argument_bytes"); err != nil {
		return nil, err
	}
	if c.Limits.MaxRequestBytes, err = limits.whole("max_request_bytes"); err != nil {
		return nil, err
	}
	if c.Limits.MaxResponseBytes, err = limits.whole("max_response_bytes"); err != nil {
		return nil, err
	}
	ops, err := f.objects("operations")
	if err != nil {
		return nil, err
	}
	c.Operations = make([]OperationDecl, 0, len(ops))
	for _, o := range ops {
		var op OperationDecl
		if op.Name, err = o.str("name"); err != nil {
			return nil, err
		}
		state, err := o.str("state")
		if err != nil {
			return nil, err
		}
		op.State = OperationState(state)
		if op.Reason, err = o.optStr("reason"); err != nil {
			return nil, err
		}
		mode, err := o.str("mode")
		if err != nil {
			return nil, err
		}
		op.Mode = Mode(mode)
		if op.Mutates, err = o.boolean("mutates"); err != nil {
			return nil, err
		}
		if op.Versions, err = o.strings("versions"); err != nil {
			return nil, err
		}
		if op.RequestSchema, err = o.optStr("request_schema"); err != nil {
			return nil, err
		}
		if op.ResponseSchema, err = o.optStr("response_schema"); err != nil {
			return nil, err
		}
		if op.ReceiptKinds, err = o.strings("receipt_kinds"); err != nil {
			return nil, err
		}
		if op.Executes, err = o.strings("executes"); err != nil {
			return nil, err
		}
		if op.Calls, err = o.strings("calls"); err != nil {
			return nil, err
		}
		c.Operations = append(c.Operations, op)
	}
	if c.ContractVersion != ContractVersion {
		if Newer(c.ContractVersion, ContractVersion) {
			return nil, fmt.Errorf("envelope: the command speaks contract version %s; this build reads up to %s", c.ContractVersion, ContractVersion)
		}
	}
	return c, nil
}
