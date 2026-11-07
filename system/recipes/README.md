# First-party recipes

The Go platform owns recipe, planned-meal and shopping data. The Dioxus browser UI is a client, not another recipe server. The recipe store persists to `HOMELAB_RECIPES_FILE` (default: a private `recipes.json` beside the platform's state file) using atomic file replacement and revision checks. Storage and HTTP handlers are independent of the RecipeSage compatibility adapter.

## Resources

- `GET /v1/recipes?query=...`, `POST /v1/recipes`, `GET/PUT/DELETE /v1/recipes/{id}`.
- `GET /v1/recipes/{id}/nutrition?servings=N` returns a sourced nutrient preview; absent values remain absent.
- `GET /v1/recipes/health-goals` reads the existing Health integration's nutrient and physiological targets where available. It does not alter Health records.
- `GET /v1/meals`, `POST /v1/meals`, `DELETE /v1/meals/{id}`.
- `GET /v1/groceries?start=YYYY-MM-DD&end=YYYY-MM-DD` consolidates scaled quantities by ingredient name and unit. It does not assume that differently named ingredients or different units are interchangeable.
- `GET /v1/recipes/export`, `POST /v1/recipes/import`: portable, versioned JSON (version 1) holding `recipes` and `meals`.

Recipe edits use the existing stored `revision` as an optimistic concurrency guard. Deletion requires an `If-Match` header containing the current revision and fails while a meal references the recipe. An import never overwrites existing IDs. Browser writes require a valid owner-session cookie, an approved Origin, JSON media type and the session's `X-CSRF-Token`. The privileged cluster executor and MCP bearer credentials are not involved.

## Boundaries not yet implemented

The grocery response is a **derived preview**, not a checked/purchased-item ledger. Images have local-reference schema fields but no upload pipeline. RecipeSage/Schema.org importers, persistent history/version restore, cross-domain Health food-log actions, micronutrient inference and Health meal-plan synchronization require subsequent implementation. Until migration is explicitly approved, RecipeSage remains available and its data untouched. Do not import or remove its data based on an assumption that the original database is empty.
