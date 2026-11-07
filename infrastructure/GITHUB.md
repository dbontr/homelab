# Private GitHub publication

The pinned NixOS configuration is in the private [dbontr/homelab](https://github.com/dbontr/homelab) repository on `main`. Project Knowledge is migrated to approved GitHub repositories only after reconciliation and restore verification. Saturn authenticates with a dedicated write-enabled deploy key that is held outside the worktree.

## Publication contract

`saturn-github-sync.timer` starts `saturn-github-sync.service` daily with a randomized delay. The service pushes **already committed** `main` history. It does not stage files, create commits, force-push, resolve diverged histories or back up databases, media and credentials.

After an intentional local edit:

1. Inspect `git -C /etc/nixos status` and the staged diff for secrets and unrelated changes.
2. Run relevant validation and make a scoped commit.
3. Use `systemctl start saturn-github-sync.service` if an immediate push is needed.
4. Compare `git -C /etc/nixos rev-parse HEAD` against `git -C /etc/nixos ls-remote origin refs/heads/main` and verify GitHub readback.

When a change is made directly through GitHub, update a **clean** Saturn worktree with `git -C /etc/nixos pull --ff-only origin main` before further local commits. Never use a forced push to hide divergence.

Revoke the deploy key in repository settings if Saturn's key is compromised. Recover authorized GitHub access through a separate authenticated administrative route. Git-based configuration recovery must be paired with the [independent data protection plan](docs/plans/Selective%20Recovery.md).
