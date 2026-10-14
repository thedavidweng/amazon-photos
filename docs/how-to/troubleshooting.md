# Troubleshoot Authentication and Transfers

This guide helps you diagnose and resolve authentication, regional routing, and transfer errors in `aphotos`.

## Run Graded Diagnostics (`aphotos doctor`)

Start with `aphotos doctor`:

```sh
aphotos doctor --json
```

`doctor` evaluates five graded checks in order:

1. `config_permissions`: Verifies `~/.config/aphotos` is `0700` and `config.yaml` is `0600`.
2. `credentials_and_region`: Verifies required cookies (`session-id`, `ubid-*`, `at-*`) and unambiguous regional routing.
3. `account_root_and_usage`: Tests live read access to `GET /nodes?filters=isRoot:true` and `GET /account/usage`.
4. `search_read_probe`: Tests `GET /search?limit=1`.
5. `write_probe` (opt-in with `--write-probe --confirm`): Creates and trashes a temporary probe folder.

## Fix `CONFIG_ERROR` (Exit Code `8`)

**Symptom**: `missing required cookie` or `cannot infer region from cookies`.

**Fix**:

- Ensure all three cookies for your marketplace are present (`session-id`, `ubid-main`/`ubid-acb*`, `at-main`/`at-acb*`).
- If your cookie file contains cookies from multiple Amazon marketplaces, pass `--region` explicitly during import:
  ```sh
  aphotos auth import ./cookies.txt --region us
  ```

## Fix `AUTH_FAILED` / HTTP `401` (Exit Code `2`)

**Symptom**: `aphotos authentication error on /account/usage (HTTP 401)`.

**Fix**:

- Amazon browser session cookies expire periodically or when you sign out in the browser.
- Export a fresh set of cookies from an active Amazon Photos browser session and re-run `aphotos auth import`.
- `aphotos` intentionally does not retry HTTP `401` responses so expired credentials fail fast.

## Resolve `unconfirmed` Uploads (`PARTIAL_SUCCESS`, Exit Code `5`)

**Symptom**: An upload returns `"status": "unconfirmed"` or exit code `5`.

**Cause**:

- The HTTP `POST` to `cdproxy/nodes` succeeded or was interrupted after bytes were sent, and the response lacked a verifiable remote `nodeId`; or
- `--on-conflict skip` encountered a `409 Conflict` against a remote node whose metadata did not include an `md5` hash.

**Fix**:

1. Query the target folder before re-uploading:
   ```sh
   aphotos ls --parent <PARENT_NODE_ID> --filter "name:IMG_0001.JPG" --json
   ```
2. Compare `contentProperties.md5` and `contentProperties.size` against your local file (`md5sum IMG_0001.JPG`).
3. Re-run `aphotos upload ./IMG_0001.JPG --on-conflict skip` once the remote node metadata is indexed.

## Fix Local Conflict Errors on Download (`CONFLICT`, Exit Code `3`)

**Symptom**: `local file "IMG_0001.JPG" already exists`.

**Fix**:

By default, `aphotos download` uses `--on-conflict fail` to protect existing local files. Pass `--on-conflict skip` to keep existing files or `--on-conflict rename` to save alongside with a disambiguated filename (`IMG_0001 (1).JPG`).
