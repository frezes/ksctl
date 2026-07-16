# ksctl Fleet-Scoped Accounts and Credentials Design

## Goal

Model KubeSphere accounts and credentials within the Fleet they authenticate
against. Allow different Fleets to contain accounts with the same local name,
allow one Fleet to contain multiple accounts, and let Contexts select a Fleet
and one of its accounts independently.

Keep static Config credentials authoritative. Keep credentials created by
`auth login` out of the Config and cache them by Fleet and User so multiple
Contexts using the same account share one login state.

## Scope

This change:

- moves `users` from the Config root into each Fleet;
- resolves `context.user` only inside the selected Fleet;
- moves OAuth Token caches from Context scope to Fleet/User scope;
- adds optional `--fleet` naming to `auth login`;
- generates collision-free default Context names for multiple accounts;
- updates login, logout, tests, and user documentation.

The change does not support or migrate root-level `users`, old `clusters`,
`context.cluster`, `defaultWorkspace`, or old Context-level Token cache files.
The config API version remains `ksctl.kubesphere.io/v1alpha1`.

## Configuration Schema

The canonical configuration is:

```yaml
apiVersion: ksctl.kubesphere.io/v1alpha1
kind: Config
currentContext: prod-admin
fleets:
  prod:
    host: https://prod.example.com
    users:
      admin:
        username: admin
        password: "<plaintext-password>"
      readonly:
        username: viewer
        bearerTokenFile: /path/to/viewer-token
  staging:
    host: https://staging.example.com
    users:
      admin:
        username: admin
        bearerToken: "<token>"
contexts:
  prod-admin:
    fleet: prod
    user: admin
    defaultCluster: ""
  prod-readonly:
    fleet: prod
    user: readonly
    defaultCluster: ""
  staging-admin:
    fleet: staging
    user: admin
    defaultCluster: ""
```

The Go model is:

- `Config` contains `Fleets` and `Contexts`; the root `Users` field is removed.
- `Fleet` contains `Host`, `TLSClientConfig`, and `Users map[string]User`.
- `User` continues to contain optional `Username`, `BearerToken`,
  `BearerTokenFile`, and `Password` fields.
- `Context` continues to contain `Fleet`, `User`, and `DefaultCluster`.

The User map key is the account name within one Fleet. Different Fleets may
both use `admin` without collision. Within a Fleet, different keys such as
`admin` and `readonly` select distinct accounts. When `username` is empty, the
User map key is also the KubeSphere login username.

All optional empty fields are omitted. An empty `users` map and an all-zero
`tlsClientConfig` block are omitted. `defaultCluster` is always serialized and
defaults to the empty string. Config directory and file permissions remain
`0700` and `0600` respectively.

## Context Resolution

The selected Context is resolved from an explicit Context flag or
`currentContext`. Resolution proceeds in this order:

1. Find `contexts.<context>`.
2. Find `fleets.<context.fleet>`.
3. Find `fleets.<context.fleet>.users.<context.user>`.
4. Resolve the endpoint and TLS configuration from the Fleet.
5. Resolve the username and credentials from the nested User.
6. Apply `context.defaultCluster` only when `--cluster` is empty.

The resolved result carries the Context name, Fleet name, and User name. The
Fleet and User names identify the account's Token cache. An explicit workspace
flag remains supported; there is no Config-backed default workspace.

Missing Context, Fleet, or nested User references return distinct configuration
errors. A Fleet with an empty host returns the existing KubeSphere endpoint
configuration error.

There is no fallback to a root User map.

## Credential Precedence

Credentials are resolved in this order:

```text
--token
> KS_TOKEN
> fleets.<fleet>.users.<user>.bearerTokenFile
> fleets.<fleet>.users.<user>.bearerToken
> Fleet/User Token cache (including Refresh)
> fleets.<fleet>.users.<user>.password OAuth login
> login required
```

Configured Token sources remain authoritative:

- A configured Token File is read and trimmed. Missing, unreadable, and empty
  files return an immediate error.
- Token File wins when both Token File and inline Token are configured.
- A configured Token bypasses cache loading, Refresh, and password OAuth.
- API authorization errors from a configured Token are reported normally and
  do not trigger credential fallback.

When no static Token or usable login cache exists and a Config password is
present, ksctl requests `/oauth/token` using the selected Fleet endpoint, TLS
settings, username, and password. That Access Token is used only by the current
command and is not cached.

The existing secret-log protections remain: OAuth forms use an `io.Reader`,
responses use the streaming path, and unsuccessful OAuth response bodies are
redacted from verbose client logs.

## Fleet/User Token Cache

Explicit login Token responses are stored at:

```text
~/.ksctl/cache/tokens/<safe-fleet>/<safe-user>.json
```

Each Fleet directory uses mode `0700`; each Token file uses mode `0600`. Fleet
and User path components use the existing safe-name transformation and cannot
escape the cache directory.

Contexts that reference the same Fleet and User share the same Access Token,
Refresh Token, expiration, and logout state. Accounts with the same User name
under different Fleets have separate cache paths.

Old `~/.ksctl/cache/tokens/<context>.json` files are not read or migrated.

## Explicit Login

The command shape is:

```text
ksctl auth login ENDPOINT --username USERNAME --password PASSWORD
                         [--fleet FLEET] [--context CONTEXT]
```

Naming rules are:

1. If `--fleet` is set, use it directly.
2. Otherwise, always derive the Fleet name from the current Endpoint Host.
3. Do not inspect an existing Context to select or reuse its Fleet.
4. If `--context` is set, use it directly.
5. Otherwise, generate `<fleet>-<username>` and apply safe-name normalization.

On successful OAuth login, the command:

1. Loads the Config.
2. Merges the selected Fleet rather than replacing it.
3. Updates the Fleet host while preserving its TLS configuration and all other
   accounts.
4. Merges `fleets.<fleet>.users.<username>` and preserves any manually
   configured Token, Token File, or password.
5. Writes the Context with explicit Fleet and User references and an empty
   default cluster.
6. Sets `currentContext`.
7. Saves the OAuth response to the Fleet/User cache.
8. Never persists the password supplied to `auth login`.

The account key created by `auth login` is the supplied username. Manually
written Configs may still use an alias key and a different `username` field.

## Explicit Logout

`ksctl auth logout [CONTEXT]` resolves the named Context or `currentContext`,
then finds its Fleet and User and deletes that Fleet/User Token cache. It does
not modify the Config or any static credentials.

Because login state is account-scoped, logging out one Context also logs out
other Contexts that reference the same Fleet and User. Static Config credentials
continue to work after logout.

## Component Changes

### `pkg/config`

Move Users into Fleet, initialize nested User maps where needed, and update
canonical marshaling so empty nested User maps are omitted. Loading does not map
root Users into any Fleet.

### `pkg/auth`

Resolve Users from the selected Fleet. Carry Fleet and User identities in
`Resolved`. Change the Provider to load, save, and refresh login state using
Fleet/User cache coordinates while retaining the approved credential
precedence.

### `pkg/cache/token`

Change cache path, load, save, and delete functions to accept Fleet and User.
Create the intermediate Fleet directory securely. Do not probe the old
Context-based path.

### `pkg/cmd`

Add `--fleet` to login, implement the approved default naming rules, merge
nested Fleet/User entries, save the Fleet/User cache, and resolve Context before
logout deletes a cache.

### Documentation

Update both READMEs with nested accounts, multi-Fleet `admin` examples,
Fleet/User cache paths, login naming, logout sharing, and the unchanged
credential precedence.

## Testing and Verification

Tests are written before each behavior change and cover:

- two Fleets containing independent `admin` accounts;
- multiple accounts within one Fleet;
- multiple Contexts selecting different accounts within one Fleet;
- explicit and generated Fleet names during login;
- explicit and generated `<fleet>-<username>` Context names;
- proof that existing Contexts never influence implicit Fleet naming;
- login merging without clearing TLS, other accounts, or manual credentials;
- two Contexts sharing one Fleet/User Token cache;
- same-named accounts under different Fleets using separate caches;
- logout deleting one Fleet/User cache without changing Config;
- no root-User or old Context-cache compatibility;
- canonical omission of empty Users and TLS while always emitting
  `defaultCluster`.

Final verification runs:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./... -count=1
env GOCACHE=/private/tmp/ksctl-go-build-cache go build ./...
git diff --check
```
