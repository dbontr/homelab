//! Read-only inspection of unresolved privileged attempts from a trusted host console.
use std::path::PathBuf;

use homelab_executor_authority::audit::AuditJournal;
use homelab_executor_authority::policy::absolute;
use serde_json::json;

fn main() -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    if unsafe { libc::geteuid() } != 0 {
        return Err("audit inspection requires a trusted local root terminal".into());
    }
    let command: Vec<String> = std::env::args().skip(1).collect();
    if command.len() != 1 || command[0] != "unresolved" {
        return Err("usage: audit-inspect unresolved".into());
    }
    let state: PathBuf = absolute("HOMELAB_EXECUTOR_RELEASE_STATE")?;
    let audit = AuditJournal::open(&state.join("executor-actions.jsonl"))?;
    let incomplete = audit.unresolved()?;
    println!("{}", serde_json::to_string(&json!({
        "unresolved": incomplete,
        "count": incomplete.len(),
    }))?);
    Ok(())
}
