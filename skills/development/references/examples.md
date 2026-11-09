# Professional Code Examples

Use these examples when deciding whether wording or structure reflects the enduring final state or merely the history of the conversation.

## Contents

1. User changes a chart direction
2. Replacement implementation
3. Comments after a correction
4. Frontend copy
5. Tests
6. AI-specific files
7. Commit messages
8. Compatibility
9. Overengineering
10. Legitimate history
11. Chat reporting versus repository text
12. Final-state rewrite test
13. Local execution paths
14. Documentation after an implementation change
15. Backup and scratch files
16. Diff-level polish

## 1. User changes a chart direction

User request:

> I like this direction, but use a logarithmic y-axis and group by region instead.

Avoid:

```python
ax.set_yscale("log")  # Updated to use the requested logarithmic scale.
```

Avoid chart annotations such as:

```text
Updated regional grouping
New logarithmic presentation
```

Prefer implementing the accepted chart directly. Use only titles, legends, labels, and annotations that help a viewer understand the data.

## 2. Replacement implementation

Avoid:

```python
def parse_record_v2(record):
    """New parser replacing the old implementation."""
```

Prefer:

```python
def parse_record(record):
    ...
```

If a public API requires both versions, versioning becomes domain-relevant and may remain.

## 3. Comments after a correction

Avoid:

```ts
// We no longer use localStorage because the user wanted sessionStorage instead.
const storage = sessionStorage;
```

Prefer no comment if the choice is obvious:

```ts
const storage = sessionStorage;
```

Or document a durable reason if one exists:

```ts
// Session-scoped storage prevents authentication state from surviving a browser restart.
const storage = sessionStorage;
```

## 4. Frontend copy

Avoid:

```text
New streamlined checkout
Updated filters
Revised navigation
```

when those labels exist only because the implementation was changed during development.

Prefer product-native copy:

```text
Checkout
Filters
Navigation
```

A release-notes page is different: there, change history is the point of the artifact.

## 5. Tests

Avoid:

```python
def test_new_timeout_behavior():
    ...
```

Prefer:

```python
def test_request_times_out_after_configured_deadline():
    ...
```

## 6. AI-specific files

Do not create files such as these merely to aid or identify the assistant:

```text
AGENTS.md
.agents/
.claude/
.cursor/
AI_NOTES.md
CHATGPT_CHANGES.md
PROMPT.md
```

If the repository already relies on one of these files for its established workflow, do not remove it automatically. Follow existing project requirements.

## 7. Commit messages

Avoid:

```text
Implement changes requested by user
Update code based on feedback
AI-assisted cleanup
```

Prefer the product or technical result:

```text
Handle token refresh before session expiry
Simplify invoice reconciliation path
Use regional aggregation for revenue chart
```

Do not add AI co-author trailers unless required or explicitly requested.

## 8. Compatibility

Avoid preserving this solely because the conversation once used a different field name:

```python
value = payload.get("account_id", payload.get("old_account_identifier"))
```

Keep such a fallback only when real external callers, persisted records, migration requirements, or a specified compatibility contract still use the old field.

If required, document the actual reason:

```python
# Accept the legacy field until mobile clients older than 4.2 are retired.
value = payload.get("account_id", payload.get("old_account_identifier"))
```

## 9. Overengineering

For a single straightforward implementation, avoid introducing an abstract factory, registry, strategy interface, and adapter layer solely because they are recognizable design patterns.

Prefer direct code until real variation or a useful boundary exists.

## 10. Legitimate history

Do not erase meaningful history from artifacts whose purpose is historical or operational, such as:

- database migrations
- release notes
- deprecation notices
- security advisories
- compatibility documentation
- audit logs
- incident reports
- versioned public APIs

Even in these artifacts, describe technical facts rather than private prompt or assistant history.

## 11. Chat reporting versus repository text

Acceptable private chat response:

```text
Implemented regional grouping, switched the chart to a logarithmic y-axis, and updated the affected tests.
```

Do not paste that sentence into the repository unless it independently belongs in a changelog or commit/PR description. Repository documentation should describe the current system directly.

## 12. Final-state rewrite test

When uncertain about a line, imagine the accepted implementation was created in one pass by a professional engineer who never saw the earlier attempts.

Keep the line only if it would still make sense in that world.

## 13. Local execution paths

Avoid committing code or documentation that exposes the environment used to produce the change:

```python
fixture_path = "/mnt/data/generated/users.json"
```

Prefer the project-native source of truth, such as a repository-relative fixture path or configured resource:

```python
fixture_path = Path(__file__).parent / "fixtures" / "users.json"
```

Likewise, do not leave personal home-directory paths, workstation names, scratch mounts, or tool-specific temporary locations in examples or configuration unless they are intentionally part of the product.

## 14. Documentation after an implementation change

Avoid adding prose solely to memorialize development chronology:

```text
The dashboard now uses server-side pagination instead of the previous client-side approach.
```

Prefer current-state documentation:

```text
The dashboard uses server-side pagination and requests one page of results at a time.
```

Use historical wording only where the document is intentionally historical, such as release notes or migration guidance.

## 15. Backup and scratch files

Do not leave repository artifacts such as:

```text
payment_service_old.py
payment_service_backup.py
component.tsx.orig
fix.patch
before.png
```

when they exist only to support the editing process. Use version control, temporary workspace storage, or the editing environment for rollback and inspection.

## 16. Diff-level polish

A task may pass tests and still be unpolished if its diff contains unrelated formatter churn, regenerated snapshots, lockfile changes without a dependency change, temporary logs, or accidental edits in neighboring files.

Before completion, review the changed-file set as a deliverable in its own right and remove noise that is not necessary for the accepted final state.
