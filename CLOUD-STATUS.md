# Cloud status: fm/hive-design-cross-platform

## Model

No model identifier is written here: this session's environment forbids one in any artifact pushed to a repository.

- `get_session` returns `session_context.model` equal to the model this session was launched with: yes.
- `get_session` returns `session_context.effort_level` equal to the effort this session was launched with: yes.

## Change

The design gains a section, [Platforms: native on macOS, Windows and Linux](docs/design.md#platforms-native-on-macos-windows-and-linux), which states that Hive runs natively on all three and never through WSL2 or another compatibility layer, designs the one process-execution path of stage 1 for each platform (start with no shell and `.exe`-only lookup on Windows, `CommandLineToArgvW` quoting, an environment built from a base set, process groups and job objects, a three-step stop, a terminal start on a pty or a ConPTY pseudoconsole, and the mapping of every way a run ends to the exit states), and carries the table of assumptions replaced and the list of open choices. Every other section that held a platform-bound assumption is rewritten where it owns it: the credential store becomes the platform's own on each (keychain through `security`, Credential Manager and the Secret Service in process through the standard library), the Shadow and the UI server are resident starts with no platform service manager, and every proof, gate and merge bound to "the Mac" is bound to the operator's machine, with "the Mac" kept only for measurements made on the operator's own Mac. The contract change is version 1.9, a minor change (`spec/versions/1.9.md`, version line in `spec/README.md`): it rewrites `spec/distribution.md` (platforms, artefact, the full process-execution path), amends `spec/records.md` (owner-only access, atomic replacement, flushes and locks per platform, path rules), `spec/commands.md` (exit codes and outcomes read from a complete response, never an exit code), `spec/identities.md`, `spec/layout.md`, `spec/test-targets.md`, `spec/decisions.md` (decision 16 amended), `spec/pool.md`, and two definitions of `spec/schemas/common.v1.schema.json` (`absolute_path` widened to the Windows drive form, `home_relative_path` narrowed against backslashes, colons, forbidden characters and reserved device names) and two descriptions in `records/pool.item.v1`. `README.md` no longer says local execution is on the Mac.

A decision now written: the operator answered where Hive's own Windows and Linux tests physically run. GitHub's hosted Windows and Linux runners, for this public repository, are Hive's declared test environment for those two platforms; they run and report the Windows and Linux tests, the macOS runs stay on the operator's own Mac, and the gate's verdict and the merge stay on the operator's machine. It is written into the design's Platforms section and evidence table, `spec/distribution.md`, `spec/test-targets.md` (rule 9) and decision 16, and is no longer an open choice.

## Questions for the operator

### Q1: the terminal runtime, and how you watch and attach to a running agent

tmux, one of the six dependencies, has no native Windows build, so it cannot be the runtime that holds interactive agent sessions on all three platforms. Which runtime should hold them, and how do you watch and attach to a running agent?

- **(a) Hive's own session host.** Hive holds each session's terminal itself, on the platform's own pseudo-terminal (a pty on macOS and Linux, a ConPTY pseudoconsole on Windows), through the standard library and the terminal start already designed for `claude --cloud`. You attach from any terminal with a Hive command, and can watch in the Hive UI. Consequence: no new dependency, and tmux leaves the six, which then become five on every platform; that removal is a later major change of `package.v1` and of the `dispatch` and `shadow` declarations. Hive must build attach, detach, scrollback and resize itself, a per-session resident host process joins the Shadow and the UI server, `tmux attach` stops working on the Mac, and Windows sessions show ConPTY's known rendering and cursor quirks.
- **(b) Herdr on all three platforms, in place of tmux.** Consequence: one runtime everywhere and the count stays six, but Herdr is a new dependency and needs the dependency decision (`needs-decision [key=cross-platform-dependency]`); on Windows Herdr has no direct terminal attach, no file-descriptor handoff and cursor issues under ConPTY, so watching on Windows is limited.
- **(c) tmux kept on macOS and Linux, Hive's own host on Windows only.** Consequence: nothing changes on the Mac, but two runtimes must be built, qualified and kept equivalent, you attach differently on Windows than on the Mac, and the design is no longer one design for all three.

Recommendation: **(a)**. It is the only option that is one design on all three platforms without adding a dependency, and it builds on the terminal start Hive needs anyway. Its consequence is the work of attach and scrollback in stage 4 and the loss of `tmux attach` habits on the Mac.

### Q2: what prompts you to enrol a credential

Hive never accepts a secret value: a secret is entered in the platform's own prompt and Hive records only the Spoor, then checks that the item is present. On each platform, what should prompt you to enter the value?

- **(a) The store's own manager, which you run.** Keychain Access or `security add-generic-password` on macOS; Credential Manager or `cmdkey /generic:<target>` on Windows; Seahorse or `secret-tool store` on Linux. Hive's enrolment status surface shows the exact item to create, then checks its presence. Consequence: no value passes through Hive on any platform, which is today's macOS design extended; you do one manual step per secret per machine, with a different tool on each platform.
- **(b) Hive opens the platform's own system prompt for you.** `security add-generic-password -w` reading from your terminal on macOS, the system credential dialog (`CredUIPromptForWindowsCredentialsW`) on Windows, the Secret Service's own prompt on Linux. Consequence: one Hive command enrols on every platform, but on Windows and Linux the value passes through Hive's process on its way to the store, which the design's rule that Hive never accepts a value forbids; it needs a change to Secrets and a second audited path beside Tendril.
- **(c) (a) everywhere, plus (b) on macOS only**, where `security` reads the value from the terminal itself and Hive never sees it. Consequence: more convenience on the Mac only, and two enrolment flows to document.

Recommendation: **(a)**. It keeps the rule that no value ever passes through Hive on every platform and needs nothing new. Its consequence is a manual step per secret, guided by the exact names the status surface shows.

### Q3: how a notification reaches you

The design names no notification beyond the UI's Waiting on you badge and the Hive session; the inherited supervisor used macOS-only `osascript` and `say`, which Hive does not run. Should Hive notify you outside its UI, and how?

- **(a) No notification beyond the UI and the Hive session.** Consequence: nothing to build and nothing platform-bound; you learn of a decision or an outcome only when you look.
- **(b) Browser notifications from the Hive UI**, through the browser's own notification permission. Consequence: the same mechanism on all three platforms, with no dependency and no platform code; it works only while a UI page is open in a browser and notifications are allowed, and it carries names and counts, never a value or private text.
- **(c) The platform's own notification centre**: Notification Center on macOS, toast notifications on Windows, the freedesktop notification service on Linux. Consequence: notifies with the UI closed, but needs three platform mechanisms, among them a platform utility or an app bundle on macOS and substantial COM code for Windows toasts, each to be qualified.

Recommendation: **(b)**. It is one mechanism on all three platforms with no platform code, and the badge and the session stay the record.

### Q4: what the UI calls the place on your own machine

The placement value `mac`, labelled Mac, names the place a Hunt runs on your own machine. On Windows or Linux that label is wrong. What should the UI call it?

- **(a) "Local", on every platform.** Consequence: one label everywhere, matching "local Claude and Codex"; the record value stays `mac`, since renaming it would be a major change.
- **(b) The platform's name**, Mac, Windows PC or Linux, read from the machine. Consequence: familiar on the Mac, but the label changes with the machine and says nothing a Hunt depends on.
- **(c) The machine's host name.** Consequence: unambiguous, but longer, and a host name then appears in every screenshot of the UI.

Recommendation: **(a)**, one label that means the same on all three platforms.

## Already open

- **A1: what Hive does with Claude Code's login file on Windows and Linux.** Requirement: until it is answered, a tool that reports its login kept in a file makes the operation that needs it unavailable with `secret_store_unsupported`, and Hive reads, moves and changes nothing of that file; Claude Code keeps its login in a file on both platforms, so Claude Code operations there wait on A1.

## Code follow-ups

Each is landed code that the revised design now makes untrue, for the lane that owns it; none was changed here.

- **`contract/store/lock.go`** (contract library lane). Uses `syscall.Flock`, so `GOOS=windows go build` fails with `undefined: syscall.Flock` and its `LOCK_*` constants, and `contract/store`, `contract/journal` and `contract/cli` do not build for Windows. Required: `LockFileEx` and `UnlockFileEx` over the whole lock file on Windows (kernel32, through `syscall.NewLazyDLL`), every attempt non-blocking with Hive's own bounded wait, as `spec/records.md` Locks states.
- **`contract/store/home.go`** (`CheckHome`, `ownerOnly`) **and `contract/journal/journal.go`** (directory and file creation at `0o700`/`0o600`). Owner-only is checked and set as Unix mode bits; Go reports every Windows directory as `0777`, so every home would be refused, and `Chmod` only toggles the read-only attribute. Required: on Windows a protected access-control list granting the user's own security identifier alone, inheritable by every entry (`GetNamedSecurityInfoW`/`SetNamedSecurityInfoW` in advapi32), checked as such; on macOS and Linux also that the owner is the user Hive runs as; refusal of a home on a network share or a FAT or exFAT volume, and of a junction or other reparse point among a home's entries.
- **`contract/store/file.go`** (`syncDir`) **and `contract/journal/journal.go`** (`syncDir`, `syncSegment`). They flush through a read-only handle; on Windows `FlushFileBuffers` needs write access and a directory handle cannot be flushed. Required: on Windows no directory flush, and the renamed file flushed through a handle opened with write access, as `spec/records.md` step 5 states.
- **`contract/store/file.go`** (the `os.Rename` over a record). On Windows the rename fails while a reader holds the record open, because Go opens files without delete sharing. Required: a rename through the temporary file's handle with `FILE_RENAME_FLAG_POSIX_SEMANTICS | FILE_RENAME_FLAG_REPLACE_IF_EXISTS`, every Hive reader opening records with read, write and delete sharing on Windows, and a bounded retry against outside handles before failing with `io_failure`.
- **`contract/cli/invoke.go`** (`writeFile`). Sets the response file's access with `Chmod(0o600)`, which on Windows is silently not owner-only. Required: the file created with an owner-only security descriptor on Windows.
- **Tests in `contract/store`** (`home_test.go`, `replace_test.go`, `lock_test.go`), **`contract/journal`** (`append_test.go`, `reader_test.go`) **and `contract/cli`** (`cli_test.go`). They assert Unix mode bits and create symbolic links, which need Developer Mode or administrator rights on Windows. Required: an expectation per platform (the access-control list on Windows, a junction where a link is needed), never a skip, as `spec/test-targets.md` rule 9 states.
- **Path checks and schema fixtures** (`contract/store`, `contract/testdata/schemas`). The narrowed `home_relative_path` and the widened `absolute_path` have no fixtures yet. Required: invalid fixtures for a backslash, a colon, a reserved device name and a trailing dot or space, a valid Windows `C:/…` absolute path, and the store's join refusing the same segments at use.
- **`Makefile`** (repository build lane). Recipes use POSIX `sh` syntax and utilities (`if [ … ]`, `find`, `$$(…)`, `mkdir -p`), which fail under `cmd.exe`; `build` writes `dist/hive-*` without `.exe` and only for the host platform. Required: recipes that run only the Go toolchain, with any further check as a Go program run by `go run`; names with the platform's executable suffix; an artefact per platform with hard links named `.exe` on Windows, as `spec/distribution.md` states.
- **Repository attributes** (repository build lane). There is no `.gitattributes`; `git ls-files --eol` shows 2,456 files with line feeds, one mixed (`contract/canon/testdata/valid/true-with-space.json`) and one `-text`. A Windows checkout with `core.autocrlf` on would rewrite schemas and fixtures (`.canonical`, `.digest`, `.jsonl`) with carriage returns, changing the embedded bytes and failing the canonical tests. Required: a `.gitattributes` that keeps every committed file's bytes, as `spec/layout.md` rule 12 states.
- **Hosted test runs** (repository build lane). No workflow runs the targets on GitHub's hosted Windows and Linux runners, which are now Hive's declared test environment for those platforms. Required: a workflow that runs the targets on both and reports them, never as the gate.
- **`packages/dispatch/hive-package.json`, `packages/shadow/hive-package.json`.** Both list `tmux` under `executes.core`; it stays until Q1 is answered, and an answer that removes it is a major change of `package.v1`'s enum and of both declarations. **`packages/dispatch/hive-package.json`, `packages/synapse/hive-package.json`:** the `security` reason says "the platform's own keychain tool"; it is macOS's only, and the reason should say so.
- **`contract/envelope/codec.go`** (`ContractVersion`, at 1.5). Not platform-bound itself: it stays below 1.9 until the build meets 1.9's demands above, as its own comment requires.
- **`go.mod`.** `go 1.27`; no platform assumption, nothing to change.

## Dependency needed

none

The designs written need no new dependency: Windows's pseudoconsole, job objects, access-control lists, by-handle rename, file locks and Credential Manager are reached through the standard library's `syscall` package and `syscall.NewLazyDLL`, and the Linux Secret Service through a small session-bus client over the standard library's local socket. Only option (b) of Q1 would add one, Herdr, in place of tmux on all three platforms; if that option is chosen it needs the dependency decision.

## Checks

- `make check`: passes (exit 0: `gofmt -l .` empty, `go vet ./...` clean).
- `make test-contract`: passes (exit 0: `contract/canon`, `cli`, `clock`, `envelope`, `ids`, `journal`, `schema` and `store` all `ok`), on Linux x86-64 with Go 1.27.0, the first run of Hive's tests on Linux.
- Not part of the required checks, for the record: `GOOS=darwin go vet ./...` passes; `GOOS=windows go build` fails in `contract/store`, `contract/journal` and `contract/cli` at `syscall.Flock`, the first code follow-up.

working
