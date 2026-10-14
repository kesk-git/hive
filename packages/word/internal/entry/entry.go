// Package entry describes the word executable: the commands it owns, as the
// package's declaration lists them. The operations that persist through one
// handoff to the Pool are built, and so is verify, which reads the Pool; an
// operation declared and not built is refused as unsupported until it is.
package entry

import (
	"github.com/kesk-git/hive/contract/cli"
	"github.com/kesk-git/hive/contract/envelope"
	"github.com/kesk-git/hive/packages/word/internal/intake"
)

// Version is the implementation version of the executable.
const Version = "0.1.0"

// notBuilt is the reason an operation not built yet is declared unsupported.
const notBuilt = "the operation is declared and not built yet"

// commands are the command names of the package with the operations its
// declaration lists for each.
var commands = []struct {
	name       string
	operations []string
}{
	{"hive-word", []string{"hold", "revise", "answer", "release", "complete", "close", "verify"}},
}

// Executable is the hive-word executable. Started under any of its command names
// it acts as that command; the contract library's harness answers
// capabilities and --version and refuses a name the executable does not own.
// It reaches the Pool by running hive-pool, and it has no journal: word
// appends no event and has no part of the home.
func Executable() *cli.Executable { return ExecutableWith(intake.Pool{}) }

// ExecutableWith is the hive-word executable reaching the Pool through the
// callee given, behind the check of who brings each request.
func ExecutableWith(callee cli.Callee) *cli.Executable {
	// verify reads the Pool through the callee, which the guards below hide.
	reader, _ := callee.(intake.Reader)
	e := &cli.Executable{
		Package:   "word",
		Name:      "hive-word",
		Version:   Version,
		Storeless: true,
		Callee:    intake.Guard(intake.GuardAnswer(callee)),
	}
	for _, c := range commands {
		cmd := &cli.Command{Name: c.name}
		for _, name := range c.operations {
			op := intake.Operation(name)
			switch name {
			case intake.Answer:
				op = intake.AnswerOperation()
			case intake.Verify:
				op = intake.VerifyOperation(reader)
			}
			if op == nil {
				op = unsupported(name)
			}
			cmd.Operations = append(cmd.Operations, op)
		}
		e.Commands = append(e.Commands, cmd)
	}
	return e
}

// unsupported declares an operation the design names and the build has not
// reached: calling it is refused with unsupported_operation.
func unsupported(name string) *cli.Operation {
	return &cli.Operation{OperationDecl: envelope.OperationDecl{
		Name:         name,
		State:        envelope.Unsupported,
		Reason:       notBuilt,
		Mode:         envelope.Unary,
		Versions:     []string{},
		ReceiptKinds: []string{},
		Executes:     []string{},
		Calls:        []string{},
	}}
}
