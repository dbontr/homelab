# Repository References

Use this guide when creating or maintaining the required root `REFERENCES.md`.

## Purpose

`REFERENCES.md` records authoritative external technical sources that materially support the repository's current implementation. It is not a browsing log, change history, AI provenance record, or substitute for legal attribution.

## Canonical entry

Use a Markdown list entry in this form:

```markdown
- [Source title](https://canonical.example/source) — Source owner; version or publication date when material. Used for: current technical role in this repository.
```

Group entries under useful second-level headings when the page has enough sources to benefit from grouping, for example `## Standards and specifications`, `## APIs and libraries`, or `## Research`. Headings are organizational only and must not duplicate entries.

## Source selection

Include a source when it materially determines or validates current code, architecture, protocol behavior, data formats, algorithms, standards conformance, compatibility behavior, or a durable technical claim. Prefer the primary source.

Do not include:

- assistant conversations, prompts, transcripts, or shared-chat links
- search-results pages or aggregator pages when the primary source is available
- local files, sandbox paths, temporary artifacts, or developer-machine paths
- sources consulted only for general background that did not materially affect the current implementation
- stale references to removed behavior

Official documentation for an AI provider is valid when the product intentionally integrates that provider. Cite the official specification or documentation, not the assistant used during development.

## Empty state

If the repository has no material external reference, keep exactly:

```markdown
# References

No external references are currently required.
```

Remove the empty-state sentence when the first reference is added.

## Maintenance

For each code task:

1. Read the existing page before implementation.
2. Add each new material source that the accepted implementation depends on.
3. Update source versions, URLs, and `Used for:` descriptions when their present role changes.
4. Remove entries that no longer describe the current implementation unless retention is legally or operationally required.
5. Keep license and attribution obligations in their required files even when the same upstream source also appears in `REFERENCES.md`.
6. Run `go run scripts/references_page_check.go <repo-root>` against the final working tree.
