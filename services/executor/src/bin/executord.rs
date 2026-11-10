//! Privileged host executor; neither the MCP gateway nor an agent owns these credentials.
use std::path::Path;
use homelab_executor_authority::executor::Executor;
use homelab_executor_authority::transport::{self, Remote};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    if unsafe { libc::geteuid() } != 0 {
        return Err("privileged executor requires a separate root-owned service".into());
    }
    let executor = Executor::initialize()?;
    let remote = Remote::from_environment()?;
    let client_group = transport::client_group()?;
    let mut listeners = tokio::task::JoinSet::new();
    let local_executor = executor.clone();
    listeners.spawn(async move {
        transport::serve_unix(local_executor,
            Path::new("/run/homelab-executord/execute.sock"), client_group).await
    });
    if let Some(remote) = remote {
        listeners.spawn(async move { remote.serve(executor).await });
    }
    let mut termination = tokio::signal::unix::signal(
        tokio::signal::unix::SignalKind::terminate())?;
    tokio::select! {
        next = listeners.join_next() => {
            listeners.abort_all();
            match next {
                Some(Ok(Err(error))) => Err(error),
                Some(Ok(Ok(()))) => Err("privileged listener stopped unexpectedly".into()),
                Some(Err(error)) => Err(Box::new(error)),
                None => Err("executor has no listeners".into()),
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
