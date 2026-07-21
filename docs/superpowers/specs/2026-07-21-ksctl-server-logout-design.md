# ksctl Server-Side Logout Design

## Summary

Extend `ksctl auth logout [CONTEXT]` so it makes a best-effort request to the
KubeSphere `/oauth/logout` endpoint before deleting the selected Fleet/User
token cache. The request carries the cached access token as a bearer token so
KubeSphere can revoke it. Local logout remains idempotent and succeeds even
when the remote request fails.

## Goals

- Ask KubeSphere to revoke the cached access token during `auth logout`.
- Delete the local Fleet/User token cache regardless of the remote result.
- Preserve the existing Context, Fleet, User, and static credential entries.
- Preserve Fleet/User cache sharing across Contexts.
- Keep access tokens out of command output, errors, and verbose transport logs.

## Non-Goals

- Do not revoke configured `bearerToken` or `bearerTokenFile` credentials.
- Do not perform a password login or token refresh during logout.
- Do not remove or modify Config entries or `currentContext`.
- Do not report remote logout failures to the user.
- Do not add retry, redirect, browser, or identity-provider logout behavior.
- Do not apply member-cluster routing to the logout request.

## KubeSphere API

KubeSphere 4.2.1 registers:

```text
GET /oauth/logout
Authorization: Bearer <access-token>
```

The handler extracts the bearer token and calls its token operator's `Revoke`
method. A successful response is HTTP 200. ksctl does not send
`id_token_hint`, `post_logout_redirect_uri`, or `state`.

The endpoint revokes the access token carried in the Authorization header. The
refresh token stored beside it is removed from the local cache but is not sent
to the logout endpoint.

## Command Behavior

`ksctl auth logout [CONTEXT]` continues to resolve the named Context or the
current Context, then validates its Fleet and nested User references. After
validation it performs these steps:

1. Load the Fleet/User token cache entry.
2. If the entry loads successfully and contains an access token, call the
   Fleet's unscoped `<host>/oauth/logout` endpoint with that token.
3. Ignore every result from the remote call, including transport errors,
   non-success HTTP responses, and an already invalid token.
4. Delete the Fleet/User token cache unconditionally.
5. Return an error only if local Config resolution or cache deletion fails.
6. Print the existing `Logged out from "<context>"` message on success.

A missing cache is successful and makes no remote request. A malformed or
unreadable cache cannot provide an access token; ksctl skips the remote request
and still attempts to delete the cache. If deletion succeeds, logout succeeds.

Because the cache is keyed by Fleet and User, logging out one Context still
logs out every Context that selects the same Fleet/User pair.

## Components

### `pkg/auth`

Add a focused logout request operation to the existing OAuth client. Its input
contains the Fleet endpoint, cached access token, user agent, timeout, and Fleet
TLS settings. It builds an unscoped KubeSphere REST client and sends
`GET /oauth/logout` with the bearer token.

The operation returns an error for construction, transport, or HTTP failures so
the request boundary can be tested independently. The command deliberately
discards that error. Reuse the existing OAuth response-redaction transport so
server response bodies and credentials cannot leak through errors or verbose
logs.

### `pkg/cmd`

Inject the existing OAuth client and root user agent into the logout command.
The command remains responsible for Context/Fleet/User resolution, cache load,
best-effort remote invocation, unconditional local deletion, and user-facing
output.

### `pkg/cache/token`

No cache format or path changes are required. `Load` supplies the cached access
token and `Delete` remains the authoritative local logout operation.

## Error and Security Semantics

Remote logout is intentionally best-effort. A successful CLI result guarantees
that the local cache was removed, not that KubeSphere confirmed revocation. This
matches the selected failure policy: local credentials are never retained just
to retry a failed remote request.

The request must never expose the access token in output, returned errors, or
verbosity-8 REST logs. Tests use placeholder tokens and assert that failure
text and captured logs do not contain them.

Configured static tokens remain authoritative after logout. If the selected
User has `bearerToken` or `bearerTokenFile`, later commands may still
authenticate with that configured credential.

## Testing

Add focused tests for:

- the logout client using `GET /oauth/logout` with the cached bearer token and
  Fleet endpoint/TLS/request metadata;
- successful remote logout followed by local cache deletion;
- remote HTTP or transport failure still deleting the local cache and returning
  success;
- a missing cache making no remote request and remaining successful;
- a malformed cache being deleted without a remote request;
- broken Context, Fleet, or User references retaining their current errors;
- Config and static credentials remaining byte-for-byte unchanged;
- secrets remaining absent from errors and verbose logs.

Update the command and design documentation to replace the current
"local-only" description with the best-effort server revocation behavior and
record the change in the Unreleased changelog.
