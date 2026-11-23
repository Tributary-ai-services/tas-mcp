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

## Per-tenant connections (C.4)

Plan #22 C.4; design §9.6 "Connections". A connection is one tenant's credentials and parameters for one server. aiqg-dashboard-be stores it encrypted (`aiqg.mcp_connection`, the BYOK envelope) and is the only place it is written. On each invoke, the proxy resolves the caller tenant's connection for the server and injects it. This is the server-side form of aether-be's `injectConnectionParams`.

The feature is off by default. With `MCP_CONNECTION_INJECTION` unset, nothing is resolved, stripped, refused or injected.

| Variable | Default | Meaning |
|---|---|---|
| `MCP_CONNECTION_INJECTION` | off | Turns injection on. It needs `IDENTITY_ENABLED` (the tenant comes from the caller's token); without identity it stays off and logs a Warn. |
| `AIQG_DASHBOARD_URL` | — | The identity validator's base URL. The connection endpoints are on the same service. |
| `MCP_CONNECTIONS_RESOLVE_TOKEN` | — | Sent as `Internal-Auth` to `/internal/mcp/connections/*`. It is a token of its own, not the shared `INTERNAL_AUTH_TOKEN`, so the router (which holds the shared one) cannot read connection secrets. Without it injection stays off. Never logged. |
| `MCP_CONNECTION_SERVERS` | empty | `id=kind,id=kind`: the connection kind a **platform** server takes, e.g. `dbhub=database_dsn`. A tenant server says so in its record (`connection_kind`, written by AIQG). |
| `MCP_CONNECTION_REQUIRED_SERVERS` | empty | `id,id`: servers that never run a call on their own login. Read even with injection off, in which case they refuse every call but `tools/list`. Environment only: a `-config` file start does not read it. |
| `MCP_CONNECTION_CACHE_TTL` | `60s` | How long a resolved connection, the answer "none", and the cross-tenant server list are kept in memory. A rotation takes effect within this window. Errors are never cached. |

### Which servers take connections

A server **takes connections** when its record declares a `connection_kind`, when `MCP_CONNECTION_SERVERS` names it, or when any tenant holds a live connection for it. The last comes from `POST /internal/mcp/connections/servers`, which lists, for each server, the kinds of its connections and the tool arguments its DSN connections inject. It names no tenant and carries no secret.

### The rules

- **The tenant comes from the resolved `TAS-Auth` token**, never from the body or a self-asserted header.
- **A connection is keyed by `(tenant_id, server_id)`**, and nothing else. On a platform server, tenant A's call injects A's connection and tenant B's call injects B's. A tenant with no connection for the server gets nothing, and the server's own `auth` applies.
- **Unattributed calls.** A call with no token, or an unknown one, gets nothing injected and is served (observe mode, Q12). But when the token **could not be checked** (identity result `validator_unavailable` or `validator_unconfigured`) and the server takes connections, or the server list cannot be read, the call is refused with **503**. Otherwise a validator outage would run the tenant's call on the server's default credentials.
- **Connection required.** A server in `MCP_CONNECTION_REQUIRED_SERVERS` gets only `tools/list` without a connection. A `tools/call` is refused with **403** unless the proxy injected the tenant's DSN as an object under `_connection` with host, port, username, password and database all set (a connection of another kind, argument or format does not count: the bridge would ignore it, and it would fill an empty field from its own environment). Host, username, database and SSL mode may hold only letters, digits, `.`, `_` and `-`, because the bridge puts them into a URL unescaped. Every other method is refused, and `broadcast` skips the server. The list matches server ids, not endpoints. This overrides "served" in the two rules above for those servers. It covers calls through the proxy only: a pod that calls the server's Service directly is limited by that server's own login and nothing else.
- **Suspended accounts.** A suspended account's call to a server that takes connections, or for which that tenant has one, is refused with **403**, as aiqg-dashboard-be's data plane refuses a suspended token. Its connection is never used, and no OAuth grant runs with its secret. A suspended call to a server without connections is still served.
- **Fail closed.** If the resolver cannot answer, or a connection cannot be applied (an OAuth grant fails, a host breaks the egress rule), the call is refused with **503** and the upstream server is not contacted. The response carries only a generic message; the detail is logged at Warn.
- **No caller-chosen database.** On a server that takes `database_dsn` connections, a caller-supplied `_connection` (and every argument name a tenant's DSN connection uses) is removed from `tools/call` for every caller, whether or not the caller's tenant has a connection. While the server list cannot be read, `_connection` is removed from every call.
- **Egress (Q13) for stored values too.** A DSN's host and an OAuth `token_url` pass the same rule as a tenant server endpoint: an in-cluster host must be a service in `MCP_TENANT_CLUSTER_NAMESPACES`, and any other host must be on the tenant's `MCP_TENANT_EGRESS_ALLOWLIST` (an IP literal only when it is listed itself). Other cluster names (`*.svc`, `*.cluster.local`, `localhost`) are refused. With `MCP_TENANT_SERVERS` off there is no allowlist, so only public DNS names pass: IP literals, bare names and cluster names are refused. A token URL must be `https`, and **redirects from a token endpoint are refused**, because the target of a redirect passed no egress check and would receive the client secret.
- **Injected values never reach the proxy's own outputs.** The connection is added to a copy of the request that only the upstream call sees. The boundary scanner, the reducer and the tool-call event see the caller's request. Every injected value (the secret, the full header value, an OAuth access token, a DSN and its password) is replaced with `[REDACTED]` in the response (map keys included), in `error.message`, in the error text and therefore in the log lines, before the scanner runs, so a finding cannot quote one either. Each value is redacted in its raw, JSON-escaped (with and without HTML escaping), URL-escaped and base64 (standard and URL, padded and raw) forms. An upstream error body is redacted **before** it is cut to its 256-byte preview, and a prefix of 8 or more characters of a secret left at the cut is redacted too. Values shorter than 4 characters are not redacted.

### Kinds

| Kind | Params (non-secret) | Secret | What the proxy does |
|---|---|---|---|
| `header` | `header` (default `Authorization`), `prefix` (e.g. `Bearer `) | the header value | Sets `header: prefix+secret` on every call, replacing a header of the same name from the server's own `auth`. |
| `oauth_client_credentials` | `client_id`, `token_url`, `scopes` (space-separated), `audience`, `header` | the client secret | Runs the client-credentials grant and sets `Authorization: Bearer <token>`. The token is cached per tenant, server, connection id and version until 30 s before it expires, so a rotation fetches a new one. |
| `database_dsn` | `argument` (default `_connection`), `format` (`object` or `string`) | the DSN | On `tools/call` only, sets `arguments[argument]`: for `object`, aether-be's `connectionInfo` shape (`host`, `port`, `username`, `password`, `database`, `protocol`, `ssl_mode`); for `string`, the DSN itself. It also sends `TAS-Tenant-Id: <tenant>` so a database bridge keys its sessions by tenant (tas-mcp-servers bridge). |

### Test connection (`TAS-MCP-On-Behalf-Of-Tenant`)

aiqg-dashboard-be holds no tenant's `TAS-Auth` token, only its hash, so its Test connection calls invoke with the admin token and names the tenant in `TAS-MCP-On-Behalf-Of-Tenant`. The proxy honors the header **only with a valid admin token**, and only with `MCP_TENANT_SERVERS` on. Without the token it is ignored, so no tenant can assert another tenant through it. A malformed tenant id is a 400. Such a call:

- is treated as an attributed call by that tenant: it may call that tenant's servers and gets that tenant's connection injected;
- gets an **unfiltered** `tools/list`, so the UI can choose `allowed_tools` from every tool the server offers, while `tools/call` of a disallowed tool is still refused.

## The delegate credential (AIQG-88)

Plan #22 C.6 moves aether-be onto the proxy. aether-be holds no tenant's `TAS-Auth` token, and AIQG cannot issue one to a service. It therefore calls with a single **delegate credential** and names the Aether organization and the user it acts for. The proxy checks both with aiqg-dashboard-be before it attributes the call to a tenant.

The feature is off by default. With `MCP_DELEGATION_ENABLED` unset, the delegate headers mean nothing and every route behaves exactly as before.

| Variable | Default | Meaning |
|---|---|---|
| `MCP_DELEGATION_ENABLED` | off | Turns the delegate credential on. It also needs `IDENTITY_ENABLED` and `MCP_TENANT_SERVERS`. Without them, it stays off and logs a Warn. |
| `MCP_DELEGATE_TOKEN` | — | The credential, from Secret `tas-mcp-delegate-token` (key `token`). It must be at least 32 characters, must not start with `tas_qg_live_`, and must differ from `MCP_ADMIN_TOKEN`. If it breaks any of these rules, delegation stays off. It is never logged. |
| `MCP_DELEGATE_NAME` | `aether-be` | Recorded on each tool-call event as `delegated_by`. |
| `MCP_DELEGATION_VERIFY_TOKEN` | — | Sent as `Internal-Auth` to aiqg-dashboard-be `POST /internal/mcp/delegation/verify`. It is a token of its own, from Secret `aiqg-mcp-delegation-verify-token`. It is never logged. |
| `AIQG_DASHBOARD_URL` | — | The base URL of the verify endpoint (the same service as the identity validator). |
| `MCP_DELEGATION_CACHE_TTL` | `60s` | How long a verify answer, allowed or refused, is reused. Errors are never cached. |
| `MCP_DELEGATION_PORT` | `8086` | The **delegated listener**: the only place a delegated call is served. Its Service is `tas-mcp-delegated` (`prod-tas-mcp-delegated` in prod), and its NetworkPolicy rule admits only pods labelled `app=aether-backend` in namespace `aether-be`. The main HTTP port refuses the credential outright. |

### A delegated call

```
POST http://prod-tas-mcp-delegated.tas-mcp-prod.svc.cluster.local:8086/api/v1/federation/servers/{id}/invoke
TAS-MCP-Delegate-Token:      <MCP_DELEGATE_TOKEN>
TAS-MCP-On-Behalf-Of-Org:    <Aether organization id>     (required)
TAS-MCP-On-Behalf-Of-User:   <acting user's Keycloak sub> (required)
TAS-MCP-On-Behalf-Of-Tenant: <AIQG tenant uuid>           (optional; must match the org's link)
TAS-MCP-Expected-Connection-Id: <stored connection id>    (optional; 409 if the tenant's connection is another one or none)
{"method": "tools/list" | "tools/call", ...}
```

1. **Credential.** It is compared in constant time, on hashes of both values. A wrong value gets 403.
2. **Shape.** What counts is whether a header is present, not its first value. Each of these gets 400:
   - the credential sent together with `TAS-Auth` or `TAS-MCP-Admin-Token`, even with an empty value;
   - the credential, org, user, tenant or expected-connection header sent more than once;
   - a missing or malformed org or user header, or a tenant header that is not a UUID.
3. **Verify.** The proxy calls aiqg-dashboard-be with `{aether_org_id, tenant_id?, user_sub}`. The call is refused with **403** if:
   - the org is not linked to a live AIQG org account;
   - a named tenant is not the org's;
   - the user is not a member of the org in AIQG's copy of Aether's membership (`team_member_history`, at org level or in any team).
   The refusal body says only `delegated call refused: <code>` and never names a tenant.
4. **Fail closed.** If the verify call cannot be answered (an outage, a 5xx, a 401 on the proxy's own token, or an answer that does not echo the org and user it was asked about), the call gets **503**. Nothing is cached in that case.
5. **Attribution.** Only after a successful verify is the call attributed to the tenant: `identity.Result=delegated`, `TenantID`, `UserID` set to the verified sub, and `DelegatedBy`. Every other rule then applies as it does to a `TAS-Auth` caller:
   - the tenant's server visibility and `allowed_tools`, with `tools/list` filtered (the admin on-behalf path's unfiltered `tools/list` does **not** apply);
   - connection injection for that tenant;
   - SEC-38's connection-required rule;
   - the suspended-account refusal.
6. **Methods.** Only `tools/list` and `tools/call` are accepted. Any other method gets 403.
7. **Expected connection.** When `TAS-MCP-Expected-Connection-Id` is sent, the proxy resolves the tenant's connection for the server first (through the same 60 s cache). If that connection is another one, or there is none, the call gets **409** and nothing is injected. aether-be sends the id its `Database` row is bound to, so replacing a tenant's connection in AIQG can never silently repoint the row.
8. **Logs.** A refusal logs the org id and a short hash of the sub (`user_sub_hash`), never the sub itself. This keeps valid (org, user) pairs out of Loki.

### Where it is refused

There are **two listeners**.

- **The main HTTP port (8082).** With delegation on, `RefuseDelegateHeader` refuses with **403**, before routing, any request that carries `TAS-MCP-Delegate-Token` with any value, empty included, on every route, invoke included. That covers:
  - register, update and delete, even when a valid admin token is sent alongside;
  - the read routes;
  - broadcast;
  - events, forwarding, metrics and stats;
  - the legacy `/mcp` route.
- **The delegated listener (`MCP_DELEGATION_PORT`).** It serves `POST /api/v1/federation/servers/{id}/invoke` and nothing else. Any other path gets 404, any other method on invoke gets 405, and a request without the credential gets 403. A `TAS-Auth` caller is not served there either.
- **gRPC** never reads the header.

The listener split exists so the network enforces who may hold the credential. Port 8082 admits any source (a NetworkPolicy rule patched in on 2026-02-06), so a credential accepted there could be used from any pod that obtained its value. Port 8086 admits only `app=aether-backend` in `aether-be`. The rule uses `matchExpressions` because kustomize `commonLabels` rewrites a peer's `matchLabels`.

**Follow-up, not done:** bind the credential to aether-backend's identity, for example a projected ServiceAccount token checked with TokenReview, so that a copy of the value is useless even from an admitted pod.

**Not a gateway token.** The credential can never have the `tas_qg_live_` shape, because startup refuses one that does. The proxy's identity resolver treats any other `TAS-Auth` value as malformed: the call is unattributed, nothing is injected, and a tenant server gets 403. The LLM gateway applies the same shape rule before it calls `/internal/auth/validate`, and the credential is not in AIQG's token store, so the gateway refuses it.

### Events

`com.tas.aiqg.tool_call.v1` gains `delegated_by`, which is null for calls that are not delegated. On a delegated call, `tenant_id` is the verified tenant and `user_id` is the verified Keycloak sub (not a self-asserted baggage value), and `owner_sub` and `token_id` are null. The field is additive. The aggregator reads an explicit schema, so it ignores `delegated_by` until Timescale `009` gains a column for it.

Metric: `tas_mcp_delegation_decisions_total{outcome}`, where `outcome` is one of `verified`, `bad_token`, `ambiguous`, `bad_headers`, `refused`, `unavailable`, `wrong_method` or `wrong_route`.
