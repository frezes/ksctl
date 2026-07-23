# ksctl Connection and Persistence Boundary Hardening

## Goal

Make connection credentials endpoint-bound, make explicit login failures
compensable, enforce the versioned configuration contract, remove the unused
interactive option, and give KubeSphere TLS conversion one owner.

## Scope

This change:

- requires an explicit Token whenever `--endpoint` or `KS_ENDPOINT` selects an
  Endpoint;
- prevents `auth login --fleet NAME` from silently rebinding an existing Fleet
  to another Endpoint;
- makes a returned login persistence error restore the previous Config and
  Token cache state;
- strictly decodes and validates the current Config schema;
- removes the unused `NoInteractive` fields and propagation; and
- centralizes KubeSphere REST TLS conversion in one internal adapter.

This change does not add a new Config version, migrate legacy Config fields,
change credential precedence for Context-backed connections, change the
Fleet/User Token cache path, add cross-process locking, or introduce a general
transaction framework.

## Connection Identity

A connection identity consists of an Endpoint and the credential authorized for
that Endpoint. An explicit Endpoint must not be combined with credentials owned
by a selected Fleet.

`pkg/auth.Resolve` applies these rules:

1. Resolve the explicit Endpoint from `--endpoint`, then `KS_ENDPOINT`.
2. Resolve the explicit Token from `--token`, then `KS_TOKEN`.
3. If an explicit Endpoint exists without an explicit Token, return an error
   before resolving Context credential sources.
4. If an explicit Endpoint and Token both exist, use them as one connection
   identity. An explicitly selected Context may still supply identity and
   default Cluster metadata to commands that require it, but its credential
   sources never override the explicit Token.
5. If no explicit Endpoint exists, resolve Endpoint, TLS settings, username,
   configured credentials, and Token cache identity from the selected Context's
   Fleet and User as before.

Supplying only `KS_TOKEN` remains valid: the selected Fleet supplies the
Endpoint and TLS settings while the environment supplies the credential.

The error for an unpaired explicit Endpoint names both accepted Token sources so
interactive users and automation receive the same actionable contract.

## Fleet Endpoint Ownership

A Fleet name owns one Endpoint. Explicit login may merge Users into an existing
Fleet only when the normalized login Endpoint equals the Fleet Host. Endpoint
normalization trims surrounding whitespace and trailing `/` characters, matching
the existing login input normalization.

If the Fleet exists with a different non-empty Host, login stops before the
OAuth request and tells the user to select another Fleet name. A Fleet with an
empty Host may acquire the login Endpoint.

This rule keeps the Fleet/User Token cache coordinate bound to one server. It
also makes cache-first login persistence safe: replacing an existing cache entry
cannot put a Token from a different Endpoint behind the same Fleet/User key.

## Login Persistence

`auth login` separates remote authentication from local state commit:

1. Resolve login input.
2. Load the current Config and validate the Fleet Endpoint ownership rule.
3. Perform the OAuth password grant.
4. Build the updated Config in memory without mutating persisted state.
5. Capture the previous Fleet/User Token cache contents.
6. Atomically save the new Token cache entry.
7. Atomically save the updated Config.
8. If Config save fails, restore the exact previous cache contents or remove the
   newly created cache entry, then return the Config error joined with any
   compensation error.
9. Print success only after both saves complete.

If the process stops between the two saves, Config remains unchanged. For an
existing Fleet/User, the new cache Token is for the same Endpoint and remains a
valid credential candidate. For a new Fleet/User, the cache entry is unreachable
from Config and may be replaced by a later successful login. No interruption can
change the selected Context or bind a Token to another Endpoint.

The Token cache package owns capture and restoration of its file so command
wiring does not duplicate secure-file or path behavior. It exposes
`SaveWithRollback(dir, fleet, user string, entry Entry) (rollback func() error,
err error)`. The function captures the exact previous bytes, saves the new
entry, and returns an idempotent rollback closure that restores the old bytes or
deletes a newly created entry. This preserves malformed pre-existing bytes as
well as valid entries during compensation.

## Configuration Contract

`pkg/config` remains the only owner of the serialized Config schema.

Loading behavior:

- a missing or empty file still returns an initialized current Config;
- absent `apiVersion` and `kind` still receive the current defaults;
- a present `apiVersion` must equal `ksctl.kubesphere.io/v1alpha1`;
- a present `kind` must equal `Config`;
- unknown fields, duplicate fields, type mismatches, unsupported versions, and
  unsupported kinds return an error; and
- legacy root fields such as `clusters` and `users` are rejected rather than
  silently ignored.

Saving behavior:

- empty `apiVersion` and `kind` receive current defaults;
- non-empty unsupported values are rejected before writing; and
- no command may rewrite a Config version it does not understand.

This turns the version marker into a guard: an older binary cannot load a future
schema, drop fields it does not know, and write the reduced projection back.

## Internal Surface Cleanup

`NoInteractive` is removed from `pkg/client.Options`,
`pkg/auth.ResolveInput`, all getter propagation, and tests. Interactive behavior
is owned solely by `pkg/cmd/auth` and its terminal prompter.

One new internal adapter owns conversion from `config.TLSClientConfig` to
`kubesphere.io/client-go/rest.TLSClientConfig`. Both OAuth and the KubeSphere
connection getter use it. The Kubernetes conversion remains separate because it
targets a different upstream type and package boundary.

The adapter is internal so this cleanup does not create a new supported public
Go API.

## Error and Compatibility Behavior

- Existing Context-only commands keep their credential precedence and routing.
- Explicit Endpoint plus explicit Token continues to work without Config.
- Explicit Endpoint without explicit Token now fails even when a current Context
  exists; this is an intentional security tightening.
- Login to an existing Fleet at the same normalized Endpoint keeps merging its
  TLS settings, Users, and manually configured credentials.
- Login to an existing Fleet at another Endpoint fails without local mutation or
  an OAuth request.
- Configs containing unknown or legacy fields now fail to load. The error must
  identify the unsupported field or contract value.
- A failed compensation is reported together with the original persistence
  failure; it is never silently discarded.

## Testing

Tests are written and observed failing before each production change.

Authentication resolution tests cover:

- `--endpoint` without `--token`;
- `KS_ENDPOINT` without `KS_TOKEN`;
- explicit Endpoint and Token without Config;
- `KS_TOKEN` with a Context-provided Endpoint; and
- proof that configured, cached, refresh, and password credentials are not
  consulted for an unpaired explicit Endpoint.

Login tests cover:

- rejection of an existing Fleet bound to another Endpoint before OAuth;
- same-Endpoint login merging existing Fleet state;
- cache-save failure leaving Config unchanged;
- Config-save failure restoring exact prior cache bytes;
- Config-save failure removing a newly created cache entry; and
- success output appearing only after both state writes complete.

Config tests cover:

- current version and kind;
- defaulting absent version and kind;
- unsupported version and kind;
- unknown and duplicate fields;
- rejection of legacy root fields; and
- refusal to save unsupported contract values.

Internal adapter tests cover every KubeSphere TLS field and the insecure
override. Existing package tests and the repository verification target guard
behavior outside these focused cases.

## Documentation

The CLI guide documents the explicit Endpoint/Token pairing, immutable Fleet
Endpoint behavior during login, and strict Config rejection. The current
architecture document records the same connection, persistence, schema, and TLS
ownership boundaries. Historical specifications remain unchanged.
