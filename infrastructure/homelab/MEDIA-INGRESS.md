# Moonfin media ingress

Use `https://media.tail305103.ts.net` for every Jellyfin/Moonfin client.

- A downstream client using Saturn DNS resolves the hostname to `10.77.20.1`, where nginx terminates HTTPS and forwards to Jellyfin on loopback.
- A Tailscale client with MagicDNS and an active private route resolves the hostname to the media node's tailnet address. Direct connections depend on NAT traversal; Tailscale relay fallback can reduce throughput.
- An ordinary external client reaches the public Tailscale Funnel endpoint. Transcode bandwidth-intensive playback for this route.

Saturn remains a self-contained downstream router. No incoming WAN port forwarding, upstream DNS changes, VPS, or upstream-router configuration is required.

## Recovery

The NixOS modules are `media-tailnet.nix` and `media-local.nix`. The backup job saves `tailscale-media/tailscaled.state` as a restricted archive, and the off-host recovery job encrypts completed backups. This state contains credentials and the media device's Serve/Funnel settings. Never store it in Git.

Restore the identity before starting `saturn-media-tailnet.service`; keep the old identity offline to avoid simultaneous use on two hosts. Restore the archive under `/var/lib` as root, protect the state file with mode `0600`, and start the tailnet and local ingress services. HTTPS certificates can be reissued through the authorized Tailscale identity. Without the archive, enroll a media node and configure Funnel/Serve again.

## Checks

- `host media.tail305103.ts.net 10.77.20.1` should return `10.77.20.1`.
- `curl --resolve media.tail305103.ts.net:443:10.77.20.1 https://media.tail305103.ts.net/System/Info/Public` tests local HTTPS.
- Check `saturn-media-local`, `saturn-media-tailnet`, `dnsmasq`, and `saturn-media-local-cert.timer` through systemd.
- Test a non-Tailscale remote client separately to verify Funnel; local tests cannot establish real-world remote throughput.
