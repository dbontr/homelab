# Unified notifications

Homelab's notification centre is a single, durable, **owner-private** inbox exposed by the Go Platform. The protected web console includes a **System → Notifications** tab at `#notifications`.

## Sources and authorization

A source is one of: `automation`, `backup`, `compute`, `devices`, `engineering`, `health`, `learning`, `media`, `monitoring`, `motivation`, `network`, `personal`, `security`, `software`, or `system`. The publisher of each source must have its own 32+ character credential.

The optional NixOS option `services.homelabPlatform.notificationPublisherTokensFile` points to an **out-of-store, protected JSON file** containing a map from source name to unique publisher token:

```json
{
  "engineering": "<a unique secret managed outside Git>",
  "backup": "<another unique secret managed outside Git>"
}
```

Do **not** commit real tokens or write them in the Nix store. systemd imports this file as a private `LoadCredential` item. An unconfigured source cannot publish; no source is enabled automatically. Source tokens cannot match the platform's API identity or another source. A browser session and the platform-wide API bearer token cannot impersonate a source.

A source with a configured publisher sends an authenticated loopback request:

```http
POST /v1/notifications/publish/engineering
Authorization: Bearer <engineering-scoped-token>
Content-Type: application/json

{"event_key":"build-001","severity":"warning","title":"Build delayed","message":"Review the compiler log"}
```

`event_key` is stable for retries; a retry with identical contents is idempotent, while changing the payload for the same key is rejected. Valid severities: `info`, `warning`, and `critical`.

The shared inbox uses `GET /v1/notifications`, `GET /v1/notifications/preferences`, `PUT /v1/notifications/preferences/{source}`, and `POST /v1/notifications/ack/{id}`. Owner browser writes require Cloudflare Access, exact-origin checks and CSRF protection. All records are persisted in the platform's existing private state file using its atomic sync-and-rename discipline.

## Semantics

* The per-source switch controls **visibility in the inbox**, not the producer's permission, operational monitoring or audit retention.
* A muted event is retained. Re-enabling the source reveals the event again.
* Acknowledging is recorded; it is not proof that an external action was taken or that a push message reached a device.
* Only the latest 200 events are returned per inbox request. Retention is bounded at 1,024, evicting the oldest **acknowledged** event if capacity is reached; it refuses to discard unread events.
* Device/email/push delivery is intentionally not yet enabled. Adding a transport must respect separate owner opt-in, delivery receipts and source permissions.

## Integrating other areas

An area publishes through the **same** endpoint using an individually provisioned credential for its source. Existing independent alert streams (monitoring, recurring-automation observations, backup checks) continue to function while their producers are migrated to the shared notification interface; avoid duplicating their own inbox/database or treating notification delivery as an operational recovery mechanism. Each adapter must use stable deduplication keys, publish only non-sensitive summaries, and never attach secrets or full health records to a notification.

## Verification

Test and build without host activation:

```sh
go test ./services/core/store ./services/core/transport ./services/core/cmd/homelab-platform
nix build --offline --no-link .#homelab-console-web
nix eval --offline --raw .#nixosConfigurations.saturn.config.systemd.services.homelab-platform.serviceConfig.StateDirectory
```

Do not infer production deployment from a successful build. A protected release and an owner-authenticated browser acceptance test are distinct requirements.
