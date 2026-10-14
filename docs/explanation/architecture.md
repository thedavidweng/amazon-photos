# SDK and CLI Architecture

This page explains the architectural boundaries between the `pkg/aphotos` Go SDK and the `cmd/aphotos` CLI.

## Strict Separation of SDK and CLI

`amazon-photos` is divided into two distinct layers:

1. **`pkg/aphotos` (Public Go SDK)**:
   - Zero third-party dependencies (standard library only).
   - Zero CLI, GUI, or desktop framework imports.
   - Zero network or filesystem I/O inside `aphotos.NewClient`.
   - Designed to be imported directly by community Go programs, servers, and desktop clients such as `Rainforest`.
2. **`cmd/aphotos` and `internal/{cli,config,model,output}` (Cobra CLI)**:
   - Implements command-line parsing (`spf13/cobra`), `0600` YAML profile persistence (`gopkg.in/yaml.v3`), thread-safe stdout/stderr rendering, and the versioned `--json` envelope (`schema_version: "v1"`).

## Zero-I/O Construction and Transport Injection

Legacy Amazon Photos scripts frequently performed network calls (`GET /nodes?filters=isRoot:true`), created local SQLite/Parquet caches, or changed working directories inside their constructors. That made instantiation slow, stateful, and fragile in tests.

In `pkg/aphotos`, `NewClient(opts)` only validates options, normalizes cookies, resolves the regional routing table, and initializes bounded concurrency semaphores in memory. Every HTTP call flows through an injectable `*http.Client` or `http.RoundTripper`, allowing the entire protocol surface to be tested deterministically in memory.

## Bounded Concurrency and Read-Only Retry Engine

`aphotos.Client` enforces two separate concurrency semaphores:

- **Metadata and download semaphore (`metaSem`)**: Defaults to `4` concurrent requests.
- **Upload semaphore (`uploadSem`)**: Defaults to `2` concurrent streaming uploads.

Read-only requests (`GET /nodes`, `GET /search`, `GET /account/usage`, `GET /trash`) automatically retry on HTTP `429 Too Many Requests`, HTTP `5xx`, and transient network errors using exponential backoff with jitter while honoring `Retry-After` headers (delta-seconds or HTTP-date, capped at `MaxRetryAfter`).

Non-idempotent mutations (`POST`, `PATCH`) are never blindly retried by the transport layer when commit state is uncertain, preventing silent duplicate uploads.

## Thread-Safe CLI Output

`internal/output.Renderer` serializes all writes to `stdout` and `stderr` with an internal mutex. During concurrent batch uploads and downloads:

- Worker goroutines emit progress lines safely to `stderr` via `Renderer.Progressf`.
- When `--json` is set, `stdout` receives only the single redacted JSON envelope after the command completes.
- When `--quiet` is set, progress and human summary lines are suppressed.
