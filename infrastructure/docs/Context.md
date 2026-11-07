# System context

This repository defines desired infrastructure, not a continuously verified runtime inventory.

Saturn is the NixOS router, application host and K3s server. Jupiter, Venus, Mercury and Janus are K3s compute workers. The pinned flake defines those five hosts, and the operational domain modules own service enablement; Homepage and Compose integration are shared facilities. Scheduled backups are disabled until the recovery policy is approved. Infrastructure configuration is Nix and operational scripts are Bash.

The homelab repository owns infrastructure and deployment integration only. The independently released [Project Workspace MCP](https://github.com/dbontr/workspace-mcp), [Adaptive Teaching MCP](https://github.com/dbontr/adaptive-teaching-mcp), and [Personal Health MCP](https://github.com/dbontr/personal-health-mcp) own their application implementations. Their deployment is governed by the approved release, migration and recovery contract.

For current operations, verify running service versions, credentials, cluster readiness, backup policy and restoration against the actual hosts. Historical deployment observations remain in Git history, not in this design file.

Project Knowledge remains under its existing owner until the GitHub migration is reconciled and its independent restore has passed. See [selected recovery](plans/Selective%20Recovery.md).
