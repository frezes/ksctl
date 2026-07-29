# Namespace Flag Scope Design

## Goal

Limit `-n` and `--namespace` to commands that use Kubernetes namespace
selection instead of exposing the flag to every ksctl command.

## Command Surface

The root command no longer defines `-n` or `--namespace` as a persistent flag.
The flag is defined locally on:

- `get`
- `describe`
- `logs`
- `top pod`

The supported form remains:

```text
ksctl get pods -n demo
```

The former root-scoped form is no longer supported:

```text
ksctl -n demo get pods
```

Commands that do not select namespaced Kubernetes resources, including
`auth`, `config`, `api`, `tenant`, `extension`, `version`, and `top node`, do
not advertise or accept the flag.

## Implementation

`pkg/cmd/root.go` continues to construct one shared client options value and
one Kubernetes RESTClientGetter. After constructing the Kubernetes resource
commands, it binds each applicable command's local namespace flag to the
shared `Options.Namespace` field. The RESTClientGetter therefore keeps its
existing namespace override behavior without introducing another options
type or changing client interfaces.

The helper that constructs `top pod` owns its namespace flag. `top node` does
not receive the flag because Node is cluster-scoped.

The extension scope guard stops checking the namespace root flag. Extension
commands naturally reject `--namespace` as an unknown flag because neither
the root nor the extension command defines it. Explicit `--cluster` rejection
remains because `--cluster` is still a valid global connection flag whose
meaning conflicts with extension placement.

## Tests

Command-tree tests verify that:

- the root command does not define `--namespace`;
- `get`, `describe`, `logs`, and `top pod` define `--namespace` with `-n`;
- `top node` and non-resource commands do not define it;
- namespace selection after an applicable command still reaches the
  Kubernetes RESTClientGetter; and
- placing `--namespace` before the command fails as an unknown root flag.

Existing resource integration tests continue to cover namespace-aware request
paths. Extension scope tests retain explicit cluster coverage and remove the
obsolete custom namespace rejection cases.

## Documentation

The CLI guide removes namespace from the global flags table and documents it
as a Kubernetes resource-command flag. Extension documentation says those
commands do not accept namespace selection instead of claiming that they
reject a global namespace flag.
