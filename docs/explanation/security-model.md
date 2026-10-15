# Authentication and Credential Security Model

This page explains how `amazon-photos` handles browser session cookies, file permissions, secret indirection, cross-host redirects, and output redaction.

## Interactive Sign-in

`aphotos auth login` accepts a hidden Cookie request header in a terminal. It
keeps only the resolved marketplace's three required cookies and performs a
read-only root request before replacing the selected profile. Prompts go to
stderr; neither success nor error output includes the pasted secret. It does not
capture passwords, launch browser automation, or provide OAuth authorization.
The terminal returns to its previous mode after each prompt, including on cancel.

## Browser Session Cookie Authentication

Because Amazon Photos does not offer public OAuth tokens or API keys, requests authenticate using three session cookies established by signing in to Amazon Photos in a web browser:

1. `session-id`: Also sent in the required `x-amzn-sessionid` request header.
2. `ubid-main` (US) or `ubid-acb<tld>` (regional marketplaces).
3. `at-main` (US) or `at-acb<tld>` (regional marketplaces).

These cookies carry full account session authority and must be treated with the same care as passwords or bearer tokens.

## Canonical Wire Normalization

Community tools and cookie exporters sometimes rename hyphens to underscores (`session_id`, `ubid_main`, `at_main`). If both underscore and hyphenated cookies are sent on the wire, or if only underscore names are sent, requests can fail unpredictably.

`aphotos.CanonicalCookieKey` normalizes all recognized auth cookie aliases to their canonical hyphenated wire names at client construction time and rejects conflicting duplicate values with `*ConfigError`.

## Local Storage and Stateless Alternatives

When you run `aphotos auth import <cookie-file>`, the CLI:

1. Creates `~/.config/aphotos` (or `$XDG_CONFIG_HOME/aphotos`) with `0700` directory permissions.
2. Writes `config.yaml` via an atomic temporary file with `0600` permissions.

For headless servers, CI pipelines, and AI agent environments that prefer not to store plaintext cookies on disk, `aphotos` supports:

- **`env:VAR_NAME` indirection** inside `config.yaml` (e.g. `at-main: env:APHOTOS_AT`).
- **Environment variables** (`APHOTOS_SESSION_ID`, `APHOTOS_UBID`, `APHOTOS_AT`, `APHOTOS_COOKIES_FILE`, `APHOTOS_REGION`).

## Cross-Host Redirect Credential Stripping

Downloads via `GET /nodes/{id}/contentRedirection` return an HTTP `307 Temporary Redirect` to a pre-signed CDN URL (`content-*.drive.amazonaws.com/cdproxy/templink/...`).

`Client.Download` uses a dedicated redirect policy that strips `Cookie`, `x-amzn-sessionid`, and `Authorization` headers whenever a redirect crosses to a different host, ensuring session cookies are never forwarded to third-party or CDN hosts.

## Central Redaction (`aphotos.Redact`)

Every SDK error message, CLI error envelope, and `--verbose` / progress line passes through `aphotos.Redact` before being returned or written. `Redact` scrubs:

- Cookie key-value pairs (`session-id`, `ubid-*`, `at-*`, `x-amz-Cf-Id`).
- `Cookie`, `Set-Cookie`, `Authorization`, and `x-amzn-sessionid` header lines.
- `Atza|...` tokens.
- Pre-signed `/cdproxy/templink/<token>` paths and `X-Amz-Signature` / `Signature` / `token` query parameters.
