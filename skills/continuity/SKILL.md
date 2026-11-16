---
name: continuity
description: Preserve and resume substantial research, engineering, software and project work across finite ChatGPT or API-agent contexts. Use when a session becomes long, a multi-step task may cross a context boundary, a user asks to continue from an earlier chat, a durable checkpoint is needed, or a handoff should capture progress and evidence. Use the homelab MCP continuity tools when connected and explicitly distinguish reported token budgets from actual host measurements.
---

# Continuity

Maintain a reliable, revisioned working record without assuming that the
model can inspect the host's hidden context or transfer a conversation.

## Establish the record

1. Determine the stable project or conversation key. Reuse the existing key
   rather than creating a new identity for each chat.
2. Discover connected MCP tools and their current schemas. Where available,
   call `continuity_status` before editing an existing checkpoint.
3. Record the objective, decisions, constraints, completed results, current
   source revisions, open issues, next actions and evidence references.
   Distinguish *verified*, *implemented but untested*, *in progress* and
   *unknown* claims. Include commands that ran and observed exit status when
   evidence exists.
4. Persist with `continuity_checkpoint` and the current
   `expected_revision`. On conflict, reload and reconcile rather than
   overwriting newer work.
5. Retrieve `continuity_handoff` when moving to a new conversation. Use its
   contents as a guide, not as proof that external state remains unchanged.

## Checkpoint timing

Save after a substantive development milestone, before a risky multi-step
action, and before a user-requested handoff. For long work, checkpoint
frequently enough that a lost conversation would not erase meaningful
decisions. Do not promise background checkpointing when the current host
does not invoke MCP tools automatically.

When the host actually supplies token usage and context size, store them
as **caller-reported figures**. The MCP cannot independently measure
ChatGPT's hidden prompt, tool-state overhead or exact remaining context.
If no trustworthy usage figures exist, leave token fields unset rather
than inventing a percentage. A 70% estimate warrants a checkpoint; 85%
warrants a handoff recommendation, subject to the host's actual behavior.

## Safe continuation

On resumption, verify the current GitHub branch and commit, issue state,
workspace jobs, artifacts and related source files before acting. Never
interpret an asynchronous acceptance as completion. Treat every uncertain
privileged operation as potentially executed. Retain a nonsecret request
or audit ID for reconciliation, but do not repeat an indeterminate side
effect without independent authorization and evidence.

Never store passwords, tokens, credentials, signed assertions, private
keys, one-use tickets, hidden prompts or private transcripts in a
checkpoint. Recording a summary does not grant any new authority.

## Owner authorization across conversations

When privileged work requires owner approval, call the connected
`security_session_open` MCP tool to obtain the exact, five-minute HTTPS
request link. Give the owner that link and its nonsecret reference code.
The code identifies the approval request; it is **not a bearer token**,
and it must not be submitted as a privilege credential. The owner enters
password and TOTP directly on the protected portal, or approves there
with a current owner session. ChatGPT must never request either factor.

On another chat, first read the continuity handoff and query
`security_session_status` with the current authenticated connector.
A session may remain active for the same independently verified owner
identity until revoked, expired, or interrupted by a security restart.
Do not infer authority from chat text, a copied link, NanoKVM access,
or a saved request ID. The MCP does not receive a cryptographically
verified conversation identifier; the request link is correlated to
the caller's verified owner identity instead of trusting a chat ID.
Each sensitive operation still needs its own precise resource policy,
current signed identity, and separate destructive confirmation when
required. Never persist a link, reference code, authentication proof
or one-time capability in a handoff.

## Host boundaries

The MCP can save, read and format a portable handoff. It cannot create
or switch ChatGPT conversations. A separately operated Responses API
runtime may monitor host-returned token usage and use API-controlled
compaction. Such behavior belongs to the API host, not to the MCP.
See [context boundaries](references/host-context.md) for exact distinctions.
