# ksctl Executable Plugin Mechanism

Date: 2026-07-20

## Summary

Add a kubectl-compatible executable plugin mechanism to both ksctl entrypoints.
A plugin is an executable named `ksctl-<name>` that is available on `PATH`.
Both `ksctl <name>` and `kubectl ks <name>` resolve and execute the same plugin,
so ksctl has one plugin naming scheme and one plugin ecosystem.

The implementation reuses kubectl v0.36.2's exported plugin lookup and
execution behavior. ksctl owns a small dispatch adapter and a ksctl-specific
`plugin list` command because kubectl's list command hard-codes the `kubectl-`
prefix and kubectl-specific messages.

## Goals

- Invoke `ksctl-*` executables as unknown top-level ksctl commands.
- Use the same plugins through the standalone `ksctl` binary and the existing
  `kubectl-ks` entrypoint.
- Preserve kubectl's longest executable name matching behavior.
- Pass plugin arguments, environment, standard input, standard output, and
  standard error through without ksctl-specific interpretation.
- Keep built-in commands authoritative and prevent plugins from replacing or
  extending them.
- Provide `plugin list` with kubectl-compatible PATH scanning and diagnostics.
- Keep command construction injectable so dispatch and listing behavior can be
  tested without replacing the test process.

## Non-Goals

- No plugin installer, remote index, or Krew compatibility layer.
- No plugin manifest, registration database, or ksctl configuration entries.
- No version negotiation or compatibility metadata.
- No in-process Go plugin loading.
- No ksctl-specific environment variables or automatic connection flag
  translation for plugins.
- No plugin override of built-in commands.
- No plugin extension below existing commands such as `auth`, `config`, `get`,
  or `describe`.

## User Experience

An executable named `ksctl-foo` on `PATH` provides both forms:

```text
ksctl foo [arguments and flags]
kubectl ks foo [arguments and flags]
```

Nested command words are joined with dashes. Given `ksctl-foo-bar` and
`ksctl-foo`, invocation uses the longest executable name that exists:

```text
ksctl foo bar value       -> ksctl-foo-bar value
ksctl foo other value     -> ksctl-foo other value
```

As in kubectl, dashes in command words are changed to underscores while
forming executable names. For example, `ksctl foo-bar` can resolve
`ksctl-foo_bar`.

The plugin name must precede flags. `ksctl --context prod foo` is rejected
instead of invoking a plugin. `ksctl foo --context prod` invokes `ksctl-foo`
and passes `--context prod` to that executable; the plugin is responsible for
parsing and validating all of its arguments.

The built-in command tree always wins. `ksctl version` runs the built-in
version command even if `ksctl-version` exists. Likewise, an executable such
as `ksctl-auth-login` cannot replace or extend `ksctl auth login`.

## Command Construction and Dispatch

`pkg/cmd` continues to own the Cobra command tree. It adds a construction path
that accepts the raw process arguments and a kubectl `PluginHandler`. The
existing constructors remain available for package callers and tests that only
need a Cobra tree.

The two binary entrypoints construct a kubectl `DefaultPluginHandler` with
`[]string{"ksctl"}` and provide `os.Args`. Consequently, `cmd/ksctl` and
`cmd/kubectl-ks` use identical plugin lookup even though their Cobra display
names differ.

Dispatch happens before Cobra parses arguments:

1. Build the complete ksctl command tree, including `plugin list`.
2. Inspect the raw command path with Cobra's `Find` behavior.
3. If the path resolves to a built-in command, continue normal Cobra
   execution without looking for a plugin.
4. If the top-level command is unknown, call kubectl's
   `HandlePluginCommand` with the raw arguments and a minimum match length of
   one command word.
5. If a plugin is found, kubectl's handler replaces the process on Unix or
   runs the child process and exits on Windows.
6. If no plugin is found, continue normal Cobra execution so the existing
   unknown-command error and suggestions are preserved.

Help and Cobra completion requests do not trigger plugin execution. ksctl does
not adopt kubectl's special-case support for extending `create`, because ksctl
has no built-in `create` command and plugins are limited to unknown top-level
commands.

The dispatch adapter reports lookup or execution errors through the entrypoint
and preserves a non-zero exit. It does not consume, reorder, normalize, or
inject plugin arguments. The default kubectl handler inherits `os.Environ()`
and the process standard streams.

## Plugin List Command

The root command registers:

```text
ksctl plugin list
kubectl ks plugin list
```

The listing implementation lives in `pkg/cmd/plugin`, mirroring the upstream
package boundary while using ksctl naming and messages. It does not copy the
kubectl execution handler.

`plugin list` performs these steps:

1. Split `PATH` with `filepath.SplitList`.
2. Remove duplicate directories without changing their order and ignore blank
   entries.
3. Read each directory in PATH order and select non-directory entries whose
   names begin with `ksctl-`.
4. Print matching paths in PATH and directory-entry order. By default it
   prints full paths; `--name-only` prints only basenames.
5. Verify each candidate and print all warnings to the error stream.

The verifier reports:

- a `ksctl-` candidate that is not executable;
- inability to determine whether a candidate is executable;
- a later candidate with the same basename that is shadowed by an earlier PATH
  entry; and
- a plugin name whose command path resolves to an existing built-in command.

On Unix, executable status is determined from executable mode bits. On
Windows, the supported executable extensions follow kubectl's behavior.

Warnings are aggregated. One or more verification warnings cause a non-zero
result after all candidates have been printed and checked. If no candidates
are found, the command returns an explicit error. An unreadable PATH directory
is reported and skipped so later directories are still scanned; other scan
errors are aggregated and returned after scanning.

## Package Responsibilities

```text
cmd/ksctl
cmd/kubectl-ks
    construct the default ksctl handler and provide raw process arguments

pkg/cmd
    build the root command, decide whether plugin dispatch is eligible,
    register the plugin utility command, and preserve entrypoint display names

pkg/cmd/plugin
    build `plugin list`, scan PATH, and diagnose executable, shadowing, and
    built-in command conflicts

k8s.io/kubectl/pkg/cmd
    provide PluginHandler, DefaultPluginHandler, executable lookup, longest
    name matching, argument/environment forwarding, and process execution
```

No new module dependency is required because ksctl already depends on
`k8s.io/kubectl` v0.36.2.

## Error and Security Behavior

- A missing plugin falls through to the existing Cobra unknown-command error.
- A flag before an unknown plugin name returns kubectl's explicit
  flags-before-plugin-name error.
- Plugin process failures remain failures and are not converted to ksctl API
  errors.
- Built-in command parse or runtime errors never trigger a plugin fallback.
- Plugin discovery does not execute candidates.
- Plugins are arbitrary local executables and run with the user's inherited
  environment and privileges. Documentation must state that ksctl does not
  audit or sandbox them.
- ksctl does not inject resolved endpoints, tokens, contexts, or other private
  environment data. Existing variables already present in the process are
  inherited unchanged.

## Testing

Dispatch tests use an injected fake `PluginHandler` to verify:

- longest-name lookup and fallback;
- dash-to-underscore conversion;
- exact remaining arguments and inherited environment;
- rejection of flags before a plugin name;
- built-in command precedence;
- no plugin fallback for invalid built-in invocations;
- unknown-command behavior when lookup finds nothing; and
- identical `ksctl-*` lookup for the `ksctl` and `kubectl ks` entrypoints.

Listing tests use temporary PATH directories and files to verify:

- PATH order and duplicate PATH removal;
- full-path output and `--name-only` output;
- executable and non-executable candidates;
- same-name shadowing;
- built-in top-level and nested command conflicts;
- empty results;
- unreadable or missing PATH directories; and
- aggregation of multiple warnings.

A subprocess integration test places a temporary executable on `PATH` and
invokes the ksctl entrypoint to verify real argument, environment, standard
stream, and exit-status behavior without allowing `exec` to replace the Go
test process. Platform-specific executable details are isolated or skipped
where the test host cannot represent them.

The existing command, authentication, configuration, resource, race, and
build checks remain part of `make verify`.

## Documentation

Update `README.md` and `README_zh.md` with:

- the `ksctl-<name>` naming and PATH installation convention;
- standalone and `kubectl ks` invocation examples;
- nested names and longest-match behavior;
- raw argument and environment forwarding;
- `plugin list` and `--name-only` usage;
- built-in command precedence and flags-before-name limitation; and
- the arbitrary-code security warning.

Add an Unreleased entry to `CHANGELOG.md` describing executable plugin support
and `plugin list`. The user-facing behavior should link to the Kubernetes
kubectl plugin documentation as the compatibility reference:
<https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/>.

## Acceptance Criteria

- An executable `ksctl-demo` on `PATH` runs through both `ksctl demo` and
  `kubectl ks demo`.
- Nested invocation selects the longest available executable name and passes
  unmatched arguments unchanged.
- Built-in commands cannot be overridden or extended by plugin executables.
- `ksctl plugin list` and `kubectl ks plugin list` report the same `ksctl-*`
  candidates, support `--name-only`, and return non-zero for diagnostic
  warnings.
- Missing plugins retain ksctl's existing unknown-command behavior.
- All new and existing tests pass, including race tests and both binary builds.
