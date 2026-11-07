# VPN-confined download services

Saturn runs qBittorrent, Prowlarr, Radarr, Sonarr and Lidarr in the
`download-vpn` network namespace. Readarr is optional. The namespace has
a single IPv4 default route through `wg-download`; it has no IPv6
default route. No guest or downloader traffic may escape through Saturn's
ordinary upstream interface when the tunnel fails.

## DNS boundary

The namespace's `saturn-download-dns` service runs dnsmasq on
`127.0.0.53` and forwards to the configured Surfshark resolvers.
The NixOS declaration installs dedicated `/etc/netns/download-vpn/resolv.conf`
and `nsswitch.conf`. Each downloader systemd service bind-mounts those
files into its private mount namespace and masks both the host nscd and
systemd-resolved sockets. Ordinary libc lookups therefore use the
namespace-local resolver rather than delegating queries to a host-side
NSS cache.

The namespace alone is insufficient for this guarantee: host resolver
sockets remain reachable without the additional mount restrictions.
This isolation must be accepted against real running processes after
the relevant NixOS generation is activated.

## Safe acceptance sequence

1. Verify that the live NixOS generation contains this configuration.
   Do not modify the AT&T gateway or disturb the existing K3s network.
2. Execute `bash /etc/nixos/homelab/verify-download-vpn-isolation.sh`
   from an authorized Saturn console. It inspects live process network and
   mount namespaces, resolver files, masked sockets, and routes without
   changing any configuration or generating downloader traffic.
3. Independently observe a unique test DNS query originating from a
   downloader's network *and mount* namespaces. Confirm the query reaches
   the VPN resolver across `wg-download` and does not reach Saturn's
   ordinary WAN DNS path. A successful DNS answer alone is not proof
   against leakage.
4. In a guarded maintenance window, repeat the previously accepted
   tunnel-loss egress check and restart one downloader with the VPN
   namespace unavailable. Verify refusal to start or connect, followed
   by controlled route restoration and successful VPN-only egress.
5. Verify that live application access and existing download state
   survive normal service restarts. Do not remove media or application
   databases.

No acceptance result should be inferred merely from a green systemd
status or passing code test. A revoked or missing WireGuard credential
is not a reason to fall back to direct internet routing.
