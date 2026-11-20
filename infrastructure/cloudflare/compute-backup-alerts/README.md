# Compute backup alert receiver

The Homelab Compute controller posts authenticated `firing` and `resolved`
state transitions to an independent HTTPS Cloudflare Worker. The Worker
records event receipts in the `homelab-compute-backup-alerts` Workers KV
namespace, deduplicates sequential equal states by condition, and relays
non-sensitive status text to an operator-owned random ntfy topic.

This is a notification integration, not an authentication or repository
authority. All backup data and repository passwords stay on the controller
and restricted Janus SFTP service. No source file contains the bearer token,
ntfy topic value, or Cloudflare account credential.

## Deployment

Worker script: `worker.mjs`, deployed as
`homelab-compute-backup-alerts` in the Cloudflare account.

Worker environment:

- `ALERT_EVENTS`: Workers KV binding to dedicated namespace.
- `TOKEN_SHA256`: secret containing the SHA-256 hex digest of the
  *entire* Kubernetes alert token file, including its trailing newline.
  The Worker hashes the provided 64-character hex bearer value plus
  newline before comparison; this value is a verifier, not the token.
- `NTFY_TOPIC`: secret containing a random 30–64-character ntfy topic.
  The topic is a bearer-like subscription identifier; avoid public exposure.

The endpoint is
`https://homelab-compute-backup-alerts.self-optimization.workers.dev/hooks/compute`.
Only requests with `Authorization: Bearer <token>` and valid JSON event
shape are accepted. GET `/healthz` is an unauthenticated liveness check.

Controller source Helm overlays:
`infrastructure/homelab/workspace/controller-backup-values.yaml` and
`infrastructure/homelab/workspace/controller-backup-production-values.yaml`.
The latter configures only the alert URL and Secret name; both must be
included over the existing retained controller Helm release values.
Do not replace the current source-pinned image digest, delete the
controller PVC, grant new worker permissions, or disable Helm fail-closed
admission. Never put the Kubernetes token in Helm values or Git.

## Acceptance and limitations

Verify: anonymous POST 401; incorrect token 401; authenticated malformed
JSON 400; valid firing 200; identical sequential firing 202; resolved 200;
identical sequential resolved 202; Workers KV event and state receipts; and
notification-provider 2xx. A provider 5xx must result in receiver HTTP 503
and must not be recorded as delivered.

The operator **must subscribe** their device to the configured ntfy topic
and verify the actual delivery of firing and resolved notifications.
An HTTP 200 from ntfy is proof of server acceptance, not proof that a
human received a push notification.

ntfy.sh topics are accessible to anyone who knows their random topic name.
Notifications contain only the backup condition, state, and UTC timestamp.
If confidential operational metadata is later added, replace this
transport with an authenticated operator-owned destination. Cloudflare KV
state is eventually consistent and cannot provide atomic cross-region
exactly-once delivery under concurrent requests; this receiver guarantees
neither strict serialization nor eternal retention.

Completed Compute recovery certification additionally requires a real
project-workspace-tagged snapshot, `restic check --read-data`, an
independently restored exact snapshot in empty storage, verified SQLite
and project artifact integrity, a checked retention dry run, and outage
alert/recovery evidence. The Worker alone does **not** certify backups.
