// Package exec is the one process-execution path of Hive
// (spec/distribution.md, The one process-execution path): the only code in
// Hive that starts a process, by any means, and it never runs a shell. It
// alone imports the standard library's os/exec, calls os.StartProcess, calls
// a process-creation function of syscall, or reaches a platform's own
// process-creation call through a system library.
//
// A run names its program by kind and bare name, and passes its arguments as
// a list. The program is found on the search path Hive's own process was
// started with, as an absolute path; the child's environment is built from
// nothing; its standard streams are pipes, named files or the null device,
// and nothing else Hive holds open reaches it. A run is a plain start, a
// terminal start on a pseudo-terminal or a resident start for a process that
// outlives the command that starts it. On macOS and Linux a start goes
// through os.StartProcess; on Windows it is one CreateProcessW call through
// syscall, with a job object, a handle list and, for a terminal, a
// pseudoconsole.
//
// A run ends in exactly one result: not started, exited with its code, ended
// by the platform, stopped by Hive, or lost. The operation that made the run
// maps its result to the exit states of spec/commands.md; this package has
// the mappings the contract fixes.
package exec
