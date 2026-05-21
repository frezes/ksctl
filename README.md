# KubeSphere CLI

`ksctl` is a KubeSphere 4.x command-line tool for humans and automation.

The first phase authenticates through KubeSphere and directly reuses kubectl's
native `get` and `describe` commands:

```bash
ksctl get workspaces --endpoint https://kubesphere.example.com --token "$KS_TOKEN"
ksctl get pods -A -o wide
ksctl get deployments,pods -n demo -l app=web -o json
ksctl describe workspace demo
ksctl describe pod/web -n demo
ksctl auth login https://kubesphere.example.com -u admin -p "$KS_PASSWORD" --context local
ksctl auth logout local

kubectl ks get workspaces --endpoint https://kubesphere.example.com --token "$KS_TOKEN"
kubectl ks describe workspace demo
```

kubectl resource discovery, RESTMapper behavior, output formats, selectors,
watching, built-in Describers, Generic Describe fallback, and Events behavior
come from the pinned kubectl v0.36.2 implementation.

KSApiServer receives the standard Kubernetes `/api` and `/apis` request paths
unchanged and transparently proxies them to KubeAPIServer. ksctl does not map
those requests to KubeSphere-specific paths.

Configuration remains independent from kubeconfig:

```yaml
# ~/.ksctl/config.yaml
apiVersion: ksctl.kubesphere.io/v1alpha1
kind: Config
currentContext: local
clusters:
  local:
    host: https://kubesphere.example.com
    tlsClientConfig:
      insecure: false
users:
  admin:
    username: admin
    bearerToken: ""
    bearerTokenFile: ""
contexts:
  local:
    cluster: local
    user: admin
```

`ksctl auth login` writes connection metadata to `~/.ksctl/config.yaml` and writes
the complete KubeSphere `/oauth/token` response to
`~/.ksctl/cache/tokens/<context>.json` with mode `0600`. Passwords are used
only for the `login` request and are not persisted.

`username` defaults to the `users` map key when omitted. Effective credentials
are resolved in this order:

```text
--token > KS_TOKEN > token cache > selected user.bearerTokenFile > selected user.bearerToken
```

When the cached access token is expired, ksctl uses the cached refresh token to
request a new token response. A missing cache may fall back to the selected
user's `bearerTokenFile` or `bearerToken`; if refresh fails, run
`ksctl auth login` again. The config file is stored with mode `0600`.

The package tree separates commands, authentication, persistence, config, and
the two client adapters. Kubernetes and KubeSphere target the same endpoint but
construct independent transports:

```text
pkg/cmd                Cobra commands and user-facing output
pkg/auth               connection resolution, OAuth, and token selection
pkg/cache/token        token models, expiry, and cache file IO
pkg/config             config model and config file IO
pkg/client/kubernetes  RESTClientGetter, discovery, and RESTMapper adapter
pkg/client/kubesphere  KubeSphere generic REST client construction
```

`pkg/auth/oauth.go` owns `/oauth/token` and injects the generic REST client
factory from `pkg/client/kubesphere`; the KubeSphere client package contains no
login, refresh, or token-cache policy.
