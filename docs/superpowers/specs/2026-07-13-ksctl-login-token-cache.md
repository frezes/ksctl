# ksctl Login, Logout, and Token Cache

Date: 2026-07-13

Revised: 2026-07-14

## Summary

Add `ksctl auth login` and `ksctl auth logout` commands. `auth login`
authenticates with a KubeSphere endpoint, stores non-sensitive connection metadata in
`~/.ksctl/config.yaml`, and stores the full OAuth token response in a separate
local cache file. `logout` removes the cached token for a context.

The config file remains the source of connection identity. Token cache files
are the source of runtime bearer tokens.

## Goals

- Let users create a usable ksctl context without manually editing YAML.
- Keep passwords out of persistent storage.
- Keep bearer tokens out of `config view`.
- Cache the complete KubeSphere `/oauth/token` response for later commands.
- Reuse a cached access token while it is valid.
- Refresh automatically when the access token expires and a refresh token is
  available.
- Require `ksctl auth login` again when there is no usable cached token.
- Keep the first implementation non-interactive.

## Non-Goals

- No interactive username or password prompt in this change.
- No `--insecure-skip-tls-verify` option on `login`.
- No keychain or OS credential store integration.
- No browser/device-code login flow.
- No token encryption at rest.
- No logout request to the KubeSphere server unless a server-side revocation API
  is added later.
- No automatic deletion of cluster, user, or context entries during logout.

## Commands

### `ksctl auth login ENDPOINT`

Required flags:

```text
--username, -u
--password, -p
```

Optional flags:

```text
--context
```

Behavior:

1. POST the username and password to `ENDPOINT/oauth/token` using KubeSphere's
   password grant form.
2. Require a valid JSON response with `access_token`, `refresh_token`,
   `token_type`, and `expires_in`.
3. Save or update a config context:

   ```yaml
   apiVersion: ksctl.kubesphere.io/v1alpha1
   kind: Config
   currentContext: <context>
   clusters:
     <context>:
       host: <endpoint>
   users:
     <username>:
       username: <username>
   contexts:
     <context>:
       cluster: <context>
       user: <username>
   ```

4. Write the token cache for the selected context.
5. Print `Logged in to "<context>"`.

Context naming:

- If `--context` is set, use it exactly.
- Otherwise derive the context name from `ENDPOINT` host and optional port.
- Replace characters outside `[A-Za-z0-9_.-]` with `-`.

The command must not save the plaintext password.

The command must not save `bearerToken`, `bearerTokenFile`, or
`tlsClientConfig.insecure` into the config file.

### `ksctl auth logout [CONTEXT]`

Behavior:

1. Resolve `CONTEXT`; if omitted, use `currentContext`.
2. Delete that context's token cache file if it exists.
3. Clear `bearerToken` and `bearerTokenFile` from the context's user entry as a
   cleanup step for configs created before token-cache support.
4. Preserve cluster, user, context, and `currentContext` entries.
5. Print `Logged out from "<context>"`.

If no context can be resolved, return:

```text
error: context is required
```

## Token Cache

Default cache directory:

```text
~/.ksctl/cache/tokens
```

Cache file path:

```text
~/.ksctl/cache/tokens/<context>.json
```

The context segment must use the same filesystem-safe name as the context.

Cache file permissions:

```text
0600
```

Parent directory permissions:

```text
0700
```

Cache payload:

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "token_type": "Bearer",
  "expires_in": 7200,
  "obtained_at": "2026-07-13T10:42:40Z",
  "expires_at": "2026-07-13T12:42:40Z"
}
```

`obtained_at` and `expires_at` are local cache metadata, not fields returned by
KubeSphere.

`expires_at = obtained_at + expires_in seconds`.

Use a small safety window when deciding whether an access token is still valid:
if `expires_at` is within 30 seconds, treat it as expired.

## Refresh

When a normal command needs credentials:

1. Resolve endpoint and context from flags, env, and config.
2. If `--token` or `KS_TOKEN` is set, use it and do not read or write token
   cache.
3. Otherwise read the current context's token cache.
4. If the cached access token is still valid, use it.
5. If the access token is expired and a refresh token exists, POST a refresh
   grant to `ENDPOINT/oauth/token`.
6. If refresh succeeds, overwrite the cache and use the new access token.
7. If refresh fails, return:

   ```text
   error: login required for context <context>
   ```

Refresh form:

```text
grant_type=refresh_token
client_id=kubesphere
client_secret=kubesphere
refresh_token=<refresh_token>
```

The refreshed response is cached with a new `obtained_at` and `expires_at`.

## Auth Resolution

Resolution order becomes:

```text
--token > KS_TOKEN > token cache > selected user.bearerTokenFile > selected user.bearerToken
```

Password login is no longer attempted implicitly during `get` or `describe`.
Password is only used by the explicit `auth login` command.

If the selected context has no token cache and no explicit token source,
non-interactive commands fail with:

```text
error: login required for context <context>
```

## File Ownership

New code should keep responsibilities separated:

- `pkg/config`: config model, load, save, default paths.
- `pkg/cache/token`: token response model, cache path, load, save, delete, and
  expiry checks.
- `pkg/auth`: connection resolution, KubeSphere login/refresh requests, and
  token selection.
- `pkg/kubeclient`: authenticated RESTClientGetter, discovery, and RESTMapper
  adaptation.
- `pkg/cmd`: Cobra commands and user-facing command output.

## Testing

Focused tests must cover:

- `auth login` writes config context metadata and token cache.
- `auth login` does not persist password or bearer token in config.
- `auth login --context NAME` uses the requested context.
- default context name is derived from endpoint host and port.
- `auth logout` deletes token cache and preserves config metadata.
- `auth logout` clears legacy `bearerToken` and `bearerTokenFile`.
- token cache save/load preserves the full OAuth response and computes
  `expires_at`.
- valid cached token is selected by the authentication provider.
- expired token is refreshed and cache is updated.
- refresh failure returns `login required`.
- `--token` and `KS_TOKEN` bypass token cache.
- existing `get` and `describe` behavior remains unchanged once a token is
  resolved.

## Open Review Points

- Whether `logout` should clear `currentContext` when logging out the current
  context. This spec keeps it unchanged.
- Whether cache files should be keyed only by context or by context plus
  endpoint hash. This spec uses context only because contexts are unique config
  entries.
- Whether token cache should be configurable through an environment variable.
  This spec keeps only the default path.
