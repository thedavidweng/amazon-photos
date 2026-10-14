# Automate Workflows with AI Agents

This guide shows how to invoke `aphotos` safely from unattended scripts and AI coding/operations agents.

## Always Enable `--json`

Pass `--json` (or set `APHOTOS_JSON=1`) so stdout is guaranteed to contain a single versioned JSON envelope (`schema_version: "v1"`):

```sh
export APHOTOS_JSON=1
aphotos usage
```

- `stdout` contains **only** the JSON envelope (`ok`, `data`, `error`, `meta`).
- Progress lines and `--verbose` diagnostics are written exclusively to `stderr` (or suppressed completely with `--quiet`).
- All strings in `stdout` and `stderr` are scrubbed by `aphotos.Redact`.

## Use Secret Indirection Instead of Inline Cookies

Never pass raw cookies on the command line where process listings or agent transcripts could capture them. Use `~/.config/aphotos/config.yaml` with `env:` references:

```yaml
default_profile: agent
profiles:
  agent:
    region: us
    cookies:
      session-id: env:APHOTOS_SESSION_ID
      ubid-main: env:APHOTOS_UBID
      at-main: env:APHOTOS_AT
```

Or point `APHOTOS_COOKIES_FILE` to a `0600` credential file outside the workspace.

## Gate Destructive and Probing Operations

Commands that mutate trash or probe remote writes require explicit flags:

- Preview uploads first with `aphotos upload --dry-run --json`.
- Use `--on-conflict skip` or `--on-conflict fail` (default) so existing files are never overwritten.
- `aphotos trash add` and `aphotos doctor --write-probe` require `--confirm`; without `--confirm` in `--json` mode, they fail immediately with exit code `6` (`CONFIRMATION_REQUIRED`).

## Branch on Deterministic Exit Codes and `error.retryable`

Agents should inspect both the process exit code and the JSON `error` object:

```sh
out=$(aphotos search --year 2025 --json)
code=$?
```

- Exit `0`: Complete success (`"ok": true`).
- Exit `2` (`AUTH_REQUIRED`, `AUTH_FAILED`, `PERMISSION_DENIED`): Stop immediately and request refreshed session cookies from the user. Do not retry HTTP `401`.
- Exit `4` (`NETWORK_ERROR`, `RATE_LIMITED`): Inspect `error.retry_after_ms` and back off before retrying.
- Exit `5` (`PARTIAL_SUCCESS`): Inspect `data.items` to distinguish `"confirmed"`, `"skipped"`, `"unconfirmed"`, and `"failed"` transfers. Do not blindly re-POST `"unconfirmed"` uploads without checking whether the file already landed on the server.

See [JSON Schema and Exit Codes](../reference/json-schema.md) for the complete machine contract.
