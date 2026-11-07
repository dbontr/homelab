# Seerr and VPN-confined download services

Seerr stays on the private `saturn-media_default` Docker network. Its
container address is `172.19.0.3`; the private Docker gateway is
`172.19.0.1`. The network is pinned in the media Compose configuration.

Radarr, Sonarr, Prowlarr, Lidarr and qBittorrent run exclusively in the
`download-vpn` network namespace. Their outbound traffic uses Surfshark.
Seerr cannot reach that namespace directly. Socket-activated bridges
provide **only authenticated Radarr and Sonarr HTTP interfaces**:

| Service | Seerr hostname | Seerr port | VPN-namespace destination |
| --- | --- | --- | --- |
| Radarr | `172.19.0.1` | `17878` | `127.0.0.1:7878` |
| Sonarr | `172.19.0.1` | `18989` | `127.0.0.1:8989` |

The bridge listeners bind only to the private Docker gateway. NixOS
firewall input rules allow TCP to those ports only from the Seerr
container on a Docker bridge. The socket proxies join the VPN namespace
for their outbound connections to the Arr daemons. No ARR API is exposed
on Saturn's public-facing interfaces; no downloader gets a direct WAN
default route.

## Administrator setup

Seerr must be initialized through its normal Jellyfin administrator
login. This cannot be accomplished by a healthy container alone.
After signing in, configure one non-4K default Radarr and Sonarr server
using the private address and port above. Read each Arr application API
key from its protected Settings > General page; never commit it.
Select an available quality profile, a writable root folder, and enable
application scans. Do not enable automatic searches or automatic request
approval until the storage pool, download clients and indexers are
verified.

Radarr and Sonarr must each have:
- A root folder accessible from their VPN network namespace.
- A reachable qBittorrent download client inside the same namespace.
- Suitable indexers synchronized from Prowlarr.
- A valid quality profile and configured default Seerr target.

Prowlarr and qBittorrent are inside the same network namespace, so they
communicate over VPN-local loopback. Their provider accounts and indexers
must be configured through their own authenticated interfaces; the
network bridge does not create indexer credentials.

The native Prowlarr app-registration helper is
[`../prowlarr-register-apps.sh`](../../prowlarr-register-apps.sh); it is
idempotent and requires execution within the VPN namespace. Registration
of Radarr and Sonarr does not itself supply indexers. Configure only
legitimate providers for media the owner is entitled to acquire. Keep
qBittorrent Web UI authentication enabled and configure its download
client credentials through the normal authorized Arr settings pages;
do not bypass login to make an acceptance test pass.

An authenticated Seerr administrator session is required for initial
Jellyfin enrollment, importing user permissions, selecting default Arr
servers and enabling requests. The private server-side proxy and an
HTTP 401 from each Arr bridge are necessary connectivity checks but do
not substitute for initialization or an actual download-and-import test.

## Acceptance

Run `bash homelab/verify-seerr-arr-bridge.sh` on Saturn after activation.
Expected behavior: both bridge sockets are active; Seerr can connect to
both API endpoints and receives HTTP 401 without Arr credentials.
An HTTP 401 here demonstrates **reachable, authenticated APIs**, not
an end-to-end working request.

A real authenticated test must request a legal test movie/series and
trace Seerr to the default Arr instance, download client, indexer,
completed media file, Jellyfin indexing and Moonfin playback. Do not
start downloads into `tank_media` while the pool reports blocked writes
or USB transport errors. Keep existing media and the original USB intact.
