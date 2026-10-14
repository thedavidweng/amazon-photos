# ADR 0001: SDK Architecture, Regional Routing, Cookie Normalization, and Read-Only Retry Engine

## Status

Accepted

## Context

Amazon Photos relies on the private Amazon Drive v1 Web API (`https://www.amazon.<domain>/drive/v1`, `content-<region>.drive.amazonaws.com/cdproxy/nodes`, and `thumbnails-photos.amazon.<domain>/v1/thumbnail`). Existing open-source implementations exhibit several recurring reliability hazards:

1. **Constructor side effects**: Eagerly querying the root node, creating local SQLite/Parquet files, or mutating process working directories during client initialization makes clients expensive and unsafe to construct in CLIs, services, and desktop apps.
2. **Cookie and regional mismatches**: Documentation and tools mix underscore cookie names (`ubid_main`, `at_main`, `session_id`) with browser wire names (`ubid-main`, `at-main`, `session-id`), sometimes emitting duplicate cookies or silently falling back to `.com` when regional cookies (`ubid-acbca`, `ubid-acbuk`, `ubid-acbjp`) are supplied.
3. **Unbounded concurrency and retry loops**: High default concurrency (up to 2,000 connections in legacy Python code) triggers HTTP 429 storms; blind retries on HTTP 401 waste up to 12 attempts before failing; and automatic replay of non-idempotent POST/PATCH requests after ambiguous network errors risks duplicate uploads.
4. **Credential leakage**: Raw HTTP errors and redirect errors can include `Cookie`, `x-amzn-sessionid`, or pre-signed `contentRedirection` URLs in logs and CLI outputs.

## Decision

1. **Zero-I/O Construction & `http.RoundTripper` Seam**:
   `aphotos.NewClient(opts)` validates options, normalizes cookies, resolves the regional routing table, and initializes bounded concurrency semaphores in memory without performing any network or filesystem I/O. Callers may inject a custom `*http.Client` or `http.RoundTripper` for testing, proxies, or custom timeouts. TLS certificate verification remains enabled by default.
2. **Explicit Regional Routing & Fail-Fast Resolution**:
   Regions (`us`, `ca`, `uk`, `jp`, and European TLDs) map deterministically to `DriveURL`, `UploadURL` (`cdproxy`), `ThumbnailURL`, required cookie names, and an `EvidenceStatus` (`verified` for US; `experimental` for unverified regional routes). When `Region` is omitted, `InferRegionFromCookies` resolves the region only if cookie suffixes unambiguously identify a single region (`-main` -> `us`, `-acbca` -> `ca`, `-acbuk` -> `uk`, `-acbjp` -> `jp`, `-acb<tld>` -> `<tld>`). Missing, unknown, or conflicting region signals fail fast with `*ConfigError` and never fall back to `.com`.
3. **Canonical Wire Cookie Normalization**:
   Input cookie maps accept both underscore and hyphen aliases (`session_id`/`session-id`, `ubid_main`/`ubid-main`, `at_main`/`at-main`, `ubid_acb*`/`ubid-acb*`, `at_acb*`/`at-acb*`). Outgoing requests emit only the canonical hyphenated browser cookie names in deterministic order alongside `x-amzn-sessionid: <session-id>`.
4. **Bounded Concurrency & Read-Only Retry Policy**:
   Metadata requests are gated by a semaphore (default 4) and uploads by a separate semaphore (default 2). Read-only requests retry up to `MaxRetries` (default 3) on HTTP 429, HTTP 5xx, and transient network errors using exponential backoff with jitter, honoring `Retry-After` (delta-seconds or HTTP-date) capped at `MaxRetryAfter` (default 60s) and aborting immediately on context cancellation. HTTP 401 fails immediately on the first response (`*AuthenticationError`) without retry. Non-idempotent mutations (`POST`, `PATCH`, `PUT`, `DELETE`) are not automatically replayed when commit state is uncertain.
5. **Typed Errors & Central Redaction**:
   All SDK errors map to concrete typed structs (`AuthenticationError`, `PermissionError`, `RateLimitError`, `InvalidFilterError`, `ConflictError`, `NotFoundError`, `TransientNetworkError`, `RemoteProtocolError`, `PartialResultError`, `ConfigError`) supporting `errors.Is` / `errors.As` and passing all diagnostic strings through `Redact`.

## Consequences

### Positive

- `pkg/aphotos` has zero CLI or GUI framework dependencies and can be imported directly by both `cmd/aphotos` and the `Rainforest` desktop client.
- All protocol contracts, regional host pairings, retry/rate-limit rules, and error classifications are testable in milliseconds through an injected `http.RoundTripper` without live credentials.
- Credential values and signed download URLs are scrubbed before reaching caller logs or JSON envelopes.

### Negative

- Callers must either specify `Region` explicitly or provide unambiguous regional cookies (`ubid-main`/`at-main` or `ubid-acb<tld>`/`at-acb<tld>`).
- Non-idempotent writes require explicit higher-level reconciliation on network interruption rather than transparent transport retry.
