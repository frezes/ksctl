# ksctl Auth Whoami Design

Date: 2026-07-21

## Summary

Add `ksctl auth whoami` and its `kubectl ks auth whoami` counterpart. The
command uses the selected ksctl Context and the existing KubeSphere connection
stack to authenticate to KubeSphere, retrieve the selected server-side User,
and print the User name and platform-wide GlobalRole in a stable human-readable
form.

The command verifies that the resolved credential can access the selected User
resource. The User name in the request still comes from the selected Context;
the endpoint is not a general OAuth token-subject introspection API. An
explicit token with permission to read another User may therefore authenticate
successfully even when that token was not issued to the Context's User.

## Goals

- Add `auth whoami` to both supported entrypoints.
- Resolve the User from the current Context or the root `--context` flag.
- Reuse existing endpoint, TLS, credential precedence, token-cache refresh,
  user-agent, and request-timeout behavior.
- Verify the credential through an authenticated KubeSphere User request.
- Report the server-returned User name and its KubeSphere global role.
- Treat a missing global-role annotation as a valid User without a platform
  role.
- Preserve the existing local-only `auth logout` behavior.

## Non-Goals

- Do not add a KubeSphere logout, token-revocation, or session-revocation
  request.
- Do not decode a token locally or claim to prove the token's subject.
- Do not add JSON, YAML, name-only, or custom-template output modes.
- Do not add a username flag or allow whoami without a selected Context.
- Do not route the User request through a member-cluster endpoint.
- Do not change login, logout, credential precedence, token storage, or config
  formats.

## CLI Behavior

The new command is available through both entrypoints:

```text
ksctl auth whoami
kubectl ks auth whoami
```

It accepts no positional arguments and inherits the existing root connection
flags. `--context` chooses a named Context; otherwise the current Context is
used. `--endpoint`, `--token`, and `--request-timeout` continue to participate
in the existing connection resolution. A Context is still required because it
provides the User name used in the request path.

The successful output is exactly:

```text
Username: admin
Global Role: platform-admin
```

When the User has no non-empty `iam.kubesphere.io/globalrole` annotation, the
output is:

```text
Username: alice
Global Role: <none>
```

The command produces no partial successful output when request construction,
authentication, the server request, or response validation fails.

## Request and Response

The command obtains the resolved User name from the KubeSphere REST getter and
validates it as a KubeSphere URL path segment before sending a request. It then
builds an unversioned KubeSphere REST client from the same getter's REST config
and sends:

```text
GET /kapis/iam.kubesphere.io/v1beta1/users/{username}
```

This is a Fleet-level KubeSphere request. A root `--cluster` flag or a
Context's `defaultCluster` does not add `/clusters/{cluster}` to the request.

Only the following response fields are required:

```yaml
apiVersion: iam.kubesphere.io/v1beta1
kind: User
metadata:
  name: admin
  annotations:
    iam.kubesphere.io/globalrole: platform-admin
```

The implementation parses `metadata.name` and the
`iam.kubesphere.io/globalrole` annotation into a focused response structure.
An empty `metadata.name` is an invalid response. An absent or empty global-role
annotation is represented as `<none>`.

## Components

`pkg/cmd/root.go` passes the existing KubeSphere REST getter into the auth
command alongside the OAuth service.

`pkg/cmd/auth.go` adds a narrow getter interface containing the two operations
needed by whoami: resolve the KubeSphere username and return a KubeSphere REST
config. A new command constructor owns path validation, REST client creation,
the User request, response decoding, validation, and output formatting.

The existing `pkg/client/kubesphere/connection.RESTClientGetter` already
implements both required operations. Its REST config resolves the Fleet
endpoint and credentials without applying member-cluster routing, so no new
client package or discovery dependency is needed.

## Error Behavior

The command returns a non-zero result for:

- no current or explicitly selected Context;
- a Context that references a missing Fleet or User;
- an invalid User path segment;
- missing, malformed, or expired credentials that cannot be refreshed;
- REST client construction failures;
- KubeSphere authorization, not-found, transport, or timeout failures;
- malformed JSON or a response without `metadata.name`; and
- output write failures.

Errors add concise operation context while preserving safe underlying errors.
The command does not fall back to displaying the local Context identity when a
server-backed operation fails.

## Logout Behavior

`ksctl auth logout [CONTEXT]` remains unchanged. It validates the selected
Context and deletes only the shared local Fleet/User token cache. It does not
call a KubeSphere logout or revocation endpoint, remove Config entries, clear
the current Context, or invalidate manually configured credentials.

## Testing

Implementation follows test-driven development. Focused command tests cover:

- command registration under `ksctl auth` and `kubectl ks auth`;
- an authenticated request to the exact global User path;
- propagation of the resolved bearer token and user-agent;
- exact two-line output for a User with a global role;
- `<none>` output for a missing or empty annotation;
- Context and username resolution failures;
- rejection of an invalid username before any server request;
- KubeSphere authorization and not-found errors;
- malformed responses and responses without `metadata.name`;
- output write failures; and
- no regression to local-only logout behavior.

Final verification includes focused command tests, the complete normal and
race test suites, formatting, module consistency, vet, both binary builds, and
`git diff --check`.

## Documentation

Update `docs/cli.md` with the new command, output examples, missing-role
behavior, Context requirement, and server-verification semantics. Update
`docs/design.md` with the request flow and the distinction between whoami and
local-only logout. Add the feature to the Unreleased section of
`CHANGELOG.md`.
