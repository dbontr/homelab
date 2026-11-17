# Production integration and acceptance

## Authority and source baseline

The integration baseline is `dbontr/homelab` `main` at commit
`53c40842cc7354deb29ce64608dcb4bd0794a6fa` (verified by the GitHub
`refs/heads/main` endpoint on 2026-11-17). Every workstream branch below
was created from this exact commit. A newer default-branch revision is not
implicitly accepted: refresh the baseline, file ownership, dependency diffs,
and test receipts before integrating it.

**Production freeze:** do not change the deployed Compute controller, Kueue
queues, project database, knowledge store, artifact volumes, hardware leases,
running jobs, Cloudflare routes, security services, credentials or active NixOS
generations. Do not restart, scale, drain, reconfigure, upgrade or replace a
production workload. Source-development branches and disposable test systems
are the only permitted mutation targets. No PR merge, automatic merge, live
release staging on a production host, activation, or production retirement is
approved by this document.

The workstream branch is the **source owner**, not an assertion that a human
reviewer has accepted the work. Human code reviewers and the independent
release owner must be identified before promotion. Research workloads remain
under their existing authority throughout development.

## Integration matrix

| Workstream and source branch | Write ownership | Dependencies and exit gate | Candidate order |
| --- | --- | --- | --- |
| Networking and protected release — `workstream/network-release-gates` | `infrastructure/homelab/{cloudflare,mcp-ingress}.nix`, `infrastructure/homelab/access/`, `scripts/{stage-release,verify-release}.sh`, release and ingress-specific tests | Independently authenticated ingress, exact artifact/revision, staged release, isolated rollback, zero unauthorized route mutations; #4, #2, #3 | 2 |
| Rust security and execution — `workstream/rust-owner-execution` | `services/security-authority/`, `services/executor/src/{authorization,policy,transport,audit}.rs`, `services/core/security/` and specific parity tests | Denial/revocation/replay, exclusive authority and durable audit under faults, Nix packaging and isolated state migration; #143, #11, #10 | 1 |
| Encrypted backup and restore — `workstream/compute-offsite-recovery` | `services/compute/internal/controller/{offsite_backup,backup}*.go`, `infrastructure/hosts/janus/compute-backup.nix`, `infrastructure/homelab/workspace/controller-backup-values.yaml`, recovery tests and runbook | Restricted Janus SFTP identity, separately held secrets, independent encrypted scheduled snapshot, complete data check and exact-ID recovery, free-space threshold; #192 | 3 |
| Compute resilience and OCI recovery — `workstream/compute-resilience` | `services/compute/internal/controller/{kueue_backend,kueue_job_state,worker_maintenance}*.go`, `services/compute/scripts/{verify-cold-peer-pull,recover-oci-peer,verify-registry-mirror}.sh`, compute-specific fixture tests | Controller durable receipts, Kueue admission, cancellation/rollback, node recovery and true uncached verified-layer transfer without disturbing EDA; #168, #163, #151 | 4 |
| Project catalog and knowledge reconciliation — `workstream/project-catalog-reconciliation` | `services/compute/internal/controller/{project_catalog,project_knowledge_context}*.go`, associated catalog/knowledge tests and provenance reconciliation | Complete #192 restore, exact historical digest/receipt reconciliation, protected archival with no worker interruption; #196, #5 | 5 |

The order above is for **candidate reconciliation**, not permission to
merge branches into `main` or to deploy. Parallel source reviews can proceed
without modifying the active installation. Preserve each branch's commit SHA
in its proposed PR and update this table when the interfaces actually change.

### Shared-file arbitration

| Shared path or interface | Potential writers | Arbitration |
| --- | --- | --- |
| `services/compute/cmd/project-workspace-controller/main.go` | Backup, Compute, Projects | Reserved for the integration owner. Each workstream supplies an interface-level patch and a regression test; integrate once, after reviewing startup and schema compatibility. |
| `services/compute/deploy/helm/project-workspace/templates/controller.yaml` and `values.yaml` | Backup, Compute | Reserved for integration. Render the complete future manifest against the preserved existing Helm release; do not implicitly enable the scheduler or change the controller image. |
| `services/executor/src/release.rs` and release-authority transport | Networking, Security | Security owns authorization policy; Networking owns artifact verification. Coordinator reconciles their call contract and negative tests. |
| `infrastructure/hosts/saturn/configuration.nix`, `infrastructure/homelab/default.nix`, `flake.nix` | Networking, Security, Backup, Compute | Integration owner only. Treat every option activation as an independent release-stage change. |
| `services/compute/internal/controller/project_context*.go` and schema migration files | Compute, Projects, Backup | Integration owner arbitrates database writer and idempotency semantics; never establish two writers or silently alter historical identities. |
| `REFERENCES.md`, `scripts/verify-offline.sh`, `docs/deployment.md` | All | Integration owner only; consolidate source provenance and test registration once. |

No workstream may edit another row's source paths or the shared files
without first transferring ownership in its coordination issue. File-path
overlap is an integration blocker, not a reason to accept the later writer's
changes. Compare each branch to the pinned base and inspect renames and
generated files, not only textual conflicts. Do not automatically resolve
differences in state schema, operation policy, Helm defaults, service
identities or NixOS option defaults.

## Evidence register (as of November 17, 2026)

| Workstream | Demonstrated evidence | Not yet demonstrated |
| --- | --- | --- |
| Networking / release | PR #191 added owner-authorized Saturn release policy; PR #195 added monitoring Access audience; current connected monitoring returned 11 healthy targets and no alerts. | Exact clean-source staged release, full ingress and DNS policy acceptance, independently approved activation and isolated rollback for this candidate. |
| Rust security | Rust security and executor operate on Saturn in recorded read-only checks; owner-authenticated signed MCP job succeeded, and an attempted unauthorized deployment restart was denied. PR #194 merged Rust source-format changes. | All revocation/CSRF/replay and concurrent-writer negative tests, protected audit recovery, migration/failover and full exact-head offline source verification. |
| Backups | An age-encrypted Janus recovery archive exists; a past independent manual restore checked SQLite and artifact hashes. PR #193 implements source-owned Restic retention and Janus SFTP declarations. | Restricted identity and Restic path installed in Janus's live generation, credential provisioning, automatic snapshots, `restic check --read-data`, exact snapshot restoration, retention and alert acceptance. Janus recovery filesystem was about 83% used during the recorded read-only inspection. |
| Compute | A signed owner-scoped Kueue CPU Job reached `succeeded` and its durable output was read back; identical retry was observed as replayed. Five nodes and 25 deployments were Ready in the latest recorded console inspection. Cold OCI fallback has worked in bounded trials. | Controlled worker/controller outage tests, all cancellation and retry branches, independent cold layer validation across all intended nodes, protected recovery under active work and complete release certification. |
| Projects | PR #197 merged reversible revision-checked catalog visibility tests; records report 10 acceptance and 16 Workspace artifacts copied and checksum-checked, with original records retained. | Catalog feature deployed, three historical IDs hidden under authorization, every legacy record and independent source archive reconciled, post-migration encrypted restore and safe worker retirement. The live catalog still exposes seven projects. |

Authoritative evidence and unresolved criteria:

- [Release tracking #4](https://github.com/dbontr/homelab/issues/4), [release policy PR #191](https://github.com/dbontr/homelab/pull/191), [ingress PR #195](https://github.com/dbontr/homelab/pull/195).
- [Rust authority #143](https://github.com/dbontr/homelab/issues/143), [privileged executor #11](https://github.com/dbontr/homelab/issues/11), [Rust source PR #194](https://github.com/dbontr/homelab/pull/194).
- [Off-host recovery #192](https://github.com/dbontr/homelab/issues/192), [backup source PR #193](https://github.com/dbontr/homelab/pull/193).
- [Kueue integration #168](https://github.com/dbontr/homelab/issues/168), [cold-peer OCI #163](https://github.com/dbontr/homelab/issues/163), [image distribution #151](https://github.com/dbontr/homelab/issues/151).
- [Catalog archival #196](https://github.com/dbontr/homelab/issues/196), [project visibility PR #197](https://github.com/dbontr/homelab/pull/197).
- [Unified MCP acceptance #3](https://github.com/dbontr/homelab/issues/3), [architecture acceptance #148](https://github.com/dbontr/homelab/issues/148).

These are evidence citations and test inventories, not proof that all source
tests passed at the current baseline. The active monitoring snapshot is not a
substitute for read-only Kubernetes readiness, database health, or authorization
negative testing.

## Integrated acceptance suite

The source-only entry point is
`scripts/verify-production-integration.sh`. Its default `--plan`
mode performs no testing or mutations. The `--run` mode is intended for a
clean, exact-commit checkout inside a disposable, **network-isolated**
non-root VM/container without Kubernetes, backup, Cloudflare or production
credentials. The script refuses several unsafe host/environment conditions,
but these guards do **not** establish a security boundary by themselves.
Isolation is enforced outside the script by the runner.

```sh
bash scripts/verify-production-integration.sh --plan
# In a disposable unprivileged environment with production egress denied:
set -o pipefail
HOMELAB_ACCEPTANCE_ISOLATED=1 \
  bash scripts/verify-production-integration.sh --run | tee acceptance.jsonl
```

The driver records each gate's result and exact source SHA. It runs the
repository's full offline Nix/Go/Rust/Helm verifier plus release, security,
executor, Restic round-trip, Kueue, OCI and catalog fixture tests. A fail or
skipped dependency is **not** production certification. Record toolchain
versions, fixture source and the terminal exit status in the independent
evidence store. The full suite is pending execution at the integration head.

The following additional **isolated** integration gates are mandatory
before a cutover proposal can be approved:

1. **Ingress and release:** Test an isolated Cloudflare/OAuth configuration or
   faithful non-production IdP fixture, anonymous denial, wrong audience,
   expired identity, route equivalence, signed release manifest, one failed
   health probe and complete rollback of the isolated release.
2. **Security and execution:** Use synthetic credentials and a separate Rust
   authority with no production vault mount. Test expired/revoked grants,
   concurrent requests, unauthorized resource, substituted arguments,
   one-use destructive confirmation, missing audit, crash after accepted
   execution, fail-closed behavior and single-writer state migration.
3. **Encrypted recovery:** Use an isolated Restic repository and fresh
   disposable SFTP server, with separate authorized identity. Test scheduled
   snapshot, transport failure/retry, full-data integrity, exact snapshot ID,
   empty-target restore, missing artifacts, SQLite constraints and retention
   with unrelated snapshots preserved.
4. **Compute:** Use a disposable K3s/Kueue cluster or equivalent isolated
   control-plane test fixture. Exercise quota blocking, idempotency,
   cancellation, controller restart, worker outage and recovery, orphan
   cleanup, exact job-result retrieval and a genuinely uncached OCI layer
   verified against the source digest. Do not use production clusters.
5. **Projects:** Start from a verified read-only export copied into an
   independent recovery environment. Confirm old jobs and knowledge are
   recoverable, original SHA-256 values remain equal, visibility is
   revision-fenced and reversible, research projects remain visible, and
   no worker or active lease is deleted by catalog archival.

A candidate is **conflict-free** only after all five branch heads have been
pinned, cross-branch overlaps reconciled, the integration diff reviewed,
all applicable tests passed on that exact integration SHA, and no required
database/schema or authority changes remain unspecified. A clean Git
three-way merge alone is insufficient.

## Independently approved deployment sequence

This is an operator runbook for a *future* separately approved cutover.
It is not permission for a protected operation.

1. **Freeze and ownership:** Confirm the independent release owner, signed
   identity, exact intended host and resource, exact candidate SHA/digests,
   permitted operation, valid authorization and separate confirmation for
   destructive trust changes. Recheck all five physical nodes, current
   deployment revisions, running Jobs, live EDA sessions, maintenance leases,
   existing storage capacity, credential custodians and all 25 declared
   worker Deployments. Any ambiguous or unknown result blocks activation.
2. **Protect research data first:** Preserve the current PVC and all manual
   recovery archives. Establish a restricted backup destination and prove
   off-host encrypted snapshot restoration on a separate host **before**
   approving controller or catalog state changes. No production credentials
   may enter a development workspace.
3. **Stage outside production:** Build and verify the exact source, Nix
   closure, Rust service artifact, OCI digest, Helm diff, rollback receipts,
   and complete manifest in the independent release environment. Stage
   without changing Cloudflare, production services or host generations.
4. **Authorize each small change:** Use the independent owner-approved
   release/executor contract and a bounded exact-resource request. Confirm
   that one authority owns the privileged policy and audit journal.
   Revalidate readiness before each operation; no authorization is inferred
   from a successful ChatGPT conversation or a branch merge.
5. **Enable backup safely:** Provision only the approved Janus restricted
   identity and protected Secrets; preserve its old generations and archives.
   Review controller Deployment and Helm values against the installed
   revision, including current image digest and PVC. Do not restart an
   occupied controller merely to install a configuration. Observe the
   scheduled Restic snapshot, integrity check, exact independent restore,
   retention and alerts before any data migration.
6. **Network/security cutover:** Make one independently authorized
   target-scoped change at a time. Check the previous origin route, protected
   Access policies, identity assertions, owner portal and deny-by-default
   behavior. Fail closed rather than enabling a direct unauthenticated path.
7. **Compute resilience:** Only after explicit maintenance clearance and
   controlled workload placement, trial one eligible node at a time with
   rollback and independent SSH/NanoKVM recovery. Preserve Jupiter/Janus
   EDA sessions. Check Kueue, worker leases, complete cold OCI content and
   retained durable results between trials.
8. **Project archival last:** Check that no historical registration has an
   active Job, workspace, lease or dependent service. Confirm recovery
   and original source provenance. Apply exactly the owner-approved allowlist
   for `acceptance`, `workspace-mcp` and `health-stack`; never delete
   history or repurpose an ID. Confirm exactly the expected visible catalog
   and a reversible administrative listing.
9. **Acceptance:** Verify authenticated cross-chat access, fresh research
   job submission/result, full knowledge/artifact recovery, worker/queue
   health, secure denial tests, monitoring, and independently retrievable
   backup receipts. Attach exact timestamps, checksums, versions and audit
   IDs to the appropriate open issues.

## Rollback and stop conditions

- Before every approval, retain immutable source/image digests, the
  pre-change Helm release and NixOS closure, the exact previous Cloudflare
  configuration, read-only database/schema inventory, archived job receipts,
  and the Restic recovery snapshot ID. Never modify the old recovery copy.
- On denied permission, missing audit receipt, unknown operation outcome,
  unavailable restore, unready node, ongoing EDA work, incorrect image digest,
  quota mismatch, altered policy, or unexpected state writer: **stop**. An
  uncertain execution is not a failed operation and must be reconciled by
  original request ID before any retry.
- Use the independently authorized release controller to restore the
  prior verified executable/Helm/NixOS configuration. Do not bypass its
  audit boundary with a root console; NanoKVM remains an emergency
  recovery interface, not a substitute for owner authorization.
- Roll back catalog visibility through its revision-checked administrative
  operation if safe. Never blindly reverse a data migration, restore an
  older database over the running PVC, or discard artifacts, encryption keys,
  project histories, jobs or external archives. Any destructive data
  restoration requires its own incident authorization and independent
  offline verification.

## Exit checklist

| Gate | Status at creation | Required proof |
| --- | --- | --- |
| Pinned ownership and five independent branches | Established | Six branch refs derive from the exact baseline SHA; check new commits against it. |
| Shared paths and compatibility interfaces | Defined, not reconciled | Changed-file intersection report, explicit arbitration and source/Helm/schema parity. |
| Offline unified acceptance at candidate SHA | Not run | Driver exit 0 and recorded per-gate receipts, with no skipped required test. |
| Five isolated failure/recovery suites | Not run together | Signed test records and exact fixture/build provenance for every workstream. |
| Live automatic off-host backup | Not active | Real scheduled snapshot, `restic check --read-data`, independent exact-ID restore. |
| Independent owner authorization for cutover | Not requested | Exact resource/change, active owner grant, single-writer and journal preflight. |
| Production activation | Prohibited by freeze | Explicit separate approval and reversible release receipt. |
| Historical project retirement | Not authorized | Verified backup, provenance, zero leases, owner-approved allowlist. |
| Production completion | Not certified | End-to-end acceptance against the deployed revision; all relevant issues retain evidence. |

Keep #4, #11, #143, #192, #168, #163, #151, #196, #3 and #148 open
until their specific acceptance evidence is attached. No checkbox here
closes an issue or changes a production deployment.
