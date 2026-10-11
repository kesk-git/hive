package envelope

// Receipt is the receipt envelope (spec/receipts.md): an immutable record
// that one named fact was established. Its schema and schema_version are
// fixed: hive.receipt at ContractVersion. An empty string or a zero number is
// an absent field; Evidence is written as an empty array when nil.
type Receipt struct {
	ReceiptID     string
	Kind          string
	KindVersion   string
	WrittenAt     string
	Writer        Writer
	RequestID     string
	CorrelationID string
	Scope
	SourceRevision string
	Authority      Authority
	Subject        *RecordRef
	Evidence       []Evidence
	// Body is the kind's own content, empty for a kind that registers none.
	Body map[string]any
}

// DecodeReceipt reads a receipt: a Hive record, of a version this build
// reads, valid under receipt.v1. It fails with a *Failure as DecodeRequest
// does.
func DecodeReceipt(data []byte) (*Receipt, error) {
	obj, err := decodeDocument(data, ReceiptSchema, ReceiptSchemaID, ContractVersion)
	if err != nil {
		return nil, err
	}
	r, err := readReceipt(fields{obj, "receipt"})
	if err != nil {
		return nil, Common(CodeSchemaViolation, err.Error())
	}
	return r, nil
}

func readReceipt(f fields) (*Receipt, error) {
	r := &Receipt{}
	var err error
	if r.ReceiptID, err = f.str("receipt_id"); err != nil {
		return nil, err
	}
	if r.Kind, err = f.str("kind"); err != nil {
		return nil, err
	}
	if r.KindVersion, err = f.str("kind_version"); err != nil {
		return nil, err
	}
	if r.WrittenAt, err = f.str("written_at"); err != nil {
		return nil, err
	}
	writer, err := f.object("writer")
	if err != nil {
		return nil, err
	}
	if r.Writer, err = readWriter(writer); err != nil {
		return nil, err
	}
	if r.RequestID, err = f.str("request_id"); err != nil {
		return nil, err
	}
	if r.CorrelationID, err = f.str("correlation_id"); err != nil {
		return nil, err
	}
	if r.Scope, err = readScope(f); err != nil {
		return nil, err
	}
	if r.SourceRevision, err = f.optStr("source_revision"); err != nil {
		return nil, err
	}
	authority, err := f.object("authority")
	if err != nil {
		return nil, err
	}
	if r.Authority, err = readAuthority(authority); err != nil {
		return nil, err
	}
	if r.Subject, err = readOptRecordRef(f, "subject"); err != nil {
		return nil, err
	}
	list, err := f.objects("evidence")
	if err != nil {
		return nil, err
	}
	r.Evidence = make([]Evidence, 0, len(list))
	for _, e := range list {
		var x Evidence
		if x.Role, err = e.str("role"); err != nil {
			return nil, err
		}
		ref, err := e.object("ref")
		if err != nil {
			return nil, err
		}
		if x.Ref, err = readRecordRef(ref); err != nil {
			return nil, err
		}
		r.Evidence = append(r.Evidence, x)
	}
	body, err := f.object("body")
	if err != nil {
		return nil, err
	}
	r.Body = body.m
	return r, nil
}

// Tree is the receipt as a JSON tree, with schema and schema_version set.
// The body is copied.
func (r *Receipt) Tree() map[string]any {
	m := map[string]any{
		"schema":         ReceiptSchema,
		"schema_version": ContractVersion,
		"receipt_id":     r.ReceiptID,
		"kind":           r.Kind,
		"kind_version":   r.KindVersion,
		"written_at":     r.WrittenAt,
		"writer":         r.Writer.tree(),
		"request_id":     r.RequestID,
		"correlation_id": r.CorrelationID,
		"authority":      r.Authority.tree(),
	}
	r.Scope.into(m)
	put(m, "source_revision", r.SourceRevision)
	if r.Subject != nil {
		m["subject"] = r.Subject.tree()
	}
	evidence := make([]any, 0, len(r.Evidence))
	for _, e := range r.Evidence {
		evidence = append(evidence, map[string]any{"role": e.Role, "ref": e.Ref.tree()})
	}
	m["evidence"] = evidence
	body := cloneObject(r.Body)
	if body == nil {
		body = map[string]any{}
	}
	m["body"] = body
	return m
}

// Encode writes the receipt in canonical form with one final line feed,
// after checking it against receipt.v1: the form of the file at
// <package>/receipts/<receipt_id>.json.
func (r *Receipt) Encode() ([]byte, error) {
	return encodeDocument(r.Tree(), ReceiptSchemaID)
}
