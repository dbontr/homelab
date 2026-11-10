//! Root-only, out-of-band repair of an interrupted release transaction.
use std::sync::Arc;
use homelab_executor_authority::audit::StateLease;
use homelab_executor_authority::policy::Policy;
use homelab_executor_authority::release::ReleaseController;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    if unsafe { libc::geteuid() } != 0 {
        return Err("recovery requires a trusted local root terminal".into());
    }
    let requested: Vec<String> = std::env::args().skip(1).collect();
    if requested.len() != 1 || requested[0] != "recover" {
        return Err("expected exact local recovery operation".into());
    }
    let policy = Arc::new(Policy::load()?);
    if !policy.release_enabled {
        return Err("release recovery disabled by host policy".into());
    }
    // An active Go or Rust executor holds this lock for its entire lifetime.
    let _exclusive_owner = StateLease::acquire(&policy.state_dir)?;
    let controller = ReleaseController::new(policy);
    controller.recover().await?;
    println!("previous verified NixOS generation restored");
    Ok(())
}
