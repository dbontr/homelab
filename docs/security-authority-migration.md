# Controlled security-authority migration

The owner security authority has one persistent state directory and one
exclusive writer. The Go and Rust daemons use compatible version-one
`authenticator.json` and encrypted `credentials.json` formats. They share
`/var/lib/homelab-securityd/.securityd.lock` and never run as concurrent
writers. The selected daemon holds that lease for its full lifetime.

Migration changes the **executable**, not the authentication or vault files.
An archived snapshot is evidence for inspection and disaster recovery; it is
not a safe rollback image. Restoring it after a credential rotation, revocation,
failed sign-in, or completed TOTP step could resurrect withdrawn credentials
or permit replay. Ordinary rollback retains the **current** persistent state.

## Release prerequisites

Before selecting Rust or switching back to Go:

1. Build both security implementations and their offline preflight binaries
   from reviewed, pinned sources. The Rust build requires an authentic,
   committed `Cargo.lock`. Do not bypass the NixOS lockfile assertion.
2. Verify the source and Go–Rust protocol contracts, TOTP replay preservation,
   encrypted-vault authentication and state-version parity in an isolated
   environment. These checks precede any production migration.
3. Provision the same owner identity, password verifier, TOTP secret, vault
   key, Access identity and operation policy to the selected implementation.
   The secret files remain protected systemd credentials, not Nix store paths.
4. Ensure an independently authorized operator can recover the previous
   service generation and access the protected host console. Stopping the
   active daemon terminates all in-memory owner sessions and confirmations.

This repository only supplies the mechanisms. It does not authorize an
operator to stop, select, deploy or restart a live authority.

## Prepare an independent state snapshot

Stop the current authority through the separately authorized host release
process. Confirm that the daemon has stopped. Prepare the exact intended
environment variables and protected file paths for the offline preflight.
The selected state owner invokes:

```sh
security-migrate prepare --target rust \
  --state-dir /var/lib/homelab-securityd \
  --snapshot-dir /path/to/private/unique-snapshot
```

The snapshot path must be absent, absolute, outside the live state tree, and
inside a trusted parent directory. The preparation command:

- Acquires the same nonblocking file lock used by both daemons. A live writer
  makes preparation fail; it is never stopped by the command.
- Reads owner-only, regular, single-link state files without following
  symbolic links. Requires a valid version-one authenticator record.
- Creates an owner-only independent directory and copies exact file bytes
  with independently owned inodes, `0600` file modes and synchronized writes.
- Runs the sibling `security-preflight` against the copy using the intended
  runtime environment. This checks password/TOTP configuration, policy, pinned
  Access keys, encrypted-vault authentication, credential grants and optional
  remote/OAuth configuration without binding sockets or modifying the copy.
- Emits `authority-cutover.json` **only after** preflight succeeds. It records
  the intended target, SHA-256 of exact authentication and vault bytes, a
  random nonce and the fingerprint of the current selection marker. Failed
  preparation removes its newly created incomplete snapshot.

For a Go rollback, use `--target go`, point
`HOMELAB_SECURITY_GO_PREFLIGHT` to an absolute, protected, reviewed
`homelab-security-preflight` executable, and prepare against the
**current Rust-written state**. Both Rust and Go preflights must accept the
same copy before a Go receipt is issued.

The security daemon never reads plaintext secrets from the snapshot. The
snapshot contains encrypted credentials and authentication replay metadata;
store it under protected custody and retain it only under an explicit
retention policy.

## Approve and select a release

An authorized host operator supplies the validated receipt as a protected
runtime file, then configures the **new** NixOS generation:

```nix
services.homelabSecurity.implementation = "rust";
services.homelabSecurity.migration.receiptFile =
  "/protected/operator-provisioned/authority-cutover.json";
```

The Nix module requires the receipt option for Rust and passes the file
through systemd `LoadCredential`, never by embedding its contents in the
Nix store. The service's executable selector remains the single managed
`homelab-securityd.service` unit; no second writer unit is installed.

After this generation has been separately approved and activated, the selected
daemon obtains the exclusive lease, validates the current state and protected
runtime configuration, verifies the receipt and **atomically publishes** an
owner-only `.authority-selection.json` marker, synchronized to disk,
*before* opening the owner, browser or remote listeners.

The marker identifies the selected implementation and fingerprint of its
receipt. A restart of that same implementation may use the same receipt as
authentication and vault state legitimately evolve, but must preserve a
valid replay record and any required encrypted vault. A transition to the
other implementation requires a newly prepared receipt that names the exact
current marker as its parent and matches the live state bytes at activation.
Old receipts cannot be replayed across an intervening selection.

## Roll back without restoring state

If Rust fails acceptance or must be rolled back, stop its managed unit under
operator control. While it is stopped, prepare a **new** Go receipt using the
latest state, with both offline preflights succeeding. Select the guarded
Go implementation in another explicitly approved NixOS generation:

```nix
services.homelabSecurity.implementation = "go";
services.homelabSecurity.migration.receiptFile =
  "/protected/operator-provisioned/go-rollback-receipt.json";
```

The Go daemon must hold the same exclusive lock and successfully parse the
current authentication state, decrypt every stored credential, validate
configuration, and accept the marker/receipt chain *before* any socket opens.
It then atomically records the Go selection. An older Go binary that does
not implement this gate is **not** an eligible rollback artifact.

If a state copy cannot be parsed by both implementations, normal rollback
must stop. Do not overwrite live files with the old snapshot, delete a
selection marker, reset the TOTP replay step, substitute a previous vault,
or make an unverified authority writable. Such cases require a separate,
documented recovery decision with evidence for credential revocations and
replay-state continuity.

## Non-negotiable invariants

The Nix system generation is the executable selector; the protected receipt
is the transition authorization input; the process-lifetime file lock
excludes concurrent writers; the persistent marker chains successive
selections. A failed lock, invalid receipt, incompatible state version,
damaged ciphertext, wrong credential or stale parent marker denies startup.

Successful source compilation or an independently verified state snapshot
does **not** establish authorization to deploy. Production selection requires
the full offline acceptance gate and a separate owner-authorized release.
