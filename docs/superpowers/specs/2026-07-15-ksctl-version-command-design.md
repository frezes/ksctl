# ksctl Version Command Design

## Goal

Make `ksctl version` report only the versions users need to identify the
client and its target environment:

```text
ksctl Version: v0.1.0
KubeSphere Version: v4.2.0
Kubernetes Version: v1.31.0
```

## Scope

The command reports three values:

- the local `ksctl` binary version;
- the target KubeSphere version;
- the Kubernetes version of the target cluster.

Git commit, build date, Go version, and other build metadata are not part of
the command output.

## Version Sources

The local version continues to come from the value injected into the `ksctl`
binary at build time, with `dev` as the development fallback.

The two server versions come from one authenticated request to
`GET /kapis/version` through the existing ksctl connection configuration. The
response fields are:

- `gitVersion` for KubeSphere;
- `kubernetes.gitVersion` for Kubernetes.

The command does not issue a separate Kubernetes discovery or `/version`
request.

## Command Behavior

The command prints the local version first, followed by KubeSphere and
Kubernetes versions. Each value occupies one stable, human-readable line.

If configuration resolution, authentication, transport, response decoding, or
a server version field is unavailable, the affected server versions are
printed as `unknown`. The command still succeeds so the local ksctl version is
available during offline diagnostics.

## Implementation Boundaries

The root command will construct the version command with the same connection
options and authentication provider used by the other server-backed commands.
The version command owns the single `/kapis/version` request, response parsing,
fallback values, and output formatting.

No unrelated connection, discovery, configuration, or authentication behavior
will change.

## Tests

Focused command tests will verify:

- the exact three-line output for a successful `/kapis/version` response;
- only `/kapis/version` is requested;
- local `ksctl` version is retained while both server values fall back to
  `unknown` when no usable server connection is available;
- Git commit, build date, and Go version are absent from the output.
