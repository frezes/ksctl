# ksctl Login Interaction Trigger

Date: 2026-07-17

## Summary

Remove the global `--insecure-skip-tls-verify` and `--no-interactive` flags,
and make `ksctl auth login` enter its interactive flow only when Endpoint,
Username, or Password is initially missing.

When all three required login values are supplied, the command logs in without
any prompts. Omitted Fleet and Context names are derived silently. When any
required value is missing and stdin is an interactive terminal, the command
prompts for the missing required values and then continues to prompt for Fleet
and Context. When stdin is not interactive, missing required values produce the
existing field-specific errors.

## Goals

- Remove `--insecure-skip-tls-verify` and `--no-interactive` from the root CLI
  interface and help output.
- Make a fully specified login suitable for automation without requiring an
  explicit non-interactive flag.
- Preserve the existing complete guided login when required input is missing.
- Keep Fleet and Context override prompts available during a guided login.
- Preserve config-based TLS behavior and existing login persistence behavior.

## Non-Goals

- No change to OAuth requests, credential precedence, token cache storage, or
  Config merging.
- No change to Fleet or Context naming rules.
- No hidden or masked Password input.
- No retry loop for empty prompt responses or failed authentication.
- No new TLS flag on `auth login` or another subcommand.
- No removal of config-based `tlsClientConfig.insecure` support.

## CLI Interface

The root command no longer registers these persistent flags:

```text
--insecure-skip-tls-verify
--no-interactive
```

The remaining connection flags are unchanged. TLS certificate validation can
still be disabled explicitly in a Fleet's `tlsClientConfig.insecure` setting.

The login command continues to accept zero or one positional Endpoint and the
existing login-specific flags:

```text
ksctl auth login [ENDPOINT]

--username, -u
--password, -p
--fleet
--context
```

## Interaction Decision

The login input resolver normalizes the initial Endpoint and Username, then
records whether any required login value is missing:

```text
guided = endpoint is empty OR username is empty OR password is empty
```

Prompting is enabled only when `guided` is true and the input stream is an
interactive terminal. This decision is based on the initial required values;
it does not change after prompts fill them.

If `guided` is false:

1. Do not prompt for any field.
2. Derive Fleet from Endpoint when `--fleet` is absent.
3. Derive Context from the final Fleet and Username when `--context` is absent.
4. Continue directly to OAuth login and existing persistence.

If `guided` is true and prompting is available:

1. Prompt for each missing required value in Endpoint, Username, Password
   order.
2. Derive the default Fleet and prompt for Fleet when `--fleet` is absent.
3. Derive the default Context from the final Fleet and Username and prompt for
   Context when `--context` is absent.
4. Continue to OAuth login and existing persistence.

Explicit required values and explicit Fleet or Context values suppress their
corresponding prompts during the guided flow.

If `guided` is true but stdin is not an interactive terminal, the resolver
does not read stdin and returns the first applicable existing required-value
error:

```text
error: endpoint is required
error: --username is required
error: --password is required
```

## Behavior Examples

A complete command performs no interaction and derives names silently:

```text
ksctl auth login https://prod.example.com -u admin -p temporary-password
Logged in to "prod.example.com-admin"
```

A command missing only Password prompts for Password, then offers Fleet and
Context overrides:

```text
ksctl auth login https://prod.example.com -u admin
password: temporary-password
fleet [prod.example.com]:
context [prod.example.com-admin]:
Logged in to "prod.example.com-admin"
```

A command missing Username and Password prompts for both before the optional
names:

```text
ksctl auth login https://prod.example.com
username: admin
password: temporary-password
fleet [prod.example.com]:
context [prod.example.com-admin]:
Logged in to "prod.example.com-admin"
```

Running `ksctl auth login` without arguments keeps the current complete guided
flow, including the Endpoint prompt.

## Components

### Root Command

`pkg/cmd/root.go` stops binding the two removed flags. Existing connection
resolution remains config-driven where no corresponding CLI override exists.

### Login Command

`pkg/cmd/auth.go` no longer looks up or forwards `--no-interactive`. It passes
the login arguments, flags, and terminal prompter to the input resolver, then
retains the existing OAuth, Config, token cache, and success-output path.

### Login Input Resolver

`pkg/cmd/auth/input.go` removes the `NoInteractive` option and determines the
guided-flow state from the initially supplied required fields. The resolver
continues to own normalization, missing-value errors, prompt ordering, and
Fleet and Context defaults.

Internal authentication and Kubernetes client structs may retain non-CLI TLS
fields where they are still used to carry resolved config into REST clients.
The change removes the user-facing global override, not config-based TLS
support.

## Error Handling

- Empty required prompt responses return the existing required-value error.
- Prompt read failures continue to identify the affected field.
- Missing required input on non-terminal stdin fails without reading piped
  data.
- OAuth and persistence errors are unchanged.
- A removed flag is rejected by Cobra as unknown.

## Testing

Focused tests verify:

- the root command does not register either removed persistent flag;
- commands and tests no longer rely on `--no-interactive`;
- Endpoint, Username, and Password supplied initially cause zero prompts while
  deriving omitted Fleet and Context names;
- missing only Password prompts for Password, Fleet, and Context;
- missing Username and Password prompts for both, then Fleet and Context;
- missing Endpoint preserves the complete guided flow;
- explicit Fleet and Context values suppress their prompts during a guided
  flow;
- non-terminal missing input returns the existing required-value error without
  prompting;
- config-based insecure TLS settings still reach REST clients; and
- the existing OAuth request, Config merge, token cache, and sensitive-data
  tests remain green.

## Documentation

Update the English and Chinese READMEs to remove both flags from the global
flag tables and automation examples. Explain that a complete Endpoint,
Username, and Password command is automatically non-interactive, while missing
required login input triggers the guided flow on an interactive terminal.
Retain the config documentation for `tlsClientConfig.insecure`.
