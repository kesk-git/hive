package store

import (
	"errors"
	"fmt"

	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/contract/schema"
	cstore "github.com/kesk-git/hive/contract/store"
)

// Failure turns an error of the checks or of the contract's store into the
// failure that ends the request: a body that holds a credential shape is
// secret_shape_recognized, one that breaks its schema is schema_violation, and
// the store's own refusals keep the common codes they carry. what says which
// record or file the error is about.
func Failure(what string, err error) *envelope.Failure {
	if f := envelope.AsFailure(err); f != nil {
		return f
	}
	var shape *schema.ShapeError
	if errors.As(err, &shape) {
		return envelope.Common(envelope.CodeSecretShapeRecognized, what+" holds a value of a credential shape at "+shape.Where()+": "+shape.Shape).Because(err)
	}
	if violation(err) {
		return envelope.Common(envelope.CodeSchemaViolation, what+" breaks its schema").Because(err)
	}
	var interrupted *cstore.InterruptedError
	if errors.As(err, &interrupted) {
		return envelope.Common(envelope.CodeRecordMalformed, "an interrupted transaction of the Pool cannot be completed").Because(err)
	}
	switch code := cstore.Code(err); code {
	case "":
		return envelope.Common(envelope.CodeIOFailure, what+" cannot be handled").Because(err)
	case envelope.CodeRevisionConflict, envelope.CodeAlreadyExists, envelope.CodeLockHeld:
		// A conflict carries no detail: it changes nothing and records nothing.
		return envelope.Common(code, fmt.Sprintf("%s: %v", what, err))
	default:
		return envelope.Common(code, what+": "+code).Because(err)
	}
}
