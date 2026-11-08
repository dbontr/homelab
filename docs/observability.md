# Observability

The Go platform provides read-only, owner-authenticated monitoring resources for the Rust/Dioxus console. Prometheus retains metric storage and alert evaluation. Loki retains system journals. The platform does not duplicate either database or construct another metrics retention engine.

## Native endpoints

| Route | Purpose |
| --- | --- |
| GET /v1/monitoring/summary | Current CPU, memory, filesystem, GPU and collector measurements |
| GET /v1/monitoring/series?metric=cpu&range=6h | Selected metric history (1h, 6h, 24h or 7d) |
| GET /v1/monitoring/targets | Active Prometheus targets and scrape health |
| GET /v1/monitoring/alerts | Reported firing and pending alerts |
| GET /v1/monitoring/logs?range=1h&limit=100 | Read-only Loki system-journal records |

Only fixed metric expressions and the `systemd` journal stream are available. The browser cannot construct arbitrary PromQL or LogQL, change a scrape configuration, silence an alert or issue commands. All endpoints require the existing owner session or a separate authorized bearer credential.

A metric not returned by a collector is **unknown**, not zero. Historical charts preserve observed timestamps. Targets report Prometheus scrape health, not the operational health of every service. Alerts show rules currently reported by Prometheus; an empty response is not proof that the cluster is healthy.

## Deployment

NixOS `services.homelabPlatform.monitoring` supplies loopback origins for Prometheus and Loki. The Go client refuses credentials in URLs, proxy use, redirects, non-loopback sources and unbounded responses. Both origins are optional and do not activate the platform when it is disabled.

The monitoring Compose stack binds Prometheus port 9090 and Loki port 3100 only to the configured loopback `SERVICE_BIND_IP`. It retains the existing Grafana service during migration. The first-party console is enabled independently with `services.homelabConsole.enable`. Replacing the homepage remains an explicit operator action; source changes do not change public routing.

Metrics available reflect the active exporter configuration. Jupiter GPU data are sampled only when a running GPU-assigned Pod can be inspected. No sample means no measurement. Additional exporter targets for other cluster nodes, NPU devices and Kubernetes controllers should be added only when the corresponding collectors exist and their trust and cardinality limits are understood.

## Bounds

- Snapshot queries: at most 24 labelled series per metric; no fabricated zero values.
- History: at most 150 points per series, with fixed time windows.
- Targets and alerts: bounded to 200 records each.
- Journal queries: at most 200 entries, with a maximum message length of 4096 bytes.
- Backend responses: 2 MiB limit and request timeouts. No backend query contains a user-provided metric expression.
- The browser session is read-only for monitoring. Cluster administration continues through a distinct privileged executor.

Keep Grafana dashboards available until the Rust dashboard has been compiled, viewed and accepted; they serve as a reference and an operational fallback during migration.
