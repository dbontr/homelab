//! Independent owner authority: no local HTTP ingress is exposed over TCP.
use std::ffi::CString;
use std::path::Path;

use homelab_security_authority::bootstrap;
use homelab_security_authority::remote::Remote;
use homelab_security_authority::transport::{self, Shared};

fn group_id(name: &str) -> Result<u32, &'static str> {
    let value = CString::new(name).map_err(|_| "invalid security group")?;
    let entry = unsafe { libc::getgrnam(value.as_ptr()) };
    if entry.is_null() {
        return Err("security socket group does not exist");
    }
    let gid = unsafe { (*entry).gr_gid };
    if gid == 0 {
        return Err("security socket group must not be root");
    }
    Ok(gid)
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let settings = bootstrap::initialize(Path::new("/var/lib/homelab-securityd"))?;
    let remote = Remote::from_environment()?;
    let browser_enabled = settings.browser_origin.is_some();
    let shared = Shared::new(settings.authority, settings.browser_origin)?;

    let client_gid = group_id("homelab-security-client")?;
    let mut listeners = tokio::task::JoinSet::new();
    {
        let shared = shared.clone();
        listeners.spawn(async move {
            transport::serve_unix(
                Path::new("/run/homelab-securityd/owner.sock"), 0o600, None,
                transport::owner_router(shared)
            ).await
        });
    }
    {
        let shared = shared.clone();
        listeners.spawn(async move {
            transport::serve_unix(
                Path::new("/run/homelab-securityd/client.sock"), 0o660, Some(client_gid),
                transport::client_router(shared)
            ).await
        });
    }
    if browser_enabled {
        let browser_gid = group_id("homelab-security-browser")?;
        let shared_browser = shared.clone();
        listeners.spawn(async move {
            transport::serve_unix(
                Path::new("/run/homelab-security-browser/browser.sock"), 0o660,
                Some(browser_gid), transport::browser_router(shared_browser)
            ).await
        });
    }
    if let Some(remote) = remote {
        let shared = shared.clone();
        listeners.spawn(async move { remote.serve(shared).await });
    }

    #[cfg(unix)]
    let mut termination = tokio::signal::unix::signal(
        tokio::signal::unix::SignalKind::terminate()
    )?;

    tokio::select! {
        next = listeners.join_next() => {
            listeners.abort_all();
            match next {
                Some(Ok(Ok(()))) => Err("security listener stopped unexpectedly".into()),
                Some(Ok(Err(e))) => Err(e),
                Some(Err(e)) => Err(Box::new(e)),
                None => Err("security service has no listeners".into())
            }
        }
        _ = tokio::signal::ctrl_c() => {
            listeners.abort_all();
            Ok(())
        }
        _ = termination.recv() => {
            listeners.abort_all();
            Ok(())
        }
    }
}
