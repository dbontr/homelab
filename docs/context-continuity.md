# Conversation continuity

A model's finite context is distinct from durable project state. The MCP
cannot inspect ChatGPT's hidden instructions, complete message history,
internal reasoning, effective context-window size or remaining token budget.
Consequently, no server-generated percentage is labeled as an actual
ChatGPT context measurement.

The unified MCP exposes three owner-scoped tools:

- `continuity_checkpoint`: store one concise working summary, objective,
  next steps, evidence references and an optimistic-concurrency revision.
- `continuity_status`: read the latest checkpoint and its qualified context
  pressure signal.
- `continuity_handoff`: return a portable continuation prompt from durable
  state without creating another chat.

The checkpoint is persisted in the existing owner-state file. The snapshot
contains only the facts explicitly supplied to the tool, not the original
chat transcript, tool outputs or invisible model context. Keep source
references, branch/commit identities, unresolved work, verification status,
and the next concrete action in the summary. Do not store passwords,
provider credentials, tokens or other secrets.

## Context-pressure estimates

When no host usage is available, the tool returns
`source: unavailable`, `status: unknown`. It never guesses a remaining
token count from the number of MCP tool calls.

If the caller provides `reported_input_tokens`,
`context_window_tokens` and optional `reserved_output_tokens`, the
tool computes a budget signal. Those values remain
`source: caller_reported_unverified`: hidden host overhead or inaccurate
model information may make the result incomplete.

The tool classifies estimated utilization below 70% as normal, from 70%
as checkpoint recommended and from 85% as handoff recommended. These are
conservative user-interface hints, not promises that a model can accept
another request or guarantee the full context is retained.

Use `expected_revision` to prevent a stale conversation from overwriting
later verified work. A new conversation can call
`continuity_handoff` with the same key to retrieve the portable prompt.
The returned prompt tells the next agent to verify source and repository
state before continuing. No code claims to transfer control automatically.

## ChatGPT and host-managed limits

An ordinary remote MCP has no standardized, trusted feed of the complete
ChatGPT context or a tool for opening another ChatGPT conversation. Tools
can advertise checkpoint support and issue suggestions in their results;
they cannot force the ChatGPT host to call them or silently move an ongoing
conversation to a fresh thread. For this reason, checkpoint creation
requires an explicit tool call before capacity is exhausted.

Where a future host exposes reliable remaining-context metadata, a
dedicated trusted adapter may report it to the continuity service without
treating untrusted MCP arguments as authoritative measurements.

## Responses API agent runtime

Conversations controlled by a separate API agent can use returned
`usage.input_tokens`, the input-token counting endpoint and model
context-window limits for stronger budget accounting. The OpenAI Responses
API also supports server-side context compaction via
`context_management.compact_threshold`, and an explicit compaction
endpoint. Its output is a model state item, not a human-readable handoff,
so durable project evidence and checkpoint references remain valuable.

The API caller, not the MCP server, owns that response chain and may elect
automatic compaction or start a new agent context. This capability does
not grant the MCP permission to manipulate a user's ChatGPT conversations.

Primary references:
- https://developers.openai.com/api/docs/guides/compaction
- https://developers.openai.com/api/docs/guides/conversation-state
- https://developers.openai.com/api/reference/resources/responses/subresources/input_tokens/methods/count
