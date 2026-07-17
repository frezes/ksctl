# ksctl Interactive Login

Date: 2026-07-16

## Summary

Improve `ksctl auth login` so a user can run it without supplying every value
on the command line. In an interactive terminal, the command prompts for the
Endpoint, Username, Password, Fleet, and Context. Fleet and Context prompts
show their derived defaults, and an empty response accepts the displayed
value.

Existing explicit arguments and flags remain supported for automation. When
required input is missing, `--no-interactive` and non-terminal input cause the
command to fail instead of reading from stdin.

Password input is visible in this version. Hidden password entry is outside
the scope of this change.

## Goals

- Let users complete `auth login` through a guided terminal interaction.
- Prompt for missing required values and for optional Fleet and Context
  overrides when explicit values were not supplied.
- Let interactive users inspect or override derived Fleet and Context names.
- Preserve the existing fully specified, non-interactive command behavior.
- Keep prompt collection separate from OAuth, Config, and Token cache writes.
- Keep prompt behavior deterministic and testable without a real terminal.
- Keep the top-level `pkg/cmd` package focused on Cobra orchestration.

## Non-Goals

- No hidden or masked password input.
- No authentication retry loop.
- No browser, device-code, or alternative authentication flow.
- No changes to Fleet or Context naming rules.
- No changes to Config merging, Current Context selection, or Token cache
  storage.
- No prompts for TLS, cluster, workspace, or namespace settings.
- No third-party interactive prompt framework.

## Command Interface

The command accepts zero or one positional Endpoint:

```text
ksctl auth login [ENDPOINT]
```

Existing flags remain available:

```text
--username, -u
--password, -p
--fleet
--context
```

The existing persistent `--no-interactive` flag controls whether missing input
may be prompted for.

## Interactive Flow

When stdin is an interactive terminal and `--no-interactive` is not set, the
command resolves input in this order:

1. Use the positional Endpoint when supplied; otherwise prompt for it.
2. Use `--username` when supplied; otherwise prompt for it.
3. Use `--password` when supplied; otherwise prompt for it.
4. Derive the default Fleet from the resolved Endpoint.
5. If `--fleet` is absent, prompt for Fleet and display the derived default.
   An empty response accepts the default.
6. Derive the default Context from the final Fleet and Username.
7. If `--context` is absent, prompt for Context and display the derived
   default. An empty response accepts the default.
8. Make one OAuth login request with the resolved Endpoint, Username, and
   Password.
9. On success, execute the existing Config merge, Token cache save, Current
   Context update, and success output behavior.

Example:

```text
$ ksctl auth login
endpoint: https://prod.example.com
username: admin
password: temporary-password
fleet [prod.example.com]:
context [prod.example.com-admin]:
Logged in to "prod.example.com-admin"
```

The Password prompt reads an ordinary line and the terminal displays the
entered value. The command never persists the supplied Password.

Explicit values suppress their corresponding prompts. For example, this
command prompts only for Password, Fleet, and Context:

```text
ksctl auth login https://prod.example.com --username admin
```

Fleet and Context remain optional. They are prompted for in interactive mode
so the user can see or override their defaults; they are derived silently in
non-interactive mode.

## Non-Interactive Flow

The command is non-interactive when either condition is true:

- `--no-interactive` is set; or
- stdin is not a terminal.

In non-interactive mode:

- missing Endpoint returns `error: endpoint is required`;
- missing Username returns `error: --username is required`;
- missing Password returns `error: --password is required`;
- omitted Fleet is derived from the Endpoint without prompting;
- omitted Context is derived from the final Fleet and Username without
  prompting.

A fully specified login continues to work when stdin is not a terminal. The
command does not read piped input as credentials.

## Components and Responsibilities

### Login Command

The Cobra command changes its positional argument validation from exactly one
Endpoint to at most one Endpoint. It reads the inherited `--no-interactive`
value and coordinates input resolution before invoking the existing OAuth and
persistence behavior.

The command continues to own Cobra arguments and flags, the OAuth call, Config
and Token cache persistence, and success output. It delegates terminal input
collection, normalization, default-name calculation, and missing-input errors
to the command-specific authentication subpackage.

### Command Authentication Subpackage

Add `pkg/cmd/auth` as a child package with the Go package name `authcmd`, which
distinguishes it from the existing domain-level `pkg/auth` package. This child
package owns:

- the unresolved and resolved login input types;
- the prompt abstraction and terminal-backed implementation;
- interactive availability checks;
- line input and field normalization;
- Fleet and Context default-name calculation; and
- interactive and non-interactive missing-input validation.

The child package accepts standard readers and writers and must not import its
parent `pkg/cmd` package. It does not perform OAuth requests or read or write
Config and Token cache state.

The existing `pkg/cmd/auth.go` imports the child package, passes the relevant
arguments, flags, streams, and `--no-interactive` value to it, then uses the
resolved result in the existing login and persistence flow. Logout behavior
remains in `pkg/cmd/auth.go` and is unchanged.

### Prompt Abstraction

Add a small internal abstraction in `pkg/cmd/auth` with two responsibilities:

- report whether prompting is available for the current input stream;
- print a prompt and read one line of input.

The production implementation is constructed from the readers and writers in
the existing `IOStreams`. A test implementation supplies predetermined
responses and records prompts. The abstraction does not perform OAuth requests
or know about Config and Token cache types.

No full-screen UI or third-party prompt framework is introduced.

### Input Resolution

Input resolution applies explicit arguments and flags first, requests any
allowed interactive values, then computes dependent defaults. In particular,
the Context default is calculated only after the final Fleet value is known.

The resolved values are passed to the existing login request and persistence
path. No Config or Token cache write occurs during prompting.

## Input Normalization

- Endpoint, Username, Fleet, and Context discard leading and trailing
  whitespace.
- Password removes only the line ending produced by interactive input. Other
  Password characters, including leading or trailing spaces, are preserved.
- An empty required value produces its corresponding required error.
- An empty or whitespace-only Fleet response selects the displayed Fleet
  default.
- An empty or whitespace-only Context response selects the displayed Context
  default.
- Endpoint normalization continues to remove trailing `/` characters before
  login and default-name derivation.

Required inputs are prompted for once. Empty input does not start a prompt
loop.

## Output and Errors

Prompts are written to stderr so stdout remains suitable for command results.
The existing success message remains on stdout:

```text
Logged in to "<context>"
```

EOF before any response and other read failures return an error that identifies
the field being read. A final response that contains data but has no trailing
newline is accepted. OAuth errors are returned without retry. If OAuth fails,
the command does not modify Config or Token cache state.

The OAuth layer's existing secret-redaction behavior remains unchanged.

## Testing

Focused command tests cover:

- a fully interactive login that supplies all five prompt responses;
- explicit Endpoint or flags suppressing their corresponding prompts;
- the displayed Fleet default being accepted with an empty response;
- a custom Fleet changing the displayed Context default;
- the displayed Context default being accepted with an empty response;
- custom Fleet and Context responses overriding their defaults;
- required interactive input being empty;
- EOF and read errors identifying the affected field;
- `--no-interactive` rejecting each missing required input;
- non-terminal input rejecting missing required input without reading it;
- fully specified non-interactive login preserving current behavior;
- omitted Fleet and Context being derived silently in non-interactive mode;
- OAuth failure making exactly one request and leaving Config and Token cache
  unchanged;
- prompts going to stderr and the success message going to stdout.

Existing tests continue to verify Config merging, derived naming, sensitive
data handling, and Token cache persistence.

Unit tests in `pkg/cmd/auth` cover input ordering, normalization, defaults,
terminal availability, EOF handling, and validation. Integration tests in
`pkg/cmd` cover the resolved input reaching OAuth and the existing persistence
path.

## Documentation

Update the English and Chinese README login examples to show the interactive
form, explain the displayed Fleet and Context defaults, state that Password
input is visible in this version, and retain a fully specified example for
automation and `--no-interactive` usage.
