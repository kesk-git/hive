# Distribution contract

A delivered Hive needs five other programs and nothing else: **Git, gh, Claude Code, Codex CLI and no-mistakes**. This file fixes what "delivered" means, on which platforms, what a Hive executable may run and how it runs it, and how the limit of five is proved rather than promised.

Hive is delivered natively for macOS, Windows and Linux. It never runs through WSL2, Cygwin, MSYS2 or any other compatibility layer: each platform has its own executables, built from the same revision, which use that platform's own facilities through the Go standard library and the two named exceptions of [the toolchain](#the-toolchain). An interactive session runs in Hive's own [terminal host](../docs/design.md#the-terminal-host), in a session host of `dispatch`'s, started by [the resident start](#a-resident-start), that holds the session on the platform's own pseudo-terminal through [the terminal start](#a-terminal-start). No terminal program is a dependency: tmux, which the tooling ruling named and which has no native Windows build, left the dependencies by the operator's decision, and nothing replaces it.

Owner: the repository. Proved by `test-distribution`, and for [the one process-execution path](#the-one-process-execution-path) by `test-contract`.

## The five

| Dependency | Program run | Declared by |
| --- | --- | --- |
| Git | `git` | `spawning-pool`, `dispatch`, `synapse` |
| gh | `gh` | `dispatch`, `shadow`, `synapse` |
| Claude Code | `claude` | `dispatch`, `synapse` |
| Codex CLI | `codex` | `dispatch`, `synapse` |
| no-mistakes | `no-mistakes` | `synapse` |

- Hive finds each on the search path at the moment it runs it, by the lookup rules of [the one process-execution path](#finding-the-program). It bundles none of them, and by itself it installs, upgrades and configures none of them: a repair the Hive Mind presses at a failed check of the app's [setup check](../docs/design.md#hives-own-app) is the one way any of that happens through Hive, and a program a repair runs is declared like every program a Hive executable runs, by the stage that builds the repair.
- On Windows a program is found only as a native executable, `<program>.exe`. A dependency present there only as a batch file, such as the `.cmd` shim a package manager like npm installs, is `unavailable` with `dependency_unusable`, naming the file found: running it would run `cmd.exe`, a shell. The remedy is a native executable of that dependency on the search path under its program name; Hive installs nothing by itself.
- Hive records the version it observed where a record depends on it, as the design's execution and capability records require. This specification sets no minimum version: the stage that first uses a program states what it needs of it and probes for that.
- A program that is missing or refuses to run makes the operation that needs it `unavailable`, with `dependency_missing` or `dependency_unusable`. It never makes another operation fail, and never causes a fallback to another program.
- no-mistakes is the long-lived plugin point behind Synapse, by the Hive Mind's ruling of 2026-10-11 on the shape of the gate's stage; it is one of the five, not an exception. A package declaration lists it under the key named `transitional`, which [`package.v1`](schemas/package.v1.schema.json) keeps as that field's name: the word there names the field, not the plugin's status, and renaming the field is a major change of this specification left for a later revision.

One exception exists, named, selected and outside the five:

| Exception | Program | When it is run | Declared by |
| --- | --- | --- | --- |
| Optional browser stack | `chrome-devtools-axi` and what it needs | Only for a selected browser task | The package that first selects it; none at stage 1 |

A Hive without the exception present is whole. It is not checked at start, and its absence is not an error until an operation that selected it is called.

A project's own validation commands are a third thing: they belong to the project, run in the environment the project supplies, and may need whatever the project needs. They are declared as `project` and are never part of Hive's distribution.

## The artefact

The build produces one directory per platform:

```text
hive-<version>-<os>-<arch>/
  bin/
    hive-pool  hive-spawning-pool  hive-dispatch  hive-shadow
    hive-synapse  hive-word  hive-ui            seven executables
    hive-admit -> hive-pool  ...                twenty-one links
  LICENSE
  NOTICE
  manifest.json
```

- **Names on Windows.** Every name in `bin/` of a Windows artefact carries the `.exe` suffix: `hive-pool.exe`, and `hive-admit.exe` beside it. A program on Windows is found by that name and no other.
- **Seven executables, twenty-one links.** Each link points at its package's executable in the same directory. See [commands.md](commands.md#executables-and-command-names). On macOS and Linux each link is a relative symbolic link. On Windows, where creating a symbolic link needs Developer Mode or administrator rights, each is a hard link named `<name>.exe`; a copy of the directory that does not keep hard links turns each into a whole copy of its executable, which is larger and behaves the same, and `test-distribution` accepts either.
- **Self-contained.** Each executable is built without the C toolchain's involvement and with file-system paths trimmed, so it links nothing but what the platform itself always provides: the system library on macOS, nothing on Linux, and on Windows the system libraries such as `kernel32.dll` and `advapi32.dll`, reached through the standard library's `syscall` package. `hive-ui`, Hive's own app, also loads at run time the platform's own window system and web engine, through purego on macOS and Linux and through `syscall` on Windows: the system's own frameworks, WebKit among them, on macOS; the system C library, GTK and WebKitGTK on Linux; and on Windows Microsoft's WebView2 loader, with the WebView2 engine it loads. The WebKitGTK package on Linux and the WebView2 loader on Windows are accepted as system parts, like the platform's own libraries, and are not dependencies. The schemas, and for `hive-ui` the app's page files, are embedded. Nothing is read from the repository at run time.
- **The app's platform parts.** What a platform needs beside `hive-ui` for the app to carry its icon, menus and notifications, such as an application bundle on macOS, enters this artefact with stage 6, in the change that builds the app, as a change to this contract. It registers nothing with the platform at install. On Windows the app notifies from its own icon in the notification area, which needs nothing beside `hive-ui` and nothing registered: no Start Menu shortcut and no application identity.
- **`manifest.json`** records the Hive version, the contract version, the source revision, the Go release used, the platform, and for every file its size and digest, and for every link its target.
- **`NOTICE`** carries the notices of everything the [import records](imports.md) cover.
- **One archive per platform.** Each platform's directory is delivered as one gzipped tar archive, which the `tar` each of the three platforms carries extracts; its extraction keeps the links where the extracting tool can and makes whole copies where it cannot.
- **Installed by copying.** There is no installer, no post-install step, no download and no service to register on any platform: no launchd job, no systemd unit, no Windows service and no scheduled task. Removing the directory removes Hive; private homes live in Hive's root, outside it, as [records.md](records.md#the-home) places them, and are untouched.
- **Versioned as a whole.** The seven executables of one artefact are built from one revision and delivered together. Mixing executables from two artefacts is unsupported, and each executable reports its source revision in `capabilities` so that a mix can be seen.

Instructions and skills that a supervisor session loads are part of Hive and will ship in the same directory, as read-only parts of the artefact that no session writes at run time. Their place in it is fixed by the stage that first delivers one.

## Platforms

| Platform | Status |
| --- | --- |
| macOS on Apple silicon and on Intel | Delivered, built and tested. |
| Windows on x86-64 and on ARM64 | Delivered, built and tested. Windows 10 version 1809 or later, or Windows Server 2019 or later: the pseudoconsole and the job attribute of [the one process-execution path](#the-one-process-execution-path) need it. |
| Linux on x86-64 and on ARM64 | Delivered, built and tested. |

Delivered means that the Hive and its Fleets run there: they run on the operator's machine, whichever of the three it is. No platform is the original that the others port: each behaviour is designed for all three, and where a platform needs its own mechanism the contract that owns the behaviour states it for each.

Behaviour that depends on a platform is proved on that platform. A passing run on one of the three establishes nothing about the other two, and a Linux run in a Void session establishes Linux behaviour only. Hive's own targets pass on all three before a stage lands and in the scheduled full run, and their Windows and Linux results are informational at every other head, as the design's [How Hive tests](../docs/design.md#how-hive-tests) states. By the operator's decision, GitHub's hosted Windows and Linux runners, for this public repository, are Hive's declared test environment for those two platforms: they run Hive's targets on Windows and on Linux and report the results, which are evidence of Hive as a project, read like any project's declared environment: a remote producer whose head-bound results the gate reads as that producer's evidence, under the design's [hybrid on evidence](../docs/design.md#synapse-on-the-operators-machine-with-both-reviewers), and never the gate. The macOS runs take place on the operator's own Mac. The gate's verdict and the merge stay on the operator's machine, as the design's local-first gate states.

## The toolchain

- Hive is written in **Go**, at the one release line `go.mod` pins: **1.27**. The Go toolchain is needed to build and to test, and is not needed to run. It builds every platform's executables from any one platform, without the C toolchain.
- The **Go standard library** is the only library at stage 1. It reaches each platform's own facilities through its `syscall` package, and through `syscall.NewLazyDLL` for a function of a Windows system library; that is the standard library, not a third-party module.
- Hive uses the standard library only, with **two named exceptions**. Each is pinned to one version, used for its one purpose and nothing else, and enters by the declaration of the package that links it, with an [import record](imports.md) for it and for every module it brings, in the change that first uses it:
  1. **zalando/go-keyring**, the first, linked by the contract library inside Tendril's package, the one place that resolves secrets, from stage 4: through it the store adapters reach Credential Manager on Windows and the Secret Service on Linux. It is built into the Windows and Linux executables only. macOS keeps its keychain route through `security`, and the module's own macOS code, which starts `security` itself, is in no executable.
  2. **ebitengine/purego**, the second, linked by `ui` from stage 6: through it one build of Hive's own app reaches the macOS and Linux system libraries without a C compiler.
- A module of either exception never starts a process. Where one holds a way to start one, its import record names it and the condition Hive keeps so that it is never reached: the session bus client that go-keyring brings on Linux starts `dbus-launch` when it finds no session bus, so the Linux adapter calls into it only where the session bus's address is already known, and otherwise answers as a host with no Secret Service. A further third-party module is a change to this contract.
- `make` drives the targets. It is a development tool and is not needed to run. Each target's recipe runs the Go toolchain and nothing that needs a shell's syntax or a POSIX utility, so the same target runs from the repository root on all three platforms; a check a recipe needs beyond the Go toolchain's own commands is a Go program of the repository, run by `go run`.
- **No other toolchain exists in the repository**: no Node, no Python, no shell-script layer, no bundler, no generator whose output is committed. A change that needs one is a change to this contract.

So the delivered core has exactly five runtime prerequisites, and a sixth cannot arrive by accident: there is nothing in the repository that could need one.

## What a Hive executable may run

A Hive executable starts another program only through `contract/exec`, and only one of:

1. one of the five, where its package declares it;
2. the exception above, where its package declares it and the operation selected it;
3. a project's declared validation command, where its package declares `project`;
4. another Hive command, where its package declares the call;
5. a platform utility the package declares under `platform`, with its reason.

- `contract/exec` takes the program as one of those named kinds and its arguments as a list. It never passes a command line through a shell, and never builds one from text. It builds the child's environment itself, from nothing. The only way a resolved value enters a child is the one declared variable that Tendril, the launch primitive of the design's [Secrets](../docs/design.md#secrets-hive-keeps-none) section, which sits in this path, sets there, never an argument; and a program that needs a value is started directly, never through the terminal runtime.
- A package that needs a platform utility as a separate process lists it in its declaration under `platform` with its reason. No package runs a platform utility at stage 1. A list stays empty except for a utility the design names. The first is `security`, macOS's own keychain tool, declared by `dispatch` and `synapse`, which first run it in the stages that build them, and run it only on macOS: on Windows and Linux the design's store adapters reach the platform's own credential store in process, through the first named exception of [the toolchain](#the-toolchain), and run no utility for it. What the earlier shell implementation asked of `ps`, `stat`, `shasum` and the like, the standard library does in process on all three platforms. A platform utility is part of the platform and is not a sixth dependency.
- An interactive harness is started from an argument list Hive gives it, by the terminal start of its session host. The Imperative reaches the harness by argument vector or a supported input channel, as the design requires, and is never interpolated into shell code.
- For the third kind, the program is the one the project declares, found by the same lookup rules. A project whose check is itself a shell script or a batch file declares its interpreter, `sh` or `cmd`, as the program, with its own fixed arguments: that is the project's command, run as declared, and Hive adds to it no argument built from text.

## The one process-execution path

`contract/exec` is the only code in Hive that starts a process, by any means: it alone imports the standard library's `os/exec`, calls `os.StartProcess`, calls a process-creation function of `syscall`, or reaches a platform's own process-creation call through a system library. The rules below hold on macOS, Windows and Linux; where a platform needs its own mechanism, the rule names it for each. A run starts one program in one of three ways: a **plain start**, with its standard streams as pipes, files or the null device; a **terminal start**, which gives it a pseudo-terminal; and a **resident start**, for a process that outlives the command that starts it.

### Finding the program

- A run names its program by its kind and a bare name, never a path or a line of text; the project's own command is the one exception, as above, and is found by these rules too.
- The search path is the one Hive's own process was started with. Its directories are tried in order. An empty or relative entry is skipped, so the current directory is never searched by accident.
- On macOS and Linux the first regular file of that name with an execute bit is the program.
- A program's name is a bare word with no extension on every platform: on Windows the run appends `.exe`, and a name that ends in `.exe`, in any case, is refused as not a bare name.
- On Windows the program is found only as `<name>.exe`, the name compared without regard to case as Windows compares names. `PATHEXT` is not consulted, and a `.bat`, `.cmd`, `.ps1`, `.vbs` or `.js` file is never run. Where no `<name>.exe` exists on the search path but a batch file of that name does, the run is not started and says which file it found.
- The result is an absolute path. On Windows it is passed to the process-creation call as the application name, so that Windows makes no search of its own, in the current directory or anywhere else.
- A program that the kernel starts through an interpreter line, as a script with `#!` on macOS and Linux, is run as the kernel runs it: the interpreter receives the argument list unchanged and no shell parses it. A batch file on Windows is different: it runs only under `cmd.exe`, which parses its command line again by rules of its own, and that is a shell.

### Arguments

- A run's arguments are a list of strings fixed by the operation. Nothing joins, quotes or escapes text for a shell, on any platform.
- No argument contains a NUL character, and on Windows every argument is valid UTF-8, so that its conversion to the UTF-16 Windows takes changes nothing. A run that breaks either rule is not started, and the operation that built it reports `failed` with code `internal`, as it does for a program name that is not a bare word, a working directory that is not absolute and a stream the path does not admit.
- On macOS and Linux the list reaches the program unchanged, as its argument vector.
- On Windows a process receives one command line. `contract/exec` builds it from the list by the rules `CommandLineToArgvW` and the Microsoft C runtime use to split it again: an argument that is empty or holds a space, a tab or a quote is quoted, and backslashes are doubled only where they precede a quote, as the standard library's `syscall.EscapeArg` does. The first word is the resolved path. A program that splits its command line by these rules receives the list unchanged; one that splits it by rules of its own is outside that guarantee, and `cmd.exe` is one, which is why a batch file is never run.
- Before anything starts, the arguments are checked against the platform's limits: on macOS and Linux the total of arguments and environment, and on Linux also 32 memory pages for one argument, 131,072 bytes at the usual page size; on Windows 32,766 UTF-16 code units for the whole command line after quoting, so that with its terminating NUL it fits the 32,767 characters `CreateProcessW` takes, far less than the other two allow. A run over a limit is not started and its operation answers `too_large`, never with a truncated argument. A caller whose content may exceed it passes it by file or standard input wherever the program accepts one.

### The environment

The child's environment is built from nothing, for each run, and holds only:

1. the base variables a platform needs for any program to run, each copied by name from Hive's own environment where it is set: on macOS and Linux `PATH`, `HOME`, `USER`, `LOGNAME`, `TMPDIR`, `TZ`, `LANG` and the `LC_` variables; on Windows `PATH`, `PATHEXT`, `SystemRoot`, `SystemDrive`, `windir`, `ComSpec`, `TEMP`, `TMP`, `USERPROFILE`, `HOMEDRIVE`, `HOMEPATH`, `APPDATA`, `LOCALAPPDATA`, `ProgramData`, `ProgramFiles`, `ProgramFiles(x86)`, `ProgramW6432`, `CommonProgramFiles`, `USERNAME`, `USERDOMAIN`, `COMPUTERNAME`, `NUMBER_OF_PROCESSORS`, `PROCESSOR_ARCHITECTURE` and `OS`, matched without regard to case as Windows matches them, since a program run without `SystemRoot`, for one, cannot use the network;
2. the variables of the program's own configuration that the stage which first runs it names, copied by name from Hive's own environment, never invented;
3. for a terminal start on macOS and Linux, `TERM`, set to `xterm-256color`, which describes the pseudo-terminal Hive gives; Windows needs none;
4. the one variable Tendril sets from a resolved value, and no other variable from a value.

Nothing else of Hive's environment passes, neither whole nor by pattern. On Windows the block is passed in UTF-16, sorted by name without regard to case, as Windows requires.

### Streams, handles and the working directory

- A plain start's standard input, output and error are each a pipe, a file the operation names, or the null device. Nothing else Hive holds open reaches the child: on macOS and Linux every other descriptor is closed when the program starts, and on Windows the child inherits exactly its standard handles, listed in the start's handle list (`PROC_THREAD_ATTRIBUTE_HANDLE_LIST`).
- The working directory is the absolute directory the operation names. A run never takes the caller's current directory by default.
- Bytes are passed as they are: nothing translates line endings or decodes text on any platform.

### Starting a process

- **macOS and Linux.** Through the standard library's process API, `os.StartProcess` with its system attributes: a plain start in a new process group of its own (`Setpgid`), a terminal start in a new session (below).
- **Windows.** Through one `CreateProcessW` call that `contract/exec` makes through the standard library's `syscall` package, because Go 1.27's `os.StartProcess` cannot place a process in a job object as it is created or give it a pseudoconsole. The call names the resolved path as the application, passes the command line and the environment block above, sets `CREATE_UNICODE_ENVIRONMENT`, sets `CREATE_NO_WINDOW` for a plain start, never sets `CREATE_NEW_PROCESS_GROUP`, which turns Control-C off for the new process, and passes an extended startup block (`EXTENDED_STARTUPINFO_PRESENT`) whose attribute list carries the job the process is created in (`PROC_THREAD_ATTRIBUTE_JOB_LIST`), the handle list, and, for a terminal start, the pseudoconsole. A later Go release that offers these through its process API may replace the call without changing any rule here.
- **Process identity.** A started process is identified by its process id together with its start time, never by its id alone, since every platform reuses ids: on Linux the start time `/proc` reports, on macOS the one `sysctl` reports, on Windows the creation time `GetProcessTimes` reports, each read in process.

### Process trees, and how a run is stopped

A run's tree is its program and every process the program starts that stays with it:

- **macOS and Linux.** The process group the start created. A descendant that leaves it on purpose, as a daemon does by starting a session of its own, leaves the tree, as it would under any supervisor, and Hive never relies on ending such a process through its starter's tree.
- **Windows.** The job object the start created the process in, with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` and `JOB_OBJECT_LIMIT_BREAKAWAY_OK`, so every descendant is in it unless it asks to leave with `CREATE_BREAKAWAY_FROM_JOB`. A descendant that does leaves the tree, as it would on macOS and Linux, and Hive never relies on ending such a process through its starter's tree. Jobs nest, so the start works where Hive itself runs inside another job, as under a terminal or a hosted runner.

A run is stopped in up to three steps, each taken only after the one before has had the grace period the operation states:

1. **Interrupt**, for a terminal start only: Hive writes the terminal's interrupt character, Control-C, byte `0x03`, to its input. On macOS and Linux the terminal's line discipline turns it into `SIGINT` for the foreground process group; on Windows the pseudoconsole turns it into a `CTRL_C_EVENT` for the processes attached to it. This is an adapter's `interrupt` where the harness supports it.
2. **Request.** On macOS and Linux, `SIGTERM` to the process group, and for a terminal start also closing the controller side, which hangs the terminal up (`SIGHUP`). On Windows, for a terminal start, closing the pseudoconsole, which sends `CTRL_CLOSE_EVENT` to its processes; for a plain start, closing its standard input, the one request Windows offers a process that has no console. A resident operation of Hive's is asked to stop through its own command, as the stage that builds it states.
3. **End.** On macOS and Linux, `SIGKILL` to the process group. On Windows, `TerminateJobObject` with the exit code `0xC000013A`, the status Windows itself gives a console program ended by Control-C, which no Hive status uses.

Before it ends a tree, the run records that it is ending it, so that its result is read from that record and never inferred from an exit code. A unary command waits for, or ends, every run it started before it returns, and nothing of it outlives it but a resident start. If the command itself dies, its runs may go on as orphans on macOS and Linux, and on Windows they end with its job; either way the command's intent stands and the next command reconciles, so no contract depends on which.

### A resident start

The Shadow's observer, Hive's own app, each session host and any other operation the contract declares `resident` are started so that they outlive the command that starts them:

- **macOS and Linux.** In a new session (`setsid`), with standard streams on the null device or files the operation names, and not waited for.
- **Windows.** With `DETACHED_PROCESS`, `CREATE_BREAKAWAY_FROM_JOB` and `CREATE_SUSPENDED`, standard handles likewise, and in no job of its own. Hive's own jobs allow that breakaway. An outer job of a runner or a service may not, and when jobs nest a breakaway can leave the inner job and keep the outer one without the call failing, so the call's success proves nothing. Before it resumes the process, `contract/exec` checks with `IsProcessInJob`, naming no job, that the process is in no job at all. If it is in one, or the check fails, `contract/exec` ends the process without resuming it, so that none of the resident's work has run, and the start is `unavailable` with `dependency_unusable`, naming the refused breakaway. A resident is reported as started only after it was resumed outside every job, and never as detached otherwise, since it would end with the job.

A resident's own runs follow every rule above, so on Windows its tree ends with it. No resident is registered with a platform's service manager: no launchd job, systemd unit, Windows service or scheduled task.

### A terminal start

A program that needs an interactive terminal, an interactive harness session or a launch command that refuses a non-interactive one as `claude --cloud` does, is given one from the platform's own pseudo-terminal, never through a wrapper program such as `script`, which Windows does not have and whose arguments differ between macOS and Linux.

- **macOS and Linux.** `contract/exec` opens the pseudo-terminal controller, `/dev/ptmx`, read-write and without making it a controlling terminal; grants and unlocks it and reads the name of its terminal side through the platform's own `ioctl` requests, `TIOCSPTLCK` and `TIOCGPTN` on Linux, `TIOCPTYGRANT`, `TIOCPTYUNLK` and `TIOCPTYGNAME` on macOS; sets its window size with `TIOCSWINSZ`; and starts the program with the terminal side as its standard input, output and error, in a new session with that terminal as its controlling terminal (`Setsid` and `Setctty`). Hive keeps the controller side and closes its own copy of the terminal side.
- **Windows.** `contract/exec` creates two pipes and a pseudoconsole over them with `CreatePseudoConsole`, a function of `kernel32.dll` reached through `syscall.NewLazyDLL`, and starts the program with the pseudoconsole in its attribute list (`PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE`) and no standard handle of Hive's. Hive keeps the writing end of the input pipe and the reading end of the output pipe, resizes with `ResizePseudoConsole`, and closes with `ClosePseudoConsole` while it goes on draining the output, which that call needs in order to return.
- The holder writes input and reads output as bytes. What a terminal shows is its rendering, never a record: the Windows pseudoconsole re-renders what a program writes, so one program's output differs in bytes between platforms, and nothing Hive decides is read from it.
- This is how Hive's own terminal host gives an interactive agent its terminal. The session host holds the controller side and the session's recent output in memory, and when a terminal joins it through the join operation, which is read-only, it resizes the session, with `TIOCSWINSZ` or `ResizePseudoConsole`, so that the agent redraws itself: no screen is emulated, and the agent's tree is the session host's run, ended by its process group or job object.

### What a run reports

A run ends in exactly one of these results, read from the operating system and from the run's own record:

| Result | macOS and Linux | Windows |
| --- | --- | --- |
| `not_started` | Not found, found but not executable, refused by the lookup rules, or the start call failed. | The same, or found only as a batch file. |
| `exited`, with its code | The program exited (`WIFEXITED`). | The process ended with a code below `0xC0000000`, and Hive did not end it. A process another program ended with `TerminateProcess` reports the code that program chose and reads as `exited`. |
| `ended` | The process ended by a signal it did not handle (`WIFSIGNALED`), which is named. | The process ended with an exception status, `0xC0000000` or above, such as an access violation or Control-C. |
| `stopped_by_hive` | Hive ended the tree, as recorded before it did. | The same, whatever exit code `TerminateJobObject` gave. |
| `lost` | Waiting failed, so how it ended cannot be known. | The same. |

The operation that made the run maps its result to the [command contract's exit states](commands.md#exit-and-error-states):

- **Another Hive command.** Its result is its complete response, as the command contract states. A run that leaves no complete response agreeing with its exit code, whatever its result above, is `unknown` with `outcome_unknown`. A command reached through its link in `bin/` that is not found or not executable is `unavailable` with `dependency_missing` or `dependency_unusable`, as a core program is.
- **An external program, not started.** Nothing found is `unavailable` with `dependency_missing`, or `platform_utility_missing` for a platform utility; found but not usable, a batch file on Windows among them, is `unavailable` with `dependency_unusable`, or `platform_utility_unusable`.
- **An external program, `exited`.** The operation reads the code by that program's own contract, which the stage that first runs it states. Hive gives no code a meaning of its own.
- **An external program, `ended`, `stopped_by_hive` or `lost`.** The program gave no result. Where the run may have had an effect outside Hive, recorded as an intent before it started, the operation is `unknown` with `outcome_unknown` and reconciles before any retry. Where it could have had none, the operation is `unavailable` with `dependency_unusable`, or `platform_utility_unusable`.
- **A project's validation command.** `exited` with 0 is a pass and every other code a failure, and every other result is not a pass: missing, cancelled and timed-out results never pass, as the design's checks contract states.

`test-contract` proves these rules on each of the three platforms, as [test-targets.md](test-targets.md#the-targets) lists.

## Resident processes and listeners

- The only operations that keep running are the Shadow's bounded observer; Hive's own app, when selected, which keeps running after its window closes until the operator chooses Quit, which stops Hive as a whole as the design's [Credentials, files and resident processes](../docs/design.md#credentials-files-and-resident-processes) states: a stop intent for the Hive and every Fleet, local agents parked, every session host and the Shadow ended, and a cloud session left running for collection at the next start; and the session host of each interactive session, for as long as that session runs. Each is declared `resident` and started by the resident start above: the observer and the app from the session start of the home they serve, and a session host by the launch of its session. A resident is started only where its lifetime can hold, outside every job around the command, and otherwise not started. Nothing else of Hive's stays behind after a command returns, and no new always-on service is added.
- No resident is started by the platform at login or after a reboot, with no exception. An install registers nothing. Until a Hive session starts again, nothing else of Hive runs, and the design's rules for what was missed while nothing ran apply.
- Hive opens no network listener, on loopback, on a private network or anywhere else, and the app opens no listener of any kind: it serves its pages from inside the program, through a URL scheme of its own. A session host is reached by its join operation, which gives a terminal of the operator's own a read-only view of the session and takes no input from it, and by `dispatch` through a channel local to the machine and open to the operator's own user alone, never through a network address: a socket on macOS and Linux, created in the home, and a named pipe on Windows.
- Hive makes no network connection of its own. Every connection is made by one of the five, under that program's own configuration and credentials, or, for a program run under the identity a Spoor names, under the one value Tendril resolved from the platform's store into that process. The Linux store adapter's connection to the user's session bus is made to a local socket only, never to a network address.

## How it is proved

`test-distribution` runs against the built artefact of each platform, on that platform, and fails on any of the following.

1. The directory's contents differ from the artefact above; a link on macOS or Linux is absolute or dangling; a link on Windows is neither a hard link to nor a byte-identical copy of its package's executable; a name on Windows lacks its `.exe` suffix; or `manifest.json` disagrees with the files.
2. An executable needs a shared library beyond the platform's own, which for `hive-ui` include the system parts [the artefact](#the-artefact) accepts, or was built with the C toolchain involved.
3. Any Go package other than `contract/exec` starts a process by any means: imports the standard library's `os/exec`, calls `os.StartProcess`, calls a process-creation function of `syscall`, or reaches a platform's process-creation call through a system library. A module of the two named exceptions of [the toolchain](#the-toolchain) passes only where its import record names each such start and the condition that keeps Hive from reaching it; and a macOS executable that holds go-keyring fails.
4. A package starts a program its declaration does not list, found by the kinds passed to `contract/exec`.
5. Any `platform` list is non-empty without a reason, or names a program that is one of the five.
6. The integration tests fail when run with a search path that holds only the artefact's `bin/`, recorded fakes of `gh`, `claude`, `codex` and `no-mistakes`, on macOS a recorded fake of `security`, which stands for the declared platform utility and not for a sixth dependency, and the real `git`: that is, with no sixth program reachable. On Windows and Linux the store adapters run against their recorded fakes in process. Every fake is an executable the test builds, never a script, so that the lookup rules find it on all three platforms.
7. The repository holds a build file of another toolchain.

A passing run is evidence about the artefact and the platform it ran against. It is not a claim that any installation already runs it.
