# Unified MCP interface

`homelab-mcp` is the optional, separately executable MCP entry point. It routes authenticated `POST /mcp` requests to the private service runtime, which retains one canonical tool catalog and delegates to independently operating services. It owns no domain database, scheduler or background worker.

This process is disabled unless `services.homelabInterfaces.mcp.enable` is set in the NixOS configuration. Give it a distinct client-facing bearer token and an MCP-only service credential that the domain runtime accepts only for `POST /mcp`. The listener binds only to a numeric loopback address; an authenticated ingress may publish the approved route. Browser Origins are denied unless explicitly allowlisted.

Stopping this listener does not stop services, the Rust console, or the optional native API. A missing backend returns a service-unavailable response; no local substitute state is created.

## Acceptance

From the authorized operator host, run the read-only Streamable HTTP
acceptance probe against the local MCP listener with the protected
client-token file supplied to the service:

```sh
bash scripts/accept-unified-mcp.sh \
  http://127.0.0.1:8401/mcp /path/to/private/mcp-client-token task_add
```

The probe verifies that an anonymous initialize request receives HTTP 401,
then checks authenticated initialization, a unique tool catalog containing the
expected tool, and MCP ping. It does not execute domain tools or change
application data. Credentials are read from a local file and placed in a
restricted temporary curl header file, not process arguments; neither their
values nor raw MCP response bodies are printed.

This probe establishes only the local interface contract. Separately verify
that Cloudflare Access admits the owner at the public hostname, rejects
unauthorized identities, forwards the intended route, and preserves the
platform's independent authorization controls. A successful `/healthz` check
or a bare HTTP 401 is insufficient to establish authenticated tool access.
