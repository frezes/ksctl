# ksctl Dynamic Resource Discovery Design

## Problem

`ksctl get` delegates command parsing and output to kubectl, but its
`RESTClientGetter` does not preserve kubectl's resource resolution semantics.
The current KubeSphere core-v1 discovery fallback has two regressions:

- when a core mapper can be built, `ToRESTMapper` returns before wrapping the
  mapper with kubectl's shortcut expander, so server-advertised short names such
  as `po` are not resolved consistently;
- the Console endpoint proxies concrete paths such as `/api/v1`,
  `/apis/apps/v1`, and `/apis/<custom-group>/<version>`, but serves frontend
  HTML for the `/api` and `/apis` discovery roots. The existing fallback only
  exposes core v1, so every non-core built-in resource and CRD is discarded.

## Approaches Considered

### 1. Preserve kubectl's mapper and repair discovery input (selected)

Keep kubectl's standard deferred discovery mapper and shortcut expander. Repair
the KubeSphere-specific discovery adapter so it reconstructs discovery from the
leaf endpoints that the Console proxy exposes: registered Kubernetes group
versions, CRD definitions, and APIService definitions.

This keeps aliases, singular/plural names, qualified group names, and CRDs
driven by Kubernetes metadata. It restores kubectl semantics without a static
table of resource names or aliases.

### 2. Add hard-coded aliases

Translate common inputs such as `po` to `pods` before invoking kubectl. This is
small but incomplete: it cannot cover server-defined short names or arbitrary
custom resources and would drift from kubectl.

### 3. Maintain separate static and dynamic mappers

Keep the explicit core mapper before the deferred mapper and add more fallback
rules for custom groups. This preserves the existing structure but duplicates
client-go discovery behavior and makes ordering and partial-failure handling
harder to reason about.

## Design

### Discovery

The fallback discovery client first uses ordinary client-go discovery. If that
succeeds, behavior is unchanged. When either discovery root is unusable, it
builds candidate group versions from three generic sources:

1. core v1 and the Kubernetes group versions registered by the matching
   client-go release;
2. every served version in the cluster's CRD list;
3. standard Kubernetes and KubeSphere APIService definitions, when those lists
   are accessible.

For CRDs, the definition already contains plural, singular, kind, scope,
shortNames, and categories, so the adapter synthesizes the corresponding
`APIResourceList` without probing every custom group. Other candidates are
validated through their concrete group/version discovery endpoints in parallel,
matching client-go's discovery strategy. Only successful group versions are
returned, and their resource lists are reused by the surrounding memory cache.
If no leaf endpoint provides usable discovery, the original error is returned.

### REST mapping

`RESTClientGetter.ToRESTMapper` follows kubectl's standard construction:

1. create `DeferredDiscoveryRESTMapper` from the cached discovery client;
2. always wrap it with `NewShortcutExpander`.

The separate eager core mapper is removed because the repaired discovery client
now exposes core v1 and non-core groups through one coherent discovery surface.

### Compatibility and errors

- No resource names, aliases, or custom API groups are hard-coded.
- Existing authentication, KubeSphere endpoint paths, namespace handling, and
  output formatting remain unchanged.
- Normal Kubernetes discovery remains the primary path; fallback logic runs only
  when ordinary group discovery fails.
- Inaccessible CRD or APIService lists reduce only those optional discovery
  sources; core and built-in group probing can still succeed.

## Testing

Tests will exercise behavior through the real kubectl command construction and
the `RESTClientGetter`:

- `get po` resolves the `po` short name advertised by core-v1 discovery and
  requests the pods endpoint;
- a non-core built-in short name such as `deploy` resolves through a concrete
  group/version discovery endpoint;
- an arbitrary CRD resolves from its definition and is fetched while both
  discovery roots return KubeSphere Console HTML;
- the existing core-v1 fallback mapping remains functional;
- focused package tests, the full repository test suite, and `git diff --check`
  pass.

## Scope

This change only repairs discovery and REST mapping used by the existing native
kubectl `get` and `describe` commands. It does not add commands, specialized
describers, static resource tables, or output behavior.
