# Conversation continuity for engineering work

## Checkpoint boundary

The unified MCP keeps revisioned working checkpoints with
`continuity_checkpoint`, `continuity_status`, and `continuity_handoff`.
These tools store project progress, not raw ChatGPT conversations. Check the
current tool discovery and schema before invoking them.

Save a checkpoint when a meaningful phase ends, before a multi-step operation
with uncertain external effects, and whenever a handoff or context-pressure
warning is warranted. Prefer an existing stable key associated with the
project or research thread. The assistant must make the checkpoint explicit;
an MCP server cannot force the host to invoke it.

Record the objective, demonstrated progress, precise source revision,
important constraints, pending work, accepted decisions and open questions.
Put GitHub pull requests, issues, files, artifact identifiers and other
verifiable references in `evidence`. A saved test command is not an observed
passing test. An accepted asynchronous job is not a completed job.

Use `expected_revision` to prevent an older conversation from overwriting
newer results. On a revision conflict, read `continuity_status`, reconcile
the competing work and retry only with the current revision.

## Token accounting

A normal MCP receives only its own requests and responses. It cannot inspect
all hidden ChatGPT context, exact remaining capacity, or implicit compaction.
Do not infer a numeric budget from message length, elapsed time or MCP traffic.
If the host has supplied actual model input and window figures, pass them as
**caller-reported** data. Without those figures, the correct status is
`unknown`.

Treat `checkpoint_recommended` and `handoff_recommended` as advisory.
Avoid doing a long irreversible action at high or unknown context pressure
without first preserving the current state.

## Resume

Call `continuity_handoff` with the same stable key in the next conversation.
Confirm that its sources, branch, open PR and referenced artifact versions are
still current. Recover any status that was not known at checkpoint time
before writing more code or issuing another mutation.

A checkpoint never carries owner passwords, private keys, access tokens,
operation tickets, session assertions, OAuth credentials or secrets. It may
record a nonsecret audit or request ID to enable safe reconciliation.
Unknown or indeterminate external operations cannot be repeated solely
because a new conversation begins.

## Host-managed automatic continuity

A separate Responses API application can use measured token usage and
host-controlled compaction or initialize a fresh context with a reviewed
checkpoint. The ordinary ChatGPT MCP does not have a built-in action for
creating another ChatGPT conversation or moving messages automatically.
Do not simulate such authority.
