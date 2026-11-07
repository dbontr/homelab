# Homelab application access

The owner portal and administration are protected by Cloudflare Access.
Guest services use independent application credentials. Public endpoints are
not authorization grants and must never expose private monitoring or projects.

## Hostnames and authorization

| Hostname | Audience | Transport | Required authorization |
| --- | --- | --- | --- |
| `dbontr.me` | Owner | Cloudflare Tunnel | Owner-only Cloudflare Access |
| `monitor.dbontr.me` | Owner | Cloudflare Tunnel | Owner-only Cloudflare Access and Grafana authentication |
| `mcp.dbontr.me` | MCP gateway | Cloudflare Tunnel | Owner-only Cloudflare Access; independent upstream authorization |
| `recipes.dbontr.me` | Approved recipe users | Cloudflare Tunnel | Cloudflare Access and RecipeSage account authentication |
| `media.tail305103.ts.net` | Approved media users | Local split DNS, private tailnet, or Tailscale Funnel | Separate Jellyfin accounts and applicable tailnet ACLs |

`media.dbontr.me` is not a media ingress route. A public DNS record left
behind does not make the service available. The supported client server URL is
`https://media.tail305103.ts.net`.

Cloudflare Access applications are scoped to exact owner-only hostnames. The RecipeSage application uses its own Access policy, and no bypass is
allowed on owner-only services. The Cloudflare Tunnel validates Access
audience IDs on protected origins, including RecipeSage. The tunnel's default unmatched route is HTTP 404.

Cloudflare Access is not a substitute for a service's internal authorization.
Grafana, Jellyfin, RecipeSage and the project broker must enforce
their own account or role boundaries.

## MCP client endpoints

The Cloudflare Access-protected `mcp.dbontr.me` origin hosts independent
services under stable, exact paths. The gateway applies the same owner-only
Access policy to every path. Individual MCP servers enforce their own
credentials or verified caller identity; gateway authentication is not a
universal authorization grant.

| Service | Endpoint | Origin |
| --- | --- | --- |
| Workspace MCP | `https://mcp.dbontr.me/workspace` | Kubernetes controller over verified TLS |
| Adaptive Teaching MCP | `https://mcp.dbontr.me/adaptive-teaching` | Local service on 127.0.0.1:8200 |
| Personal Health MCP | `https://mcp.dbontr.me/personal-health` | Local service on 127.0.0.1:8201 |

Personal Health provides health and recipe tools from one authenticated MCP service.
The service's `/healthz` probe is private to localhost. Unmatched and obsolete MCP paths return 404.

The gateway validates the Cloudflare Access audience before forwarding
requests. Workspace MCP uses the gateway's audience for its own Access JWT
verification. The controller's Kubernetes Secret retains a separate
administrator token. The origin's TLS certificate must be verified against
`/srv/mcp-trust/pwm-controller-ca.pem`.

On a recovered Saturn host, restore the public controller trust certificate
before enabling the ingress. Recreate `/srv/mcp-trust/pwm-controller-ca.pem`
from the Kubernetes `pwm-controller-ca` Secret in `pwm-system`, grant Nginx
read-only access to the PEM and traversal of its parent, and verify the
certificate fingerprint against the running controller. Do not loosen the
permissions of `/srv/homelab` or its secrets.

Personal Health verifies the Cloudflare Access assertion at its own origin. The
private RecipeSage application is managed by `saturn-recipesage` inside the
Personal Health domain. RecipeSage API integration uses optional, protected
service credentials in `/srv/homelab/secrets/personal-health-mcp.env`.
The standalone RecipeSage MCP service and endpoint are not part of the
infrastructure. Never commit credentials or include their values in Nix configuration.

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
