# Session messages

The session dock, the Read Hive reader and an external terminal all show one supervisor conversation. This file defines the record of one message of that conversation and the receipt that places one input in its order. The adapter that produces them, and its qualification for each harness, are stage 4's.

Owner: `dispatch`, through its same-session adapter. Schemas: [`session-message.v1`](schemas/session-message.v1.schema.json) and [`receipts/dispatch.session-input.accepted.v1`](schemas/receipts/dispatch.session-input.accepted.v1.schema.json). Proved by `test-contract` for the shapes, by `test-dispatch` for the adapter and by `test-ui` for the dock and the reader.

## The message

| Field | Meaning |
| --- | --- |
| `message_id` | Stable for the message through streaming, reconnect and history retrieval. |
| `session_id` | The one supervisor conversation of a home. It survives a restart. |
| `session_generation` | The process generation of the session that produced or accepted the message. It rises by one at each restart. |
| `home_id` | The home the session belongs to. |
| `harness` | `claude` or `codex`. |
| `sequence` | The message's place in the conversation: strictly increasing within a `session_id`, continuing across generations. |
| `role` | `operator`, `supervisor` or `system`. |
| `input_id` | For an `operator` message, the input that became it. |
| `parts` | The content in order: `text`, `attachment` or `artifact`. |
| `streaming_revision` | 0 for a message recorded whole; otherwise it rises by one with each recorded update. |
| `completion` | `streaming`, `complete`, `interrupted` or `unknown`. |
| `created_at`, `updated_at` | When the message was first recorded and last changed. |

**Roles.** `operator` is the Hive Mind's input, whichever client sent it. `supervisor` is the session's own output. `system` is a notice from the harness or from Hive; it is never an answer, and nothing is synthesized into a `supervisor` message from command acceptance, routing receipts, terminal output or the state of the map.

**Parts.** A `text` part holds text exactly as produced or typed. An `attachment` part names a file in the session's home by path, digest and size. An `artifact` part names a registered [review](review.md) and optionally one of its revisions. A part keeps its index for the life of the message; a streaming update extends the last text part or adds a part, and never reorders or removes one.

**Streaming.** A message that arrives in pieces is recorded as `streaming` and replaced as it grows, each replacement raising `streaming_revision` and `record_revision` together. Updates may be batched; a reader shows what is recorded and no more. The message ends `complete`, or `interrupted` if it ended early and what is recorded is all there is, or `unknown` if its end could not be observed. An `unknown` message is never shown as whole.

**Order.** `sequence` is the only order. It is assigned by the adapter when the message is first recorded and never changes. A reader that reconnects asks for messages after the last `sequence` it holds and receives each `message_id` once; a message it already holds may return with a higher `streaming_revision`, which replaces the copy it has.

## Input

Text sent to the session from the dock or from an external terminal is one **input**. The client gives it an `input_id` and reuses that identifier if it has to send again.

The adapter places every input in the session's **one canonical input sequence**, whichever client it came from, and writes a `dispatch.session-input.accepted` receipt:

| Body field | Meaning |
| --- | --- |
| `input_id` | The client's identifier for the input. |
| `session_id`, `session_generation` | The session and the generation that accepted it. |
| `input_sequence` | Its place in the one input order. |
| `client` | `dock` or `terminal`. |
| `text_digest`, `text_bytes` | The digest and length of the text as sent. |
| `message_id` | The operator message it became, once that message is recorded. |

- The receipt proves that the input was accepted and ordered. It proves neither that the model has read it nor that there is a reply.
- A second send of the same `input_id` returns the first receipt and creates no second turn.
- Input is an ordinary instruction to the supervisor. It passes through the Hive's intake like any other, and a message record never authorizes anything.
- The session's messages and receipts are records of `dispatch`. The `ui` package reads them through `dispatch`'s commands and submits input the same way; it writes none of them.

## What is not defined here

Pending approvals, the stream by which a reader is told of a new message, attachment to a session that is already running and the control of worker endpoints are the same-session adapter's own contracts. Stage 4 defines and qualifies them, separately for Claude and for local Codex, and adds what it needs to this file as a minor change.
