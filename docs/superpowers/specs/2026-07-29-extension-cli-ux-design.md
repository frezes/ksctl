# ksctl Extension CLI UX Improvements

## Summary

Improve the day-to-day extension workflow without weakening the existing
exact-version, validation, or structured-output contracts.

The change makes `extension install` use the Extension's recommended version
when `--version` is omitted, adds an explicit `--all-clusters` shortcut for
multicluster agent placement, simplifies the default `list` and `show` output,
adds member scheduling status to `show`, and makes `diagnose` problem-focused
by default.

## Goals

- Allow `ksctl extension install NAME` without an explicit version.
- Keep an explicitly supplied version exact and opaque.
- Make installing a multicluster extension agent across the current Fleet
  easier without changing the default host-only behavior.
- Reduce the default information density of human-readable query output.
- Surface `clusterSchedulingStatuses` directly from `extension show`.
- Make healthy and failing diagnoses easier to scan.
- Preserve deterministic output, structured output, and existing exit-code
  behavior.

## Non-Goals

- Do not make all-cluster placement the default.
- Do not dynamically add future Clusters to an existing InstallPlan.
- Do not add interactive Cluster selection.
- Do not change `extension upgrade`, `extension configure`, or their version
  requirements.
- Do not change the KubeSphere extension controller.
- Do not edit the pinned `staging/` sources.
- Do not add color, icons, or terminal-dependent output.

## Install Version Selection

`--version` becomes optional for `extension install` only.

When the flag is omitted, the service gets the named Extension and uses:

```text
Extension.status.recommendedVersion
```

The selected value remains exact and opaque. The service performs the same
controller-compatible ExtensionVersion lookup and identity checks used for an
explicit version. It does not remove a `v` prefix, parse the value as semantic
versioning, or substitute the newest item returned by `extension versions`.

When `status.recommendedVersion` is empty, installation stops before any write
and reports that no recommended version is available. The error directs the
user to:

```text
ksctl extension versions NAME
```

When `--version VERSION` is supplied, it takes precedence over
`status.recommendedVersion` and retains the current behavior.

The resolved version is used consistently in the new InstallPlan,
`Operation.TargetVersion`, accepted-spec validation, wait expectations, and
lifecycle completion output.

## All-Cluster Placement

`extension install` adds:

```text
--all-clusters
```

The flag is explicit and preserves the existing host-only default when neither
`--clusters` nor `--all-clusters` is supplied. `--all-clusters` and
`--clusters` are mutually exclusive.

When `--all-clusters` is present, the service lists:

```text
/apis/cluster.kubesphere.io/v1alpha1/clusters
```

It selects every Cluster that:

- is not being deleted;
- has a `KSCoreReady=True` condition; and
- does not have a `Schedulable=False` condition.

Host Clusters are not removed from the result. Host handling remains the
KubeSphere controller's responsibility. The selected names are sorted
lexically and written as an explicit snapshot to:

```text
spec.clusterScheduling.placement.clusters
```

An explicit snapshot gives the invocation deterministic semantics: Clusters
added to the Fleet later are not silently added to the InstallPlan.

If no eligible Cluster exists, the command fails before creating an
InstallPlan. If the selected ExtensionVersion is not `Multicluster`, existing
installation-mode validation rejects `--all-clusters` with an actionable
error.

`--override CLUSTER=FILE` can be used with `--all-clusters`. Each override must
name a Cluster in the resolved snapshot, using the same validation applied to
explicit `--clusters`.

The internal Cluster model remains deliberately small. It contains only the
metadata and condition fields required for identity, deletion, readiness, and
schedulability decisions rather than importing the complete staged API.

## List Output

The default table removes `TARGET`:

```text
NAME  CATEGORY  RECOMMENDED  INSTALLED  STATE
```

Wide output uses:

```text
NAME  CATEGORY  RECOMMENDED  INSTALLED  STATE  PROVIDER  ENABLED
```

The requested target version remains available from status and structured
resources; it is not rendered by `extension list` in either table mode.
Filtering, ordering, JSON, and YAML behavior remain unchanged.

## Show Output

`extension show NAME` accepts `table`, `wide`, `json`, and `yaml`.

The default table prints only non-empty values from this ordered field set:

```text
Name
Display Name
Description
Category
State
Installed Version
Recommended Version
```

`Name` is always present. Missing optional fields are omitted instead of being
rendered as `<none>`.

Wide output prints the complete existing human-readable detail set:

```text
Name
Display Name
Description
Category
Provider
State
Enabled
Installed Version
Target Version
Recommended Version
Versions
Conditions
```

Wide output preserves `<none>` for unavailable detailed fields so every
detailed field has a stable location.

When the InstallPlan contains member scheduling status, both table modes append
a separately titled section:

```text
clusterSchedulingStatuses

CLUSTER   VERSION  STATE
member-a  1.2.1    Installed
member-b  1.2.1    Installing
```

Rows are sorted by Cluster name. The default section columns are:

```text
CLUSTER  VERSION  STATE
```

Wide output adds:

```text
NAMESPACE  JOB
```

The host installation status remains in the main field table and is not
duplicated in `clusterSchedulingStatuses`.

`extension show NAME --version VERSION` retains its current exact-version
detail table. JSON and YAML continue to return the complete selected server
object and do not include synthesized table sections.

## Diagnose Output

`extension diagnose NAME` becomes problem-focused by default.

If diagnosis completes with no `WARN` or `ERROR` checks, it prints one line:

```text
extension/NAME: healthy (N checks passed)
```

If warnings or errors exist, default output prints the existing
`CHECK / STATUS / MESSAGE` table with only `WARN` and `ERROR` rows, followed by
a deterministic count summary. The original check order is preserved.

The command adds:

```text
--verbose
```

Verbose output prints all `OK`, `INFO`, `WARN`, and `ERROR` rows in their
existing order, followed by the same count summary.

If the service returns an error after accumulating checks, the renderer must
not report the Extension as healthy. It reports that diagnosis is incomplete,
prints any applicable problem or verbose rows, and then returns the original
service error.

The existing diagnosis rules are unchanged:

- `ERROR` checks cause a non-zero diagnosis exit after output is written.
- `WARN` and `INFO` alone do not cause a non-zero exit.
- A service error remains the returned error.
- Existing messages and suggested `kubectl` follow-up commands are preserved.
- Output writer failures remain wrapped with operation-specific context.

## Architecture

The command layer owns Cobra concerns:

- optional `--version`;
- `--all-clusters` flag parsing;
- mutual exclusion with `--clusters`;
- `--verbose` parsing;
- table versus wide output selection; and
- local validation before constructing the service.

The extension service owns server-derived decisions:

- recommended-version resolution;
- exact ExtensionVersion validation;
- Cluster discovery and eligibility;
- deterministic Cluster ordering;
- installation-mode and override validation; and
- creation of the final InstallPlan.

`APIClient` gains `ListClusters`. The REST implementation decodes the minimal
Cluster list while preserving the same contextual error style used by the
existing extension resources.

Output formatting remains in `pkg/cmd/extension/output.go`. The show renderer
receives the selected table format, and diagnosis rendering receives the
Extension name, verbosity, and completion state. No new dependency is needed.

## Data Flow

For `extension install NAME [--version VERSION] [--all-clusters]`:

1. Cobra validates the name, flag combinations, wait flags, and local input.
2. The service gets the Extension.
3. The service uses the explicit version or resolves
   `status.recommendedVersion`.
4. The service gets and validates the exact ExtensionVersion.
5. Existing installation-mode validation rejects all-cluster placement for a
   non-multicluster version.
6. If requested, the service lists Clusters, filters eligible objects, and
   sorts the resulting names.
7. Existing configuration, scheduling, dependency, and InstallPlan
   preflight checks run using the resolved values.
8. The service creates one exact InstallPlan and validates the accepted
   response.
9. The command returns immediately or waits using the resolved operation
   target.

## Error Handling

Validation remains at the earliest boundary with sufficient information:

- Cobra rejects `--clusters` with `--all-clusters`.
- The command rejects local file, stdin, and wait-flag errors before API
  construction where possible.
- The service reports an absent recommended version before ExtensionVersion
  lookup.
- Cluster list and decode errors identify the all-cluster discovery
  operation.
- An empty eligible Cluster set is an error because the user explicitly
  requested all-cluster placement.
- Existing mode, dependency, identity, conflict, and accepted-response errors
  retain their current guarantees.

No fallback silently changes requested scope or version.

## Testing

Implementation follows test-driven development.

Version-selection tests cover:

- omitted version uses `status.recommendedVersion`;
- explicit version takes precedence;
- an empty recommended version returns an actionable error without a write;
- the resolved recommendation still requires the exact ExtensionVersion
  resource and matching `spec.version`; and
- operation, InstallPlan, and wait target use the resolved version.

All-cluster tests cover:

- Cobra mutual exclusion with `--clusters`;
- Cluster REST path and decoding;
- deletion filtering;
- `KSCoreReady=True` inclusion;
- exclusion when KSCoreReady is absent or not True;
- exclusion only when `Schedulable=False`;
- host Cluster inclusion;
- stable lexical ordering;
- empty eligible results;
- HostOnly rejection;
- compatible overrides; and
- InstallPlan placement contents.

Output tests cover:

- default and wide list headers without `TARGET`;
- concise show field order and empty-value omission;
- wide show field order and missing-value rendering;
- `clusterSchedulingStatuses` naming, sorting, and columns;
- unchanged exact-version show output;
- unchanged complete JSON and YAML output; and
- terminal-control escaping and writer errors.

Diagnosis tests cover:

- a healthy one-line summary;
- default WARN/ERROR filtering;
- verbose full check output;
- deterministic status counts;
- incomplete diagnosis without a false healthy result;
- unchanged diagnosis and service exit errors; and
- writer-error propagation.

Focused package tests run during each red-green cycle. Final verification runs:

```text
make verify
```

## Documentation

Update:

- `docs/cli.md` for the new flags, defaults, examples, and output contracts;
- `docs/design.md` for service-owned recommendation and Cluster resolution;
  and
- the existing extension-management design to record the amended behavior.

Command help examples show both:

```text
ksctl extension install NAME
ksctl extension install NAME --all-clusters
```
