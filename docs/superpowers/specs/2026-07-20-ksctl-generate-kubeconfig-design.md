# ksctl Generate Kubeconfig Design

Date: 2026-07-20

## Goal

Add `ksctl config generate kubeconfig` so the currently logged-in user can
retrieve a kubeconfig from the KubeSphere API. The command writes the API
response to standard output and supports selecting a target cluster with the
existing global `--cluster` flag.

## Command Contract

The command is invoked as:

```text
ksctl config generate kubeconfig [--cluster NAME]
```

The command writes the returned kubeconfig bytes to standard output. It does
not parse, normalize, merge, or persist the kubeconfig and does not modify
`~/.kube/config`.

The command accepts no positional arguments and does not add a command-local
username or output-file flag.

## Identity Resolution

The requested username is the username of the current login context, or the
context explicitly selected with the existing global `--context` flag. The
selected context must exist and must reference an existing fleet user.

If the referenced user has a non-empty `username`, that value is used. If it
is empty, the user entry name is used. A bearer token is not decoded to infer
identity. Supplying only `--endpoint` and `--token` without a selectable login
context is therefore insufficient and returns an explicit error.

## Cluster Resolution

Cluster selection follows the existing shared connection rules:

```text
--cluster > selected context.defaultCluster
```

When the resolved cluster is non-empty, the KubeSphere request is scoped
through the cluster proxy. When both values are empty, the request targets the
unscoped host cluster endpoint.

Cluster and username values must each be valid single URL path segments. The
command rejects values that could alter the request path.

## Architecture

The root command owns shared connection options and two protocol-specific
getters. Kubernetes resource commands continue to use the Kubernetes getter.
Kubeconfig generation uses a KubeSphere getter that provides:

- a native `kubesphere.io/client-go/rest.Config`, including authentication,
  TLS, timeout, and user agent;
- the resolved KubeSphere cluster; and
- the username belonging to the selected login context.

The config command constructs the existing KubeSphere REST client directly
from that native configuration and applies cluster scope through the
KubeSphere request builder's `Cluster` method. It does not derive an HTTP
client from Kubernetes `rest.Config` and does not reimplement config
precedence, password login, token refresh, TLS, or timeout behavior.

The existing `config current-context`, `config use-context`, and `config view`
commands remain local operations and retain their current behavior.

## Request Flow

The command performs the following steps:

1. Resolve the selected context, username, KubeSphere REST configuration,
   credentials, and target cluster through the KubeSphere getter.
2. Reject a missing login context or an invalid username or cluster path
   segment before issuing the request.
3. Construct a KubeSphere REST client from the resolved REST configuration.
4. Send an authenticated `GET` request to:

   ```text
   /kapis/resources.kubesphere.io/v1alpha2/users/{username}/kubeconfig
   ```

5. Write the successful response body unchanged to standard output.

The getter keeps the KubeSphere host unscoped. The KubeSphere request builder
adds `/clusters/{cluster}` when a cluster is selected, producing:

```text
/clusters/{cluster}/kapis/resources.kubesphere.io/v1alpha2/users/{username}/kubeconfig
```

or, when no cluster is resolved:

```text
/kapis/resources.kubesphere.io/v1alpha2/users/{username}/kubeconfig
```

The cluster prefix is added exactly once.

## Error Handling

The command returns errors for:

- a missing or unknown selected context;
- a context referencing a missing fleet or user;
- an invalid username or cluster path segment;
- credential resolution, password login, or token refresh failure;
- REST or HTTP client construction failure;
- a non-success KubeSphere API response; and
- failure while writing the response to standard output.

Errors retain enough operation context to identify kubeconfig generation while
preserving the existing credential redaction guarantees. The command never
writes a partial response before confirming that the API request succeeded.

## Testing

Focused tests must prove:

- the command is registered as `config generate kubeconfig` and rejects
  positional arguments;
- an explicit `--cluster` overrides `context.defaultCluster`;
- omitting `--cluster` uses `context.defaultCluster`;
- an empty flag and empty default target the unscoped host endpoint;
- a stored username is used, with fallback to the user entry name;
- a missing login context returns an error without making an API request;
- invalid username and cluster values are rejected before a request;
- the command uses the existing bearer-token and refresh-backed authentication
  behavior;
- a non-success response is returned as an error; and
- successful response bytes are written to standard output unchanged.

The focused package tests, repository test suite, `git diff --check`, and a
binary build provide final verification.

## Non-Goals

- Writing or merging `~/.kube/config`.
- Adding `--user`, `--output`, or filename flags.
- Decoding tokens to discover a username.
- Parsing or validating the returned kubeconfig document.
- Adding a new config field or changing existing config serialization.
