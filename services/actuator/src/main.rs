use std::collections::HashSet;
use std::env;
use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::path::{Component, Path};
use std::process::{Command, Stdio};

const POLICY_MAX_BYTES: usize = 64 * 1024;
const SENSITIVE_SERVICES: [&str; 25] = [
    "network", "firewall", "router", "wireguard", "vpn", "dns",
    "nftables", "iptables", "nginx", "cloudflared", "pihole",
    "tailscale", "systemd", "docker", "k3s", "kube", "jellyfin",
    "moonfin", "ssh", "security", "executor", "fleet", "prometheus",
    "loki", "alertmanager",
];

#[derive(Debug)]
struct Policy {
    services: HashSet<String>,
    deployments: HashSet<String>,
    max_replicas: u32,
}

#[derive(Debug, PartialEq, Eq)]
enum Action {
    Service { verb: String, unit: String },
    Restart { namespace: String, deployment: String },
    Scale { namespace: String, deployment: String, replicas: u32, version: Option<String> },
}

fn valid_service(value: &str) -> bool {
    if value.is_empty() || value.len() > 128 || !value.ends_with(".service") {
        return false;
    }
    if !value.bytes().all(|b| b.is_ascii_alphanumeric() || b"_.@-".contains(&b)) {
        return false;
    }
    let lower = value.to_ascii_lowercase();
    !SENSITIVE_SERVICES.iter().any(|name| lower.contains(name))
}

fn valid_kube_name(value: &str) -> bool {
    if value.is_empty() || value.len() > 63 {
        return false;
    }
    let bytes = value.as_bytes();
    if !bytes[0].is_ascii_lowercase() && !bytes[0].is_ascii_digit() {
        return false;
    }
    if !bytes[bytes.len() - 1].is_ascii_lowercase()
        && !bytes[bytes.len() - 1].is_ascii_digit()
    {
        return false;
    }
    bytes
        .iter()
        .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || *b == b'-')
}

fn read_policy(path: &str) -> Result<String, &'static str> {
    let candidate = Path::new(path);
    if !candidate.is_absolute()
        || !path.starts_with("/nix/store/")
        || candidate.components().any(|component| {
            matches!(component, Component::ParentDir | Component::CurDir)
        })
    {
        return Err("policy must be an immutable Nix store file");
    }
    let meta = fs::symlink_metadata(candidate).map_err(|_| "policy file unavailable")?;
    if !meta.is_file() || meta.file_type().is_symlink() || meta.permissions().mode() & 0o222 != 0 {
        return Err("policy file must be immutable");
    }
    if meta.len() > POLICY_MAX_BYTES as u64 {
        return Err("policy file exceeds size limit");
    }
    let data = fs::read(candidate).map_err(|_| "policy file unreadable")?;
    if data.len() > POLICY_MAX_BYTES {
        return Err("policy file exceeds size limit");
    }
    String::from_utf8(data).map_err(|_| "policy file is not UTF-8")
}

fn read_allowlist(data: &str, service: bool) -> Result<HashSet<String>, &'static str> {
    let mut names = HashSet::new();
    for line in data.lines() {
        if line.is_empty() {
            continue;
        }
        if line != line.trim() || names.len() >= 8192 {
            return Err("invalid resource policy");
        }
        let valid = if service {
            valid_service(line)
        } else {
            match line.split_once('/') {
                Some((ns, dep)) => valid_kube_name(ns) && valid_kube_name(dep),
                None => false,
            }
        };
        if !valid || !names.insert(line.to_owned()) {
            return Err("invalid or repeated resource policy entry");
        }
    }
    Ok(names)
}

impl Policy {
    fn load() -> Result<Self, &'static str> {
        let services = read_allowlist(
            &read_policy(&env::var("HOMELAB_ACTUATOR_SERVICES").map_err(|_| "service policy missing")?)?,
            true,
        )?;
        let deployments = read_allowlist(
            &read_policy(&env::var("HOMELAB_ACTUATOR_KUBERNETES").map_err(|_| "Kubernetes policy missing")?)?,
            false,
        )?;
        let max_replicas = env::var("HOMELAB_ACTUATOR_MAX_REPLICAS")
            .map_err(|_| "replica limit missing")?
            .parse::<u32>()
            .map_err(|_| "invalid replica limit")?;
        if max_replicas == 0 || max_replicas > 1000 {
            return Err("replica limit outside policy");
        }
        Ok(Self { services, deployments, max_replicas })
    }
}

fn admit(args: &[String], policy: &Policy) -> Result<Action, &'static str> {
    if args.len() == 3 && args[0] == "service" {
        let verb = &args[1];
        let unit = &args[2];
        if !matches!(verb.as_str(), "start" | "stop" | "reload" | "restart")
            || !valid_service(unit)
            || !policy.services.contains(unit)
        {
            return Err("service action outside policy");
        }
        return Ok(Action::Service { verb: verb.clone(), unit: unit.clone() });
    }
    if args.len() == 4 && args[0] == "deployment" && args[1] == "restart" {
        let namespace = &args[2];
        let deployment = &args[3];
        if !valid_kube_name(namespace)
            || !valid_kube_name(deployment)
            || !policy.deployments.contains(&format!("{namespace}/{deployment}"))
        {
            return Err("Kubernetes resource outside policy");
        }
        return Ok(Action::Restart {
            namespace: namespace.clone(), deployment: deployment.clone()
        });
    }
    if (args.len() == 5 || args.len() == 6) && args[0] == "deployment" && args[1] == "scale" {
        let namespace = &args[2];
        let deployment = &args[3];
        let raw = &args[4];
        if !valid_kube_name(namespace)
            || !valid_kube_name(deployment)
            || !policy.deployments.contains(&format!("{namespace}/{deployment}"))
            || raw.is_empty()
            || !raw.bytes().all(|b| b.is_ascii_digit())
        {
            return Err("Kubernetes scale action outside policy");
        }
        let replicas = raw.parse::<u32>().map_err(|_| "invalid replica count")?;
        if replicas > policy.max_replicas {
            return Err("replica count outside policy");
        }
        let version = if args.len() == 6 {
            let value = &args[5];
            if value.is_empty() || value.len() > 128
                || !value.as_bytes()[0].is_ascii_alphanumeric()
                || !value.bytes().all(|b| b.is_ascii_alphanumeric()
                    || matches!(b, b'.' | b'_' | b'-'))
            {
                return Err("invalid Kubernetes version precondition");
            }
            Some(value.clone())
        } else {
            None
        };
        return Ok(Action::Scale {
            namespace: namespace.clone(), deployment: deployment.clone(), replicas, version
        });
    }
    Err("unrecognized privileged action")
}

fn checked_binary(key: &str) -> Result<String, &'static str> {
    let path = env::var(key).map_err(|_| "fixed administrative binary missing")?;
    if !Path::new(&path).is_absolute() || path.contains('\0') {
        return Err("administrative binary path must be absolute");
    }
    Ok(path)
}

fn invoke(action: Action) -> Result<(), &'static str> {
    let mut command = match action {
        Action::Service { verb, unit } => {
            let mut command = Command::new(checked_binary("HOMELAB_ACTUATOR_SYSTEMCTL")?);
            command.arg(verb).arg(unit);
            command
        }
        Action::Restart { namespace, deployment } => {
            let kubeconfig = checked_binary("HOMELAB_ACTUATOR_KUBECONFIG")?;
            let mut command = Command::new(checked_binary("HOMELAB_ACTUATOR_KUBECTL")?);
            command.args([
                format!("--kubeconfig={kubeconfig}"),
                format!("--namespace={namespace}"),
                "--request-timeout=15s".to_owned(),
                "rollout".to_owned(),
                "restart".to_owned(),
                format!("deployment/{deployment}"),
            ]);
            command
        }
        Action::Scale { namespace, deployment, replicas, version } => {
            let kubeconfig = checked_binary("HOMELAB_ACTUATOR_KUBECONFIG")?;
            let mut command = Command::new(checked_binary("HOMELAB_ACTUATOR_KUBECTL")?);
            command.args([
                format!("--kubeconfig={kubeconfig}"),
                format!("--namespace={namespace}"),
                "--request-timeout=15s".to_owned(),
                "scale".to_owned(),
                format!("deployment/{deployment}"),
                format!("--replicas={replicas}"),
            ]);
            if let Some(expected_version) = version {
                command.arg(format!("--resource-version={expected_version}"));
            }
            command
        }
    };
    command.env_clear();
    command.env("LANG", "C");
    command.env("PATH", "/run/current-system/sw/bin:/usr/bin:/bin");
    command.env("HOME", "/var/empty");
    command.stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::null());
    let status = command.status().map_err(|_| "administrative command unavailable")?;
    if !status.success() {
        return Err("administrative command failed");
    }
    Ok(())
}

fn main() {
    let result = (|| -> Result<(), &'static str> {
        let policy = Policy::load()?;
        let action = admit(&env::args().skip(1).collect::<Vec<_>>(), &policy)?;
        invoke(action)
    })();
    if let Err(message) = result {
        eprintln!("{message}");
        std::process::exit(1);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn policy() -> Policy {
        Policy {
            services: ["homelab-platform.service"].into_iter().map(str::to_owned).collect(),
            deployments: ["apps/workspace"].into_iter().map(str::to_owned).collect(),
            max_replicas: 4,
        }
    }

    fn request(args: &[&str], policy: &Policy) -> Result<Action, &'static str> {
        admit(&args.iter().map(|s| (*s).to_owned()).collect::<Vec<_>>(), policy)
    }

    #[test]
    fn only_exact_service_actions_are_permitted() {
        let p = policy();
        assert!(matches!(request(&["service", "restart", "homelab-platform.service"], &p),
            Ok(Action::Service { .. })));
        assert!(request(&["service", "restart", "nginx.service"], &p).is_err());
        assert!(request(&["service", "--force", "homelab-platform.service"], &p).is_err());
        assert!(request(&["service", "restart", "--system"], &p).is_err());
        assert!(request(&["service", "restart", "homelab-platform.service", "extra"], &p).is_err());
        assert!(!valid_service("homelab-securityd.service"));
        assert!(!valid_service("homelab-executor.service"));
    }

    #[test]
    fn kubernetes_scope_and_replica_bounds_are_exact() {
        let p = policy();
        assert!(matches!(request(&["deployment", "restart", "apps", "workspace"], &p),
            Ok(Action::Restart { .. })));
        assert!(matches!(request(&["deployment", "scale", "apps", "workspace", "0"], &p),
            Ok(Action::Scale { replicas: 0, .. })));
        assert!(request(&["deployment", "scale", "apps", "workspace", "5"], &p).is_err());
        assert!(request(&["deployment", "scale", "apps", "workspace", "-1"], &p).is_err());
        assert!(request(&["deployment", "scale", "apps", "workspace", "1.5"], &p).is_err());
        assert!(request(&["deployment", "scale", "kube-system", "workspace", "1"], &p).is_err());
        assert!(request(&["deployment", "scale", "apps", "unknown", "1"], &p).is_err());
        assert!(request(&["deployment", "scale", "apps", "workspace", "2", "other/unsafe"], &p).is_err());
        assert!(matches!(
            request(&["deployment", "scale", "apps", "workspace", "2", "v_123"], &p),
            Ok(Action::Scale { version: Some(_), .. })
        ));
        assert!(request(&["deployment", "scale", "apps", "workspace", "2", "--force"], &p).is_err());
        assert!(request(&["deployment", "scale", "apps", "workspace", "2", "1/2"], &p).is_err());
        assert!(request(&["deployment", "scale", "apps", "workspace", "2", ""], &p).is_err());
        assert!(request(&["deployment", "scale", "apps", "workspace", "2", "v1", "extra"], &p).is_err());
    }

    #[test]
    fn allowlist_validation_denies_duplicates_and_unsafe_names() {
        assert!(read_allowlist("homelab-platform.service\n", true).is_ok());
        assert!(read_allowlist("homelab-platform.service\nhomelab-platform.service\n", true).is_err());
        assert!(read_allowlist("nginx.service\n", true).is_err());
        assert!(read_allowlist("homelab-platform.service \n", true).is_err());
        assert!(read_allowlist("apps/workspace\n", false).is_ok());
        assert!(read_allowlist("apps/workspace/other\n", false).is_err());
    }

    #[test]
    fn policy_paths_cannot_escape_store_using_parent_components() {
        assert!(read_policy("/nix/store/../etc/passwd").is_err());
        assert!(read_policy("/nix/store/../../tmp/outside").is_err());
        assert!(read_policy("/tmp/non-store-policy").is_err());
    }

    #[test]
    fn no_generic_shell_or_unrecognized_actions() {
        let p = policy();
        assert!(request(&["sh", "-c", "id"], &p).is_err());
        assert!(request(&["service", "reload", "homelab-platform.service"], &p).is_ok());
        assert!(request(&["deployment", "delete", "apps", "workspace"], &p).is_err());
    }
}
