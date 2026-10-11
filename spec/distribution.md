# Distribution contract

A delivered Hive needs six other programs and nothing else: **Git, gh, tmux, Claude Code, Codex CLI and no-mistakes**. This file fixes what "delivered" means, what a Hive executable may run, and how the limit of six is proved rather than promised.

Owner: the repository. Proved by `test-distribution`.

## The six

| Dependency | Program run | Declared by |
| --- | --- | --- |
| Git | `git` | `spawning-pool`, `dispatch`, `synapse` |
| gh | `gh` | `dispatch`, `shadow`, `synapse` |
| tmux | `tmux` | `dispatch`, `shadow` |
| Claude Code | `claude` | `dispatch`, `synapse` |
| Codex CLI | `codex` | `dispatch`, `synapse` |
| no-mistakes | `no-mistakes` | `synapse` |

- Hive finds each on the search path at the moment it runs it. It bundles none of them, installs none, upgrades none and changes none of their configuration.
- Hive records the version it observed where a record depends on it, as the design's execution and capability records require. This specification sets no minimum version: the stage that first uses a program states what it needs of it and probes for that.
- A program that is missing or refuses to run makes the operation that needs it `unavailable`, with `dependency_missing` or `dependency_unusable`. It never makes another operation fail, and never causes a fallback to another program.
- no-mistakes is the long-lived plugin point behind Synapse, by the Hive Mind's ruling of 2026-10-11 on the shape of the gate's stage; it is one of the six, not an exception. A package declaration lists it under the key named `transitional`, which [`package.v1`](schemas/package.v1.schema.json) keeps as that field's name: the word there names the field, not the plugin's status, and renaming the field is a major change of this specification left for a later revision.

One exception exists, named, selected and outside the six:

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

- **Seven executables, twenty-one links.** Each link is relative and points at its package's executable in the same directory. See [commands.md](commands.md#executables-and-command-names).
- **Self-contained.** Each executable is built without the C toolchain's involvement and with file-system paths trimmed, so it links nothing but what the platform itself always provides. The schemas, and for `hive-ui` the browser files, are embedded. Nothing is read from the repository at run time.
- **`manifest.json`** records the Hive version, the contract version, the source revision, the Go release used, the platform, and for every file its size and digest, and for every link its target.
- **`NOTICE`** carries the notices of everything the [import records](imports.md) cover.
- **Installed by copying.** There is no installer, no post-install step, no download and no service to register. Removing the directory removes Hive; private homes live elsewhere and are untouched.
- **Versioned as a whole.** The seven executables of one artefact are built from one revision and delivered together. Mixing executables from two artefacts is unsupported, and each executable reports its source revision in `capabilities` so that a mix can be seen.

Instructions and skills that a supervisor session loads are part of Hive and will ship in the same directory. Their place in it is fixed by the stage that first delivers one.

## Platforms

| Platform | Status |
| --- | --- |
| macOS on Apple silicon | Delivered. This is where the Hive and its Fleets run. |
| macOS on Intel | Delivered. |
| Linux on x86-64 and on ARM64 | Built and tested, so that Hive's own package targets run in a Linux session and in hosted checks. Not a platform a supervisor is delivered for. |
| Windows | Not supported. |

Behavior that depends on the Mac is proved on the Mac. A passing Linux run does not establish it.

## The toolchain

- Hive is written in **Go**, at the one release line `go.mod` pins: **1.27**. The Go toolchain is needed to build and to test, and is not needed to run.
- The **Go standard library** is the only library at stage 1. A third-party module enters by a package's declaration and an [import record](imports.md), in the change that first uses it.
- `make` drives the targets. It is a development tool and is not needed to run.
- **No other toolchain exists in the repository**: no Node, no Python, no shell-script layer, no bundler, no generator whose output is committed. A change that needs one is a change to this contract.

So the delivered core has exactly six runtime prerequisites, and a seventh cannot arrive by accident: there is nothing in the repository that could need one.

## What a Hive executable may run

A Hive executable starts another program only through `contract/exec`, and only one of:

1. one of the six, where its package declares it;
2. the exception above, where its package declares it and the operation selected it;
3. a project's declared validation command, where its package declares `project`;
4. another Hive command, where its package declares the call;
5. a platform utility the package declares under `platform`, with its reason.

- `contract/exec` takes the program as one of those named kinds and its arguments as a list. It never passes a command line through a shell, and never builds one from text. It builds the child's environment itself, from nothing. The only way a resolved value enters a child is the one declared variable that Tendril, the launch primitive of the design's [Secrets](../docs/design.md#secrets-hive-keeps-none) section, which sits in this path, sets there, never an argument; and a program that needs a value is started directly, never through tmux.
- A package that needs a platform utility as a separate process lists it in its declaration under `platform` with its reason. Every list is empty at stage 1. The lists stay empty except for a utility the design names, the first being `security`, the Mac's own keychain tool, declared by `dispatch` and `synapse` in the stages that build them; what the earlier shell implementation asked of `ps`, `stat`, `shasum` and the like, the standard library does in process. A platform utility is part of the platform and is not a seventh dependency.
- A harness started inside tmux is started by tmux from an argument list Hive gives it. The Imperative reaches the harness by argument vector or a supported input channel, as the design requires, and is never interpolated into shell code.

## Resident processes and listeners

- The only operations that keep running are the Shadow's bounded observer and, when selected, the UI server. Both are declared `resident`. Nothing else stays behind after a command returns, and no new always-on service is added.
- The only listener Hive opens is the UI server's, bound to loopback, or to the operator's existing private-network interface when that is selected. There is never a public listener.
- Hive makes no network connection of its own. Every connection is made by one of the six, under that program's own configuration and credentials, or, for a program run under the identity a Spoor names, under the one value Tendril resolved from the platform's store into that process.

## How it is proved

`test-distribution` runs against the built artefact and fails on any of the following.

1. The directory's contents differ from the artefact above, a link is absolute or dangling, or `manifest.json` disagrees with the files.
2. An executable needs a shared library beyond the platform's own, or was built with the C toolchain involved.
3. Any Go package other than `contract/exec` imports the standard library's process-execution package.
4. A package starts a program its declaration does not list, found by the kinds passed to `contract/exec`.
5. Any `platform` list is non-empty without a reason, or names a program that is one of the six.
6. The integration tests fail when run with a search path that holds only the artefact's `bin/`, recorded fakes of `gh`, `tmux`, `claude`, `codex` and `no-mistakes`, a recorded fake of `security`, which stands for the declared platform utility and not for a seventh dependency, and the real `git`: that is, with no seventh program reachable.
7. The repository holds a build file of another toolchain.

A passing run is evidence about the artefact it ran against. It is not a claim that any installation already runs it.
