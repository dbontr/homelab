# Homelab browser console

The first-party browser client uses React, TypeScript, Tailwind CSS and shadcn/ui components. It consumes the Go platform's existing same-origin `/v1/*` resources and `/ui/session` endpoint. It does not contain an administrator token, MCP bearer credential, independent password login or privileged operation handler.

## Build

Install a supported Node.js release and the exact dependency graph recorded in `package-lock.json`. Run `npm ci`, `npm run typecheck` and `npm run build`; the static bundle is placed in `dist/`.

The npm lockfile is source-controlled and determines the immutable dependency graph. The Nix package `.#homelab-console-web` builds the bundle with `importNpmLock` from the exact locked npm packages. An independently reviewed source revision is required for release. Never use `npm install --force` or suppress TypeScript errors to qualify a release.

During development, `npm run dev` starts Vite on loopback. Access the Go API through the same protected origin in production. Local development proxying must not forward long-lived credentials into an untrusted browser.

## Authentication

The Go platform verifies the signed Cloudflare Access assertion, exact issuer, application audience, expiration, and owner identity. `GET /ui/session` returns authenticated status and, when allowed, an assertion-bound CSRF token. The UI never collects passwords or TOTP secrets. Mutations include `X-CSRF-Token`; the server still enforces protected Origin, content type, owner identity, and per-operation authorization. A browser session never implies infrastructure administrator permission.

## Scope

The console provides accessible navigation, live data views, research projects and workspaces, engineering verification runs, monitoring, and read-only views into personal and service resources. It does not manufacture data when an upstream service is unavailable. Canonical records and computations remain owned by the existing Go service interfaces.

Additional editing and specialized app interactions require independent feature-by-feature acceptance against the Go APIs before retiring the corresponding deployed Dioxus views. The previous web client is not removed until functional parity and a tested rollback are demonstrated.

## Release

Releases use an immutable static bundle packaged by Nix from the exact committed dependency lockfile. The Go HTTP gateway and Cloudflare Access configuration remain the same. The previous browser package and homepage remain available until an owner-approved, health-checked replacement is verified.
