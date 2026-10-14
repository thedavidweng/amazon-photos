# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.1] - 2026-10-14

### Fixed
- Synchronized `output.Renderer` writes with a mutex so concurrent batch upload/download progress callbacks never race on `stderr` or `stdout`.
- Hardened `Upload` `--on-conflict skip` (`handleConflictSkip` / `findConflictingRemoteNode`) to require a verified matching remote `contentProperties.md5` before reporting `skipped`; nodes lacking MD5 return `unconfirmed` (`*PartialResultError`) rather than a false skip.
- Protected `Download` against overwriting existing local files using atomic file creation (`os.Link` / `O_CREATE|O_EXCL`) and `ConflictPolicy` (`fail`, `skip`, `rename`).
- Updated `DownloadBatch` to fetch remote node metadata via `GetNode`, verify `ExpectedSize` and `ExpectedMD5`, and emit an explicit warning when a remote node lacks an MD5 digest.

### Changed
- Added `Client.GetNode`, `DownloadRequest.ConflictPolicy`, `BatchDownloadOptions.ConflictPolicy`, `TransferResult.MD5Verified`, `TransferResult.Warning`, and `aphotos download --on-conflict`.
- Reorganized documentation under `docs/{tutorials,how-to,reference,explanation}` following the Diátaxis standard.
- Added read-only `fmt-check`, `vet`, `test-race`, `mod-tidy-check`, GoReleaser snapshot verification, and `jactionlint` workflow linting in CI.

## [0.1.0] - 2026-10-14

Initial release of the `amazon-photos` Go SDK (`pkg/aphotos`) and `aphotos` CLI (`cmd/aphotos`).

### Added

#### Go SDK (`pkg/aphotos`)
- Zero-side-effect `aphotos.NewClient` constructor with injectable `http.Client` / `http.RoundTripper` and zero third-party dependencies.
- Regional routing table (`us` verified; `ca`, `uk`, `jp`, and European TLDs `experimental`) pairing `drive/v1`, `cdproxy/nodes`, and thumbnail endpoints with canonical browser cookies (`session-id`, `ubid-*`, `at-*`) and fail-fast region inference.
- Canonical cookie normalization accepting underscore (`session_id`, `ubid_main`, `at_main`, `ubid_acb*`, `at_acb*`) and hyphen input names while emitting only canonical hyphenated wire cookies alongside `x-amzn-sessionid`.
- Bounded concurrency semaphores (metadata default 4, upload default 2) and read-only retry engine honoring `Retry-After` (delta-seconds and HTTP-date) with exponential backoff and jitter.
- Central `Redact` sanitizer scrubbing cookie values, `x-amzn-sessionid`, tokens, and pre-signed `contentRedirection` URLs from all error messages and logs.
- Account root (`GetRoot`), storage usage (`GetUsage`), media search (`Search`, `SearchAll`), generic node queries (`QueryNodes`, `QueryAllNodes`), folder children (`ListChildren`, `ListAllChildren`), and aggregations (`GetAggregations` across `all`, `allPeople`, `clusterId`, `familyMembers`, `favorite`, `location`, `people`, `things`, `time`, `type`).
- Safe filter DSL builder (`FilterBuilder`) alongside explicit raw server filter passthrough (`SearchOptions.Filters`).
- Deduplicating pagination advancing at `offset + limit` (fixing legacy page-0 duplication) and honoring the `/nodes` and `/trash` `offset + limit <= 9999` server constraint.
- Streaming `cdproxy/nodes` upload (`Upload`, `UploadFile`, `UploadBatch`) distinguishing `confirmed` (HTTP 200/201 with verified node ID), `skipped` (verified conflict match), `unconfirmed`, and `failed` transfers, with `--on-conflict skip|fail|rename` policies.
- Atomic streaming download via `/nodes/{id}/contentRedirection` (`Download`, `DownloadBatch`) with size and MD5 verification and cross-platform download filename sanitization.
- Album (`CreateAlbum`, `ListAlbums`, `AddAlbumMembers`, `RemoveAlbumMembers`, `RenameNode`), folder (`CreateFolder`, `ListFolders`), trash (`ListTrash`, `ListAllTrash`, `MoveToTrash`, `RestoreFromTrash` chunked at 50 IDs), and node settings (`SetFavorite`, `SetHidden` via `PATCH /nodes/{id}`).

#### CLI (`cmd/aphotos`)
- Cobra CLI with commands: `auth import|status|logout`, `doctor`, `usage`, `search`, `photos`, `videos`, `ls`, `aggregations`, `albums list|create|add|remove|rename`, `folders list|create`, `upload`, `download`, `trash list|add|restore`, `favorite`, `hide`, `version`, and `completion`.
- Multi-profile `0600` configuration store (`~/.config/aphotos/config.yaml`) supporting Netscape `cookies.txt`, JSON array/map, raw `Cookie` headers, `env:VAR_NAME` indirection, and stateless `APHOTOS_*` environment variables.
- Versioned `--json` output envelope (`schema_version: "v1"`, `ok`, `data`, `error`, `meta`) with deterministic exit codes (`0`–`8`, `130`).
- Graded `aphotos doctor` diagnostics with `--allow-unconfigured` for CI and `--write-probe --confirm` for opt-in remote folder creation/trash verification.
