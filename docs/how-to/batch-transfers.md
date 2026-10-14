# Batch Upload and Download

This guide shows how to upload and download batches of files safely using `aphotos` and the `pkg/aphotos` Go SDK.

## Upload a Directory Recursively

To upload all files under a directory while skipping hidden dotfiles:

```sh
aphotos upload ./DCIM --recursive --on-conflict skip
```

Preview the planned batch without sending any HTTP requests:

```sh
aphotos upload ./DCIM --recursive --dry-run --json
```

## Upload into a Specific Folder or Album

Upload files into a specific parent folder node ID and attach confirmed uploads to an album:

```sh
aphotos upload ./Trip2026 \
  --recursive \
  --parent FOLDER_NODE_ID \
  --album ALBUM_NODE_ID \
  --on-conflict skip \
  --continue-on-error \
  --json
```

Only items that reach `confirmed` status with a verified remote `nodeId` are added to the target album.

## Choose an Upload Conflict Policy

When Amazon Photos returns HTTP `409 Conflict`, `--on-conflict` controls how `aphotos` responds:

- `--on-conflict fail` (default): Fails immediately with `CONFLICT` (exit code `3`).
- `--on-conflict skip`: Queries the target parent for a remote file with the same name and verifies its `contentProperties.md5` against the local stream's MD5. It marks the item `skipped` **only** when the MD5 matches. If the remote node has a different MD5, it returns `CONFLICT`; if the remote node lacks an MD5, it marks the item `unconfirmed` (`PARTIAL_SUCCESS`, exit code `5`).
- `--on-conflict rename`: Replays the local file once using a disambiguated filename (`IMG_0001 (1).JPG`).

## Download Multiple Nodes with Integrity Verification

Download multiple media nodes into a target directory:

```sh
aphotos download NODE_ID_1 NODE_ID_2 NODE_ID_3 \
  --out ./restored-photos \
  --on-conflict skip \
  --continue-on-error \
  --json
```

For each node ID, `aphotos`:

1. Queries remote node metadata (`GET /nodes/{id}`) to retrieve the canonical filename, `contentProperties.size`, and `contentProperties.md5`.
2. Streams the file to a temporary `.aphotos-dl-*.part` file inside `--out`.
3. Verifies the byte count and MD5 digest before atomically linking the file into place.
4. Emits a warning in `meta.warnings` (and `items[].warning`) if the remote node metadata did not provide an MD5 digest.

## Handle Local File Conflicts on Download

`aphotos download` never overwrites an existing local file:

- `--on-conflict fail` (default): Returns `CONFLICT` (exit code `3`) if the target file already exists.
- `--on-conflict skip`: Preserves the existing local file and records `"status": "skipped"`.
- `--on-conflict rename`: Writes to a disambiguated filename such as `photo (1).jpg` using atomic exclusive file creation (`os.Link` / `O_CREATE|O_EXCL`).
