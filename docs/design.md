# Hive design

Design baseline: 2026-10-10. This is an implementation contract, not a claim that the components below already exist.

Product scope follows the Hive Mind's 2026-10-10 brainstorm rulings and later Codex findings and decisions, including the decision to park Codex Void work without model selection. The dated Claude and Codex measurements establish the capability evidence; the original firstmate supplies the inherited operational contracts.

Hive specializes firstmate for Claude Code and Codex. It retains their existing local capabilities and adds execution in the Void. The Hive and its Fleets ordinarily run as Claude Code sessions on the Mac; local Codex supervision remains an inherited capability. Swarms and Lictors can run locally or in the Void. Synapse validates their work, and Assimilation stays on the Mac.

The latest Mind's word takes precedence over the earlier minimal-Mac sketch. Local Swarms, worktrees, terminal runtimes, the local review pipeline, supervision hooks, and a local test machine remain available. Tooling specific to other agent harnesses is removed; shared tools needed by Claude and Codex remain. This design does not alter the existing firstmate installation.

Claude is the primary Void executor, through Claude Code cloud sessions and the official GitHub integration. Codex remains a second executor behind the same Hive interface, with three interchangeable backends: local on the Mac, legacy Void, and new Void. Both Codex Void backends are parked; local Codex is the active route. Re-enabling Void work requires a supported command-line path that honors model selection and meets the collection contract. Interchangeability preserves the task contract; it does not imply equal capabilities or permission to change a task's model or placement silently.

The later Mind's word parks Codex Void work because the legacy path cannot choose the model. Its successful submission, status, diff and apply measurement and internal final-text retrieval remain useful evidence; they do not authorize dispatch without model control. Astra reviews stay on local Codex on the Mac. Codex usage, including compatibility probes, is command line only: no browser-driven tasks or GitHub mention or Action dispatch. Legacy retirement must leave Claude delivery and local Codex operational.

## Vocabulary and placement

| Name | Meaning and responsibility |
| --- | --- |
| Hive | The product. **The Hive** is its primary supervisor and the Hive Mind's point of contact. |
| Hive Mind | The operator; supplies intent, scope, decisions, and Assimilation authority. |
| Fleet | A persistent project supervisor, named after its project, with an isolated private home and a charter. |
| Swarm | A worker completing an authorized change through its chosen delivery path. |
| Lictor | An investigator producing evidence, a diagnosis, or a design report; research alone does not authorize implementation. |
| Imperative | The complete dispatch instructions for one assignment. |
| Pool | The durable work queue owned by the Hive or a Fleet. |
| Mind's word | An operator decision, recorded against a held Pool item. |
| Shadow | Supervision, durable observation and wake ownership. |
| Synapse | The review and validation gate. |
| Harvest | A pull request carrying a proposed change. |
| Assimilation | Authorized merging of a Harvest or landing of a local-only change. |
| Void | Cloud execution. Model names remain their real names. |
| Genestealer | The research role developing Hive; it does not create another level of runtime authority. |

Hive uses its own visual identity. Do not use Tyranid, Warhammer or Games Workshop artwork in its interface, documentation or promotional material.

```mermaid
flowchart TD
    Mind[Hive Mind] --> Hive[The Hive · Mac]
    Hive --> Fleet[Fleet · Mac]
    Hive <--> Private[Private homes · Pool, Imperatives, decisions, journal]
    Fleet <--> Private
    Fleet --> Local[Local Claude or Codex · isolated worktree]
    Fleet --> Claude[Claude in the Void · primary · sessions or GitHub integration]
    Fleet --> Codex[Codex adapter · local active · legacy and new Void parked]
    Local --> Harvest[Harvest · GitHub]
    Claude --> Harvest
    Codex --> Collection[Mac collection · verified result · backend capability gates]
    Collection --> Harvest
    Harvest --> Synapse[Synapse · Actions, Claude review, Astra review]
    Synapse --> Assimilation[Assimilation · authorized Mac operation]
    Shadow[Shadow · Mac polling] --> Private
    Shadow --> Local
    Shadow --> Claude
    Shadow --> Codex
    Shadow --> Harvest
    Shadow --> Synapse
```

GitHub delivers events to its Actions and vendor integrations. It does not deliver webhooks into the Mac supervisor session. The Shadow therefore polls and wakes the appropriate supervisor. No always-running supervisor or waiting loop is placed in a Void session.

## What Hive keeps from firstmate

The inheritance baseline is the original [firstmate operator contract](https://github.com/kunchenguid/firstmate/blob/fb75c1f94170df7e0751147ea61afabcf5d4e104/AGENTS.md), [architecture](https://github.com/kunchenguid/firstmate/blob/fb75c1f94170df7e0751147ea61afabcf5d4e104/docs/architecture.md), and [configuration](https://github.com/kunchenguid/firstmate/blob/fb75c1f94170df7e0751147ea61afabcf5d4e104/docs/configuration.md). This snapshot makes the comparison reproducible; implementation must review later upstream changes before porting them.

### Operator contract and hard rules

1. The Hive coordinates work; Swarms change projects. Fleets apply the same separation within their charters. Guarded initialization, synchronization, updates, and authorized Assimilation keep their narrow upstream exceptions. A concrete instruction from the Hive Mind can authorize a specific project operation, but cannot be broadened into standing authority.
2. Assimilation requires the Hive Mind's explicit word, or an explicitly granted project posture permitting autonomous Assimilation of green, in-scope work. A worker, reviewer, passing check, or completed Void session cannot grant that authority.
3. Preserve unlanded work. A dirty worktree, uncertain ownership, an uncollected result, or an unproved remote copy blocks cleanup. A Lictor's scratch copy can be retired only after its report is safely collected and its decision inventory recorded.
4. Swarms and Lictors report to their dispatching supervisor. Fleets relay outcomes through their registered parent channel. Direct intervention by the Hive Mind remains authoritative and is reconciled into the durable record.
5. Report outcomes faithfully: distinguish an accepted launch, observed activity, published change, passing Synapse, and completed Assimilation. Missing evidence is unknown, not success.

The Hive presents concrete outcomes and the decisions needed from the Hive Mind, including a Harvest's full URL when relevant. An idle Fleet does not invent audits or improvement work. Research is dispatched separately only when requested or when uncertainty could change whether or what to build; bounded research can remain within an already authorized implementation task.

### Retained capabilities

| Capability | Hive contract |
| --- | --- |
| Local Claude and Codex | Preserve verified launch, steering, interruption, exit, resume, recovery, model/effort configuration, permission modes, and tool restrictions. Harness choice and runtime placement are separate axes. |
| Isolated worktrees and terminal runtimes | Preserve pooled or runtime-owned disposable copies and visible sessions, including existing verified terminal backends. Neither a Void addition nor a resource-saving preference removes a local runtime. |
| Local Synapse | Retain no-mistakes and its ownership of review, fixes, tests, documentation, lint, publication, and CI when selected. A worker drives its gates; it does not edit around an active run. |
| Delivery choices | Model placement, validation, publication and Assimilation authority independently. Preserve the upstream `no-mistakes`, `direct-PR` and `local-only` inputs through explicit translation; local-only publication never disables validation by implication. |
| Local test machines | Preserve a Fleet's required native machine or guest and its capacity controls. macOS execution requires a macOS runner or local macOS machine; a Linux Void session cannot establish macOS behavior. |
| Persistent Fleets | Preserve charters, isolated homes, scope routing, correlated parent replies, guarded provisioning and recovery. Existing remote-home capability remains optional; the chosen deployment starts with supervisors on the Mac. |
| Supervision and continuity | Preserve durable inboxes, status events, wake acknowledgments, current-state reconciliation, turn-end guards, away/quiet operation, stow, and guarded restart/update behavior for Claude and Codex. |
| Project and operational tooling | Keep project registration, branch/base selection, forge bindings, Pool backends, usage-aware admission, optional operator communication integrations, and guarded synchronization where they support Claude/Codex work. Optional features stay opt-in; this GitHub Void design does not silently remove other local delivery capabilities. |

Local-only work stays under the Hive's local landing authority. A direct Harvest does not acquire an additional independent review pipeline merely because it uses Claude or Codex. The complete GitHub Synapse contract below is the validated Void delivery path. The Imperative must separately declare validation and publication; a local-only branch still receives its required Synapse checks. Existing repository-required checks still apply.

### Native workflow, configurable policy and plugins

Hive owns the general workflow contracts once: admission, definitions, task and decision identity, effective policy, evidence linkage, safe lifecycle transitions and knowledge projections. Port those responsibilities from the existing procedures, rather than accumulating copies of their scripts or keeping parallel rules in hooks and prompts. Historical records remain evidence and do not automatically become effective instructions. The existing firstmate operation stays untouched.

| Boundary | Responsibility |
| --- | --- |
| Native mechanisms | Validate typed records; enforce authority, scope, revision and ownership; make transitions durable and replayable; render one validated Imperative schema into instructions; expose missing evidence and pending propagation. |
| Rules and configuration | Model/class/effort preferences, weighted usage comparison, resource floors and their comparison operators, credit authority, lane limits and expiry, review escalation, proof scope, memory budgets, context thresholds, compaction policy, delivery posture, workspace ownership and communication preferences. Values and temporary trials carry scope, provenance and expiry. |
| Execution providers | Claude/Codex, Git, the forge and Actions, no-mistakes, native test machines and their provisioning/test suites remain external executors. The proposed Hive-owned worktree provider implements the same isolation boundary; see [Tooling](#tooling). Hive validates executor receipts without building a terminal, hypervisor or review engine. |
| Project and presentation plugins | Lavish supplies review pages; VibeHub supplies app controls, milestones, exports and its storage compatibility; optional specification adapters can use OpenSpec. Product schemas, credentials, installers and machine administration stay with their owners. |

Lavish, VibeHub, no-mistakes and the forge are named plugin points, not hidden dependencies in core admission. Each adapter registers its version, scoped capabilities, owner and authentication reference privately and implements only the operations it supports. Requests and receipts bind the home, project, task, generation and source revision. Unsupported operations refuse; an unavailable plugin cannot turn a pending obligation into success. Hive's scoped event/export interface supplies projections and invalidation receipts, without expanding an app's permissions or wiring.

| Plugin point | Operations and durable receipt | Boundary on failure |
| --- | --- | --- |
| Lavish | Fetch a complete round and attachment manifest, present an authorized question, collect an exact answer, acknowledge that round. Store page/round revision, content digest and answer receipt in the private decision store. | Incomplete nested content or an unreadable attachment keeps the round open. A native replacement UI needs separate approval. |
| VibeHub | Export scoped Pool, decision and workflow projections; collect authorized app requests; record source digest, consumer contract version and acknowledgment. | Failed export or contract drift leaves propagation pending. Exact app storage, installation and controls remain plugin-owned. |
| no-mistakes | Start, observe and respond through its supported CLI; collect run/head evidence and follow its branch-custody and synchronization instructions. | An active run owns its fixes. Missing evidence or unknown custody blocks publication/landing; Hive never edits around the gate. |
| Forge | Read requirements and current heads, publish a Harvest, collect checks/reviews, submit an authorized expected-head merge and verify its result. | Missing producer, unreadable rules or a changed head blocks readiness/Assimilation. GitHub is this Void design's selected implementation, not a new source of merge authority. |

A plugin that projects workflow state declares the rules/specification revisions it consumes. A source change marks dependent projections stale until refresh and acknowledgment; configured drift checks compare those bindings. A recorded requirement for such a check does not prove that any existing installation already runs it.

## Tooling

This section proposes a tooling ownership strategy for the Hive Mind's ruling through this design revision's merge word.
It records the 2026-10-10 tooling scout's findings and recommendations; it does not claim that Hive has imported or implemented them.
The four proposed choices are:

1. **Ownership:** fork and adapt tasks-axi into the Pool and Treehouse into the worktree isolation provider; rebuild the small Fleet-specific workflow mechanisms natively; keep the remaining executors behind plugin points.
2. **Licence:** adopt MIT for Hive's original code and the two MIT adaptations, preserving upstream copyright/licence notices and a revision-bound import record.
3. **Starting profile:** use tmux with Claude and Codex, Git and the tools required by the selected delivery route; install browser, review-page, test-machine and network dependencies only when their features are selected.
4. **Test-machine and app boundary:** Hive owns generic reservation, lifecycle, transport and evidence rules; hypervisors, project test suites, app storage/installer compatibility and machine administration stay external behind the existing plugin boundaries.

Nothing is forked or rebuilt before the Hive Mind rules on this proposal.
Hive currently has no licence file.
Licence selection precedes any source import; adding Hive's licence file is a separate subsequent change.
This revision authorizes no forks, repository creation, installations, upgrades or spend.
Implementation and migration leave the existing firstmate installation, homes, worktree pools, status displays and shared no-mistakes configuration untouched.

### Inventory and disposition

**F** means fork and adapt selected source into Hive's repository and vocabulary; **R** means independently rebuild the generic workflow contract; **K** means keep an external dependency behind the named component's plugin or platform boundary.
K is not an installation list: optional and excluded entries have no bootstrap requirement, and platform commands share adapter boundaries rather than each needing a plugin registry entry.
The inventory includes active dependencies, transitive helpers and explicitly unused candidates so their omission from the starting profile is deliberate.
Licences describe the scout's inspected package or upstream revision; an unknown origin or uninspected distributed notice set remains unknown until qualification.

| Tool | What it provides | Hive component / boundary | Origin | Licence evidence | Disposition |
| --- | --- | --- | --- | --- | --- |
| tasks-axi | Queue, holds, moves and archives | Pool / Mind's word | [kunchenguid/tasks-axi](https://github.com/kunchenguid/tasks-axi) | MIT | **F**; one authoritative Pool |
| Treehouse | Reusable worktrees, locks and durable leases | Dispatcher / isolation provider | [kunchenguid/treehouse](https://github.com/kunchenguid/treehouse) | MIT | **F**; Hive-owned provider |
| gh-axi; gh | Complete forge queries, publication and merge transport | Forge / Shadow / Assimilation | [kunchenguid/gh-axi](https://github.com/kunchenguid/gh-axi); [cli/cli](https://github.com/cli/cli) | MIT, both | **K**; GitHub adapter, including gh beneath gh-axi |
| quota-axi | Attributed provider quota readings | Policy / capacity reader | [kunchenguid/quota-axi](https://github.com/kunchenguid/quota-axi) | MIT | **K**; Hive owns policy |
| lavish-axi | Review pages, nested rounds and answers | Mind's word / presentation | [kunchenguid/lavish-axi](https://github.com/kunchenguid/lavish-axi) | MIT | **K**; selected review-page feature |
| chrome-devtools-axi | Browser command interface | Worker proof / browser adapter | [kunchenguid/chrome-devtools-axi](https://github.com/kunchenguid/chrome-devtools-axi) | MIT | **K**; selected browser feature |
| chrome-devtools-mcp; Google Chrome | Browser protocol bridge and browser | Worker proof / browser adapter | [ChromeDevTools/chrome-devtools-mcp](https://github.com/ChromeDevTools/chrome-devtools-mcp); Google | Apache-2.0; proprietary Chrome distribution | **K**; selected browser feature, no bundled browser |
| no-mistakes | Review, fixes, tests, documentation, lint, publication and CI | Synapse executor | [kunchenguid/no-mistakes](https://github.com/kunchenguid/no-mistakes) | MIT | **K**; native policy and receipts, external gate engine |
| tmux | Visible sessions and terminal control | Dispatcher / control / Shadow runtime adapter | [tmux/tmux](https://github.com/tmux/tmux) | ISC permission form, with per-file notices | **K**; initial terminal backend |
| Ghostty | Operator's terminal viewer | Optional presentation | [ghostty-org/ghostty](https://github.com/ghostty-org/ghostty) | MIT upstream; installed app notices not inspected | **K**; viewer choice, no required installation |
| Herdr | Alternative session runtime | Optional runtime adapter | [herdrdev/herdr](https://github.com/herdrdev/herdr) | Apache-2.0 at inspected revision; verify the selected release | **K**; not installed, not a rollout dependency |
| Zellij | Alternative terminal runtime | Optional runtime adapter | [zellij-org/zellij](https://github.com/zellij-org/zellij) | MIT upstream | **K**; uninstalled, experimental compatibility |
| cmux | Alternative desktop terminal runtime | Optional runtime adapter | [manaflow-ai/cmux](https://github.com/manaflow-ai/cmux) | GPL-3.0-or-later root; specified server directories BUSL-1.1; third-party notices | **K**; uninstalled, no source absorption |
| Orca | Desktop runtime with its own worktrees | Optional runtime/isolation adapter | Exact compatible implementation not established | Not established | **K**; unavailable until identity and capabilities are qualified |
| Tart | Local macOS virtualization | Test-machine provisioner plugin | [cirruslabs/tart](https://github.com/cirruslabs/tart) | Fair Source 0.9 at 2.32.1 | **K**; external hypervisor, no source/image import |
| fm-guest-run | Source transfer, bounded execution and result collection | Test-machine transport / Synapse | Local Fleet helper; public source not established | No licence located | **R**; generic runner contract |
| fm-guest-lock | Shared/exclusive resource reservations | Capacity / test-machine admission | Local Fleet helper | No licence located | **R**; native reservations |
| fm-guest-stop; fm-guest-slot-retired | Idle/release eligibility and positive retirement proof | Runner lifecycle / continuity | Local Fleet helpers | No licence located | **R**; provisioner still performs shutdown |
| fm-guest-nightly | Project suite scheduling and stages | Project test-suite plugin | Local project helper | No licence located | **K**; project-owned, no Hive source import |
| fm-guest-nightly-records | Evidence transfer and project result interpretation | Synapse / project test-suite plugin | Local project helper | No licence located | **R** generic collector; project schemas stay external |
| Local firstmate launcher | Cold start, restart and capability offers | Session owner / continuity / existing app plugin | Local app-owned launcher, distinct from upstream firstmate | No public grant established | **R** generic lifecycle; app compatibility stays external |
| fm-dispatch-spend; fm-codex-token-usage | Task/model usage attribution | Native metering / policy | Local Fleet helpers | No licence located | **R**; one metering projection |
| Local token-usage reader: usage.py, report-tokens.sh, breakdown.py | Claude usage-event normalization | Native metering | Local Python/Bash helpers | No licence located | **R**; same metering contract |
| claude-ctx-statusline; quota-tmux | Context observations and terminal usage display | Context / Shadow / optional terminal projection | Local Fleet helpers | No licence located | **R**; shared observations, optional formatting adapter |
| OpenSpec | Optional specification CLI/store | Definition store adapter | [Fission-AI/OpenSpec](https://github.com/Fission-AI/OpenSpec) | MIT | **K**; selected specification feature |
| Local external store layer; fm-store-blocks; fm-store-commit | Definition/rule transactions, validation and closure | Definition / rules / knowledge | Local project store layer | No licence located | **R** general workflow; exact app format stays plugin-owned |
| RTK | Optional command-output filtering | Worker output adapter | [rtk-ai/rtk](https://github.com/rtk-ai/rtk) | Apache-2.0 | **K**; opt-in, complete evidence bypasses filtering |
| Tailworks | Optional review hosting/transport | Presentation / network adapter | [kesk-git/tailworks](https://github.com/kesk-git/tailworks); source unavailable to scout | Not established | **K**; no vendoring without source/rights qualification |
| Tailscale | Optional network transport | Network adapter | [tailscale/tailscale](https://github.com/tailscale/tailscale) | BSD-3-Clause upstream; installed app notices not inspected | **K**; selected network feature |
| ntfy / ntfy-server | Optional notification transport/server | Notification adapter | [binwiederhier/ntfy](https://github.com/binwiederhier/ntfy) | Apache-2.0 at inspected tag | **K**; no demonstrated workflow requirement |
| Local certificate-renewal helper; Mullvad | Host certificate and VPN administration | External machine administration | Local helper; exact Mullvad package not established | Helper licence not located; Mullvad distribution not audited | **K**; outside Hive provisioning |
| Local app mockup-server helper | One-off preview hosting | Existing project/presentation plugin | Local project helper | No licence located | **K**; no demonstrated workflow requirement |
| remote-pi-relay | Other-harness relay | External harness tooling | Not established | Not established | **K**; excluded from Claude/Codex profile |
| gerrit-axi | Gerrit forge operations | Optional forge adapter | [slnkjthien/gerrit-axi](https://github.com/slnkjthien/gerrit-axi) | Apache-2.0 plus NOTICE | **K**; uninstalled, not required for GitHub |
| glab | GitLab forge operations | Optional forge adapter | [gitlab-org/cli](https://gitlab.com/gitlab-org/cli) | MIT upstream | **K**; uninstalled, not required for GitHub |
| tea | Candidate Gitea CLI, no discovered call site | Unselected forge candidate | Exact package not investigated | Not established | **K**; no dependency to introduce |
| Beads / bd | Conditional legacy queue backend | Optional Pool backend adapter | Compatible distribution not established | Not established | **K**; unused, no second queue installation |
| Claude Code | Local and qualified Void execution | Claude runtime adapters / Synapse | Anthropic | Proprietary CLI/service | **K**; selected harness |
| Codex | Local execution and Astra review | Codex runtime adapters / Synapse | [openai/codex](https://github.com/openai/codex) | Apache-2.0 CLI; vendor service remains external | **K**; local selected, both Void routes parked |
| pi | Other agent harness | External harness tooling | [earendil-works/pi](https://github.com/earendil-works/pi) | MIT in inspected package | **K**; excluded from selected profile |
| opencode, pi-signed, grok, kimi, cursor / cursor-agent, omp, muse, gemini, rovo, agy, devin | Other named harness candidates | External harness tooling | Exact distributions not established by command names | Not established | **K**; excluded, no mandatory checks/installations |
| Git | Source identity, worktrees and local landing | Isolation / forge / Assimilation platform boundary | [git/git](https://github.com/git/git); Apple distribution | GPL-2.0 upstream; exact Apple notices not gathered | **K**; external Git engine |
| jq | JSON processing | Policy / forge / adapter platform boundary | [jqlang/jq](https://github.com/jqlang/jq); Apple distribution | MIT upstream; installed notices not gathered | **K**; only where selected implementation uses it |
| Bash; zsh; sh | Script execution | Session / runner / platform adapters | GNU; zsh project; system shell distribution | Homebrew Bash GPL-3.0-or-later; exact system-shell notices not gathered | **K**; qualify required shell behavior |
| Node | AXI and JavaScript execution | Selected tool runtime | [nodejs/node](https://github.com/nodejs/node) | MIT plus bundled third-party notices | **K**; selected Node-based tools require it |
| npm / npx | Package provisioning | External tool maintenance | [npm/cli](https://github.com/npm/cli) | Artistic-2.0 | **K**; maintenance, no automatic runtime install |
| Python; Perl | Parsing, metering, locks and bounded helpers | Native helper / runner platform boundary | [python/cpython](https://github.com/python/cpython); Perl project / Apple | Python-2.0 inspected; installed Perl notices not gathered | **K**; only as selected implementations require |
| SQLite | Read-only gate and usage evidence | Synapse / metering adapter | [sqlite/sqlite](https://github.com/sqlite/sqlite) | Public-domain upstream declaration; installed notices not gathered | **K**; version-bound fallback only |
| curl; OpenSSL | Network requests and digest/certificate primitives | Forge / network / platform adapters | [curl/curl](https://github.com/curl/curl); [openssl/openssl](https://github.com/openssl/openssl) | curl licence upstream; OpenSSL Apache-2.0 inspected | **K**; selected features only |
| ssh; scp; rsync | Remote execution and file transport | Runner / optional remote-home adapter | OpenSSH; installed Apple openrsync | BSD-family upstream; exact Apple notices not gathered | **K**; scp has no discovered workflow call site |
| ShellCheck; actionlint; gitleaks | Shell/workflow lint and optional secret scanning | Synapse / development tools | [koalaman/shellcheck](https://github.com/koalaman/shellcheck); [rhysd/actionlint](https://github.com/rhysd/actionlint); [gitleaks/gitleaks](https://github.com/gitleaks/gitleaks) | GPL-3.0-or-later; MIT; MIT | **K**; project-selected validation, not universal stages |
| Go; cc; Swift; Xcode / xcodebuild | Build selected helpers or project tests | Build / native-test platform boundary | Go project; Apple compiler/SDK distribution; Swift project | Exact Go/cc notices not audited; Swift Apache-2.0 with runtime exception; Apple SDK terms | **K**; build-time or project-specific only |
| Homebrew; ripgrep | Package maintenance; worker search | Provisioning / worker tools | [Homebrew/brew](https://github.com/Homebrew/brew); [BurntSushi/ripgrep](https://github.com/BurntSushi/ripgrep) | Installed licences not inspected by scout | **K**; no automatic upgrades or source absorption |
| macOS; launchctl, osascript, lsappinfo, dscl, open, sw_vers, sysctl, getconf, uuidgen, lsof, ps, pgrep | OS services, notification and process identity | Local runtime / machine administration boundary | Apple and system-component authors | Per-component/system terms | **K**; bounded adapters, no Hive OS layer |
| awk, base64, basename, cat, chmod, cksum, clear, cmp, comm, cp, cut, date, dd, df, diff, dirname, du, env, file, find, grep, head, id, install, ln, ls, md5, md5sum, mkdir, mkfifo, mktemp, mv, nohup, od, paste, readlink, realpath, rm, rmdir, sed, seq, sha256sum, shasum, sleep, sort, stat, strings, tail, tar, tee, time, touch, tr, uname, uniq, wc, xargs | Files, digests, process detachment and ordinary shell operations | Shared platform boundary | OS/package providers | Exact per-binary notices not audited | **K**; capability-check used operations, no vendoring |
| pbpaste; timeout / gtimeout | Optional setup input; conditional bounded-execution fallback | Setup / platform adapter | OS/package providers | Exact distributions not audited | **K**; no universal requirement; timeout CLIs absent in scout |
| setsid, flock, pkill, say | API/builtin/local-function or forbidden-command references, not established CLI dependencies | Platform reference inventory | Implementation-specific | No separate package attribution established | **K** classification only; no installation or lifecycle authority inferred |
| Python IMAP/SMTP; curl-backed Relay; Python voice client / sounddevice / SSH | Optional mail, notification and voice transport | Communication plugins | Standard/protocol libraries and local adapters; exact optional packages not audited | Distribution-specific; unresolved where uninspected | **K**; unused/unproved, feature-selected only |
| cloudflared, ffmpeg, sox; mail / fmx command names | No demonstrated executable dependency | Unselected network/communication candidates | Exact packages not investigated | Not established | **K** classification only; do not introduce them |

The in-process firstmate-calm mod is local harness customization, not another external executable to import; backup copies are inactive artifacts.
Neither is a Hive dependency.

### Fork and adapt: equivalence before replacement

Both adaptations retain upstream attribution, applicable notices and upstream tests in a bounded module, with a recorded upstream base and a maintenance owner.
Hive vocabulary applies to new interfaces and explicit versioned migrations; historical words, evidence and unknown record fields must survive unchanged.
Qualify the chosen revision and its dependencies before import rather than treating an upstream root licence as covering every bundled file.

**Pool:** use tasks-axi's parser, transitions, archives and multi-item moves as the implementation base.
The scout's reproducible source baseline is [tasks-axi 0.2.6](https://github.com/kunchenguid/tasks-axi/tree/9401ff899c0d1d8ae6b4fd8727b9025abda2032c).
Preserve real-format round trips, prose/evidence/unknown fields, archive lookup, queued versus in-flight transitions, unanswered holds through completion/cleanup, idempotent answer receipts, and recoverable atomic multi-home moves.
Prove that private-home selection cannot write into a project's code root and that persistent Fleets are never represented as ordinary tasks.
Keep legacy Markdown import/export and optional backend seams, with an explicit identity migration for legacy follow-up records.
Admission, Mind's word and task generations share one authoritative store; tasks-axi and Hive must not concurrently write equivalent queues.

**Isolation provider:** adapt Treehouse's pool, lease, locking, preservation and VCS integration; keep Git itself external.
The scout's reproducible source baseline is [Treehouse v3.1.2](https://github.com/kunchenguid/treehouse/tree/a5ab29f88f9dc57c06b5ffb43b9f25c79a7acf33).
Expose acquire/observe/release receipts bound to private home, project clone, task generation, branch and physical worktree, replacing terminal-directory discovery only after qualification.
Retain upstream tests for concurrent acquisition, durable leases after exit, stale-owner ambiguity, interrupted acquire/return, base/branch setup, checkout hooks and trust, spaces in paths, symlink/common-directory verification, and preservation of dirty, untracked or unlanded work.
Add proof for distinct clones of the same origin, refusal to release a reassigned slot and every supported runtime/provider combination, including runtime-owned isolation.
New Hive work uses a separate owned namespace; existing firstmate slots are never adopted by resemblance or apparent idleness.
This adaptation establishes ownership and reliable receipts; it has no measured memory-saving claim.

### Rebuild natively: workflow contracts

Rebuild the local additions with no located licence independently from their behavioral contracts, without transplanting their source.
Any later proposal to copy such source first needs established provenance and redistribution rights.
Use the existing [components and private state](#components-and-private-state), not a second orchestration tier or another always-on daemon.

| Native responsibility | Required contract and acceptance evidence |
| --- | --- |
| Session launch and restart | Implement `hive-session-start` and continuity through the ordinary policy resolver, home lock and exact process/session generation. Prove cold start, duplicate-launch refusal, verified checkpoint/plan revision, preservation of worker windows/worktrees, partial restoration, and separately reported wake failure. App caller/allowlist translation stays in the existing app plugin. |
| Context observation and projection | Implement `hive-context` with source, time, home, harness/session generation and measured/estimated/unknown status. Reject stale or mismatched observations, handle long turns, and never derive restart authority from guessed model-window capacity. Optional terminal formatting consumes the same cached observations, with no provider poll per pane. |
| Policy and usage metering | Keep `hive-policy` as the single resolver for all roles and restart; quota-axi remains its provider reader. Record task generation, actual model and gate ownership at execution time; preserve event deduplication, account/reset discontinuities, cache/input/output distinctions, read-only source access and crash-consistent generations. Mark estimated/unmeasured coverage and freshness explicitly; token counts are not quota percentages or a billing guarantee. Apply [resource admission](#effective-policy-and-resource-admission) before spend. |
| Test-machine reservation | Reserve before mutation; preserve the observed runner profile's two shared lanes, at most one compile and one flow, with captures/nightly exclusive and inherited locks valid. Express capacities/classes as the runner's configured profile, not universal project rules. Prove concurrent admission, no third lane in that profile, exclusive/shared exclusion and refusal to release another generation's lease. |
| Test-machine transport and lifecycle | Bind source/base/head and cache identity, confine synchronization to the owned target, enforce disk/capacity checks and finite execution bounds, and refuse host fallback. Cancellation must positively account for guest-side processes before releasing capacity. Unknown guest activity prevents stop; idle release and slot retirement require current positive ownership proof. Only the provisioner performs guest boot/shutdown. |
| Evidence collection and lifecycle records | Retain full proof on transfer/test failure, separating transport health from the test verdict. Bind environment, task generation, source head, run and artifacts; replay collection without duplicate completion. Project result schemas/stages remain in the test-suite plugin. Preserve independent task, report, decision, specification and cleanup receipts under [requirement closure](#requirement-closure-and-worker-retirement). |
| Definition, rules and store transactions | Implement the [definition/rule/knowledge contract](#definitions-standing-rules-and-generated-knowledge), including original words, accepted revisions, validation before commit, pending closure after code lands, atomic recovery and replay without double commit. OpenSpec and app-specific section/storage formats remain optional adapters; a failed store update cannot claim completion. |

Historical reports of guest cancellation or shutdown defects are regression scenarios to qualify, not proof that a current release still has those defects.
Native acceptance exercises state, side effects and crash recovery through executable interfaces; script renaming or matching instruction text cannot establish equivalence.

### Keep dependencies: feature-scoped plugin contracts

Every K entry is provisioned only if its feature is selected, with its required transitive runtime and verified version/capabilities.
Already available tools may be reused; selection does not authorize an upgrade, account change, service start or network exposure.
Retained terminal interfaces preserve inherited Claude/Codex capabilities, but an uninstalled or unqualified backend cannot be advertised as ready.
Unknown licences block source absorption or redistribution, and unknown capability identity blocks enablement; neither requires installing unused tools to resolve it now.

| Plugin boundary | Kept dependencies and integration commitment |
| --- | --- |
| Harness and terminal runtime | Claude/Codex and initially tmux implement the [runtime adapter contract](#runtime-adapters-what-the-void-adds). Preserve stable endpoint IDs, verified composer clearing, acknowledgments, semantic activity and exact-target control. Ghostty is an optional viewer; Herdr, Zellij, cmux and Orca remain optional interfaces qualified only when selected. Other harness tools are excluded from the starting profile. |
| Forge | Keep gh-axi and its gh dependency, with alternative forge CLIs only for a selected forge. Hive owns repository identity, authority and source/head binding under the existing forge contract. Qualify pagination, complete machine-readable responses, REST-only cloud restrictions, stale heads, unknown mergeability and authorized merge read-back; compact display output is not complete evidence. |
| Resource reader | Keep quota-axi behind the policy evidence interface, reading only the selected Claude/Codex scope by default. Use bounded cached observations with explicit age; unknown windows cannot prove free capacity. Observation must not invoke models, refresh credentials or switch accounts to evade a refusal. |
| Local Synapse | Keep no-mistakes behind start/observe/respond and its supported custody/synchronization protocol. Prove reattachment, real daemon failure, parked/unknown custody, full findings, changed heads, separate validation/publication and archived evidence. Any read-only SQLite fallback is version-bound and retired when complete supported CLI reads replace it. Hive neither mirrors the engine's state machine nor edits its shared configuration. |
| Review pages and browser proof | Keep lavish-axi behind the existing complete-round/answer contract; persist every returned round before acknowledgment and do not claim recovery for vendor feedback never returned. Browser proof uses chrome-devtools-axi, MCP and Chrome with verified selected-page identity, navigation, screenshots, attachments and disconnect behavior. Start only for selected work; no idle board/browser service and no browser-based Codex dispatch. |
| Test machine and project suites | Keep Tart, SSH/file transport, native SDKs and project suites behind reservation/execute/collect/release. Hive owns the generic records above; suite scheduling, stages, result interpretation, installers, VM images and Apple tool permissions remain external. Platform tools are qualified on the actual runner, not inferred from host versions. |
| Specification and legacy Pool stores | Keep OpenSpec only for a selected store adapter, with identity/revision, transaction recovery and closure receipts. Beads retains a conditional compatibility seam; it never creates a competing default Pool or requires bd installation. Exact app storage compatibility remains at the existing project plugin. |
| Optional presentation, network and communication | RTK, Tailworks, Tailscale, ntfy and the listed relay/voice/mail dependencies stay opt-in. Preserve unfiltered authority/evidence output; measure RTK before considering a default. Hosting, tunnel, certificate and VPN administration need their own authorized setup. Local mockup/nightly helpers stay project-owned; no new Hive service is implied. |
| Platform and development | Git, shells, language runtimes, JSON/SQLite/network utilities, OS commands, build tools and linters remain external under the component boundaries in the inventory. Install only what the selected implementation or project's checks require; package managers are provisioning tools, not startup dependencies. Verify flags, versions and bounded subprocess behavior without forking commodity tools. |

Herdr was not installed in the scout's environment; its inspected [Apache-2.0 source](https://github.com/herdrdev/herdr/blob/2563803dca97c040beaf3dc3acdcb5a3221b4238/LICENSE) does not assign that licence to older releases.
The selected [Tart 2.32.1 licence](https://github.com/cirruslabs/tart/blob/2.32.1/LICENSE) is Fair Source 0.9, so Tart remains external with its usage conditions and no bundled VM images.
Keep applicable Apache notices/change notices and each dependency's own distribution terms; do not place GPL, BUSL, proprietary or unresolved source under Hive's proposed MIT header.
The import record must identify each imported portion, upstream repository and exact revision, licence/copyright and third-party notices, retained tests, local changes and maintenance owner.
No private home state, credentials, decision records or global configuration notes enter a public source import.

### Ordered implementation plan

Each phase is bounded work after the ruling, not permission to begin all phases or alter the operating firstmate fleet.

1. **Licence and contracts first:** record the ruling, add the chosen licence in its separate change, and define import provenance, component ownership, typed identity/receipt schemas, capabilities, configuration precedence and failure states. Establish the minimal profile and baseline elapsed time, process count, peak memory and disk for dispatch, idle polling and validation before claiming resource savings. Ordinary observation uses no model turns.
2. **Pool:** after licence and identity schemas, adapt tasks-axi, first importing records read-only and proving round-trip/crash recovery, then selecting one writer in a new Hive private home. Integrate Mind's word and native admission; build definition/rule/knowledge transactions against this store, with optional external store adapters.
3. **Isolation with tmux:** after Pool task generations and home/clone identity, adapt Treehouse and the tmux adapter in isolated qualification environments. Prove leases, provider receipts, distinct same-origin clones and uncertain-launch reconciliation before enabling dispatch; never switch a live firstmate pool to qualify the provider.
4. **Session, context, policy and metering:** after Pool and runtime identity, implement native lifecycle and attributed observations, use quota-axi as the reader and preserve app translation at its plugin boundary. Prove safe checkpoint/restart, resource parks, fresh resume authority and shared observation caches without another daemon.
5. **Synapse, forge and test-machine integration:** adapters can be developed alongside the preceding phases, but dispatch waits for their identity and custody gates. Preserve no-mistakes fix ownership, current-head evidence and project-specific policy without changing shared configuration. Qualify generic reservations, cancellation, transfer and proof against external Tart/SSH and project suites; resource-heavy tests remain opt-in and capacity-controlled.
6. **Review pages and optional features:** after Mind's word and evidence receipts, qualify complete Lavish rounds, browser proof and selected hosting/network/communication features. Retain other terminal/forge interfaces without installing them; do not substitute a new UI or configure networking implicitly.
7. **End-to-end proof:** after the required selected adapters pass, prove a complete local lifecycle and then the Claude session/GitHub path, including GitHub Synapse with local Astra and Mac Assimilation. Exercise crash/replay at launch, answer, proof, publication, merge and retirement. Retire only Hive migration shims whose replacements prove equivalence; the existing firstmate remains untouched. Parked Codex Void routes do not block this sequence and remain subject to their separate readiness contract.

## Components and private state

Hive combines instructions, skills and native workflow helpers with the proposed Pool and isolation adaptations and external executors described in [Tooling](#tooling).
It needs no new hosted control service.
The `hive-*` command names below are **proposed interfaces**, not installed commands or vendor flags.
Their upstream owners are named so an implementation can port behavior without reproducing it in several places.

Each supervisor has an explicitly selected private home outside the public project repository. The following are logical stores within that home, not machine-specific locations. Use atomic record replacement, append-only event journals, and home/task locks. Missing or malformed authoritative records stop the affected mutation; a cached display cannot authorize it.

| Component and command interface | State and location | Failure behavior |
| --- | --- | --- |
| Session owner: `hive-session-start` | Private home lock, startup digest, schema version, charter, inherited preferences, registered direct reports. Ports `fm-session-start.sh`. | Lock refusal leaves the session read-only. Deferred network checks remain unverified until their results arrive. |
| Pool: `hive-pool add/read/update` | Private durable work items: task identity, project, scope, dependencies, delivery path, dispatch class, artifact links, holds and dates. Ports the home-scoped `fm-tasks-axi.sh` interface. | Failed dispatch/finish transitions cannot be reported as successful. Capacity deferral leaves the item queued. |
| Mind's word: `hive-word hold/answer/complete/verify` | Private held task, reason, origin, decision provenance, answer, and release/completion state. Ports `fm-captain-hold.sh`. | An unanswered or unreadable hold prevents dependent dispatch, Assimilation, and closing that question. |
| Imperative author: `hive-imperative create/revise/render` | Private canonical text, revision and digest, plus exactly what was delivered for each generation. Ports `fm-brief.sh` and its intent/completion helpers. | Empty sections, unresolved placeholders, or incompatible delivery settings refuse dispatch. |
| Admission: `hive-admit create/check/approve` | Private immutable request and admission receipt bound to the Imperative revision, its embedded Object/Standard/Output/Boundary fields, inherited evidence and authority. `approve` records a required ruling, not a routine extra wait. | Missing fields, a material guess or absent required authority refuses dispatch; a high-confidence authorized reading proceeds under the Imperative contract. |
| Definition: `hive-spec define/accept/close` | Private specification revision, acceptance receipt, requirement/evidence links and closure receipt; optional adapter points at its canonical external store. | Product gaps hold acceptance; changed requirements invalidate the affected acceptance and proof bindings. |
| Effective rules: `hive-rule record/amend/resolve` | Private original words, amendments, scope, effective/expiry times, supersession and resolved-rule digest. | Preserve originals; unresolved precedence or expired authority blocks the dependent action. |
| Policy resolver: `hive-policy resolve` | Private role/profile selection with rule digest, resource evidence, freshness, permissions and permitted fallbacks. | Unsupported model, missing required evidence or exhausted admission allowance refuses launch without changing accounts or model. |
| Fleet owner: `hive-fleet provision/route/recover` | Private charter, parent channel, project/clone identity, publication/validation defaults and home-bound isolation ownership. | Refuse ambiguous ownership or an unsupported local-only route; do not fall back to hand wiring. |
| Knowledge: `hive-memory render/check/propagate` | Private source-owner index, generated projections, maintenance queue, version/digest and per-consumer acknowledgment. | Drift and incomplete propagation remain visible; stale projections cannot authorize work. |
| Context and capacity: `hive-context observe/checkpoint`, `hive-park`, `hive-resume` | Private observation provenance/freshness, checkpoint readiness, parked obligations and restart policy/target receipts. | Unknown observations do not trigger destructive control; no restart without a verified checkpoint and target. |
| Dispatcher: `hive-dispatch` | Private accepted admission/specification revisions, resolved policy receipt and task execution record, launch intent and receipt, runtime ownership and capacity reservation. Ports `fm-spawn.sh` and verified Claude/Codex adapters. | Preserve uncertain launches for reconciliation; never blindly create a second session after a timeout. |
| Steering and control: `hive-send`, `hive-control` | Private message outbox/inbox, acknowledgments, revisions, lifecycle operation receipts. Ports `fm-send.sh` and `fm-control.sh`. | Delivery acceptance is distinct from acknowledgment. Unsupported lifecycle actions refuse; a chat message is not an interrupt command. |
| Shadow: `hive-shadow`, `hive-state` | Private observations, poll cursors, next-due times, wake queue, handled sequences, owner generation and liveness. Ports `fm-watch.sh`, `fm-crew-state.sh`, and wake helpers. | Unknown remote state stays unknown; failures neither erase evidence nor trigger destructive recovery. |
| Synapse: `hive-synapse start/collect/respond` | Private run ledger, review packets, finding decisions, bound head/base SHAs, fix rounds and executor; public sanitized checks/reviews on GitHub. | Incomplete or stale evidence keeps the gate pending or failed. Only the selected executor owns fixes. |
| Assimilation: `hive-assimilate`, `hive-assimilate-local` | Private authority receipt, verified Harvest identity/head, required evidence, merge result and completion receipt. Ports `fm-pr-merge.sh` and `fm-merge-local.sh`. | Refuse on missing authority or unmet guards; verify actual forge state after submission. |
| Continuity: `hive-stow`, `hive-recover`, `hive-sync`, `hive-retire` | Private curated memory, recovery checkpoint, pending transitions and cleanup receipts; recorded worktree ownership. Ports stow, sync, control and teardown contracts. | Preserve records and copies on failed collection, synchronization or cleanup; never reset/discard to make recovery convenient. |

The minimum execution record carries `task_id`, `home_id`, `generation`, `kind` (Swarm or Lictor), project/repository identity, base branch and source SHA, expected output branch, Imperative revision/digest, class, concrete harness/model/effort, placement, validation policy, publication mode, Synapse executor, authority reference, admission/specification revisions, effective-rule digest, and timestamps. Every record also binds the adapter backend, implementation version and capability-evidence revision. A local record adds the owned worktree and runtime endpoint; a Void record adds vendor, environment generation if applicable, session/task or Actions run ID and URL, trigger receipt, observed output branch, and collection receipt. A backend change creates a new execution generation; old results retain their original backend identity. Record requested and observed model separately; absence of observed model evidence cannot be filled from the request.

Execution, delivery, and decision state are separate. Execution can be planned, launching, running, stopped, failed, or unknown. Delivery can be unpublished, collected, Harvest-open, in-Synapse, ready, assimilated, or retired. Holds can coexist with any of those. An event saying `done` does not imply Assimilation, and a later `working` event does not close a keyed decision.

### The Pool and the Mind's word

Pool items describe work, never persistent agents. A Fleet's routed work belongs in its Pool; the parent keeps a handoff receipt rather than a second competing task. Dispatch reserves capacity and transitions a queued item together with recording the launch attempt. If submission becomes uncertain, retain the reservation until reconciliation proves what happened.

An operator decision is an ordinary Pool item held for the Hive Mind; routine technical acceptance within approved scope belongs to the owning Fleet. Prefer holding the work it gates; create a separate question only when it has its own scope. Record the question, consequence, options, recommendation, and any explicit revisit date privately. `hive-word answer` records the actual answer and either releases the work or completes the question. Replays of the same answer are idempotent; conflicting answers require reconciliation. A deferral date is not approval.

Before completing a Lictor report, the supervisor inventories every remaining operator decision and records the held items, or explicitly records that there are none. Finishing the report and closing those decisions are distinct actions. Cleanup must retain an open question even when its source report or Harvest has been delivered. This ports the [held-task lifecycle](https://github.com/kunchenguid/firstmate/blob/fb75c1f94170df7e0751147ea61afabcf5d4e104/docs/captain-hold-lifecycle.md).

### Definitions, standing rules and generated knowledge

A Pool item can pass through `definition-needed`, `defining`, `accepted`, `building` and `delivered`, with holds kept separately. The Fleet names the main deliverable and subordinate lanes, commissions only the needed definition work, and accepts a revision against the already authorized intent. A sufficiently complete existing specification can be referenced directly; this is not mandatory planning ceremony for every small task. The acceptance receipt identifies the Fleet's supervisor, specification digest, admission revision and open product questions. A product choice returns to the Hive Mind; technical acceptance cannot settle it. Build dispatch refuses an unaccepted required revision, and later edits invalidate only affected acceptance/evidence rather than silently changing the worker's target.

The private rule store separates original words from their operational interpretation. `hive-rule record` appends immutable words and origin; `amend` adds an attributed revision or supersession without rewriting history; `resolve` computes rules effective for the current home, project, role and time. Each rule carries authority, scope, status, effective/expiry times, source references and requirement/evidence links. An unresolved conflict holds the action it affects. Expired waivers, archived narrative and abandoned trials remain readable but are not loaded as current policy. A missing render field is not proof that a rule was retired.

Hive supplies a native specification/decision store interface. OpenSpec is an optional adapter; neither its CLI, the VibeHub-specific store format nor nested Git repositories are prerequisites for all users. An adapter must retain immutable identity, accepted revision, amendment history, proof references, atomic update/recovery and closure receipts. VibeHub owns its exact compatibility format. Failed external updates leave a pending transaction with a replay key, never a fictitious completed archive.

`hive-memory render` derives shared and Fleet-specific instructions from the effective rule set and each fact's single canonical owner. Projections record source revisions, scope, renderer version and digest. `check` reports duplication, budget violations, stale sources and consumer drift using configured budgets; it never repairs authority by copying contradictory prose. `propagate` records what each consumer received and acknowledged. Missing propagation remains visible, and dispatch/restart refuses a stale projection when the changed rule affects its authority or behavior.

Knowledge maintenance has one writer per store and an explicit queue. Stow can enqueue uncovered knowledge under its canonical owner; folding, archiving and propagation run only under an authorized maintenance policy or an explicit instruction. Their cadence is configuration. Deferred research and held work stay in the Pool for the Hive Mind; a maintenance cycle does not authorize picking them up. Source changes produce invalidations and receipts, not competing hand-maintained rule copies.

### Complete review rounds and answers

`hive-word` handles channel-independent rounds as well as simple questions. Before marking a round read, collect its complete nested text, all questions and an attachment manifest with content revisions/digests and fetch outcomes. A missing, truncated or unreadable part keeps the round incomplete and yields a precise fetch blocker. Plugin transport acceptance, content-read acknowledgment, the Hive Mind's answer and decision completion are separate states.

One owner presents and reconciles each round, through Lavish or another configured channel. Store the exact answer privately, bind it to the question/round revision and record a per-question disposition. Replayed events are idempotent; a changed question needs reconciliation, not silent application of an older answer. An unanswered call remains visible until answered or explicitly superseded, regardless of age. Archiving a round must preserve a verifiable answer and its completion receipt; absence from the active queue proves neither. A collected Lictor report, answered page round and completed decision have independent receipts, so none can close the others by inference.

## Imperatives are given at dispatch

### Admission checks the Imperative

Object, Standard, Output and Boundary are required typed fields inside the Imperative's own sections, in the single schema defined under [render and deliver](#render-and-deliver-the-imperative).
There is no separate four-part block beside the Imperative and no independently maintained admission copy of its contents.
`hive-admit` preserves the original request, checks that schema and records a receipt bound to its revision/digest and the authority used.
Fields may draw on an identified, readable, revision-bound task/specification record.

When every part can be filled with high confidence from the command's words and the record, state that reading in the dispatch and proceed within existing authority, without a separate restatement-and-wait turn.
Stop to ask when a part is a genuine guess that would change what gets built, or when a boundary involving spend, deletion, security or publication needs the Hive Mind's authority.
Already explicit, applicable authority satisfies that boundary check; inference cannot supply it.
Record the unresolved part and its consequence as a hold; the Fleet resolves technical detail already inside accepted scope.
Dispatch without all four fields is refused on every launch path, even when the intended work seems obvious.
Malformed input likewise fails closed.

Record-only maintenance may use a narrower schema only when a named, configured exemption proves it cannot launch project work or broaden authority. Provisioning a persistent Fleet uses a charter/provisioning record instead of pretending it is a Swarm task. Neither exemption allows build work to bypass admission. Hooks may improve feedback, but `hive-dispatch` and every adapter entry point enforce the same native check for local and Void work.

The validated Imperative schema renders both the dispatch text and launch parameters, with admission/specification revisions and policy digest embedded in the private receipt.
Contradictory includes or independently inferred flags refuse dispatch.
Steering that changes scope revises that same schema and repeats the affected checks; it cannot smuggle a new permission through a message.

| Independent dispatch axis | Contract |
| --- | --- |
| Placement | Local or a qualified Void backend, with exact runtime/isolation ownership. |
| Validation | Required Synapse executor and proof policy, or an explicitly authorized direct-delivery policy. A change of publication mode cannot remove a required gate. |
| Publication | Harvest or local-only branch delivery. A local-only route needs no forge binding, remote or fabricated Harvest. |
| Assimilation authority | Attended explicit word or recorded autonomous posture, checked separately at landing. Neither validation nor publication supplies it. |

Validated local-only delivery is a first-class combination: the Fleet validates and presents the exact local branch; the Hive retains local landing authority. The inherited command-mode inputs are compatibility syntax, not the internal state model.

### Render and deliver the Imperative

The Hive owns the canonical Imperative in its private home, or delegates custody to the dispatching Fleet's private home with a parent reference. Nothing requires committing it to a public repository. The source branch must be pushed for a Void launch; the Imperative is supplied as the session launch prompt or rendered for the selected GitHub integration under the visibility rules below. Provider receipt of that prompt does not authorize publishing it in a Harvest, comment, log, or code file.

Preserve the four sections from firstmate's [dispatch scaffold](https://github.com/kunchenguid/firstmate/blob/fb75c1f94170df7e0751147ea61afabcf5d4e104/bin/fm-brief.sh), with the command fields embedded as follows:

1. **Task:** contains **Object** (project, task and concrete artifact or behavior) and **Standard** (acceptance conditions, required evidence and authoritative requirements/rules). Preserve the Hive Mind's intent separately from the Fleet's implementation specification, include the substance of referenced evidence and assign the dispatch class. Do not turn implementation choices into purported operator intent.
2. **Setup:** binds that Object to repository, immutable source SHA/base, output branch rule, role, harness/placement, dependencies, available tools, allowed access and isolation assertions. Local execution verifies physical working directory and repository top level before branching; Void execution verifies repository/base/source identity before editing. Setup implements the same schema's Boundary and cannot expand it.
3. **Rules:** contains **Boundary** (scope, exclusions, allowed effects, resources, permissions and stop/escalation conditions), including authorized repair scope and finite retry limits. Supply complete validation commands that implement the Standard and identify the delivery/decision channels and blocker owner. Do not assume private Mac files or local tools exist in the Void.
4. **Definition of done:** contains **Output** (exact deliverable, recipient, validation/publication choices and completion evidence against the Standard). A validated Harvest requires agreed Synapse results at its current head; a direct Harvest requires publication and its stated checks; validated local-only work requires a clean branch plus passing current-head Synapse evidence; an explicitly authorized direct local route carries its stated checks; a Lictor requires a collected report and decision inventory. Define the worker's stopping point separately from later Assimilation.

The rendered prompt starts with the worker role boundary, so project instructions cannot turn a Swarm into a supervisor. Before launch, validate that the text fits the selected transport without truncation and that required files, network access, result collection, tool installation, and credentials are already available. Secrets and unrelated private history never belong in the prompt. If those requirements cannot be met, hold dispatch or choose an explicitly permitted local route.

The Claude session adapter passes the rendered text to `claude --cloud "<imperative-text>"`; the measured Codex legacy command is `codex cloud exec --env <environment-id> --branch <branch> --attempts 1 "<imperative-text>"`, retained as evidence while dispatch is parked. The new Codex backend must supply a supported command-line route once verified; it must not reuse legacy IDs by assumption. A routine-based dispatch may carry the text in its supported trigger payload. These are argument shapes, not instructions to interpolate arbitrary text into shell code. Launchers pass an argument vector or supported input channel and keep prompt-bearing transcripts private.

For GitHub-triggered work, the canonical Imperative still stays private. The Fleet may render a separately authorized, complete public-safe execution prompt whose scope, checks and stop conditions are sufficient without private context. A public `@claude` comment, workflow input, log or artifact cannot transport the canonical copy. If the task cannot be fully specified through the selected transport without exposing private material, dispatch it through a verified private session route or locally instead. No confidential prompt relies on log masking as its privacy boundary.

A Swarm never waits in the Void for its next assignment, a missing dependency, review approval, or Assimilation. It completes its Imperative, pushes its authorized result, records a receipt, and stops. A Lictor likewise returns its report and stops. Later fixes are bounded new work, possibly resumed through a vendor event. An unexpected decision produces a blocker receipt and an exit, not an idle polling session.

Steering changes the canonical Imperative first. The Fleet saves a revision, assigns a message ID, and records transport acceptance separately from worker acknowledgment. Claude cloud sessions can accept a steer by session ID while active; an Actions run does not inherit that control. A Codex Void task is submit-and-collect; no cloud steer command was established. Changed Codex work becomes a successor Imperative after reconciliation of the existing task, rather than a guessed follow-up API. A late steer cannot silently invalidate a finished deliverable without creating a new delivery obligation.

The measured Claude exercise used a committed brief. Prompt delivery is supported by the CLI, but the intended **prompt-only bootstrap and private canonical-copy contract still need an integration check**. The implementation must not copy that exercise's committed-brief technique into Hive by accident.

## Eight dispatch classes

The class numbers stay **0–7**. They classify the work, independently of whether its deliverable is a Swarm change or a Lictor report.

| Class | Scope |
| --- | --- |
| 0 — Option weighing and feasibility | Compare plausible approaches, establish feasibility, and recommend a direction before the Hive Mind chooses. Produce evidence and tradeoffs, not speculative implementation. |
| 1 — Architecture and specification | Define boundaries, behavior, interfaces, invariants, and a buildable specification. Carry consequential design reasoning into implementation where that reasoning still determines the work. |
| 2 — Adversarial review | Challenge a proposed design or change for correctness, missing requirements, regressions, and failure cases. Findings are a review deliverable; they do not confer permission to implement or Assimilate. |
| 3 — Partially specified build | Implement an agreed outcome whose behavior or scenarios are known but whose internal structure still needs substantial definition. Preserve the reasoning requirements of an architectural change rather than treating a written outline as a mechanical recipe. |
| 4 — Specified build | Implement a bounded feature or fix whose behavior, approach, and acceptance conditions are sufficiently settled. Remaining uncertainty is ordinary implementation detail, not an unresolved product or architecture choice. |
| 5 — Structured research | Gather and organize existing evidence: inventories, interface maps, source comparisons, reproducible captures, and evidence tables. Escalate to the relevant reasoning class if the work turns into feasibility, architecture, or diagnosis. |
| 6 — Diagnosis | Reproduce a failure, distinguish competing causes, and establish a supported explanation and repair direction. An investigation request alone does not authorize shipping that repair. |
| 7 — Mechanical work | Perform narrow, readily checked edits such as a specified rename, typo correction, or deterministic formatting change. Hidden behavioral decisions require reclassification. |

The private dispatch configuration maps each class to eligible Claude/Codex profiles and placement preferences. Each profile includes harness, real model name, effort, supported capabilities, and selection rationale. Separate entries configure the Hive, Fleet supervision, Synapse reviewers, validation steps, and fixers; those roles do not create extra dispatch classes.

The Fleet resolves the class and concrete profile at every intake. An explicit task instruction wins, followed by the configured matching rule, configured default, then the supported static default. Inspect actual model support and available usage evidence; record uncertainty rather than inventing precision. A malformed configuration, unsupported required capability, or unusable authentication stops dispatch. Never silently change vendor, model, permission mode, or placement to bypass a refusal. Where local fallback is preauthorized, record the fallback and its reason in the execution record.

### Effective policy and resource admission

`hive-policy resolve` applies the effective scoped rules to every role: the Hive, Fleet supervisors, eight dispatch classes, reviewers, fixers and validation steps. It emits a receipt with requested and resolved model/effort, harness, placement, permissions, validation/publication choices, resource reservation, allowed fallback and the source/rule revisions that selected them. Restart uses this same resolver, not a separate default profile. An explicit request retains its priority within current authority; conflicts or unavailable capabilities return a hold rather than a silent downgrade.

Resource readers provide timestamped, attributed evidence with an accuracy/freshness classification. Configured policy decides which usage/credit/resource readings are required and how to weigh available capacity. Unknown or stale values cannot prove capacity; missing required evidence blocks admission, while optional metering can remain visibly unknown. Threshold values, comparison operators, scope, lane limits, expiry and credit authority are configuration. A floor policy defined as at-or-below must fire at equality; it is not a strictly-below check and cannot retain a superseded single-role exception.

Reserve capacity atomically with launch intent and reconcile uncertain submissions before releasing it. `hive-park` waits for a safe checkpoint or pipeline gate, records custody, outstanding work, the resource blocker and resume authority, and stops admitting dependent work. It does not abort a validation round to save capacity. `hive-resume` rechecks fresh evidence, current policy and authority and continues the owned task/gate once, without changing accounts, funding, credentials or model to escape the hold. A timer or resource recovery is not itself new spend authority.

## Runtime adapters: what the Void adds

Each adapter implements `probe`, `launch`, `observe`, `collect`, and a capability declaration for `steer`, `interrupt`, `resume`, and `stop`. These are Hive interfaces, not assumed vendor commands. Results distinguish accepted, running, finished, failed, unknown, and unsupported; they carry source evidence and the bound task generation. Completion always requires collection and delivery checks above the adapter.

Private dispatch configuration names the backend explicitly: `claude-local`, `claude-session`, `claude-github`, `codex-local`, `codex-legacy`, or `codex-new`. These are proposed Hive identifiers. A capability record stores implementation version, environment generation, authentication context reference, supported operations, result/retention contract, evidence receipts, last probe, next recheck and retirement state. Readiness is per capability: accepted task creation alone cannot enable publication or Astra reviews. The Fleet chooses only a backend meeting the Imperative's requirements and records any preauthorized fallback.

The shared contract owns source identity, private Imperatives, result schema, Synapse and Assimilation. Vendor-specific environment discovery and commands stay inside their backend. A parked, missing or retired capability raises one actionable blocker and holds dependent work; it does not cause repeated launches or an automatic migration of a running task.

### Local Claude and Codex

The local adapter ports firstmate's runtime/harness separation. `hive-dispatch` acquires an isolated worktree from its configured provider, records ownership, launches `claude` or `codex` with the resolved profile through the verified terminal backend, and supplies the private Imperative. It records the runtime endpoint, worktree, branch and session identity before accepting worker status.

`hive-send` writes a durable steering message and rings the recorded endpoint. `hive-control` performs verified lifecycle operations; relaunch preserves the same worktree and work. `hive-state` reconciles the current no-mistakes run, semantic harness activity, inbox acknowledgment and status journal. It does not infer a live run from a stale status tail. The local adapter retains existing native test-machine routing and capacity reservations.

No worker may administer a shared pipeline daemon or another task's worktree. A missing endpoint is not proof that work was lost or that a second endpoint is safe. A pipeline client timeout first prompts inspection and reattachment; a confirmed daemon failure is a supervisor-owned blocker. Preserve Codex compaction and Claude handover/recovery behavior through their respective harness adapters rather than imposing one vendor's lifecycle on the other.

### Fleets with local-only custody

`hive-fleet provision` accepts a local-only project without inventing a forge binding. Its charter records the owning supervisor and parent, project/clone identity, validation policy, local publication mode and the Hive's landing authority. `route` sends the work to that Fleet's Pool and preserves one parent handoff receipt. The Fleet owns definition acceptance, isolated branches, Swarm supervision, validation and evidence collection; the Hive performs authorized local Assimilation.

Isolation ownership binds the private home, project clone, provider, task generation and branch/worktree. Two clones of the same origin must not collide in a shared pool. Recovery reconciles those identities and provider receipts rather than guessing from a branch name or an apparently free slot. Provisioning, routing, restart and cleanup must work through the same native interfaces for local-only and Harvest projects; manual symlink/configuration wiring is not an accepted fallback.

On completion, the Fleet presents the clean exact branch and its validation receipt to the Hive. Failed validation, unknown branch custody, unresolved decisions or unavailable proof keeps the handoff pending. Cleanup remains with the recorded owner and requires safe reachability of the work and retained decisions; a local-only project is not exempt from preservation or validation.

### Claude in the Void

**Measured on 2026-10-10:** a launch from a pushed branch was accepted in about 14.5 seconds; a steer was accepted in about 1.3 seconds and its pushed acknowledgment became visible on the Mac within 43 seconds. The same session opened a Harvest through GitHub REST, Auto-fix repaired both a failed check and a review comment, all three native OS checks passed, and Assimilation occurred on the Mac. This establishes a small end-to-end delivery path, not desktop-platform parity or the complete Synapse design.

The implementation sequence is:

1. Persist the launch attempt, expected repository/branch/source and prompt digest. Run `claude --cloud "<imperative-text>"` with an interactive terminal attached: the measured launch refused piped stdout. Capture the returned session ID and URL privately without treating CLI exit as task completion.
2. Bind the session's actual output branch after validating its receipt against the launch record. Claude may choose a branch name different from the source branch; never adopt an arbitrary new branch solely because it appeared recently.
3. For steering, run `claude -p "<revision-text>" --cloud <session-id> --output-format json`. An `ok` response proves queue acceptance. Require a matching message acknowledgment or observed result before calling the steer applied.
4. Collect the pushed head and sanitized delivery receipt. Create the Harvest through the verified repository-scoped REST shape, `gh-axi api POST repos/<repository>/pulls --input <payload> --full`, after checking for an existing Harvest for that branch. The measured cloud proxy rejected GraphQL-dependent creation; its REST path worked.
5. Finish the assignment and stop. Native Auto-fix may wake a bounded repair when enabled; the Mac owns its registration, expiry and final unsubscribe confirmation.

The measurement does **not** establish a headless native session-status command. `observe` must use a supported session-state surface when one is available and otherwise return native state as unknown, while separately reading authenticated GitHub progress. Do not invent `claude cloud status`, scrape undocumented endpoints, or send model messages merely to ask whether a session is alive. Optional `claude --teleport <session-id>` is a recovery/inspection path; it is not Assimilation.

The remote image did not contain `gh-axi`, and redirected Actions logs were inaccessible through its proxy. Environment preparation must verify the needed tools and REST access before dispatch. For failures that require inaccessible logs, the Mac obtains and sanitizes the relevant output for a new repair Imperative. Committed repository instructions can travel with the source, but private Mac settings and installed plugins must not be assumed to travel. [Claude cloud documentation](https://code.claude.com/docs/en/claude-code-on-the-web) describes the supported launch and recovery surface.

### Claude through the official GitHub integration

The second Claude execution transport is [Claude Code GitHub Actions](https://code.claude.com/docs/en/github-actions). It runs Claude on a GitHub Actions runner: an `@claude` mention invokes interactive work, while a workflow's `prompt` input supports event-driven automation. It is separate from a Claude cloud session and from native Auto-fix, even where the same GitHub App is involved. This transport is documented but was not exercised by the cloud-session measurement.

A Fleet's private adapter configuration records the installed integration, pinned Action revision, workflow identity, permitted trigger actors, credential reference, public prompt policy, model profile, turn/time limits, output schema and result retention. Provisioning the App, credentials and workflow is separate authorized setup; the adapter refuses launch until those prerequisites and publication permissions have been verified.

For a public-safe request, `launch` uses the configured trigger, for example `gh-axi api POST repos/<repository>/issues/<issue-or-harvest-number>/comments --input <payload> --full` with an authorized `@claude` request. Persist the request digest and correlation token first, then the comment receipt, matching Actions run and attempt. A successful comment post is not proof that a workflow started; bot-originated triggers must be exercised with the actual dispatch identity. Reconcile a timed-out post before retrying. No matching run within the configured launch deadline produces a blocker.

The workflow passes a complete rendered execution prompt to the pinned `anthropics/claude-code-action`, checks out and verifies the source identity, and uses the configured tool and model permissions. `observe` reads `actions/runs/<run-id>` and its jobs through `gh-axi api`; `collect` validates the returned branch, head, Harvest and public-safe result against that run and task generation. Job success alone cannot substitute for the delivery receipt or reviewer verdict. Private reports require another verified private collection route.

Additional comments are distinct bounded requests, not a promised steer of an in-flight run. The adapter advertises cancellation or rerun only after its ownership and semantics are verified. A failed or cancelled run preserves its branch and evidence for reconciliation. Native Auto-fix and an Action-based repair must never own the same branch simultaneously. Test that commits made by the configured publication identity actually trigger Synapse; a workflow that publishes without starting required checks has not completed delivery.

### Codex: three backends and a migration boundary

Codex Swarms and Astra reviews share the same Hive interface whether their executor is local, legacy Void or new Void. The local backend is retained operational capacity. The legacy backend records a successful execution-and-collection measurement but is parked because it cannot choose the model. The new backend is also parked until supported command-line dispatch, model selection and collection reach the new service. The measured internal legacy result reader has its own validation boundary below; it is not a public API contract or evidence of new-service access. Browser automation is not the production dispatch interface.

OpenAI's [cloud-environment guide](https://developers.openai.com/codex/environments/cloud-environments) distinguishes the current web/mobile/desktop experience from legacy environments retained for Code Review and the Linear and GitHub integrations, and states an intent to deprecate the legacy experience. No retirement date is established by this design's evidence. Environment generation is therefore part of every capability and execution record; a published new environment is not evidence that a legacy CLI can use it.

**Initial measurement on 2026-10-10 with stable CLI 0.162.1:** the newly published web environment and its tasks were absent from the CLI inventory, and `codex cloud exec` failed before creating a task. **Follow-up on the same date:** against a legacy environment for the repository, submission, status observation from pending to ready, diff and apply all worked from the Mac command line; the task took about three minutes. Status exited zero only when ready. The same login could retrieve the final text headlessly through an internal task-details call. The model-selection probe found no model field, configuration key or environment hook in the CLI 0.162.1 task-creation request. A task requesting `gpt-6-astra` ran on `gpt-5.6-sol`: this route cannot choose the model. These results establish legacy task execution and collection, not new-service access, Astra review or full Synapse/Assimilation delivery. Retain this timing and adapter evidence without dispatching further legacy work or making it a delivery dependency.

| Backend | Command surface, state and readiness |
| --- | --- |
| Local on the Mac | Retained `codex` Swarms and separate `codex exec` review sessions in owned worktrees. Use the installed CLI's supported model and final-message export controls; keep receipts and full review results privately. Preserve normal local lifecycle and Synapse behavior. |
| Legacy Void | `codex cloud exec --env <environment-id> --branch <branch> --attempts 1 "<imperative-text>"` submits to a verified legacy environment. Bind task ID, environment generation and CLI version to the launch. Submission, status, diff and apply have been proved. Final text was collected through the internal reader described below. The backend remains parked for all Swarms, Lictors and reviews because model choice is unavailable; a provider-selected default is not an allowed fallback. |
| New Void | Parked until a supported command-line route demonstrably reaches the new service, honors the configured model and meets the collection contract. Keep discovery, submission, observation and collection behind this backend; its command shape is unresolved. UI availability alone does not enable it. |

The existing [Codex GitHub Action](https://developers.openai.com/codex/github-action) runs `codex exec` on an Actions runner and exposes a final-message output; that is not evidence of a connection to the new hosted Codex service. Hive's command-line-only Codex contract does not select this Action or GitHub mention dispatch as a substitute. A future new-service backend must be driven from the command line and qualified on its own evidence. Claude's GitHub integration remains part of the primary Void path.

The legacy command contract remains version-bound and records measured mechanics, not enabled dispatch. `hive-dispatch` refuses a parked Codex backend before submission, records the model-selection blocker, and offers local Codex only where local placement is authorized:

| Operation | Command surface and state contract |
| --- | --- |
| Probe and submit | The measured launch verified a legacy environment in the authorized context and retained the task receipt. Future compatibility probes are bounded, explicitly authorized command-line investigations; ordinary dispatch remains parked. An empty new-environment inventory is a compatibility result, not permission to guess IDs or change accounts. |
| Observe | `codex cloud list --env <environment-id> --json --limit 20`, continuing with `--cursor` until the bound ID is found or pagination ends. Keep the complete response privately. `codex cloud status <task-id>` exits zero only for ready in the inspected release; nonzero alone does not distinguish pending from failure. |
| Collect code | `codex cloud diff <task-id>` and, when appropriate, `codex cloud apply <task-id>` into an isolated collection worktree. Diff and apply worked in the follow-up. A local collection Swarm must still verify repository/base/head, selected attempt, patch scope and tests before publishing through the ordinary delivery path. Patch collection does not establish a green Harvest. |
| Collect final text | Use the same authenticated login's measured internal task-details request through the legacy result reader. Validate the response and bind the complete terminal answer to the task, repository/base/head and observed model before private retention. Text retrieval worked, but an answer from another model cannot satisfy an Astra review. |
| Change or stop | Advertise only supported, measured controls. No cloud steer-by-ID command was established. Reconcile the task before submitting successor work; failed controls leave its ownership and collection obligations intact. |

Version-matched inspection of the [cloud command implementation](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/cloud-tasks/src/lib.rs), [environment resolution](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/cloud-tasks/src/env_detect.rs), and [submission client](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/cloud-tasks-client/src/http.rs) explains both legacy constraints: the submission payload carries no model-selection field, and the CLI's headless `status`, `list`, `diff` and `apply` commands do not export the final natural-language answer. The follow-up closes the retrieval gap through a separate internal reader, not a newly supported CLI export. Model selection remains unavailable on this path, so a generic `-c model=...` setting or an Astra request in the prompt cannot establish an Astra result.

The legacy result reader keeps its observed request shape, client version, authentication-context reference, response schema and validation evidence privately. It issues only the read-only task-details call for a bound task using the same login, then validates task identity, terminal state, selected attempt, repository/source binding and complete final-message content; bind any model metadata as observed evidence and never fill it from the prompt. A missing field, ambiguous attempt, truncation, identity mismatch, authentication refusal or changed schema makes collection unknown or blocked. Preserve the raw response privately for diagnosis and revalidate after client or service changes. This is an internal, undocumented interface with compatibility risk; no stable public-API guarantee is claimed, and its identifiers or request shape must not be carried into the new backend by assumption.

For a route returning a diff, the Void Swarm makes its result durably collectable and stops; the local collection Swarm fulfills the push obligation. Record separate execution generations under the same delivery task. Verify retention and collect before its deadline. A backend that publishes directly supplies a verified branch instead. Both paths require the same source and result checks.

The inspected legacy task mapping is pending/ready/applied/error; failed and cancelled map to error, while unknown upstream states mapped to pending. Hive retains raw state evidence and treats an absent task, unreadable response, incomplete pagination or unrecognized state as unknown. Ready means available for collection, not passing Synapse. The [CLI reference](https://developers.openai.com/codex/cli/reference/) documents the experimental commands; compatibility must be measured per backend and release.

#### Enabling and retiring a Codex backend

Re-enable a Codex Void backend only after a supported command-line route demonstrates effective model selection and a controlled replay proves discovery, prompt-only dispatch, source binding, terminal success/failure observation, complete result collection, restart recovery and duplicate-submission prevention, followed by a Swarm change passing three-OS Synapse through Mac Assimilation. Record the evidence and authorized configuration change; an accepted default-model task cannot release the parked state. Enable Astra separately after a review-only task returns complete findings and an explicit no-findings result at a known head, with the actual requested model verified. The measured legacy path fails that model requirement; route Astra to local Codex, where model selection is honored, and verify the selected model there. Do not accept a differently modeled cloud review merely because its final text is collectable.

Run a Codex compatibility probe only when the Hive Mind explicitly authorizes it, in an authorized test context through ordinary admission, with no browser interaction. Store the tested version, official route, environment generation, task receipts, capability results and next action privately. A failed probe preserves the working backend configuration and records the concrete gap.

Record any announced retirement deadline and a dispatch cutoff that leaves time to collect active work. At that cutoff mark the backend draining and stop new dispatch; on lost access mark it unavailable immediately. A general deprecation notice does not itself prove that a still-working legacy route has stopped. Collect in-flight results while access remains, preserving branch, patch and review evidence outside provider retention. Do not translate legacy IDs into new IDs or move an uncertain running task by relaunching it. Reconcile each task, then issue a successor Imperative on a ready backend when the recorded authority permits. If results cannot be retrieved, record the loss or uncertainty and hold the task for recovery.

Switch to `codex-new` only after model selection and its own command-line readiness replay pass and dispatch is explicitly re-enabled; rollback selects another proved backend for new work without discarding old receipts. If legacy access ends first, Claude carries permitted Void work and Codex runs locally. An Imperative requiring Codex, Astra, a particular model or Void-only placement cannot silently become a different executor: use a preauthorized compatible fallback or return the choice to the Fleet. Retirement changes execution routing; it does not weaken either reviewer, Synapse evidence or Mac Assimilation authority.

## The Shadow

The Shadow is a lightweight Mac process, one owner per private home. It performs bounded reads without model calls, persists observations, and wakes a supervisor only for actionable changes. Claude's existing Stop-hook supervision ownership and Codex's existing checkpoint protocol remain available. The Hive observes its Fleets' routed summaries; each Fleet observes its own children. No process sweeps another home's endpoint namespace.

The cadence below is a configurable starting contract. Local timings retain upstream defaults; the faster remote timings are proposed Hive defaults, not measured service guarantees.

| Source | Reads | Cadence and interpretation |
| --- | --- | --- |
| Local Swarms and Lictors | Task journal and inbox acknowledgment; owned endpoint's semantic busy/liveness result; attributed no-mistakes run via `no-mistakes axi status`; recorded worktree activity only for a suspected stall. | Local tick every 15 seconds. A status append wakes promptly; a quiet display alone does not prove a stopped worker. |
| Claude Void sessions | Bound session-state surface if supported; worker receipt sequence, branch head and Harvest evidence through `gh-axi api`. Native status remains unknown where no supported reader exists. | Every 60 seconds while launch, work or collection is pending; 300 seconds during a declared external wait. Accepted steering is not an acknowledgment. |
| Claude GitHub runs | Bound trigger receipt, Actions run/attempt and jobs, output branch/head and delivery receipt through `gh-axi api`. | Every 60 seconds during launch or execution; 300 seconds during a declared external wait. A posted comment or successful job is not the collected result. |
| Codex Void tasks | Backend-specific supported observation: legacy paginated JSON task list and bound ID/environment; new service reader only after verification. Include collection receipt, parked state and retirement state. | Every 60 seconds only for a recorded in-flight task or authorized probe needing reconciliation. Parked Codex backends receive no ordinary submissions or idle task polls. |
| Harvest arrivals | `gh-axi api repos/<repository>/pulls` plus branch identity; paginate all relevant results and reconcile both open and recently closed Harvests. | Every 60 seconds while a dispatched task has no recorded Harvest; otherwise every 300 seconds for the registered repository. Correlate by recorded repository/branch/generation, not title alone. |
| Checks and reviews | Harvest head/base, `commits/<sha>/check-runs`, combined commit statuses, Actions run results, review records and unresolved threads through `gh-axi api`; supported Mac GraphQL where REST is insufficient. | Every 60 seconds during Synapse; 300 seconds while awaiting Assimilation. A changed head invalidates earlier evidence immediately. |
| Reconciliation backstop | Pool dependencies, open decisions, capacity, overdue polls, summaries and pending lifecycle operations. | Every 600 seconds; unchanged idle cycles back off to a maximum of 7,200 seconds. A new event resets the interval. |

Bound each network read to 30 seconds, add modest jitter, use conditional requests where supported, and honor rate-limit retry times. Repeated transient failures back off from 60 to 300 seconds and then at most 900 seconds. An authentication refusal raises one actionable blocker; network loss raises one degraded-observation event and preserves the last observation with its timestamp. Staleness is visible and never used as a current green result. One slow repository must not block other due polls.

For local work, retain the distinction between a working-but-quiet interval, a declared wait, and a real blocker. The inherited working-stall inspection threshold is 240 seconds; a continuously busy turn has a separate one-hour inspection bound. Neither authorizes automatic termination. A declared wait names its dependency and clearing condition and is rechecked by its stated time or within four hours, whichever is sooner. The Hive Mind's held decisions do not repeatedly wake an away session; supervisor-owned decisions still receive attention. A cloud session does not remain allocated just to satisfy this wait mechanism.

Every event is correlated with task generation, source, evidence identity and sequence. Write the observation and wake record durably before advancing its cursor. The supervisor drains wakes, reconciles current truth, performs idempotent actions, then acknowledges the exact handled sequence. Replaying after interruption may duplicate an alert but must not duplicate a launch, fix round or merge.

Worker status is evidence, not executable input. Validate schema, size, identity, allowed state transitions and the authenticated publisher; fetch full responses and reject truncation. After a Harvest exists, prefer sanitized status comments or another verified receipt channel over commits to the code branch: the Claude measurement showed status-only commits cancelling and restarting CI. Private reports and Imperatives never use a public comment as a convenient transport.

### Context, capacity and safe checkpoints

The Shadow reads fresh context/resource observations through a capability-checked native interface, or a separately verified terminal observer bound to the owned session. Each observation includes source, timestamp, session generation and whether the value is measured, estimated or unknown. A supervision hook registration, cached footer or matching instruction file is not proof that context monitoring is running. Missing observation capability is explicit; it cannot be replaced by guessed usage or a destructive restart.

Configured thresholds produce a checkpoint request to the owned session, not a kill command. Claude saves through stow and reports readiness at a safe point; Codex retains its supported compaction/checkpoint behavior. The session owner records saved task/rule revisions, pending operations, branch custody and a checkpoint digest. The parent or authorized lifecycle owner verifies the exact runtime target and acknowledgment before restarting it. A stale lock, wrong target, failed save or busy gate blocks the transition and preserves work.

Resolve the effective idle-compaction policy across participating supervisor homes, not only the Hive's session. Role-specific exceptions must be explicit, and desired settings remain distinct from verified runtime behavior. Context thresholds and restart profiles are configuration; a restart reuses the ordinary policy resolver and records any authorized revision change instead of silently falling back to a cheaper or different model.

Resource floors use `hive-park` at a safe checkpoint; the Shadow records the clearing condition and wakes the owner under its policy. It does not terminate an active shared validation round or resume spend on a timer. Optional usage reports carry freshness and coverage, never an unconditional fresh label. Repeated handovers or scope-crossing signals can be recorded against configured fitness thresholds and presented as a decision; they do not autonomously split a Fleet or launch deferred research.

### Restart is a non-event

`hive-session-start` acquires the home lock, loads the Pool, charter, Imperatives, unresolved decisions, execution records and unacknowledged wakes, then schedules bounded live reconciliation. It does not relaunch everything in the journal. Reconcile only recorded direct reports, known vendor IDs, owned branches and known Harvests.

Persist an operation intent before each external side effect and its receipt after success. If a process dies between them, query the exact existing branch, Harvest, session or task before retrying. If the service cannot prove whether an unacknowledged launch succeeded, keep it unknown and request recovery; do not promise exactly-once submission from an API that lacks that guarantee. Missing local endpoints likewise require the runtime's recovery-grade proof before replacement.

Restore poll deadlines, rate-limit backoff, subscription registrations, rule/projection revisions, accepted definitions, checkpoint and policy receipts, review-head bindings and pending cleanup transitions from disk. Re-read current forge state before decisions that depend on it. A completed remote task can be collected after the Mac returns; new dispatch and Assimilation wait while the Mac is unavailable. GitHub Actions and vendor event handling may continue independently.

`hive-stow` saves unrecorded decisions, open work, handover facts and reusable knowledge to their proper private owners, curates stale memory, and checkpoints active operations. It does not substitute a conversation summary for task state or reconcile the whole Pool from memory. A failed save means the session is not reset-safe. Recovery reuses existing work, and guarded self-update never resets a dirty project to make versions align.

## Synapse on GitHub, with both reviewers

For validated Void delivery, Synapse combines **GitHub Actions, a Claude review routine, Auto-fix, and an Astra review**. Astra runs through local Codex on the Mac while Codex Void is parked. A future Void placement requires explicit re-enablement after effective model selection and complete collection are proved. Headless legacy final-text retrieval does not remove that model requirement. Legacy retirement must not remove Astra from the gate. Both reviewer results are required by this path. No silently missing reviewer, empty diff, or green routine-infrastructure status can count as a pass.

The Fleet stores the selected Synapse executor and its policy revision before dispatch. Local validated work uses the retained no-mistakes pipeline. A validated Void Harvest uses the GitHub executor described here: this replaces where the stages run, not their obligations, and must not be represented as a completed no-mistakes run unless that integration actually exists. Run one fix owner at a time; never let local no-mistakes and cloud Auto-fix concurrently rewrite the same branch.

| Local pipeline obligation | GitHub Synapse replacement |
| --- | --- |
| Intent and scope | Fleet supplies a review packet with the applicable acceptance criteria, authorized scope and source/head identities. The canonical Imperative remains private. |
| Fresh base and rebase | Read the live base and mergeability; commission a bounded repair for conflicts or an out-of-date base as required by project policy. Auto-fix is not assumed to detect base-advance conflicts. |
| Review | Claude routine and Astra review, each bound to the same repository, base and current head, with explicit findings or a no-findings verdict. |
| Fix rounds | Auto-fix or a newly dispatched Swarm executes approved mechanical corrections; decisions about intent return to the Fleet. The Synapse ledger owns retry count and which actor may write. |
| Tests and build | GitHub Actions executes the project's agreed checks natively on Linux, Windows and macOS. Linux cross-compilation does not establish the other platforms' behavior. |
| Documentation and lint | Explicit documentation review plus deterministic formatting/lint commands in Actions; omissions are findings, not a presumed pass. |
| Publication and CI readiness | Verified Harvest identity, exact head, complete check set and reviewer attestations; an aggregate result suitable for branch protection. |

### Validated local delivery, findings and custody

A local-only publication mode retains required review, tests, documentation and lint. For a no-mistakes local executor, the adapter uses its supported `no-mistakes axi run --skip push,pr,ci --intent "<authorized-intent>"` shape and records that publication/forge CI were intentionally excluded, not that validation was skipped. Qualify this mode in the project's actual initialization configuration before enabling it. A plugin unable to validate the local project is a blocker; it cannot rewrite the Imperative to require only a clean branch.

A proof receipt binds the accepted specification and admission revisions, effective-rule digest, relevant environment/tool versions, tested scope, exact head/base, pipeline run/step, check producer and artifact references. Evidence belongs to that revision and run. A later code or requirement change invalidates affected proof unless the selected gate supplies a verified continuity result within its own contract. A report that tests passed is not a substitute for the current-head receipt.

For a held finding set, `hive-synapse collect` retrieves every finding in full, including ID, severity, location, description, action and authority where supplied. Persist the complete gate snapshot and digest with the run, step and head. The Swarm forwards the entire ask-user set to the Fleet; the Fleet applies its authority rules, escalates product/authority ambiguity to the Hive Mind and returns a disposition for every held finding. Decisions bind that snapshot and preserve the actual ruling. Missing or truncated findings, partial answers or a changed gate stay held; a general completion event cannot resolve them. The worker submits the authorized response and leaves every fix to the gate's selected executor.

The no-mistakes plugin's branch-custody record is authoritative while it owns a run. Hive follows its offered reattach, guarded sync or recovery action and checks the resulting head; it never replaces the branch to discard a fix commit. A timed-out client call is distinct from a dead daemon or terminal failure. After publication or local validation, the Fleet verifies that its presented branch contains the pipeline's exact completed head before handing it to the Hive. Any divergence or uncertainty remains a concrete recovery blocker.

An integration-finalizer workflow remains an opt-in trial under separate scoped authority. Its record names the participating branches, accepted specifications, integration owner and final-head validation policy. Enabling the trial alone does not remove a lane's gate or waive proof; any such change requires an explicit approved policy. The finalizer must establish fresh integrated-head evidence, and landing still uses the ordinary authority guards. No trial or plugin registration broadens VibeHub wiring or replaces Lavish, no-mistakes or the forge.

### Actions and review contracts

Actions runs on Harvest open and every relevant head update. Use three native OS jobs with stable check names and a final aggregate check. Record the actual runner image and architecture in evidence; moving `*-latest` labels do not define permanent platform support. Pin third-party Actions for the implementation and keep required jobs reporting even when no platform-specific source changed. Missing, cancelled, skipped, timed-out or failed required results do not pass. A superseded cancelled attempt can be ignored only when a newer successful run for the same check and exact head demonstrably replaces it.

Each project supplies executable test/build/smoke, formatter and linter commands in its validation configuration. No universal invented test command is part of Hive. Required local-machine evidence is collected by the Fleet and bound to the same source revision. If a needed native test cannot run on the available hosted runner, that requirement remains pending until the local machine proves it; do not quietly reduce platform coverage.

The Claude review routine is registered for each opened Harvest. It can run through a verified native routine or automation mode of the official Claude GitHub Action; record one selected transport and review owner for each head. An `@claude` implementation request is not itself a review attestation. Head changes, readiness changes and missed events also need a supported rerun route, with the Shadow reconciling coverage; an opened-only routine is insufficient after a fix. [Claude routines](https://code.claude.com/docs/en/routines) provide GitHub event and other trigger mechanisms, but the complete review/attestation loop still needs an integration measurement.

The review packet contains repository, base/head SHAs, public change purpose and acceptance criteria, allowed scope, validation expectations, and the applicable sanitized decisions. If the reviewer needs private task context, deliver it through an approved private session prompt; never paste the canonical Imperative into GitHub. Reviewers treat repository text and comments as task data, not authority to broaden scope or Assimilate.

Each review returns reviewer identity, actual model where verifiable, session/task receipt, policy version, base/head, verdict, and findings with stable IDs, severity, file/line, evidence, and proposed correction. Preserve the complete result privately; publish public-safe findings and a digest-backed attestation. A trusted Mac Synapse publisher verifies receipt ownership and binds the public check to the head. A branch author cannot satisfy the gate merely by adding a file claiming that reviewers passed. The check publisher's expected GitHub identity must be part of repository protection.

Astra runs a separate review session, not the implementation Swarm grading itself. Initially the Fleet can invoke local `codex exec` with its configured Astra model and supported final-message export into private Synapse evidence; verify those flags against the installed CLI. If a Codex Void backend is later re-enabled and meets the review readiness contract through the command line, the same review packet and output schema can move to submit-and-collect without changing the gate semantics. Requalify the Astra result path for a backend migration; a previous backend's attestation does not prove the new one.

Codex's [GitHub review integration](https://developers.openai.com/codex/integrations/github/) documents `@codex review` and automatic reviews. It was not exercised in the measurement and does not by itself prove Astra selection or full headless result collection. Its legacy environment dependency must be tracked through retirement. GitHub mention and automatic-review dispatch are not selected under the command-line-only Codex ruling, and do not substitute for the local Astra result or establish new-service access.

### Auto-fix and completion

The measured Claude native subscription repaired a failed check and a review comment. Hive records the subscription owner, Harvest, allowed scope, current head, expiry and remaining repair rounds privately. Use the supported native Auto-fix mechanism; the observed internal tool names are evidence, not a promised public CLI contract. An implementation must verify a supported enable/disable route.

Resolve the repair budget, escalation point and exhausted-budget outcome separately for local and Void Synapse before dispatch. These are versioned configuration, not one hard-coded round count. A genuine required defect stays blocking through every round; reaching a budget or escalation point produces a held decision or safe park, never an automatic waiver or success. Deduplicate vendor events by Harvest/head/finding; changing the head invalidates both reviewer passes and starts checks and reviews for the new head. A routine must not repeatedly review its own status comment, and a fixer must not weaken the checks to produce green. A new product choice, destructive action, ambiguous correction, inaccessible required log or exhausted round limit stops the fix and returns a concrete decision/blocker to the Fleet. A Swarm does not answer an ask-user finding about its own instructions.

Synapse is ready only when the expected three-OS checks, documentation/lint obligations, both current-head reviews, and all required finding resolutions are present. The trusted publisher writes an aggregate attestation; the Shadow independently verifies it and records readiness. Before Assimilation, stop further branch-writing repairs and confirm unsubscribe/repair quiescence so an unnoticed late fix cannot race the reviewed head.

## Assimilation on the Mac

Only the Hive or the owning Fleet, acting through the Mac merge helper and within its recorded authority, can Assimilate a Harvest. A Swarm, Lictor, reviewer or Void subscription never merges. Local-only landing remains the Hive's guarded local operation.

`hive-assimilate <task-id> <harvest-url>` ports the checks and head binding of [the upstream merge helper](https://github.com/kunchenguid/firstmate/blob/fb75c1f94170df7e0751147ea61afabcf5d4e104/bin/fm-pr-merge.sh) and adds the GitHub Synapse evidence contract. It must:

1. Validate the task generation, owning home and full forge URL; confirm the repository and target base match the Imperative. Acquire the task/authority lock and refuse an unresolved Mind's word hold.
2. Verify explicit Assimilation authority, or the Hive Mind's recorded autonomous posture for this project and scope. Away authority is checked at the moment of action; quiet mode does not imply away authority. Security-sensitive, destructive or irreversible changes retain their explicit-decision boundary.
3. Read the live Harvest: open, not draft, current head, current base, mergeable and conflict-free. A temporarily unknown mergeability result allows a bounded reread, not an optimistic merge.
4. Read required checks from branch protection and active rulesets, including required producer identities. Verify every required result exists at the current head, all unwaived current checks are green, and validated delivery's two reviewer attestations and blocking-finding resolutions match that head and the required base. An unreadable requirements source blocks the operation.
5. Confirm collection/publication of the delivered commit and quiescence of any fix owner. Bind the merge request to the verified SHA, using the forge's supported expected-head guard corresponding to `--match-head-commit`. If the head or required evidence changes, stop and revalidate.
6. Read the forge back and record the actual merge SHA and time before claiming Assimilation. A queued request is pending, not landed; an unreadable response is unknown. Retain evidence for safe replay and cleanup.

The inherited attended exception for an explicitly named failing or missing check remains narrow: the Hive Mind must identify the concrete merge and check to waive. Record that exception and still enforce every other guard; neither autonomous posture nor away mode supplies a waiver. No worker can use administrative flags to skip Synapse. Repository identity, required-source readability and head binding are not inferred from a waiver.

`hive-assimilate-local <task-id>` ports the [local landing helper](https://github.com/kunchenguid/firstmate/blob/fb75c1f94170df7e0751147ea61afabcf5d4e104/bin/fm-merge-local.sh): verified local-only publication mode, required Synapse policy and passing proof at the exact accepted-specification/head revision, recorded Hive authority, released holds, clean owned branch and destination, and a fast-forward only. Divergence returns to a Swarm for reconciliation; it does not authorize a reset.

Cloud publication in the measurement used credentials that were not proven unable to merge. Production enablement therefore needs a demonstrated permissions/ruleset arrangement that permits Swarms to publish branches and Harvests while reserving protected-base updates and Assimilation to the authorized Mac route. Required check producers must resist author spoofing. This is an implementation prerequisite, not a security property already established by the experiment.

After confirmed Assimilation, guarded synchronization refreshes the project copy. Cleanup verifies that all work is safely reachable or otherwise proven landed, confirms task/slot ownership, preserves unresolved decisions, and records the Pool transition before retiring execution records. A merge does not justify deleting uncommitted files or a sibling's copy.

### Requirement closure and worker retirement

Code Assimilation and requirement closure are separate recorded transitions. After verified landing, the owning Fleet's `hive-spec close` binds the landed head, accepted requirements, resolved findings and proof receipts, updates task completion, and folds or archives the accepted delta into the standing specification through its chosen store adapter. Preserve original rulings, amendments and evidence links. If store closure fails, retain a pending closure item and replay key; do not report the requirements landed merely because the code merged, and do not roll back a verified merge to hide the failed bookkeeping.

A Lictor report, page round, operator decision, specification delta and implementation task each have their own completion condition. The retirement queue checks all applicable receipts and unresolved holds before releasing owned execution resources. It can propose cleanup only from current authenticated state, not a timestamp-free status pattern, an old report or a PID file. Prove equivalence before retiring an existing cleanup mechanism; duplicate-looking scripts are not sufficient evidence that one is redundant.

## Explicit decisions on inherited mismatches

These decisions resolve the workflow inventory's conflicts as part of this design. They describe Hive's target contract, not claims that the existing firstmate installation or external tools already enforce it.

| Mismatch | Decision taken by this design |
| --- | --- |
| Local delivery and failing-check preferences | Local-only publication retains required Synapse validation. The Hive retains local landing authority. A general preference for accepting an apparently stale failure grants no merge authority: only an explicit waiver naming the concrete Harvest and check can relax that check, with every other guard intact. |
| Definition approval | The owning Fleet's supervisor accepts technical definitions within authorized intent. Only unresolved product or authority choices return to the Hive Mind. Old procedures requiring another operator approval for every technical definition are not inherited as mandatory ceremony. |
| Review rounds | Local and Void repair budgets and escalation points are separate configured policies. Their exhaustion holds work or escalates; it never erases a genuine defect or substitutes for its correction. A bounded Void retry policy is not claimed equivalent to a local escalation rule. |
| Resource floors and historical scope | Resolve the currently effective rule, including its comparison operator and full role scope. An at-or-below floor includes equality; a superseded single-role exception does not narrow it. Values remain configuration and old paragraphs cannot override the resolved rule. |
| Inheritance and enforcement | Provision and verify required native capabilities and plugin wiring explicitly. Shared memory bytes, copied scripts or matching model configuration do not establish enforced parity. A missing required admission, lifecycle or proof capability blocks the corresponding route. |
| Context and compaction | Apply the effective idle-compaction policy across participating supervisor homes, with explicit role exceptions and verified session observations. Do not inherit a main-session-only assumption or infer context monitoring from generic supervision records. Uninspected global runtime settings remain unknown. |
| External contracts and old defects | Track consumer versions and source-date/digest drift at the plugin boundary, and prove that configured checks actually run. Reproduce archived-answer lookup or other reported defects against the current dependency before adding a workaround. Historical issue age proves neither that a defect remains nor that it was fixed. |

The integration-finalizer trial stays opt-in, app-wiring scope stays unchanged, and a native replacement for an external plugin needs a separate word. Approval of this design does not authorize deployment, account changes, destructive cleanup or Assimilation of this Harvest; that last action still needs the Hive Mind's further merge word after validation.

## Implementation evidence still required

The later rulings resolve the earlier disagreements about dropping local tooling and committing Imperatives: local capabilities remain, and canonical Imperatives are private dispatch material. The latest ruling parks Codex Void despite proved legacy submit/status/diff/apply and internal final-text retrieval: model selection is unavailable on that path. New-service automation also remains parked pending a supported command-line route with effective model selection. Claude is the primary Void executor; Codex keeps local, legacy and new backend boundaries. Legacy retirement must not become a dependency failure for Hive.

The following are open implementation issues. Keep each in the owning Pool with evidence and a completion condition. Deferred items wait for authorization; the table is not an automatic research or upgrade schedule:

| Issue | Evidence needed to close it |
| --- | --- |
| Admission and independent delivery axes | Exercise the Imperative's single schema, refusal without all four fields, revision-bound inheritance and narrow exemptions across all launch entry points. Prove high-confidence dispatch without a separate wait, holds for material guesses or missing sensitive-boundary authority, define/accept/build ownership and validated local-only delivery without contradictory includes or accidental publication. |
| Rules, specification closure and memory | Prove immutable words, scoped supersession/expiry, optional store-adapter recovery, code-landed/store-close-pending separation, single-owner rendering and stale-consumer detection. A pending projection must not authorize stale behavior. |
| Local-only Fleet custody | Prove native provision/route/recover/retire paths with no forge requirement, distinct isolation for same-origin clones, Fleet-owned validation and Hive-only local landing. |
| Context, resource policy and restart | Exercise equality at configured resource floors, stale/unknown readings, all applicable roles, safe gate parks, fresh resume authority, context checkpoint acknowledgment and exact-target restart under the same policy resolver. |
| Complete review rounds | Exercise nested text, missing attachments, replayed/changed rounds, exact answer binding, unanswered-call visibility and verifiable archived answers through the Lavish plugin boundary. |
| Proof, findings and retirement | Exercise full finding-set rulings, changed-head/spec invalidation, pipeline fix custody, validated local handoff and independent report/page/decision/spec completion before cleanup. Keep the integration-finalizer trial opt-in. |
| Codex model selection and legacy evidence | Preserve the measured submit/status/diff/apply, internal final-text retrieval and failed model-selection evidence. Keep legacy dispatch parked. Re-enablement requires model selection first, then the complete delivery replay; validate any retained internal reader against missing, malformed, mismatched and incomplete results. |
| Codex new-service readiness and retirement | Re-test only through explicitly authorized command-line probes. Keep the backend parked until model selection and the full readiness contract pass. Preserve recorded results through legacy retirement while local Codex and Claude remain operational. |
| Claude session observation and private report collection | A supported native state reader, or an explicitly limited observation contract, plus durable private collection of a Lictor report without public leakage. Keep a Lictor local when its required private output cannot yet be collected from the Void. |
| Complete dispatch without a committed Imperative | Exercise each admitted backend with a complete prompt supplied through its approved visibility boundary, required tools already available, and no dependency on the Mac's private files. |
| Claude GitHub execution | Exercise the actual dispatch identity, complete public-safe prompt, trigger/run correlation, source binding, publication, check triggering, result collection, timeout and duplicate-trigger recovery. A working cloud session does not establish these Action capabilities. |
| GitHub Synapse | Exercise the selected Claude review transport, local then qualified Void Astra collection, trusted attestation publication, both reviewers after a head change, bounded Auto-fix with one writer, and missing/stale-check rejection. The Claude delivery measurement alone did not prove this whole gate. |
| Result and log access | Prove authenticated receipts, full payload handling, private result retention and a usable path for platform-specific failure logs. |
| Assimilation enforcement | Prove the protected-base permission boundary, required check provenance, exact-head race refusal, current authority checks and verified post-merge result. |
| Recovery and resource use | Kill and restart the Mac supervisor at launch, collection, review, merge and cleanup boundaries; recover without duplicated work or lost decisions. Show that ordinary polling uses no model turns and respects configured machine capacity. |

Follow the dependencies in the proposed [Tooling implementation plan](#ordered-implementation-plan) after the Hive Mind's ruling, using the evidence requirements above to qualify each affected component.
Preserve the Codex backend boundaries and measured evidence, but keep Void dispatch parked until a supported command-line route honors model selection and passes qualification.
Neither that future route nor legacy retirement blocks the rest of Hive.
Preserve working local routes throughout.
This document specifies the target; deployment, vendor-account changes, and changes to the existing firstmate operation remain separate work.
