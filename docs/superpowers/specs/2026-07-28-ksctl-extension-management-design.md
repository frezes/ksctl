# ksctl Extension Management

## Goal

Add a first-class `extension` command group that turns the KubeSphere
extension-management workflow into a safe, scriptable CLI. The command group
covers discovery, inspection, installation, status tracking, upgrade,
configuration, uninstall, dependency validation, and diagnosis.

The implementation follows the KubeSphere extension-management skill and the
KubeSphere 4.2.1 API contract. It manages the host cluster's
`kubesphere.io/v1alpha1` resources rather than running Helm directly:

- `Extension`
- `ExtensionVersion`
- `InstallPlan`

The server remains responsible for reconciling an `InstallPlan` into Helm Jobs
and deployed extension resources.

## Scope

This change adds:

```text
ksctl extension list
ksctl extension show NAME [--version VERSION]
ksctl extension versions NAME
ksctl extension status [NAME]
ksctl extension install NAME --version VERSION
ksctl extension upgrade NAME --version VERSION
ksctl extension configure NAME
ksctl extension uninstall NAME
ksctl extension diagnose NAME
```

The same commands are exposed through `kubectl ks extension ...`.

The command group:

- requires an exact version for install and upgrade;
- validates required external extension dependencies without installing them;
- supports global YAML configuration and per-cluster YAML overrides;
- supports explicit multicluster placement;
- submits lifecycle changes asynchronously by default;
- optionally waits for a terminal state;
- preserves concurrent and unknown server-side fields during updates;
- provides human-readable and structured query output; and
- diagnoses InstallPlan, dependency, Job, Pod, and multicluster status.

## Non-goals

This change does not:

- add an `extension logs` command or read container logs;
- run Helm from the client;
- automatically select a latest or recommended version;
- recursively install missing dependencies;
- manage extension repositories;
- add enable or disable commands;
- store extension state in the ksctl configuration;
- route extension management through a Context's member Cluster;
- change the generic `get` or `describe` commands;
- modify the pinned source snapshots under `staging/`; or
- provide KubeSphere 3.x compatibility.

## Command and Package Architecture

The root command registers a built-in `extension` command alongside `auth`,
`config`, `plugin`, `version`, `get`, and `describe`.

Implementation ownership is divided as follows:

```text
pkg/cmd/extension.go
    Root composition and dependency wiring

pkg/cmd/extension/
    Cobra command construction
    Flags and argument validation
    Stable user-facing output

internal/extension/
    Focused wire models
    Extension REST client
    Lifecycle workflows
    Dependency validation
    Wait and status polling
    Diagnosis
```

`pkg/cmd/extension` consumes a narrow service interface owned by the command
package. It does not construct URLs, interpret InstallPlan states, or contain
dependency logic. `internal/extension` does not write directly to process
streams and does not depend on Cobra.

The internal client uses the existing unversioned KubeSphere REST client
factory. It sends standard Kubernetes API requests to:

```text
/apis/kubesphere.io/v1alpha1/extensions
/apis/kubesphere.io/v1alpha1/extensionversions
/apis/kubesphere.io/v1alpha1/installplans
```

Private wire models contain only fields needed by the workflows. Every query
also retains the complete raw JSON object so JSON and YAML output can preserve
server fields unknown to the CLI. This avoids upgrading the currently pinned
KubeSphere API module solely to obtain newer convenience fields.

## Host Scope and Connection Behavior

Extension management is a Fleet/host operation. It must never inherit member
Cluster routing from the Kubernetes REST client getter.

The command uses the KubeSphere connection getter's unscoped Fleet REST
configuration. It does not call `KubeSphereCluster` when building management
requests. A Context's `defaultCluster` is therefore ignored.

An explicitly supplied root `--cluster` flag is rejected before a request is
made. The error explains that:

- `--clusters` selects multicluster extension placement; and
- `extension diagnose --target-cluster` selects a member Cluster for remote
  Job and Pod inspection.

An explicitly supplied root `--namespace` or `-n` flag is also rejected because
Extension, ExtensionVersion, and InstallPlan are Cluster-scoped resources.

When diagnosis explicitly names a target Cluster, the internal client applies
that Cluster only to select the matching
`status.clusterSchedulingStatuses[cluster]` entry. The extension controller
creates executor Jobs and Pods on the host and passes the member kubeconfig to
the Job as Helm input, so Job and Pod inspection remains host-scoped.

## Resource Identity and Version Selection

An InstallPlan is keyed by the extension name:

```text
metadata.name == spec.extension.name
```

Install and upgrade require:

```text
--version VERSION
```

`VERSION` is treated as an exact, opaque value. ksctl does not rewrite it,
remove a `v` prefix, or substitute `recommendedVersion`. The corresponding
ExtensionVersion must exist and its `spec.version` must equal the requested
value.

Extension and Cluster names are validated as Kubernetes path segments before
the connection is resolved. The ExtensionVersion lookup is constrained by the
`kubesphere.io/extension-ref=<name>` label and then matched by exact
`spec.version`, avoiding assumptions about how the version is encoded in the
resource name.

## Query Commands

### `extension list`

`list` displays available extensions.

Flags:

```text
--category CATEGORY
--installed
-o, --output table|wide|json|yaml
```

`--category` uses the `kubesphere.io/category` label. `--installed` joins the
Extension list with current InstallPlans instead of relying only on
`Extension.status.installedVersion`, which is absent from older KubeSphere 4.x
schemas. An InstallPlan being deleted or reporting `Uninstalled` does not count
as installed.

Default columns:

```text
NAME  CATEGORY  RECOMMENDED  INSTALLED  STATE
```

Wide output adds provider and enabled state when available. Table rows are
sorted by extension name.

### `extension show`

`show NAME` displays extension metadata, available and installed versions,
state, conditions, provider information, and descriptions.

The default field order for an Extension is:

```text
Name
Display Name
Description
Category
Provider
State
Enabled
Installed Version
Recommended Version
Versions
Conditions
```

`show NAME --version VERSION` displays exact version details, including:

- chart location;
- target Namespace;
- installation mode;
- KubeSphere and Kubernetes version constraints; and
- external dependencies.

The default field order for an ExtensionVersion is:

```text
Name
Extension
Version
Category
Installation Mode
Namespace
KubeSphere Version
Kubernetes Version
Chart URL
Dependencies
```

JSON and YAML output return the complete selected server object.

### `extension versions`

`versions NAME` lists ExtensionVersions selected by the
`kubesphere.io/extension-ref=<name>` label.

Default columns:

```text
VERSION  MODE  KS-VERSION  KUBE-VERSION  NAMESPACE
```

Rows use semantic version order when all versions are valid semantic versions;
otherwise they use a stable lexical order.

### `extension status`

`status` lists InstallPlans. `status NAME` displays one InstallPlan with its
host and multicluster status.

Default columns:

```text
NAME  VERSION  ENABLED  STATE  NAMESPACE  JOB
```

`status NAME --watch` polls for the named InstallPlan and prints only state
changes. `--watch` requires a name. It uses `--wait-timeout`, whose default is
`10m`. Explicitly supplying `--wait-timeout` without `--watch` is an error.
Watch output prints `STATE  NAMESPACE  JOB` once, followed by one row for the
initial state and each distinct subsequent state.

JSON and YAML output preserve the complete InstallPlan object. Structured
output is incompatible with `--watch`.

## Configuration and Scheduling Inputs

Install, upgrade, and configure accept:

```text
--config FILE|-
--clusters CLUSTER[,CLUSTER...]
--override CLUSTER=FILE|-
```

`--override` is repeatable. `--config -` or an override whose file is `-` reads
stdin. At most one input in an invocation may consume stdin.

Configuration and overrides must be non-empty, single-document YAML. ksctl
validates syntax before submitting a write. The original YAML text is retained
apart from normalizing trailing line endings; the server remains responsible
for schema-specific validation and merge semantics.

Cluster names are validated and de-duplicated without changing their first
specified order. Scheduling fields are only allowed when the selected
ExtensionVersion has `installationMode: Multicluster`. A HostOnly version
rejects them before a write.

For install:

- omitted configuration remains omitted;
- omitted scheduling remains omitted;
- an override must name a Cluster in `--clusters`.

For upgrade and configure:

- omitted fields preserve their current values;
- `--config` replaces `spec.config`;
- `--clusters` replaces placement with explicit `placement.clusters` and
  removes an existing `clusterSelector`;
- replacing placement removes stale overrides for Clusters no longer placed;
- `--override` sets or replaces the named override;
- an override must name a Cluster in the resulting placement;
- `--remove-override CLUSTER` removes one override;
- `--clear-config` removes `spec.config`; and
- `--clear-cluster-scheduling` removes placement and all overrides.

When the current placement uses only `clusterSelector`, ksctl cannot prove
that a new `--override` target belongs to the selected set. Setting an
override in that state therefore requires `--clusters` in the same invocation
to replace the selector with an explicit Cluster list. Removing an existing
override does not require replacing the selector.

Positive and clearing flags for the same field are mutually exclusive.
`--clear-cluster-scheduling` is incompatible with cluster and override flags.
Install does not expose clearing flags because no prior InstallPlan exists.

## Install Workflow

`extension install NAME --version VERSION` performs:

1. Validate arguments, scope flags, local files, and flag combinations.
2. Get the named Extension.
3. Get the exact ExtensionVersion.
4. Validate installation mode and scheduling inputs.
5. Validate required external dependencies.
6. Confirm that no same-name InstallPlan exists.
7. Create an InstallPlan with:

   ```yaml
   apiVersion: kubesphere.io/v1alpha1
   kind: InstallPlan
   metadata:
     name: <name>
   spec:
     enabled: true
     extension:
       name: <name>
       version: <exact-version>
     upgradeStrategy: Manual
   ```

8. Add config and cluster scheduling only when requested.
9. Return after the create request by default.
10. If `--wait` is present, wait for a terminal state.

If an InstallPlan already exists, install does not mutate it. The error directs
the user to `extension upgrade` or `extension configure`.

## Upgrade Workflow

`extension upgrade NAME --version VERSION`:

1. Gets the current InstallPlan.
2. Gets and validates the exact target ExtensionVersion.
3. Validates required dependencies and requested scheduling changes.
4. Preserves config and scheduling unless their flags request a change.
5. Applies a minimal JSON Merge Patch that changes the exact target version and
   any explicitly requested fields.
6. Returns after the patch by default or waits when `--wait` is present.

Upgrade may set configuration and scheduling in the same operation. It never
changes `spec.extension.name` or `spec.enabled`, and it enforces
`upgradeStrategy: Manual`.

The target version must differ from the current `spec.extension.version`.
A same-version request returns an error directing the user to
`extension configure`, even if configuration flags were also supplied.

Upgrade rejects an InstallPlan that is being deleted or is currently
`Installing`, `Upgrading`, or `Uninstalling`.

The KubeSphere controller does not retry a host `InstallFailed` or
`UpgradeFailed` plan for a version-only change. Upgrading either failed state
therefore requires `--config` or `--clear-config` to produce a real change to
the global `spec.config`; otherwise ksctl rejects the request before writing
and explains that corrected global configuration is required.

The update includes the current `metadata.resourceVersion` as a concurrency
precondition. A conflict is returned to the user rather than retried against
potentially changed intent.

## Configure Workflow

`extension configure NAME` keeps the current extension version and changes
only explicit configuration or scheduling fields.

The command requires at least one of:

- `--config`;
- `--clear-config`;
- `--clusters`;
- `--override`;
- `--remove-override`; or
- `--clear-cluster-scheduling`.

It gets the current InstallPlan and ExtensionVersion, validates the resulting
configuration, enforces `upgradeStrategy: Manual`, and applies a
resourceVersion-guarded JSON Merge Patch.

Configure rejects an InstallPlan that is being deleted or is currently
`Installing`, `Upgrading`, or `Uninstalling`. For a host `InstallFailed` or
`UpgradeFailed` plan, configure requires `--config` or `--clear-config` to
produce a real change to the global `spec.config`; scheduling-only and
same-value configuration requests cannot make the controller retry that
failure and are rejected before writing.

Configure returns after the patch by default or waits when `--wait` is present.

## Uninstall Workflow

`extension uninstall NAME` deletes the same-name InstallPlan without an
interactive confirmation.

The default behavior returns after the delete request is accepted. With
`--wait`, ksctl polls until the InstallPlan returns NotFound. The extension
controller's finalizer remains responsible for completing Helm uninstall work.

If the InstallPlan enters `UninstallFailed` while it still exists, wait returns
an error immediately with the relevant conditions.

Deleting a missing InstallPlan returns a normal NotFound error and does not
claim success.

## Dependency Validation

Before install or upgrade, ksctl evaluates every required
`spec.externalDependencies` entry of the selected ExtensionVersion.

For each required extension dependency, ksctl:

1. finds its InstallPlan;
2. verifies that it is in a successful installed state;
3. reads its installed exact version from `status.version`, falling back to
   `spec.extension.version` only when the status is successful; and
4. checks the declared semantic version constraint.

Missing, unsuccessful, unparsable, or incompatible dependencies stop the
operation before the write request. The error lists every failing dependency,
its required constraint, and its observed state or version.

Optional dependencies are reported by `show` and `diagnose` but do not block a
lifecycle operation. Unknown dependency types are reported as unsupported when
required and ignored with an informational diagnostic when optional. An empty
dependency type has the API-defined default meaning of `extension`.

ksctl never creates dependency InstallPlans automatically.

## Asynchronous and Wait Behavior

Install, upgrade, configure, and uninstall are asynchronous by default.

Accepted asynchronous operations write one stable line:

```text
extension/<name> install requested
extension/<name> upgrade requested
extension/<name> configuration requested
extension/<name> uninstall requested
```

Each lifecycle command supports:

```text
--wait
--wait-timeout 10m
```

Explicitly supplying `--wait-timeout` without `--wait` is an error. Timeouts
must be positive. The root `--request-timeout` continues to control each
individual HTTP request and is independent of the lifecycle wait.

Wait uses Context-aware polling. It emits state transitions to stderr and
writes the final success line to stdout. It does not emit the asynchronous
`requested` line when waiting.

Each distinct transition uses:

```text
extension/<name> state: <state>
```

An empty state is rendered as `<pending>`. Final success uses exactly one of:

```text
extension/<name> installed
extension/<name> upgraded
extension/<name> configured
extension/<name> uninstalled
```

Known successful terminal states are `Installed`, `Upgraded`, and deletion
NotFound as appropriate. Any state whose name ends in `Failed` is terminal
failure. Empty or unknown non-failure states continue until success, failure,
timeout, or Context cancellation.

An update may initially return the terminal status of the previous
reconciliation. The waiter records the create or patch response as its
baseline and does not attribute an unchanged pre-existing failure to the new
operation. It evaluates failure after a subsequent resource or status change;
an immediate new success is still accepted. If the controller never observes
the submitted change, the operation times out instead of returning the stale
failure.

Failure errors include state conditions, target Namespace, and Job name when
available.

## Diagnosis

`extension diagnose NAME` produces a check table without reading logs.

The table header is:

```text
CHECK  STATUS  MESSAGE
```

Status values are `OK`, `INFO`, `WARN`, and `ERROR`. Check names are stable
resource identities such as `extension`, `version`, `install-plan`,
`dependency/<name>`, `job`, `pod/<name>`, `cluster/<name>`, and `clock`.

Checks cover:

- Extension existence and status;
- exact installed ExtensionVersion availability;
- InstallPlan state and conditions;
- required and optional dependencies;
- target Namespace and Job existence;
- Job completion or failure;
- Pods selected by `job-name=<status.jobName>`;
- Pod phase and container termination summaries;
- multicluster scheduling statuses; and
- a Job that completed while its InstallPlan remains in an install or upgrade
  transition.

Default diagnosis inspects the host Job and Pod resources. For a multicluster
extension:

```text
--target-cluster CLUSTER
```

selects one member Cluster's scheduling status and inspects the corresponding
Job and Pods on the host. The target must appear in the
InstallPlan's cluster scheduling statuses. ksctl does not query the member
Cluster for executor Jobs or Pods.

Clock skew is reported only as a possible cause when the evidence matches the
documented symptom, such as a completed Job whose InstallPlan remains in a
transition and inconsistent completion timestamps. The CLI does not claim to
measure or repair node NTP state.

Diagnostics print all completed checks. Definite errors produce a non-zero
exit status after the table is written. Warnings, including possible clock
skew, do not alone make the command fail.

The output includes exact Namespace, Job name, Pod names, and suggested
follow-up `kubectl` commands where further log inspection is useful.

## Output Contract

Human-readable query output is deterministic:

- stable column names;
- stable name or version ordering;
- `<none>` for missing scalar values; and
- one final newline.

JSON output is valid JSON followed by a newline. YAML output is converted from
the complete JSON response and also ends with one newline. Filters apply to
structured list output while preserving unknown fields in retained items.

Ordinary failures produce no successful stdout output. Wait progress belongs
on stderr. Diagnosis is the deliberate exception: its accumulated check table
is useful even when the final exit status is non-zero.

Output writer failures are returned with operation-specific context.

## Validation and Error Handling

Validation happens at the earliest boundary that has enough information:

- Cobra owns argument count and mutually exclusive flag validation.
- The command layer rejects explicit root scope flags and conflicting stdin
  inputs.
- The service validates names, YAML, current resource state, installation
  mode, dependencies, and requested resulting scheduling.
- The server remains authoritative for authorization, admission, and extension
  configuration schema.

Every API error is wrapped with the operation and resource identity while
preserving the underlying error. NotFound, Conflict, Forbidden, and timeout
details remain recognizable.

No operation falls back to another version, silently drops invalid config,
automatically retries a concurrency conflict, or reports a write as successful
before the server accepts it.

## Security Properties

- Configuration and override data are read only for the current invocation and
  are never persisted locally.
- Configuration content is not included in normal output or errors.
- Bearer tokens remain owned by the existing connection and authentication
  layers.
- Explicit Endpoint and Token security rules remain unchanged.
- Resource names are validated before they enter request paths.
- The command does not execute extension-provided code locally.
- Diagnosis does not retrieve logs, Secrets, or rendered Helm values.

## Compatibility

The command supports KubeSphere 4.x servers exposing
`kubesphere.io/v1alpha1` extension resources. Private wire models tolerate
additional response fields and avoid requiring the server to match one exact
Go API snapshot.

The generic forms remain valid:

```text
ksctl get extensions
ksctl describe extension NAME
ksctl get installplans
```

They are not aliases for the new workflow commands and retain kubectl behavior.

Registering built-in `extension` reserves that top-level command path.
Executable plugins named `ksctl-extension` or nested beneath that command can
no longer provide the path. Plugin diagnostics report them as built-in
conflicts.

Both `ksctl` and `kubectl ks` expose identical behavior and examples use the
active Cobra display name.

## Testing

Tests are written and observed failing before production changes.

### Internal REST client tests

`httptest.Server` tests verify:

- exact method and API path for every resource operation;
- category and extension-ref label selectors;
- complete create bodies;
- resourceVersion-guarded Merge Patch bodies;
- delete behavior;
- host scope despite a Context default Cluster;
- host Job and Pod routing even when diagnosis selects a target Cluster;
- bearer token and user-agent propagation;
- non-2xx Kubernetes Status responses;
- malformed JSON and missing required response identity;
- preservation of unknown fields for structured output; and
- output-independent cancellation and request timeouts.

### Workflow tests

Fake client tests cover:

- exact version lookup with no fallback;
- existing InstallPlan rejection during install;
- missing, failed, incompatible, optional, and unknown dependency types;
- HostOnly scheduling rejection;
- Multicluster placement and overrides;
- config preservation, replacement, and clearing;
- placement replacement and stale override removal;
- individual override set and removal;
- selector-only placement requiring explicit clusters before adding an
  override;
- failed host plans requiring a real global configuration change before
  upgrade or configure;
- resourceVersion conflicts;
- async success;
- install, upgrade, configure, and uninstall wait state sequences;
- every failure state;
- timeout and Context cancellation;
- uninstall finalizer completion through NotFound; and
- diagnosis of healthy, failed, pending, multicluster, and possible clock-skew
  scenarios.

### Cobra and composition tests

Command tests cover:

- help and exact argument validation;
- required `--version`;
- output formats and stable tables;
- stdin input and multiple-stdin rejection;
- mutually exclusive flags;
- explicit `--cluster` and `--namespace` rejection before requests;
- `status --watch` name and output constraints;
- `--wait` and `--wait-timeout` behavior;
- no partial stdout for ordinary failures;
- diagnostic exit behavior;
- output writer failures;
- command registration under both entrypoints; and
- built-in precedence over `ksctl-extension` executable plugins.

Focused package tests run during development. The completed implementation runs
formatting, module checks, vet, normal and race tests, and both binary builds
through the repository verification target.

## Documentation

`docs/cli.md` gains:

- the complete extension command reference;
- discovery and inspection examples;
- exact-version install and upgrade examples;
- config, stdin, placement, and override examples;
- asynchronous and `--wait` examples;
- uninstall behavior;
- status watching;
- diagnosis examples and limitations; and
- host-scope guidance.

`docs/design.md` records:

- extension management as an explicit exception to the otherwise read-only
  built-in resource surface;
- the private extension service boundary;
- Fleet/host routing;
- resourceVersion-guarded writes; and
- the plugin command-path compatibility change.

## Acceptance Criteria

The feature is complete when:

- every command in the approved command surface is available through both
  entrypoints;
- install and upgrade require an exact existing version;
- InstallPlans always preserve the name invariant, enabled state, and Manual
  strategy;
- required dependencies block unsafe writes without being auto-installed;
- configuration and multicluster updates have deterministic preserve, replace,
  and clear behavior;
- lifecycle commands are asynchronous unless `--wait` is supplied;
- status and diagnosis expose actionable controller state without reading logs;
- host routing cannot be redirected accidentally by member Cluster defaults;
- structured output preserves unknown server fields;
- ordinary failures do not emit partial success output;
- documentation describes the public contract; and
- the repository verification target passes.
