# ksctl Config Redesign

Date: 2026-07-13

## Summary

`ksctl` config remains independent from kubeconfig, but its stored connection
fields now follow the KubeSphere REST client concepts used by
`kubesphere.io/client-go/rest.Config`.

The old config shape is not supported by this change. Users must write the new
field names directly.

## Goals

- Keep the existing kubeconfig-style top-level sections: `clusters`, `users`,
  `contexts`, and `currentContext`.
- Make cluster entries describe the KubeSphere API endpoint and TLS settings.
- Make user entries describe KubeSphere credentials with names matching
  KubeSphere REST config semantics.
- Keep context entries as references plus KubeSphere default scope metadata.
- Preserve the current CLI resolution order:
  `--token > KS_TOKEN > selected user.bearerToken > selected user.password login`.
- Preserve password login through KubeSphere `/oauth/token`.
- Preserve kubectl-backed `get` and `describe` behavior, including standard
  Kubernetes `/api` and `/apis` request paths through KSApiServer.

## Non-Goals

- No compatibility with the previous `server`, `tls.insecureSkipVerify`, or
  `token` field names.
- No kubeconfig read/write support.
- No keychain, credential plugin, exec plugin, or external credential reference.
- No persistence of password-login issued tokens.
- No exposure of runtime-only REST config fields such as custom transport,
  dialer, proxy function, serializer, rate limiter, or warning handler.

## Config Shape

```yaml
apiVersion: ksctl.kubesphere.io/v1alpha1
kind: Config
currentContext: local
clusters:
  local:
    host: https://kubesphere.example.com
    tlsClientConfig:
      insecure: false
      serverName: ""
      caFile: ""
      caData: ""
users:
  admin:
    username: admin
    bearerToken: ""
    bearerTokenFile: ""
    password: "<plaintext-password>"
contexts:
  local:
    cluster: local
    user: admin
    defaultCluster: host
    defaultWorkspace: demo
```

## Field Semantics

`clusters.<name>.host` maps to `kubesphere.io/client-go/rest.Config.Host`.
It is the KSApiServer endpoint.

`clusters.<name>.tlsClientConfig` maps to the serializable subset of
`kubesphere.io/client-go/rest.TLSClientConfig`. The supported fields are
`insecure`, `serverName`, `certFile`, `keyFile`, `caFile`, `certData`,
`keyData`, `caData`, and `nextProtos`.

`users.<name>.username` is the KubeSphere login username. If it is empty,
the user map key is used as the username.

`users.<name>.bearerToken` maps to `rest.Config.BearerToken`.

`users.<name>.bearerTokenFile` maps to `rest.Config.BearerTokenFile`.
When set, the local file is read during resolution and takes precedence over
`bearerToken`, matching the REST client configuration contract.

`users.<name>.password` is plaintext and is used only when no bearer token is
available. The issued access token stays in memory for the current invocation.

`contexts.<name>.cluster` and `contexts.<name>.user` select a cluster and
user entry. `defaultCluster` and `defaultWorkspace` remain ksctl scope metadata.

## Resolution

Endpoint resolution remains:

```text
--endpoint > KS_ENDPOINT > selected cluster.host
```

Token resolution remains:

```text
--token > KS_TOKEN > selected user.bearerTokenFile > selected user.bearerToken > selected user.password login
```

`--insecure-skip-tls-verify` overrides the selected cluster TLS config for the
current invocation only.

## Testing

Focused tests must cover:

- saving and loading the new field names;
- absence of old field names in saved config;
- context resolution through `host`, `tlsClientConfig.insecure`,
  `username`, `bearerToken`, `bearerTokenFile`, and `password`;
- flag and environment precedence over config;
- password login still posts to `/oauth/token`;
- RESTClientGetter still builds Kubernetes `rest.Config` for kubectl resource
  commands without rewriting `/api` or `/apis`.
