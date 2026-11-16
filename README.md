# Homelab skill installation bundles

These five individual ZIPs are installable ChatGPT skills, assembled from `dbontr/homelab` at merged source commit `43b182fabad8adc1ca9bf3869d8caba5af5e8b77` (2026-11-16). Each archive has one named skill directory containing `SKILL.md`, `agents/openai.yaml`, its references, and any utilities. The bundles are independent of the running homelab generation.

Open the `bundles/<skill>/skill.zip` file in GitHub and choose Download raw file, then install each ZIP through ChatGPT's supported skill installer. The `development` and `continuity` instructions describe request-scoped owner approval and cross-conversation handoff. Installable instructions do not grant administrator permissions. The browser login remains independently protected by Cloudflare Access, password, and TOTP.

These are binary ZIP blobs preserved in this release-only branch. Do not merge this distribution branch into the application source branch.
