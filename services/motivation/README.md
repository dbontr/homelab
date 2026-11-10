# Motivation

This independent Rust domain service cultivates motivation around an owner-authored identity and an authentic, personally meaningful outcome. Its state belongs to this service, not to the UI, MCP or public API.

The service starts disabled. Enable it with an exact-revision profile update that supplies an identity, desired outcome and next step. All suggested interventions are owner-requested, inspectable, limited in frequency and recorded with their exact strategy. Feedback records **self-reported** engagement, progress and well-being, not independently verified learning or performance. Strategy selection uses an upper-confidence-bound exploration rule on this feedback; it must not be represented as proven psychological efficacy.

The service retains one outstanding intervention until feedback is recorded or the profile is revised or paused. Revisions cancel stale prompts without manufacturing feedback. Completed interventions have a 25-minute cooldown and a UTC-day limit. Optional quiet hours are specified in UTC; identical start and end disables quiet hours. A well-being report of 0 or 1 automatically pauses further suggestions. The owner can pause at any time, inspect history and reactivate only with a revision-matched profile update. The service does not push messages, lock devices, collect browsing history, shame the owner or modify their declared goals.

## Private service contract

- GET /healthz: local readiness without personal records
- GET /v1/motivation/state: profile and recorded interventions
- PUT /v1/motivation/profile: revise all profile fields, including explicit enabled flag and expected_revision
- POST /v1/motivation/interventions: request a selected intervention
- POST /v1/motivation/feedback: record one response for an intervention
- POST /v1/motivation/pause: immediately disable further interventions

Every /v1 request requires the dedicated Bearer token. The token file must be a private regular file; the server binds numeric loopback only and holds an exclusive state lock. Persisted state is JSON written by atomic replacement, with a bounded history. The Go core is a credential-holding transport client, never a second writer. No credentials, logs or personal records belong in Git.

Configuration: HOMELAB_MOTIVATION_TOKEN_FILE (required), HOMELAB_MOTIVATION_STATE_FILE (optional absolute state path), HOMELAB_MOTIVATION_LISTEN_ADDR (optional numeric loopback address). Provision the parent state directory and token separately. Building this crate does not activate it.
