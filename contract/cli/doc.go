// Package cli is the command-line harness of spec/commands.md: every hive-*
// command is invoked, answered and failed the same way, and this package is
// that one way.
//
// A package describes its executable once, as an Executable: its name, the
// commands it owns and, for each, the operations with their declarations and
// handlers. Main then does everything the contract fixes around an
// operation's own work: it reads the arguments and refuses a long one before
// anything else, reads the request from a file or standard input and refuses
// one over the limit before it is parsed, validates the whole request before
// anything else is read or written, checks the home, the scope and the
// operation's state and version, takes the store's lock and answers a
// replayed request from its recorded outcome, runs the handler, writes the
// handler's records, receipts and events with the recorded outcome in one
// transaction, and writes one complete response, or too_large in its place,
// with the exit code its status fixes. The capabilities operation and
// --version are answered here too, from the declaration.
//
// A handler sees a Call and returns a Result. It never writes a response,
// never exits and never parses an argument; it returns a *envelope.Failure
// to end the invocation with a status other than ok, and the receipts a
// refusal or a hold leaves ride on that Failure.
package cli
