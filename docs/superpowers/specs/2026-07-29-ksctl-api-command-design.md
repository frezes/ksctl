# ksctl API Command Design

## Goal

Add a curl-like `ksctl api` command that sends an authenticated request to an
arbitrary KubeSphere API path while reusing ksctl's existing connection,
credential, TLS, context, user-agent, and timeout handling.

## Command Surface

The command accepts exactly one server-relative API path:

```text
ksctl api API_PATH [-X METHOD] [-d DATA]
```

The flags are:

- `-X, --method METHOD` sets the HTTP method.
- `-d, --data DATA` sends the provided inline string as the raw request body.

`METHOD` defaults to `GET`. When `--data` is explicitly present and `--method`
is not, the command uses `POST`. An explicitly selected method always wins, so
`-X PUT -d DATA` sends a `PUT` request.

The command is registered on the shared root and is therefore available as
both `ksctl api` and `kubectl ks api`.

## Path and Scope Semantics

`API_PATH` must begin with `/` and may include a query string:

```text
ksctl api '/kapis/example.io/v1/items?limit=10'
```

The command rejects absolute URLs and URL fragments. It uses the KubeSphere
REST request builder's request-URI support so query values reach the server
without being interpreted as part of the path.

The command never applies cluster scope automatically. The persistent
`--cluster` flag and a context's `defaultCluster` do not add a
`/clusters/<cluster>` prefix. A caller that wants a cluster-scoped endpoint
must include the complete prefix in `API_PATH`, for example:

```text
ksctl api /clusters/member/kapis/example.io/v1/items
```

## Request Construction

The command receives the existing KubeSphere REST client getter and client
factory from the root command. At execution time it:

1. validates and normalizes the path and method;
2. resolves the KubeSphere REST configuration;
3. creates the existing generic KubeSphere REST client;
4. creates a request with `Verb(method)` and `RequestURI(path)`;
5. when `--data` is present, sets `Content-Type: application/json` and supplies
   `[]byte(data)` as the body;
6. executes the request with the Cobra command context; and
7. writes the received response body to stdout without modification.

Converting data to `[]byte` is required because the REST request builder treats
a Go string body as a filename. The initial command accepts inline data only;
it does not support `@file` syntax or reading the body from stdin.

The method is trimmed and converted to uppercase. Empty methods and methods
containing characters that are invalid in an HTTP token are rejected before
any connection is resolved. Otherwise, custom HTTP methods remain valid.

Flag presence, rather than a non-empty data value, determines whether a body
was supplied. Consequently, `-d ''` sends an empty body with
`Content-Type: application/json` and defaults to `POST`.

## Output and Error Handling

Any response body received from the server is written byte-for-byte to stdout.
The command does not decode, format, or append a newline. This behavior makes
the output safe for redirection and pipelines.

For an HTTP `4xx` or `5xx` response, the command writes the response body and
also returns the REST client's error, producing a non-zero process exit status.
Connection, authentication, TLS, timeout, configuration, and request-building
errors are returned with actionable context. A stdout write failure is also
returned. If both the response and output operations fail, neither failure is
silently discarded.

## Code Organization

The implementation lives in `pkg/cmd/api.go` because it is a thin root-level
command that coordinates existing connection and REST-client abstractions. Its
tests live beside it in `pkg/cmd/api_test.go`.

No new client package or external dependency is introduced. The root command
registers the new command with the same KubeSphere getter and REST client
factory already used by other KubeSphere-facing commands.

## Testing

Focused tests cover:

- root registration for both `ksctl` and `kubectl ks`;
- presence of `-X`/`--method` and `-d`/`--data`;
- default `GET` behavior;
- query-string preservation;
- automatic `POST` when `--data` is explicitly present;
- an explicitly selected method taking precedence over the data default;
- the exact request body and `application/json` content type;
- an explicitly empty data value;
- custom valid methods and rejection of empty or syntactically invalid methods;
- rejection of missing paths, extra arguments, absolute URLs, paths without a
  leading slash, and fragments;
- no automatic path change from `--cluster`;
- byte-for-byte stdout for successful and HTTP-error responses;
- a non-nil error for HTTP-error responses after their body is written;
- connection/configuration failures without a request; and
- propagation of stdout write failures.

Focused package tests are followed by the repository test suite, formatting
checks, a build, and `git diff --check`.
