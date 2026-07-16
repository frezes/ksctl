# ksctl Cross-Cluster Request Design

## Goal

Allow commands that access Kubernetes or KubeSphere resources to target a
specific KubeSphere cluster. For example, `ksctl get po -A --cluster host`
must query the `host` cluster through the configured KubeSphere endpoint.

## Scope

The existing persistent `--cluster` flag applies to every command that sends
cluster-scoped requests through ksctl. The currently available networked
commands are `get`, `describe`, and `version`.

Local configuration commands do not send cluster-scoped requests. Authentication
requests such as login and token refresh continue to use the unscoped
KubeSphere endpoint.

This change does not add flags or alter the configuration schema.

## Configuration Semantics

The two cluster-related context fields retain their distinct meanings:

- `contexts.<name>.cluster` selects an entry in `clusters`, whose `host` and TLS
  settings define the KubeSphere endpoint. It remains required for a saved
  context.
- `contexts.<name>.defaultCluster` is the optional default target cluster for
  resource requests.

An explicit `--cluster` value takes precedence over
`contexts.<name>.defaultCluster`. When both are empty, requests continue to use
the unscoped KubeSphere endpoint and preserve the current behavior.

## Request Construction

The shared Kubernetes `RESTClientGetter` resolves the endpoint, credentials,
and target cluster once. When a target cluster is present, it uses the following
REST host for both the client-go REST config and its raw kubeconfig view:

```text
<kubesphere-endpoint>/clusters/<cluster>
```

Client-go appends its normal discovery and resource paths to this base. For
example:

```text
ksctl get po -A --cluster host
  -> /clusters/host/api
  -> /clusters/host/apis
  -> /clusters/host/api/v1/pods
```

Because `get` and `describe` share the same kubectl Factory and
`RESTClientGetter`, the routing applies consistently to discovery, RESTMapper
lookups, resource reads, watches, and describe-related requests without
command-specific wrappers.

Some KubeSphere Console endpoints reject cluster-scoped core/v1 discovery at
`/clusters/<cluster>/api/v1` even though cluster-scoped core resource requests
work. If that discovery request fails, ksctl may use the unscoped `/api/v1`
resource metadata from the same KubeSphere endpoint. This fallback applies only
to core/v1 discovery; grouped discovery and all resource requests remain
cluster scoped.

`version` uses the same cluster-scoped REST host and sends `/kapis/version`
relative to it. It must not also use the KubeSphere request builder's cluster
selection method because that would add the cluster prefix twice. The final
request is `/clusters/<cluster>/kapis/version` when a target is present and
`/kapis/version` otherwise.

Cluster routing is applied only to the final resource client configuration.
OAuth login and refresh requests continue to use the original endpoint so that
credential acquisition is never sent through a member-cluster proxy path.

## Endpoint Handling

The target path is joined onto the configured endpoint without discarding an
existing endpoint path prefix. A trailing slash does not result in a double
slash. Cluster names occupy one URL path segment.

No client-side existence check is added. An unknown or inaccessible cluster is
reported using the response returned by KubeSphere, preserving server error
details and authorization behavior.

## Error Handling

Existing endpoint, authentication, timeout, TLS, discovery, and resource
errors continue to propagate through the kubectl command path. Building a
cluster-scoped endpoint may return an error if the configured endpoint cannot
be parsed as a URL; the command returns that error before sending a resource
request.

An empty target cluster is valid and never adds `/clusters/` to the request
path.

## Testing

Focused tests must prove:

- an explicit `--cluster` overrides the current context's `defaultCluster`;
- an omitted flag uses a non-empty `defaultCluster`;
- an empty flag and empty `defaultCluster` preserve unscoped request paths;
- path-escaping cluster values such as `..`, `/`, or `%` are rejected;
- the REST config and raw kubeconfig server use the same cluster-scoped base;
- `get` discovery and resource requests use `/clusters/<cluster>/...`;
- failed cluster-scoped core/v1 discovery falls back to unscoped resource
  metadata while the subsequent resource request remains cluster scoped;
- `describe` requests use `/clusters/<cluster>/...`;
- `version` uses the explicit or context-default target cluster;
- `version` adds the cluster path exactly once;
- authentication and token refresh requests remain unscoped.

The repository test suite and `git diff --check` provide final regression and
formatting verification.
