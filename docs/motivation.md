# Identity-directed motivation

## Purpose and ownership

Motivation is an independent domain service under `services/motivation/`. It helps the owner cultivate enduring interest in difficult, meaningful work. The owner declares the desired identity, linked personal goal (when available), compelling outcome, next step and permitted intensity. These are objectives to be revisited deliberately, not values an adaptive model may rewrite.

The immediate application is intellectual work where fascination with a real result makes rigorous learning worthwhile. The service does not claim to cause obsession or guarantee preference change. It distinguishes behavioral compliance, subjective engagement and independently demonstrated mastery.

## Adaptation and observation

The service chooses among five transparent, inspectable strategies: outcome anchoring, inquiry, challenge, direct accountability and reflection. Each intervention quotes actual owner-authored commitments; it does not invent research prospects, social judgment or personal shortcomings.

After each intervention, the owner may record engagement, progress and well-being on five-point scales, including zero for an unsuccessful or ignored prompt. Those are **self-reports**. An upper-confidence-bound selection rule explores each strategy and then balances measured reward with further exploration. The reward weights are progress 50%, engagement 25%, and well-being 25%. These weights are an initial experimental heuristic, not a clinical or psychological effectiveness claim.

The service stores interventions and self-reports independently of personal tasks, goals, learning evidence and health records. A linked goal ID is a reference, not authority to modify that goal. No self-report automatically completes a task, awards teaching mastery, records health activity or verifies a scientific result.

## Control and limits

The service starts off. A revision-matched profile update explicitly enables interventions. Only owner-requested prompts are generated; no background notification scheduler or browsing surveillance is present. The owner can select guided or direct language, 1–8 interventions per UTC day and optional UTC quiet hours. There is one unresolved prompt at a time, at least 25 minutes between distinct prompts, and a 256-event bounded history. Very low well-being feedback pauses further interventions. Pausing never deletes the owner's records, and re-enabling requires a deliberate revision-qualified update.

The experiment should measure voluntary engagement when interventions are withdrawn, learning transfer and retention from the Learning service, and independent health and relationship effects where the owner elects to track them. Improving self-reported engagement alone is not evidence of successful self-transformation.

The Rust service owns its private JSON state and exclusive lock. Its dedicated loopback HTTP API requires a separate private token file. The Go core forwards only five exact routes, using a service identity rather than a browser or external API bearer. The Rust/Dioxus UI uses the platform's existing owner-session and CSRF boundary. The optional MCP exposes state, owner-requested suggestions and a pause operation; it cannot alter the aspirational identity or forge feedback. Neither optional interface is needed to run the domain service.

## Source verification

Run `cargo test --manifest-path services/motivation/Cargo.toml` and `cargo clippy --manifest-path services/motivation/Cargo.toml --all-targets -- -D warnings`. Run the ordinary Go and Dioxus checks before release. Generate and commit an approved Cargo lockfile before attempting reproducible Nix packaging. Source commits never activate a process or modify an existing service's database.
