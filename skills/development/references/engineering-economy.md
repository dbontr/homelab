# Engineering economy

Use this reference when deciding how much code or architecture a task actually needs, when reviewing a diff or repository for over-engineering, when admitting a dependency or abstraction, or when a bug fix could land at several layers.

## Contents

1. Operating principle
2. Selection ladder
3. Root-cause repair
4. Dependency admission
5. Abstraction admission
6. Subtraction review
7. Deliberate simplification ceilings
8. Verification proportional to risk
9. Examples

## 1. Operating principle

Minimize **owned durable surface**, not merely source lines. Owned surface includes source code, dependencies, runtime services, configuration, migrations, generated artifacts, public API, state, tests, operational procedures, and future compatibility obligations.

A smaller solution is preferred only when it satisfies the same required contract. Never trade away security, accessibility, data integrity, required observability, legal obligations, compatibility, or explicitly required behavior to make a diff smaller.

Read before reducing. A small edit in the wrong ownership layer is often more expensive than a slightly larger root-cause fix.

## 2. Selection ladder

Use the first option that satisfies the full requirement:

1. No new mechanism: existing behavior, configuration, data, or deployment already provides the outcome.
2. Existing repository path: extend the canonical helper, component, service, type, query, or pattern.
3. Language/runtime standard library.
4. Native platform capability: browser, OS, database, protocol, framework, cloud primitive, or build-system feature.
5. Already-installed dependency that the repository already trusts and uses for the same concern.
6. Small direct local implementation.
7. New dependency or abstraction with a written technical reason that survives code review.

Do not interpret rung 6 as "one line at any cost." Prefer the most direct readable form. A named helper can be better than an opaque expression when it clarifies a real domain rule or is reused.

## 3. Root-cause repair

A ticket normally describes an observed failure, not necessarily its owner.

For a non-trivial defect:

1. Establish the failing input/state and expected invariant.
2. Trace the path from entry point to failure.
3. Identify where the invariant is owned.
4. Inspect sibling callers or alternate paths that share that owner.
5. Repair the invariant at the narrowest shared layer where the rule is universally correct.
6. Add regression coverage that fails on the old behavior and exercises the ownership boundary.
7. Re-run relevant callers or integration checks when the shared change has a wider blast radius.

Prefer one correct shared guard over repeated caller patches. Prefer caller-specific logic when contracts intentionally differ.

## 4. Dependency admission

A new dependency must have a concrete benefit that outweighs new supply-chain and maintenance surface.

Check:

- Can repository code already do it?
- Can the standard library do it?
- Can the native platform do it?
- Can an installed dependency do it?
- Is a small local implementation simpler to maintain?
- Does the new dependency reduce correctness or security risk enough to justify itself?
- Are license, provenance, maintenance activity, security history, transitive dependency count, binary/native requirements, platform support, and bundle/runtime impact acceptable?
- Does the repository have an allowlist, lockfile policy, vendoring policy, or dependency review process?
- Does `REFERENCES.md` need the authoritative upstream documentation or specification?

Do not add a package because it is fashionable, because a generated example used it, or because it saves a handful of obvious lines while adding a large operational surface.

## 5. Abstraction admission

Add an abstraction when at least one durable reason exists:

- multiple real implementations or variants exist now
- a boundary protects a meaningful invariant or trust boundary
- it removes substantial repeated logic without hiding important differences
- it isolates a volatile external system or protocol
- it materially improves testability of a consequential boundary
- the existing repository architecture explicitly requires the layer

Weak reasons include:

- "we may need another implementation later"
- one class has only one caller
- a constant might become configurable someday
- a wrapper only renames another API
- a factory has one product
- an interface mirrors one concrete type without adding a contract boundary

Prefer direct composition until the evidence changes.

## 6. Subtraction review

After the implementation is correct, inspect the diff or repository for removable complexity. Use these private review categories when useful:

- **delete**: dead code, obsolete branch, duplicate path, speculative feature, stale flag, or unused configuration can disappear with no replacement.
- **reuse**: new code duplicates a canonical repository helper, component, type, query, or pattern.
- **stdlib**: custom logic duplicates a language/runtime standard facility.
- **native**: code or a dependency duplicates a browser, OS, database, protocol, framework, or infrastructure primitive.
- **collapse**: a wrapper/layer/file can be folded into its only meaningful owner without reducing clarity.
- **dependency**: a new package is not justified by its maintenance and security cost.
- **state**: duplicated or derived state can be computed from one source of truth.
- **config**: a knob or feature flag has no real operator, environment, rollout, or product need.

Do not publish these category names into source code unless the repository independently uses them. They are a review method, not product vocabulary.

When reporting a requested lean review, identify location, removable mechanism, replacement, and why the contract remains satisfied. If nothing material can be removed, say the implementation is already lean rather than inventing findings.

## 7. Deliberate simplification ceilings

Sometimes the correct engineering choice is intentionally simple but has a known ceiling. Examples:

- one global mutex is adequate at current throughput
- a linear scan is adequate for bounded collections
- polling is adequate at current latency and load
- an in-memory cache is adequate because restart loss is acceptable
- a heuristic is adequate because false positives are reviewed manually

Document the tradeoff only when a future maintainer could otherwise mistake the limitation for an accident. State:

- the current constraint or ceiling
- the measurable trigger for reconsideration
- the expected replacement or scaling path, if known

Example comment:

```text
# A single process-wide lock is sufficient below the current write rate.
# Use per-account locking if lock wait time exceeds the service SLO.
```

Do not use assistant, model, or skill names as markers. If the repository tracks technical debt through issues, ADRs, or a work queue, use that established mechanism instead of inventing a new ledger.

## 8. Verification proportional to risk

Minimal implementation does not imply minimal evidence.

Low-risk examples:

- typo or copy-only change: targeted render/lint may be enough
- mechanical rename with compiler/type coverage: type check plus targeted tests
- small pure helper: focused unit/regression test

Higher-risk examples require stronger evidence:

- authentication/authorization: negative and positive permission paths
- money/billing: boundary values, rounding/currency behavior, idempotency where relevant
- persistence/migrations: forward migration, compatibility, rollback/recovery plan as applicable
- concurrency: race/deadlock-sensitive tests or deterministic stress where feasible
- cryptography/security: authoritative algorithm/API guidance and misuse-resistant tests
- public API/schema: backward compatibility and contract tests
- destructive operations: failure paths, partial completion, and data-loss prevention
- hardware/control: calibration, tolerance, timing, and physical failure modes where applicable

Use the repository's existing test framework. Do not introduce a new testing framework merely to satisfy this reference.

## 9. Examples

### Date input

Weak: add a JavaScript date-picker package and wrapper component when the browser-native date input satisfies the product and accessibility requirements.

Better: use the native input. Add a dependency only when required formatting, localization, calendar behavior, accessibility behavior, or browser support cannot meet the contract.

### CSV parsing

Weak: implement delimiter/quoting rules manually for ordinary CSV.

Better: use the language's standard CSV parser. Hand-write parsing only for a deliberately different wire format.

### Repeated null checks

Weak: add the same guard in three HTTP handlers after one malformed database row causes a crash.

Better: determine which layer owns the invariant. If every caller requires the same normalized record, enforce it once at the repository/domain boundary and regression-test that boundary.

### Repository interface

Weak: add `AbstractUserRepository`, `UserRepositoryFactory`, and `SqlUserRepository` when there is one database and one implementation.

Better: keep the concrete repository with a clear public contract. Extract an interface when a second real implementation, trust boundary, or testing need makes the abstraction pay for itself.

### New retry library

Weak: add a retry package for one idempotent call even though the installed HTTP client already has the required retry policy.

Better: configure the existing client and test the relevant failure behavior.
