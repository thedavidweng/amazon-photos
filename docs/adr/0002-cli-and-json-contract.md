# ADR 0002: CLI Architecture, Credential Store, and Versioned JSON Envelope Contract

## Status

Accepted

## Context

`aphotos` is the command-line interface for the `amazon-photos` Go module. Its callers include interactive users, unattended NAS/server backup scripts, and AI agents. It requires:

1. Strict separation from the zero-dependency `pkg/aphotos` SDK so library consumers do not inherit CLI dependencies unless importing `cmd/aphotos`.
2. Headless, non-interactive credential storage that works identically on macOS, Linux, and Windows without hanging on OS keychain prompts in CI or SSH sessions, while supporting browser cookie exports (Netscape `cookies.txt`, JSON cookie arrays/maps, raw `Cookie` headers) and `env:VAR` secret indirection.
3. A machine-readable `--json` output contract with deterministic exit codes, strict stdout purity (all progress/verbose logs on stderr), and mandatory credential/URL redaction via `aphotos.Redact`.
4. Graded `doctor` diagnostics and explicit confirmation gates (`--confirm`) before destructive or remote-probing operations.

## Decision

1. **CLI Dependencies & Package Boundaries**:
   `cmd/aphotos` and `internal/{cli,config,output,model}` use `github.com/spf13/cobra`, `github.com/spf13/pflag`, and `gopkg.in/yaml.v3`. `pkg/aphotos` remains free of third-party dependencies.
2. **Atomic `0600` Config Store & Multi-Format Cookie Import**:
   `internal/config` persists profiles in `~/.config/aphotos/config.yaml` (or `--config` / `APHOTOS_CONFIG` / `$XDG_CONFIG_HOME/aphotos/config.yaml`) using atomic temporary-file writes (`0600` file permissions inside a `0700` directory). `aphotos auth import` parses Netscape `cookies.txt`, JSON arrays of browser cookie objects, JSON key-value maps, and raw `Cookie:` headers, normalizing underscore/hyphen aliases to canonical browser wire names (`session-id`, `ubid-*`, `at-*`). Any cookie value may use `env:VAR_NAME` indirection, and non-persistent runs may supply `APHOTOS_COOKIES_FILE`, `APHOTOS_SESSION_ID`, `APHOTOS_UBID`, `APHOTOS_AT`, and `APHOTOS_REGION`.
3. **Versioned JSON Envelope & Exit Codes**:
   When `--json` is set, stdout emits a single JSON `Envelope` (`schema_version: "v1"`, `ok`, `data`, `error`, `meta`) with all strings scrubbed by `aphotos.Redact`. All progress and `--verbose` diagnostics write exclusively to stderr. SDK errors map deterministically to exit codes: `0` (success), `1` (`VALIDATION_FAILED` / `INVALID_FILTER`), `2` (`AUTH_REQUIRED` / `AUTH_FAILED` / `PERMISSION_DENIED`), `3` (`API_ERROR` / `REMOTE_PROTOCOL_ERROR` / `RESOURCE_NOT_FOUND` / `CONFLICT`), `4` (`NETWORK_ERROR` / `RATE_LIMITED`), `5` (`PARTIAL_SUCCESS`), `6` (`CONFIRMATION_REQUIRED` / `READ_ONLY_VIOLATION`), `7` (`FILESYSTEM_ERROR`), `8` (`CONFIG_ERROR`), and `130` (`INTERRUPTED`).
4. **Safety Gates & Graded `doctor`**:
   `trash add` and `doctor --write-probe` require `--confirm` (exiting with code `6` `CONFIRMATION_REQUIRED` otherwise). Permanent purge is omitted from v1. `doctor` runs graded checks (config/permissions, cookie/region resolution, account root/usage, search read probe, and opt-in write probe) and supports `--allow-unconfigured` for CI environments without credentials.

## Consequences

### Positive

- Scripts and agents can parse `--json` output from stdout without ANSI codes, spinners, or credential leaks, and branch on deterministic exit codes.
- Headless environments can use `env:VAR_NAME` indirection or `APHOTOS_*` environment variables without writing plaintext cookies to disk.
- CLI end-to-end tests execute the full Cobra command tree in-process against an injected `http.RoundTripper` without live network calls.

### Negative

- Local config files rely on POSIX `0600`/`0700` permissions (or `env:VAR_NAME` indirection) rather than interactive OS keychains.
