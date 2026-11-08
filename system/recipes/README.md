# First-party recipes

The Go platform owns recipe, planned-meal and shopping data. The Dioxus browser UI is a client, not another recipe server. The recipe store persists to `HOMELAB_RECIPES_FILE` (default: a private `recipes.json` beside the platform's state file) using atomic file replacement and revision checks. Storage and HTTP handlers are independent of the RecipeSage compatibility adapter.

## Resources

- `GET /v1/recipes?query=...`, `POST /v1/recipes`, `GET/PUT/DELETE /v1/recipes/{id}`.
- `GET /v1/recipes/{id}/nutrition?servings=N` returns a sourced nutrient preview; absent values remain absent.
- `GET /v1/recipes/health-goals` reads the existing Health integration's nutrient and physiological targets where available. It does not alter Health records.
- `GET /v1/meals`, `POST /v1/meals`, `DELETE /v1/meals/{id}`.
- `GET /v1/groceries?start=YYYY-MM-DD&end=YYYY-MM-DD` consolidates scaled quantities by ingredient name and unit. It does not assume that differently named ingredients or different units are interchangeable.
- `GET /v1/recipes/export`, `POST /v1/recipes/import`: portable, versioned JSON (version 1) holding `recipes` and `meals`.
- `POST /v1/recipes/import-url` with `{"url":"https://..."}`: fetch a public HTTPS document, extract schema.org Recipe data and return an **unsaved draft** for human review. The same operation is available through the `recipe_import_url_preview` MCP tool.

Recipe edits use the existing stored `revision` as an optimistic concurrency guard. Deletion requires an `If-Match` header containing the current revision and fails while a meal references the recipe. An import never overwrites existing IDs. Browser writes require a valid owner-session cookie, an approved Origin, JSON media type and the session's `X-CSRF-Token`. The privileged cluster executor and MCP bearer credentials are not involved.

## Boundaries not yet implemented

The legacy `/v1/groceries` response is a derived preview. The first-party `/v1/groceries/board` and `/v1/groceries/items/{key}` routes persist an owner-confirmed checked/purchased ledger with optimistic revision checks. Identical ingredient names with different units remain separate; checklist status is keyed by normalized ingredient/unit, not by individual shopping trip. Images have local-reference schema fields but no upload pipeline. Schema.org JSON-LD recipe drafts can be extracted from pasted markup or one public HTTPS page. URL imports require public DNS answers, pin resolved addresses for the TLS request, disable proxies and redirects, and limit the response to 512 KiB over 10 seconds. A fetched draft requires explicit owner review and saving; scripts, linked assets and images are not fetched. Sites that block automated requests, redirect, or omit schema.org Recipe markup cannot be imported this way. PDF and image extraction, persistent history/version restore, micronutrient inference and automatic Health meal-plan synchronization remain unimplemented. A first-party recipe with existing nutrition may be submitted as an explicitly unconfirmed Health meal proposal. Health only records consumption after separate owner confirmation. Until migration is explicitly approved, RecipeSage remains available and its data untouched. Do not import or remove its data based on an assumption that the original database is empty.
