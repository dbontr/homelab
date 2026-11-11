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

## Host boundaries

The MCP can save, read and format a portable handoff. It cannot create
or switch ChatGPT conversations. A separately operated Responses API
runtime may monitor host-returned token usage and use API-controlled
compaction. Such behavior belongs to the API host, not to the MCP.
See [context boundaries](references/host-context.md) for exact distinctions.
