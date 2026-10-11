# Hive

A supervisor for Claude Code and Codex agents, with local execution on the Mac and optional cloud sessions. Hive owns its workflow in one repository, with six core dependencies: Git, gh, tmux, Claude Code, Codex CLI and no-mistakes, the long-lived plugin point behind its gate.

[Hive design](docs/design.md) defines the architecture, dispatch classes, delivery contracts and approved [tooling strategy](docs/design.md#tooling). It specifies the target; the native packages are not implemented yet. [The specification](spec/README.md) holds the contracts of the plan's first stage: the package owners, identities, schemas, test targets and the distribution and import contracts.

Hive is not affiliated with Games Workshop.

Vocabulary: the operator is the Hive Mind; the supervisor is the Hive; each workspace has one persistent Fleet, named after the workspace and supervising its registered projects; workers are Swarms; investigators are Lictors; the review gate is Synapse; a brief is an Imperative; the backlog is the Pool; the set of isolated worktrees a local worker's copy comes from is the Spawning Pool, which is not the Pool and is never shortened to it; a pull request is a Harvest; a merge is Assimilation; the cloud is the Void; supervision is the Shadow; in the UI the Expanse is the Hive-wide view, a Sector is a workspace and a Subsector is a project.

Research and development of Hive is carried out by a dedicated second mate of the operator's current fleet, which may read the original firstmate at https://github.com/kunchenguid/firstmate.
