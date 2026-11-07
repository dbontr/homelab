# Moonfin media and requests

Moonfin is served by the Moonbase plugin on the existing Jellyfin container.
The media endpoint is `https://media.tail305103.ts.net/`, using Jellyfin
accounts. Do not add a Cloudflare Access challenge for guests.

## Seerr

Seerr runs in the same private Docker network as Jellyfin, at
`http://saturn-seerr:5055` from inside the Jellyfin container. The Moonbase
server-side proxy uses this address; native Moonfin clients do not need to
contact Seerr directly.

Moonbase's `SeerrEnabled` and `SeerrUrl` fields can be set with
[`configure_moonbase.sh`](./configure_moonbase.sh), which preserves all other
fields, including existing secrets. Stop Jellyfin before applying a plugin
configuration change, then start it again. The script is idempotent and refuses
to overwrite a different Seerr URL.

**Seerr must first be initialized** through its setup page by an authorized
Jellyfin administrator, who connects the Jellyfin server and imports/grants
access to intended users. The Seerr service is bound to Saturn's loopback
port 5055; access its setup page via an authenticated local/SSH-forwarded
session, not a new public guest endpoint. Moonbase's proxy being enabled
does not complete Seerr initialization.

Once Seerr is initialized, each Moonfin user opens
**Settings > Integrations > Seerr**, enables the integration, and signs in
using their Jellyfin account. Request buttons and Seerr discovery rows only
work for users who exist in Seerr with appropriate permissions.

## Media storage and integrity

The Kingston USB source is mounted read-only at `/mnt/media-usb`, with
checksum-verified copies on Saturn's NVMe at
`/srv/homelab/media-stage/{Music,Games}`. Those copies and the original
USB must be preserved until the mirrored ZFS destination is independently
accepted. The permanent Jellyfin paths are
`/srv/homelab/data/media/music` and
`/srv/homelab/data/media/Games`, available read-only inside the container
as `/media/music` and `/media/Games`.

The 8 TB mirror uses JMicron JMS578 USB-SATA adapters. The Saturn NixOS
storage module sets `usb-storage.quirks=152d:0578:u`, preventing their
problematic UAS transport. Verify this argument in `/proc/cmdline`,
confirm that all four adapters bind `usb-storage`, and inspect both ZFS
device counters and kernel logs after each reboot. A bounded write/read
check does not establish sustained hardware health; a clean completed
scrub and continuing absence of I/O resets are required. Never offline a
mirror member or clear error counters to conceal a transport problem.

## Music and retro games

The Jellyfin Music library is defined at `/media/music` and the Games
Mixed Content library at `/media/Games`. These library definitions
do not prove the files have been indexed or are available to every user.
Verify with an authenticated Jellyfin administrator session, run a library
refresh when required, then test normal user permissions and Moonfin
client playback. A representative imported MP3 decodes with Jellyfin's
FFprobe; the complete library still requires indexing acceptance.

Moonbase's retro-game feature discovers ROM files from a selected
Jellyfin game library and supports a defined set of emulator cores; it
does not automatically make every file in `Games` playable. The
current imported collection contains 14 GameCube and 16 Wii game
archives, plus one macOS metadata sidecar. Moonfin's supported browser
retro emulators do **not** include GameCube or Wii. Preserve those files
for a separate compatible emulator, such as Dolphin, rather than
representing them as playable Moonfin titles. For Moonfin's own retro
games, add properly licensed ROMs for its supported consoles (for
example NES, SNES, Game Boy or GBA) under their system directories.
Validate controller input and per-user save states using a real client.

The separate `/games` mount does not create a Jellyfin library. Do not
edit Jellyfin's application database or reuse stored login credentials to
force library visibility. Library scanning and permissions belong to
its authenticated administration interface.

## Validation

```sh
nix shell nixpkgs#python3 -c python3 -m unittest -q test_configure_moonbase.sh
```

After configuring Moonbase, check the server's
`/Moonfin/Discovery` and `/Moonfin/Seerr/Status` paths, then test from an
authenticated Moonfin client. A working public discovery endpoint is not
proof of Seerr account authorization or playable game assets.
