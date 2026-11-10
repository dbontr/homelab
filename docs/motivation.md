# Identity-directed motivation

## Purpose

Motivation is a private service for cultivating enduring intellectual engagement around authentic, owner-chosen objectives. It emphasizes meaningful outcomes, rigorous inquiry and the voluntary pursuit of difficult work. It does not claim to manufacture passion.

The owner defines a desired identity, an outcome worth pursuing, a next step, and an optional personal goal reference. A model or scheduler cannot silently rewrite these commitments. The Rust service is the sole writer of its own private state.

## Interventions

Five strategies are available: outcome anchoring, inquiry, challenge, direct accountability and reflection. The owner can identify the present situation: session start, resistance, distraction, uncertainty or milestone. Initial choices are situation-specific. Subsequent strategy selection uses a bounded upper-confidence-bound heuristic that balances exploration with reported engagement, progress and well-being.

The owner chooses guided or direct language. The system uses authentic owner-supplied questions, never invented achievements, fictitious research breakthroughs or moral judgments about missing logs.

Interventions start disabled. The owner can additionally enable proactive scheduling, independent of the MCP and API. Scheduled suggestions are written to the private in-app inbox; they are not sent to external channels. One unresolved suggestion is held until feedback, dismissal, profile revision or pause. Cadence is 60–1440 minutes, with a 25-minute minimum cooldown, 1–8 suggestions per UTC day and optional UTC quiet hours. A selected weekday can suppress scheduled interventions to provide an intervention-free observation period.

Pausing and profile revisions cancel unresolved prompts without fabricating feedback. Very low reported well-being pauses further interventions. No browsing surveillance, device blocking, autonomous messaging or infrastructure operations are authorized.

## Observations

Intervention feedback records engagement, progress and well-being linked to the exact strategy. Independent check-ins record minutes of intellectual work, voluntary initiation, fascination, well-being, optional evidence reference and whether the record occurred on a scheduled intervention-free day.

The Rust service computes 28-day aggregates. These are owner self-reports, not verified task completion, demonstrated learning or causal proof of a psychological intervention. Holdout days and voluntary initiation are exploratory measures, not a randomized trial. An optional reference to a Learning or research record does not give the motivation service access to that record or authority to modify it.

## Service independence

Source is in services/motivation. The private Rust process owns the profile, scheduler, state lock and all intervention records. The Go core presents eight fixed routes through its existing owner-session and CSRF controls. The Rust/Dioxus view presents those routes. The unified MCP offers status, metrics, requested suggestions and pause without allowing the model to change the aspirational identity or submit purported self-reports.

The optional NixOS service module provides process isolation, a private state directory, credential loading and loopback transport. The service is disabled by default. Its package must be an immutable verified Rust build supplied explicitly; Cargo.lock and build verification are required before activation.

## Verification

Run Rust formatting, unit tests, Clippy, HTTP integration tests, core Go tests, Dioxus WASM build, NixOS evaluation and isolated recovery checks. Validate revoked permissions, request limits, authentication, state continuity, exclusive file ownership, quiet hours and holdouts. Code availability is not production acceptance.

The owner can additionally start a 10–240 minute focus commitment on the profile's current concrete step. The service records start and end times, self-reported completion or deliberate early exit, and an optional reflection. An early exit ordinarily requests a short reason, with an immediate exit option that requires none. Goal revisions supersede active commitments without recording a false failure. Elapsed focus intervals are capped at the declared duration in summary statistics and must not be interpreted as independently verified work.

Long-term progress is measured by independently demonstrated competence and freely initiated intellectual work, not the number of motivational prompts displayed.
