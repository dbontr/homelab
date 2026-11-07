# Personal Health MCP product scope

## Purpose

Provide a self-hosted, single-owner, longitudinal record of health, nutrition, activity, sleep, and cognitive performance through an MCP interface. Persist source evidence and measurement uncertainty separately from interpretation. Let a compatible AI host support planning and investigation without delegating medical judgment to the server.

## Core behavior

### Fitness and performance
- Versioned strength and cardio plans, exercise libraries, sets, repetitions, load, pace, zones, and workout logs.
- Progressive overload, periodization, fatigue management, deloads, mobility routines, technique-review evidence, injury and rehabilitation history.
- Recovery and readiness assessments grounded in sleep, resting heart rate, HRV, recent exertion, and subjective factors.
- Field-test VO2 max estimates, optional validated ergometer/fitness-model estimates, running economy and threshold indicators when data permit.
- Interactive in-chat workout editing and guided sessions; every session has an explicit plan identity and calendar date.
- Unlogged training is unconfirmed. A schedule is adjusted for a missed workout only after the outcome is established. A rescheduled occurrence requires separate confirmation.

### Nutrition and metabolism
- Barcode lookup, full macro- and micronutrient records, RecipeSage recipes, meals, meal plans, groceries and optional pantry inventory.
- Visual food identification through host-provided images, with separately confirmed identification, portion size and uncertainty.
- Hydration, electrolytes, supplements, food allergies/intolerances, nutrient timing and distribution.
- Nutrition goals and daily comparisons with coverage and unit checks; unavailable nutrients are not zero.
- Personalized TDEE modeling and potentially inadequate energy availability, with uncertainty and non-diagnostic reporting.

### Physiology, well-being, and clinical history
- Read-only Google Health ingestion and biometrics: sleep, activity, cardio, anthropometrics and supported physiological metrics.
- Sleep patterns, duration, timing, stage metadata, descriptive sleep shortfall, and behavioral sleep-timing regularity.
- Laboratory results, medications, symptoms, illness, cardiometabolic measures, body composition, stress and subjective mood.
- Digestive, oral, reproductive and hormonal health records when opted into; caffeine, alcohol and other relevant exposures.
- Manually recorded environmental context may accompany observations; automated bedroom-environment monitoring is not enabled.
- Longitudinal cognitive performance tests with fixed protocols and transparent device limitations.

### Analytic reasoning
- Per-metric charts with units, data-source attribution, missing-data visibility and correction history.
- Descriptive anomaly detection against personal baselines, and optional controlled N-of-1 experiments.
- Evidence-grounded recommendations with source references, evidence strength, contraindication context and review dates.
- Keep measured, estimated, user-reported, and raw-record events distinct. An AI inference is never silently promoted to measured data.

## Integrations

RecipeSage features are embedded as Go MCP tools; the RecipeSage service remains the recipe system of record until a validated data migration is available. Google Health is read-only upstream. Barcode and USDA nutrient retrieval are read-only provider operations. External wearables beyond Google Health are a later integration.

## Deferred capabilities

- Automatic bedroom-environment measurements and sleep correlation require optional sensor integration and separate approval.
- Additional wearable platforms beyond the Google Health integration are deferred.
- Posture and sedentary-behavior analysis require a separate feasibility and consent review.

No deferred capability is represented as an active integration.

## Deliberate exclusions

GPS routes and elevation-track analysis, automatic video/wearable exercise recognition, and jet-lag or circadian-adjustment scheduling are excluded. Routine preventive-care scheduling, skin/wound imaging, and medical consultation report generation are also excluded.

## Safety and privacy

- Local self-hosting and least-privilege access. Never publish medical records, images, credentials or exports in Git.
- Source corrections are auditable; imported records retain upstream identity without silent replacement.
- The service is not a regulated diagnostic device. No automated diagnosis or unsupported medication decision is permitted.
- Missing observations are unknown; correlation is not causation.
- A complete deployment requires tested authentication, data protection, restore recovery, and provider authorization.

## System structure

- `internal/health`: SQLite-backed observation, plans, goals and validation
- `internal/mcp`: MCP transport, tool registrations and embedded in-chat interfaces
- `internal/googlehealth`: authorized read-only Google Health synchronization
- `internal/fooddata`: Open Food Facts and USDA food records
- `internal/recipesage`: RecipeSage compatibility API client
- `cmd/personal-health-mcp`: executable and environment configuration

The selected persistence model uses a single-owner SQLite database to minimize operational dependencies. A larger multi-user deployment requires separate identity, schema isolation and encryption design.
