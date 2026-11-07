# Architecture decisions

| Area | Decision | Rationale |
| --- | --- | --- |
| Flake | One pinned NixOS flake covers Saturn and four workers. | Coherent reproducibility and recovery |
| Operational domains | Network & Security; Diagnostics & Observations; Downloads & Media; Learn Stack; Workspace MCP; Personal Health Stack. | Separate service and data ownership boundaries |
| Shared services | Homepage and common Compose integration are shared; recovery has an explicit domain. | Avoid duplicated privilege and implicit service ownership |
| Infrastructure implementation | Declarative NixOS modules and Bash maintenance scripts; no Python custom infrastructure daemons. | Native services and minimal owned runtime |
| Scheduling | Kubernetes controls project admission, isolation, resources and device allocation. | No competing Docker compute scheduler |
| MCPs | Learning, compute and personal health have separate upstream Go services, identities, storage and deployment definitions. | Isolated application ownership |
| Network | Preserve the existing VLANs, firewall, Access policies and fail-closed download VPN. | Maintain traffic isolation |
| Git and knowledge | Git remote is authoritative for project source; project knowledge persistence is optional. | Explicit and recoverable source ownership |
| Persistent state | Preserve RecipeSage PostgreSQL, Adaptive Teaching SQLite, Project Workspace SQLite and artifacts. | Databases cannot be rebuilt from Git alone |
| Backups | Automatic backup schedules are disabled pending a defined retention and recovery policy. Preserve current databases, cluster identity and artifacts during the policy review. | Avoid unbounded backup growth without discarding unique state |
| Deployment | Evaluate and review changes before guarded physical activation. | Protect routing, ingress and rollback |
