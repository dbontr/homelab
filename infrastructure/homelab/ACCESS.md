# Homelab application access

The owner portal and administration require Cloudflare Access. The tunnel and origin policies must be verified independently before release.
Guest services use independent application credentials. Public endpoints are
not authorization grants and must never expose private monitoring or projects.

## Hostnames and authorization

| Hostname | Audience | Transport | Required authorization |
| --- | --- | --- | --- |
| `dbontr.me` | Owner | Cloudflare Tunnel | Owner-only Cloudflare Access |
| `monitor.dbontr.me` | Owner | Cloudflare Tunnel | Owner-only Cloudflare Access and authenticated Grafana session |
| `mcp.dbontr.me` | MCP gateway | Cloudflare Tunnel | Owner-only Cloudflare Access; independent upstream authorization |
| `recipes.dbontr.me` | Approved recipe users | Cloudflare Tunnel | Cloudflare Access and RecipeSage account authentication |
| `media.tail305103.ts.net` | Approved media users | Local split DNS, private tailnet, or Tailscale Funnel | Separate Jellyfin accounts and applicable tailnet ACLs |

`media.dbontr.me` is not a media ingress route. A public DNS record left
behind does not make the service available. The supported client server URL is
`https://media.tail305103.ts.net`.

Cloudflare Access applications must be scoped to their intended hostnames and identities. The RecipeSage hostname requires a dedicated Access application with an appropriate allow policy and a matching audience in the tunnel configuration before it can be treated as available. Do not reuse the media application audience for recipes. The Cloudflare Tunnel must validate Access audience IDs on protected origins. Unmatched tunnel routes return HTTP 404. Verify the remote Access application and local tunnel audience together before activating or publishing an ingress route.

The Cloudflare zone requires TLS 1.2 or newer and redirects HTTP to HTTPS. Verify these edge settings against the live zone before each infrastructure release; NixOS does not manage them. DNSSEC should be enabled only with confirmed registrar delegation and a tested recovery procedure. A working tunnel or DNS record alone is not proof that an application is authenticated.

Cloudflare Access is not a substitute for a service's internal authorization.
Grafana, Jellyfin, RecipeSage and the project broker must enforce
their own account or role boundaries.

## MCP client endpoints

The owner-only Cloudflare Access application on `mcp.dbontr.me` provides
Managed OAuth to registered MCP clients. The authenticated origin forwards its
signed Access assertion to the unified MCP bridge. The bridge validates the
issuer, pinned signing key, exact gateway audience, owner email, issuance time
and expiry before forwarding the owner's assertion to the domain service.
The opaque OAuth access token is not a platform administrator credential.

| Service | Endpoint | Purpose |
| --- | --- | --- |
| Homelab | `https://mcp.dbontr.me/mcp` | Primary unified MCP interface for supported domain services |
| Compute | `https://mcp.dbontr.me/compute` | Direct controller integration, when separately authorized |
| Learning | `https://mcp.dbontr.me/learning` | Independent learning MCP |
| Personal Health | `https://mcp.dbontr.me/personal-health` | Independent health and recipe MCP |

The primary Homelab MCP retains a separate, protected bearer credential for
existing private clients. That credential does not grant an owner identity or
privileged execution authority. The gateway validates the OAuth-originated
signed identity independently; unverified or missing assertions cannot grant
MCP access. Privileged operations continue through the independent owner
security authority and executor, with their existing confirmation policy.

Cloudflare Managed OAuth dynamic client registration permits only the approved
ChatGPT callback URI patterns. The Access application maintains its owner-only
policy, and the tunnel verifies the expected application audience before
forwarding requests to the loopback Nginx listener. The Nginx ingress retains
the exact `/mcp` route and forwards the signed assertion unchanged. The MCP
interface accepts only the expected Cloudflare audience and owner identity;
the native API does not inherit this authorization.

A ChatGPT connection uses the primary Homelab URL and the OAuth connection
flow. After server deployment, reconnect or refresh the existing Homelab
plugin so ChatGPT discovers the current tools. Validate authenticated
initialization and tool discovery, deny forged or expired assertions,
and verify that anonymous requests still fail. Do not treat OAuth login
alone as proof of successful tool discovery or privileged authorization.

The controller's Kubernetes Secret retains its own administrator token.
The Compute TLS certificate remains verified against
`/srv/mcp-trust/pwm-controller-ca.pem`; do not loosen the permissions of
`/srv/homelab` or its secrets. The protected controller, learning, and health
services remain individually authorized behind their exact ingress paths.

## Client URLs

- Moonfin/Jellyfin: enter `https://media.tail305103.ts.net`. Local split DNS
  uses Saturn's service interface. Tailscale clients use the private tailnet;
  non-Tailscale clients can use the explicitly configured Tailscale Funnel.
  Real remote-device compatibility, ACL denial and bandwidth need testing.
- RecipeSage: open `https://recipes.dbontr.me/app/` and authenticate with
  RecipeSage. The page can be installed as a progressive web application.
- Owner monitoring: use its protected hostname. Do not add guest identities to this Access application.

## Verified boundary (2026-10-31 UTC)

After a controlled NixOS switch limited to Cloudflare Tunnel changes,
the external HTTP probes showed: RecipeSage HTTP 301 to `/app/`, owner
Homepage, monitoring HTTP 302 to Cloudflare Access,
and unauthenticated MCP HTTP 401. All five K3s nodes remained Ready. These
checks do not replace a signed owner-browser login, guest denial test, or
native Jellyfin playback test.

## Moonbase features and accounts

The Moonbase Jellyfin server plugin is required for synced settings, retro games, and in-client Seerr requests. After installation, enable the plugin's global sync and the client's Integrations > Moonbase Plugin setting. Seerr is a **separate service**: deploy and configure it for Jellyfin authentication, Radarr/Sonarr access, and per-user request permissions before enabling Moonbase's Seerr integration. The Seerr URL must be reachable from the Jellyfin container; no Seerr credential belongs on the client.

Retro games require the server-side Moonbase game configuration, authorized ROMs and corresponding compatible cores. The presence of a games dataset or plugin binary alone does not prove that a client has a playable games library. Verify from a non-admin Jellyfin account.

Create distinct Jellyfin accounts for individual users. Disable administrative privileges, restrict libraries per user, and enforce passwords. Do not share the administrator login with guests.

## Media requests

Seerr runs on Saturn as a private service. Its HTTP listener is bound to `127.0.0.1:5055`; the same container is reachable by Jellyfin on the shared media Docker network as `http://saturn-seerr:5055`. Do not expose Seerr to the public Internet without an Access policy and independent authorization review.

After initial Seerr administration, connect Jellyfin, import authorized Jellyfin users, configure the relevant Radarr/Sonarr instances, and grant request privileges per user. Moonbase's Seerr URL is the internal Docker address. Enable Seerr in Moonbase only after the setup and connection checks succeed; Moonbase proxies client requests through the Jellyfin server.

The Seerr container does not activate the downloader stack. Radarr, Sonarr and qBittorrent remain subject to the existing VPN-only deployment and kill-switch requirements. Requests cannot be considered fulfillment-capable until that pipeline and its policies pass acceptance.

## Validation

Before releasing a hostname, confirm its Access application's expected audience ID, identity-specific policy, login behavior, unauthorized denial, origin-local binding, and application-level access. Test native clients separately from browsers and verify that guests cannot navigate to monitoring, research, or MCP resources.
