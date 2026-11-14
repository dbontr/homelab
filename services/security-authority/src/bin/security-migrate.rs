//! Offline custody of cutover snapshots and explicitly selected rollback receipts.
//!
//! Stop the current security daemon before invoking this program. The program
//! must run as the security state owner with the intended protected credential
//! and policy environment configured. It does not switch services or restore
//! archived authentication state.

use std::env;
use std::path::PathBuf;

use homelab_security_authority::migration;

fn run() -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let mut args = env::args().skip(1);
    if args.next().as_deref() != Some("prepare") || args.next().as_deref() != Some("--target") {
        return Err("usage: security-migrate prepare --target rust|go --state-dir ABSOLUTE_STATE_DIRECTORY --snapshot-dir ABSOLUTE_NEW_DIRECTORY".into());
    }
    let target = args.next().ok_or("authority selection target required")?;
    if args.next().as_deref() != Some("--state-dir") {
        return Err("state directory flag required".into());
    }
    let state = PathBuf::from(args.next().ok_or("state directory required")?);
    if args.next().as_deref() != Some("--snapshot-dir") {
        return Err("snapshot directory flag required".into());
    }
    let snapshot = PathBuf::from(args.next().ok_or("snapshot directory required")?);
    if args.next().is_some() {
        return Err("unexpected migration arguments".into());
    }
    migration::prepare(&state, &snapshot, &target)?;
    println!(
        "Validated offline state copy and cutover receipt: {}",
        snapshot.join("authority-cutover.json").display()
    );
    println!("Provide the receipt as a protected systemd credential in a separate, operator-approved NixOS generation. Preserve the current state directory during rollback.");
    Ok(())
}

fn main() {
    if let Err(error) = run() {
        eprintln!("security migration denied: {error}");
        std::process::exit(1);
    }
}
