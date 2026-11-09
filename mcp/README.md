# Unified MCP interface

`homelab-mcp` is the optional, separately executable MCP entry point. It routes authenticated `POST /mcp` requests to the private service runtime, which retains one canonical tool catalog and delegates to independently operating services. It owns no domain database, scheduler or background worker.

This process is disabled unless `services.homelabInterfaces.mcp.enable` is set in the NixOS configuration. Give it a distinct client-facing bearer token and an MCP-only service credential that the domain runtime accepts only for `POST /mcp`. The listener binds only to a numeric loopback address; an authenticated ingress may publish the approved route. Browser Origins are denied unless explicitly allowlisted.

Stopping this listener does not stop services, the Rust console, or the optional native API. A missing backend returns a service-unavailable response; no local substitute state is created.
