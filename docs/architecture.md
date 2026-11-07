# Architecture

## One system

The repository describes one personal computing system. Its owner interacts with a single catalog of capabilities, operations and resources through a common Go API and MCP endpoint. Tasks, goals, projects, Git repositories, shared libraries, learning evidence, health records, computing resources and media may refer to each other using stable identifiers. A project is not defined by its Git repository.

The `system/` tree is the application's source of truth. It contains a primary Go module and independently buildable implementation modules. Module boundaries exist for reliability, toolchain constraints and distinct state ownership; they must not become separate assistant identities, duplicate records or inconsistent interfaces. Go operations remain usable without an AI client. A future native UI invokes those same operations.

Nix provides the root flake, host configuration, deployment definitions, credential loading and component activation. Saturn runs routing, the K3s control plane and persistent lightweight applications. Jupiter, Venus, Mercury and Janus execute authorized compute work. Specialized processes may be upgraded separately, but the user-facing interface remains one system. One process is not required for one product.

## Capabilities

The system implements personal planning, task reports, compute and Git workspaces, research libraries, adaptive learning, health and nutrition, media requests, and infrastructure observation. Quantitative finance is not active. Seerr owns movie and television requests; the assistant exposes those capabilities without duplicating Seerr's database.

A single capability registry and consistent authorization rules govern tool discovery and execution. Operations use native Go interfaces. An MCP transport is an adapter rather than the business-logic owner. The platform must distinguish a successful operation from one merely queued, accepted or unobserved.

## Hardware and privilege

The declared inventory is in `hardware.nix`, validated by the Go `system/devices` package and presented through the native API and MCP. An inventory entry is not proof of physical connectivity. The current Kria KV260 inventory entry permits SSH connection and inspection only and is not compute-eligible. A different physical device, or changed ownership and permissions, is represented by editing the inventory. The compute executor must enforce actual device selection and lease authorization independently of the gateway.

Go is the primary systems language. Rust may be introduced for owned device interfaces requiring precise hardware control, accelerator buffers or WebAssembly, with a stable native boundary and Nix packaging. An SSH-only FPGA does not require a Rust agent.

The main assistant process never owns deployment or unrestricted cluster credentials. A separately trusted executor must authorize sensitive operations at the actual enforcement boundary. External credentials and media files remain outside Git.

## Deployment

Source availability is not evidence of successful deployment. Builds, Nix evaluations, live authentication, state continuity, capability parity and rollback require verification before cutover. The active fleet and media remain unchanged until that acceptance succeeds.
