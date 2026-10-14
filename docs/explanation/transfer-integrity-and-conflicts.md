# Upload Confirmation, Retries, Idempotency, and Conflict Handling

This page explains how `amazon-photos` guarantees transfer integrity and prevents false success reports, duplicate uploads, and local file overwrites.

## Upload Confirmation Semantics

Streaming an upload to `POST /cdproxy/nodes` is a non-idempotent network operation. Several failure modes can occur after the client transmits file bytes:

- The connection drops after the server receives the stream but before the HTTP response headers arrive.
- A proxy or gateway returns HTTP `200`/`201` with a truncated or non-JSON body.
- The response JSON omits the created node's `id`.

In `pkg/aphotos`:

- An upload is marked **`confirmed`** (`TransferConfirmed`) **only** when the server returns HTTP `2xx` and a valid JSON payload containing a non-empty `Node.ID`.
- If HTTP `2xx` returns malformed JSON or an empty `Node.ID`, the SDK returns **`unconfirmed`** (`TransferUnconfirmed`) with `*PartialResultError` (CLI exit code `5`). It never fabricates a node ID or claims the upload succeeded.
- Non-replayable streams (`io.Reader` without `io.Seeker` or `LocalPath`) are never automatically retried on network failure.

## Verifying `409 Conflict` Before Skipping

When a file with the same name already exists in the target folder, `POST /cdproxy/nodes` returns HTTP `409 Conflict`. However, a filename collision does **not** prove that the remote file has the same content as the local file.

Under `ConflictSkip` (`--on-conflict skip`):

1. `Upload` computes the local stream's MD5 digest while uploading.
2. On `409 Conflict`, `findConflictingRemoteNode` queries `GET /nodes` for non-trashed `FILE` nodes with the matching name in the target parent folder.
3. **Matching MD5**: If a candidate node has a non-empty `contentProperties.md5` equal to the local MD5, the transfer is marked **`skipped`** (`TransferSkipped`, `ConflictResolved: true`).
4. **Mismatched MD5**: If a candidate node has a different `contentProperties.md5`, `Upload` returns `*ConflictError` (`TransferFailed`) so the caller knows a different file occupies that name.
5. **Missing remote MD5**: If a matching remote node or `ConflictingNodeID` hint exists but lacks `contentProperties.md5`, `Upload` returns **`unconfirmed`** (`TransferUnconfirmed`) with `*PartialResultError`. It never marks an unverified filename collision as `skipped`.

Under `ConflictRename` (`--on-conflict rename`), `Upload` rewinds the local file or `io.ReadSeeker` and uploads once under a disambiguated name (`photo (1).jpg`).

## Download Integrity and Atomic Local File Protection

When downloading files via `Download` or `DownloadBatch`:

1. **Metadata Lookup (`DownloadBatch`)**: Queries `GET /nodes/{id}` first to obtain the authoritative filename, `contentProperties.size`, and `contentProperties.md5`.
2. **Temporary File Streaming**: Bytes stream into a temporary `.aphotos-dl-*.part` file in the destination directory while computing the MD5 digest and tracking byte count.
3. **Size and MD5 Verification**:
   - If `Content-Length`, `ExpectedSize`, or `ExpectedMD5` does not match the downloaded stream, the `.part` file is deleted immediately and `Download` fails.
   - If `ExpectedMD5` was provided and matched, `TransferResult.MD5Verified` is `true`.
   - If the remote node metadata did not provide an MD5 digest, `TransferResult.MD5Verified` is `false` and `TransferResult.Warning` (and CLI `meta.warnings`) explicitly reports that MD5 verification was unavailable.
4. **Conflict-Safe Atomic Finalization**:
   - Instead of calling `os.Rename` directly (which silently overwrites existing files on POSIX systems), `finalizeDownloadTempFile` uses `os.Link` (falling back to `os.OpenFile` with `O_CREATE|O_EXCL`) to claim the destination filename atomically.
   - Under `ConflictFail` (default), an existing destination file returns `*ConflictError` without modifying the existing file.
   - Under `ConflictSkip`, the existing file is preserved and the download returns `TransferSkipped`.
   - Under `ConflictRename`, concurrent or sequential downloads disambiguate (`file (1).jpg`, `file (2).jpg`, ...) without TOCTOU overwrites.
