# Motivation service

This independent Rust service owns the aspirational profile, situation-specific intervention selection, a private scheduled inbox and longitudinal engagement observations. It is not an MCP listener, external API gateway or device administrator.

## Configuration

The required HOMELAB_MOTIVATION_TOKEN_FILE names an absolute private file containing a 32–4096 character bearer token. HOMELAB_MOTIVATION_LISTEN_ADDR defaults to numeric loopback 127.0.0.1:8341. HOMELAB_MOTIVATION_STATE_FILE optionally selects an absolute state path. When absent, systemd-provided STATE_DIRECTORY selects the private persistence root, falling back to /var/lib/homelab-motivation. The parent directory must exist.

The service uses a private exclusive lock, bounded JSON state and atomic replacement. Neither personal records nor credentials belong in Git.

## Private HTTP contract

Every /v1 route requires the dedicated service token. The service does not accept browser-session cookies, MCP credentials or external API keys directly.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | /v1/motivation/state | Read owner profile, check-ins and intervention history |
| PUT | /v1/motivation/profile | Apply a revision-checked profile and opt-in policy |
| POST | /v1/motivation/interventions | Request a situation-specific intervention |
| POST | /v1/motivation/feedback | Record self-reported intervention feedback |
| POST | /v1/motivation/checkins | Record voluntarily initiated work and fascination |
| GET | /v1/motivation/metrics | Read 28-day engagement aggregates |
| POST | /v1/motivation/dismiss | Dismiss an unresolved intervention without rating it |
| POST | /v1/motivation/pause | Immediately pause further interventions |
| POST | /v1/motivation/focus | Begin a bounded, owner-initiated focus commitment |
| POST | /v1/motivation/focus/end | End a focus commitment with completion or early-exit evidence |

GET /healthz reveals readiness only.

The service also stores bounded focus commitments independently of notifications. It captures the next step and desired outcome as chosen by the owner, allows immediate exit, and records elapsed time without claiming verified learning.

The optional background scheduler creates inbox records only after the owner opts into both interventions and proactive scheduling. It respects quiet hours, cooldown, daily limits, an intervention-free weekday and the unresolved inbox item. It never pushes messages to third parties, edits study records or controls devices.

The Go core is a narrow credential-holding transport client, configured with HOMELAB_MOTIVATION_SERVICE_URL and HOMELAB_MOTIVATION_SERVICE_TOKEN_FILE. Its address must be an exact numeric loopback HTTP origin; its bearer is not supplied to the UI or MCP. Browser writes are subject to the platform's existing owner-session and CSRF checks. The optional MCP offers only read, requested suggestion and pause operations.

## Build and acceptance

Generate a Cargo.lock with the approved toolchain. Verify formatting with cargo fmt --all --check, tests with cargo test --locked, and Clippy with cargo clippy --locked --all-targets -- -D warnings. Verify the Go bridge and Dioxus browser bundle. The NixOS module requires a separately provided immutable package and enables no service by default. Test restart, recovery, credentials, rate limits and state continuity before any authorized deployment.
