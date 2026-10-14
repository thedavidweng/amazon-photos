# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in `amazon-photos` or `aphotos`, please report it privately:

1. **Do not open a public GitHub issue.**
2. Use [GitHub Private Vulnerability Reporting](https://github.com/thedavidweng/amazon-photos/security/advisories/new).
3. Include a clear description of the issue, reproduction steps, and potential impact.
4. You will receive an initial response within 7 days.

## Scope

`amazon-photos` and `aphotos` handle sensitive Amazon Photos browser session cookies (`session-id`, `ubid-*`, `at-*`) and perform file transfers and account mutations. Security issues in scope include:

- Credential leakage (session cookies, `x-amzn-sessionid`, tokens, or pre-signed `contentRedirection` download URLs appearing in stdout, stderr, `--verbose` logs, or `--json` error envelopes).
- Bypass of confirmation gates (`--confirm` on `trash add` or `doctor --write-probe`).
- Path traversal or unsafe file overwrite during `download` (`Content-Disposition` filename handling).
- Insecure file permissions when writing `~/.config/aphotos/config.yaml`.
- TLS verification bypass or routing credentials to untrusted hosts.

## Security Design

- **Central Redaction (`aphotos.Redact`)**: Every SDK error message, CLI error envelope, and `--verbose` diagnostic log passes through `aphotos.Redact` to scrub `session-id`, `ubid-*`, `at-*`, `Cookie`, `x-amzn-sessionid`, and signed query parameters (`X-Amz-Signature`, `X-Amz-Credential`, `X-Amz-Security-Token`, `Signature`, `AWSAccessKeyId`, `tempLink`).
- **Restricted Config Permissions**: `aphotos auth import` writes `config.yaml` atomically with `0600` permissions inside a `0700` directory.
- **Secret Indirection & Stateless Env Vars**: Config profiles support `env:VAR_NAME` indirection, and headless environments can pass `APHOTOS_COOKIES_FILE`, `APHOTOS_SESSION_ID`, `APHOTOS_UBID`, and `APHOTOS_AT` without persisting secrets to disk.
- **TLS Enforced**: Standard HTTPS certificate verification is always enabled by default; `InsecureSkipVerify` is never enabled by the SDK or CLI.
- **Safe Downloads**: Downloaded filenames are sanitized with `SanitizeFilename` (stripping path separators, `..`, Windows reserved characters/device names, and control characters) and verified against expected byte size and MD5 before atomic rename.
- **No Telemetry**: Neither the SDK nor the CLI collects telemetry or contacts any server other than the configured regional Amazon Photos / Drive endpoints.
