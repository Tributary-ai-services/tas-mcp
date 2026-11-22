# Tenant-scoped federation registry

Plan #22 P4-Config, work package C.1; design §9.6 and decision D18.

Before C.1, the federation registry was one global set. Every caller could list and invoke every server, and anyone who could reach the proxy could register or delete one. C.1 adds tenant servers, which one tenant (an AIQG org) owns and only that tenant's tokens can see or call. It also puts a service token on the write routes.

The feature is off by default. With `MCP_TENANT_SERVERS` unset, the routes, responses and status codes are exactly what they were before C.1. Everything below describes the proxy with the flag on.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `MCP_TENANT_SERVERS` | off | Turns the tenant-scoped registry on. |
| `MCP_ADMIN_TOKEN` | empty | The service token that register, update and delete require. With the flag on and this empty, every write is refused with 503. It is never logged. |
| `MCP_TENANT_EGRESS_ALLOWLIST` | empty | A JSON object that maps each tenant id to the hosts outside the cluster that its servers may use, e.g. `{"<tenant-uuid>":["api.salesforce.com","*.qualtrics.com"]}`. If the value does not parse, the proxy logs a Warn and allows no external host. |
| `MCP_TENANT_CLUSTER_NAMESPACES` | `tas-mcp-servers` | A comma-separated list of the namespaces an in-cluster tenant server may live in. |

The tenant comes from the caller's `TAS-Auth` token, which is resolved by caller identity (T.1, `IDENTITY_ENABLED`). Nothing in the request body or in a self-asserted header can set it. If identity is off, every call is unattributed and sees only platform servers, and the proxy logs a Warn at startup to say so.

## The admin token

Send the token in this header:

```
TAS-MCP-Admin-Token: <MCP_ADMIN_TOKEN>
```

| Route | Without the header | Wrong token | No token configured |
|---|---|---|---|
| `POST /api/v1/federation/servers` | 401 | 403 | 503 |
| `PUT /api/v1/federation/servers/{id}` | 401 | 403 | 503 |
| `DELETE /api/v1/federation/servers/{id}` | 401 | 403 | 503 |

Two callers hold the token: the FederatedMCPServer operator (tas-mcp-servers C.2) and the aiqg-dashboard-be reconcile (C.3). The browser never calls the proxy.

A tenant's `TAS-Auth` token is not an admin token. On a read route, the admin token returns every row, every tenant's included, and shows disabled servers too. The operator's ownership check and the AIQG drift report both depend on that.

The proxy does not decide which writer owns a row. Both writers hold the same token, and each one checks `managed_by` before it writes: the operator leaves `aiqg` rows alone, and AIQG leaves `operator` rows alone.

## The server record

`MCPServer` gains five fields. All of them are optional.

| Field | Absent means | Notes |
|---|---|---|
| `tenant_id` | a platform server | A server with no tenant is shared by every caller. A server with a tenant is visible to that tenant only. |
| `owner_team_id` | none | The Aether team that owns the server. The proxy does not use it. |
| `managed_by` | `operator`, or `aiqg` for a tenant row | Must be `operator` or `aiqg`. A tenant row must be `aiqg`. |
| `allowed_tools` | every tool | An allowlist over the server's `tools/list`. |
| `enabled` | `true` | A disabled server is not listed to callers, not invoked, not health-checked and not probed. |

**Migration is lazy.** Existing Redis records are not rewritten when the proxy starts. A record without these fields decodes with zero values, and the accessors (`IsPlatform`, `Owner`, `IsEnabled`, `ToolAllowed`) read that as a platform server that the operator manages, is enabled and allows every tool. The next write to the record, whether a register or a PUT, stores the values explicitly. With the flag on, every GET view shows `managed_by` and `enabled` explicitly, so a reader never needs to know the default.

## Routes

| Route | With the flag on |
|---|---|
| `GET /servers`, `GET /categories/{c}/servers` | Returns the platform servers plus the caller's own tenant's, enabled ones only. The admin token returns all of them. |
| `GET /servers/{id}`, `/health`, `/probe` | Returns 404 for a server the caller cannot see, the same answer as for an id that does not exist. |
| `GET /federation/health` | The status and probe maps list only the servers the caller can see. |
| `POST /servers` | Requires the admin token. It is validated as described below. |
| `PUT /servers/{id}` | New in C.1, and routed only when the flag is on. It needs the admin token and replaces the whole definition. The path id wins, and a body id that differs is a 400. An unknown id is a 404. `created_at` is kept and the status resets to `unknown`. Every replica drops its cached connection, so the next invoke uses the new endpoint and allowlist. |
| `DELETE /servers/{id}` | Requires the admin token. |
| `POST /servers/{id}/invoke` | See the next section. |
| `POST /federation/broadcast` | Reaches only the servers the caller may invoke. |

### Invoke

- **Platform server:** served to every caller, including an unattributed one. This is the observe-only default from Q12.
- **Tenant server:** served only when the caller's token resolves to that tenant. Any other call gets **403**: no token, an unknown token, another tenant's token, or a validator outage. The body never names the owning tenant. A refusal like this is logged at Warn and emits **no** tool-call event, because that event would be attributed to the caller's tenant and would name another tenant's server.
- **Disabled server:** 403.
- **`tools/call` for a tool outside `allowed_tools`:** 403. The proxy emits a tool-call event with outcome `blocked`, and the upstream server is not contacted.
- **`tools/list`:** tools outside `allowed_tools` are removed from the result. Other fields of the result, such as `nextCursor`, are kept.
- Every other invoke error still returns 500, as before.

The tool catalogue that the probes publish (T.4) is **not** filtered. It describes what the server offers, and `allowed_tools` is applied when a tool is listed or called.

## Egress for tenant servers (Q13)

The proxy checks each tenant server when it is registered or updated:

1. Its protocol must be `http` or `sse`. A `stdio` server would run a command on the proxy, and a `grpc` endpoint has no URL to check.
2. Its endpoint must be an absolute `http(s)` URL and must not carry credentials (`user:pass@`).
3. **Inside the cluster:** the host must have the form `<svc>.<ns>.svc` or `<svc>.<ns>.svc.cluster.local`, and `<ns>` must be listed in `MCP_TENANT_CLUSTER_NAMESPACES`. A bare name like `dbhub` is refused, because its namespace is ambiguous.
4. **Outside the cluster:** the endpoint must use `https`, and its host must be on that tenant's list in `MCP_TENANT_EGRESS_ALLOWLIST`. A list entry is either an exact host name or `*.domain`, which matches any subdomain of the domain but not the domain itself. Matching ignores case and the port. An IP literal counts as outside the cluster.

A definition that breaks any of these rules is refused with 400. Platform servers are not checked: they come from reviewed CRs.

**Why the allowlist is configuration and not a registry entry:** the allowlist is a security boundary. If it lived in the registry, the same admin token that registers a server could also widen the allowlist, so it would protect nothing. Widening it should be a reviewed deploy change, and that change has to touch the NetworkPolicy anyway.

### What a deploy needs

This PR changes no manifest. To turn the feature on:

1. **The Secret.** Create a Secret that holds `MCP_ADMIN_TOKEN` in `tas-mcp-prod`, for example `tas-mcp-admin-token` with the key `token`. Mount it into the proxy as `MCP_ADMIN_TOKEN`. Put the same value in `tas-mcp-servers`, the namespace the operator reads it from (C.2 references `tas-mcp-admin-token`/`token`, optional), and in aiqg-dashboard-be's namespace when C.3 ships. Roll out the token to the operator **before** setting `MCP_TENANT_SERVERS=true` on the proxy. Otherwise the operator's writes start returning 401 and its drift healing stops. Its GETs return only platform rows until it holds the token.
2. **The flag.** Set `MCP_TENANT_SERVERS=true` with `IDENTITY_ENABLED=true`, which is already on in prod. Without identity, no tenant server can be called.
3. **The NetworkPolicy.** MCP-14 allows the proxy to reach only `tas-mcp-servers` and Redis. A tenant server outside the cluster needs one more egress rule on the proxy pod: TCP 443 to the resolved addresses of each allowlisted host, as `ipBlock` CIDRs. A NetworkPolicy cannot match a DNS name, so a CDN-hosted SaaS endpoint needs either a broad CIDR or an egress proxy or gateway that can match on name. DNS egress to kube-dns must stay open. Do not add `0.0.0.0/0`. An in-cluster tenant server in a namespace other than `tas-mcp-servers` needs a namespace-selector rule for that namespace, and must also be listed in `MCP_TENANT_CLUSTER_NAMESPACES`.
4. **The allowlist.** Set `MCP_TENANT_EGRESS_ALLOWLIST` in the same change as the NetworkPolicy rule, so that the two lists agree.
