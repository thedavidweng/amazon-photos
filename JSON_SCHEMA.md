# JSON Output Contract

Every `aphotos` command supports `--json` (or `APHOTOS_JSON=1`) to emit a single versioned JSON envelope to `stdout`.

When `--json` is enabled:
- `stdout` contains **only** the JSON envelope (`Envelope`).
- Progress updates and `--verbose` diagnostics are written exclusively to `stderr`.
- All strings in `error.message`, `error.details`, and `meta.warnings` are scrubbed by `aphotos.Redact` so cookies (`session-id`, `ubid-*`, `at-*`), `x-amzn-sessionid`, bearer tokens, and pre-signed `contentRedirection` query strings are never leaked.

---

## Envelope Schema (`schema_version: "v1"`)

### Success Response

```json
{
  "ok": true,
  "data": {
    "items": [],
    "totalCount": 0,
    "offset": 0,
    "limit": 50,
    "hasMore": false,
    "complete": true
  },
  "meta": {
    "command": "search",
    "profile": "default",
    "region": "us",
    "duration_ms": 142,
    "schema_version": "v1",
    "request_id": "req-3f8a91c04d2e11ab"
  }
}
```

### Error Response

```json
{
  "ok": false,
  "error": {
    "code": "AUTH_REQUIRED",
    "message": "no credentials configured for profile \"default\"; run 'aphotos auth import <cookie-file>' or set APHOTOS_COOKIES_FILE",
    "category": "auth"
  },
  "meta": {
    "command": "usage",
    "profile": "default",
    "duration_ms": 2,
    "schema_version": "v1",
    "request_id": "req-91bc4021e89f0012"
  }
}
```

### Partial Success Response (Exit Code `5`)

When a batch or multi-page operation partially succeeds (e.g., `upload --continue-on-error`, `download --continue-on-error`, or pagination interrupted after initial pages), the envelope includes both `data` and `error` with `code: "PARTIAL_SUCCESS"`:

```json
{
  "ok": false,
  "data": {
    "operation": "upload",
    "total": 2,
    "confirmed": 1,
    "skipped": 0,
    "unconfirmed": 0,
    "failed": 1,
    "items": []
  },
  "error": {
    "code": "PARTIAL_SUCCESS",
    "message": "upload conflict at /cdproxy/nodes: Node already exists",
    "category": "api",
    "details": {
      "cause_code": "CONFLICT"
    }
  },
  "meta": {
    "command": "upload",
    "profile": "default",
    "region": "us",
    "duration_ms": 389,
    "schema_version": "v1",
    "request_id": "req-7c1d88a20b4f9910"
  }
}
```

---

## Envelope Fields

| Field | Type | Presence | Description |
|---|---|---|---|
| `"ok"` | ` boolean` | Always | `true` when the command succeeded completely; `false` on error or partial failure |
| `"data"` | `object \| array` | On success or partial failure | Command-specific payload |
| `"error"` | `ErrorBody` | When `ok == false` | Structured machine-readable error object |
| `"meta"` | `Meta` | Always | Execution metadata |

### `ErrorBody` Fields

| Field | Type | Presence | Description |
|---|---|---|---|
| `"code"` | `string` | Always on error | Machine-readable error code constant |
| `"message"` | `string` | Always on error | Redacted human-readable error message |
| `"category"` | `string` | Optional (`omitempty`) | Error category (`validation`, `auth`, `api`, `network`, `safety`, `filesystem`, `config`, `interrupted`) |
| `"retryable"` | `boolean` | Optional (`omitempty`) | `true` for transient network or rate-limit errors (`NETWORK_ERROR`, `RATE_LIMITED`) |
| `"retry_after_ms"` | `int64` | Optional (`omitempty`) | Server-requested backoff duration in milliseconds when rate-limited (`429`) |
| `"details"` | `object` | Optional (`omitempty`) | Redacted structured context (`endpoint`, `status_code`, `request_id`, `field`, `cause_code`) |

### `Meta` Fields

| Field | Type | Presence | Description |
|---|---|---|---|
| `"command"` | `string` | Optional (`omitempty`) | Executed CLI command name (e.g. `"search"`, `"auth import"`, `"albums list"`) |
| `"profile"` | `string` | Optional (`omitempty`) | Active credential profile name |
| `"region"` | `string` | Optional (`omitempty`) | Resolved Amazon marketplace region (`"us"`, `"ca"`, `"uk"`, `"jp"`, etc.) |
| `"duration_ms"` | `int64` | Always | Wall-clock execution duration in milliseconds |
| `"schema_version"` | `string` | Always | Envelope schema version (`"v1"`) |
| `"request_id"` | `string` | Optional (`omitempty`) | Per-invocation correlation ID (`req-<16-hex>`) |
| `"warnings"` | `string[]` | Optional (`omitempty`) | Redacted non-fatal warning strings (e.g. pagination `offset + limit <= 9999` cap notices) |

---

## Error Codes, Categories, and Exit Codes

| Error Code | Category | Retryable | Exit Code | Trigger / SDK Error Mapping |
|---|---|---|---|---|
| *(none — success)* | — | — | `0` | Command succeeded (`ok: true`) |
| `VALIDATION_FAILED` | `validation` | `false` | `1` | Invalid CLI flags, missing positional arguments, invalid `--timeout` |
| `INVALID_FILTER` | `validation` | `false` | `1` | `*aphotos.InvalidFilterError` (HTTP `400` or malformed filter DSL) |
| `AUTH_REQUIRED` | `auth` | `false` | `2` | No credentials configured for the active profile |
| `AUTH_FAILED` | `auth` | `false` | `2` | `*aphotos.AuthenticationError` (HTTP `401` expired or invalid session cookies) |
| `PERMISSION_DENIED` | `auth` | `false` | `2` | `*aphotos.PermissionError` (HTTP `403` forbidden) |
| `API_ERROR` | `api` | `false` | `3` | Unclassified remote API failure |
| `REMOTE_PROTOCOL_ERROR` | `api` | `false` | `3` | `*aphotos.RemoteProtocolError` (unexpected HTTP status, malformed JSON, checksum/size mismatch) |
| `RESOURCE_NOT_FOUND` | `api` | `false` | `3` | `*aphotos.NotFoundError` (HTTP `404` missing node, album, or folder) |
| `CONFLICT` | `api` | `false` | `3` | `*aphotos.ConflictError` (HTTP `409` upload or node name conflict) |
| `NETWORK_ERROR` | `network` | `true` | `4` | `*aphotos.TransientNetworkError` or `context.DeadlineExceeded` |
| `RATE_LIMITED` | `network` | `true` | `4` | `*aphotos.RateLimitError` (HTTP `429` Too Many Requests after retry budget exhausted) |
| `PARTIAL_SUCCESS` | `api` | `false` | `5` | `*aphotos.PartialResultError` or batch transfer with mixed confirmed/failed items |
| `CONFIRMATION_REQUIRED` | `safety` | `false` | `6` | Missing `--confirm` on `trash add` or `doctor --write-probe` |
| `READ_ONLY_VIOLATION` | `safety` | `false` | `6` | Attempted remote mutation when restricted by safety policy |
| `FILESYSTEM_ERROR` | `filesystem` | `false` | `7` | Local file I/O error (`os.PathError` during upload/download) |
| `CONFIG_ERROR` | `config` | `false` | `8` | `*aphotos.ConfigError` (missing/ambiguous region, missing required cookie, invalid config file) |
| `INTERRUPTED` | `interrupted` | `false` | `130` | `context.Canceled` or `SIGINT` / `SIGTERM` interrupt |
