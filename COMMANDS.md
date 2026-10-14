# Command Reference

Complete command and flag reference for `aphotos`.

## Global Flags

Every command accepts the following persistent flags:

| Flag | Default | Env Override | Description |
|---|---|---|---|
| `--json` | `false` | `APHOTOS_JSON` | Emit versioned JSON envelope (`schema_version: "v1"`) to stdout |
| `--quiet` | `false` | `APHOTOS_QUIET` | Suppress non-JSON human output and progress |
| `--verbose` | `false` | `APHOTOS_VERBOSE` | Emit redacted diagnostic logs to stderr |
| `--region` | `""` | `APHOTOS_REGION` | Amazon marketplace region (`us`, `ca`, `uk`, `jp`, `de`, `fr`, `it`, `es`, ...) |
| `--config` | `""` | `APHOTOS_CONFIG` | Config file path (default: `~/.config/aphotos/config.yaml` or `$XDG_CONFIG_HOME/aphotos/config.yaml`) |
| `--timeout` | `2m0s` | — | Command execution timeout (must be positive) |
| `--profile` | `"default"` | `APHOTOS_PROFILE` | Credential profile name |

---

## Timestamp Semantics: `contentDate` vs `createdDate`

Amazon Photos distinguishes between two timestamps on media nodes:

- **`contentDate`** (`contentProperties.contentDate`): The **media capture timestamp** extracted from EXIF or video metadata (`content-desc` / `content-asc`).
- **`createdDate`** (`createdDate`): The **cloud upload / node creation timestamp** when the file was created in Amazon Photos (`created-desc` / `created-asc`).

The SDK and CLI preserve both timestamps in UTC and never conflate upload time with capture time.

---

## Commands

### `aphotos auth`

Manage Amazon Photos session cookie credentials.

#### `aphotos auth import <cookie-file>`

Import browser cookies from a Netscape `cookies.txt` export, JSON array of cookie objects (`[{"name":"...","value":"..."}]`), JSON key-value map (`{"session-id":"...","ubid-main":"...","at-main":"..."}`), or raw `Cookie:` header file. Both underscore (`session_id`, `ubid_main`, `at_main`, `ubid_acb*`, `at_acb*`) and hyphen (`session-id`, `ubid-main`, `at-main`, `ubid-acb*`, `at-acb*`) input keys are accepted and normalized to canonical browser wire names.

```sh
aphotos auth import ./cookies.json
aphotos auth import ./cookies.txt --region ca --profile canada
```

#### `aphotos auth status`

Show configured credential profile, resolved region, evidence status (`verified` or `experimental`), endpoints, and present cookie names (values are never printed).

```sh
aphotos auth status
aphotos auth status --json
```

#### `aphotos auth logout`

Remove stored credentials for the selected `--profile` from the configuration file.

```sh
aphotos auth logout
aphotos auth logout --profile canada
```

---

### `aphotos doctor`

Run graded diagnostic checks on configuration, credentials, regional routing, and API connectivity:

- **Grade 1 (`config_permissions`)**: Local config file and directory permission check (`0600` file, `0700` dir).
- **Grade 2 (`credentials_and_region`)**: Cookie presence (`session-id`, `ubid-*`, `at-*`) and explicit/inferred regional routing check.
- **Grade 3 (`account_root_and_usage`)**: Live read check against `GET /nodes?filters=isRoot:true` and `GET /account/usage`.
- **Grade 4 (`search_read_probe`)**: Read probe against `GET /search?limit=1`.
- **Grade 5 (`write_probe`)**: Opt-in remote folder creation and trash cleanup probe.

| Flag | Default | Description |
|---|---|---|
| `--write-probe` | `false` | Run opt-in remote folder create + trash probe |
| `--confirm` | `false` | Confirm opt-in remote write probe (required with `--write-probe`) |
| `--allow-unconfigured` | `false` | Succeed when no credentials are configured (CI safe) |

```sh
aphotos doctor
aphotos doctor --allow-unconfigured --json
aphotos doctor --write-probe --confirm
```

---

### `aphotos usage`

Show Amazon Photos / Drive account storage usage across `photo`, `video`, `doc`, and `other` categories (`total` and `billable` bytes and item counts).

```sh
aphotos usage
aphotos usage --json
```

---

### `aphotos search`, `aphotos photos`, `aphotos videos`

Search media nodes via `GET /search`.

- `aphotos search` — Search photos and/or videos (`--type all|photos|videos`).
- `aphotos photos` — Search photo nodes (`type:(PHOTOS)`).
- `aphotos videos` — Search video nodes (`type:(VIDEOS)`).

| Flag | Default | Description |
|---|---|---|
| `--filter` | `""` | Raw server filter DSL expression |
| `--type` | `"all"` | Media type filter: `photos`, `videos`, or `all` (`aphotos search` only) |
| `--year` | `0` | Filter by capture year (e.g. `2023`) |
| `--month` | `0` | Filter by capture month (`1`–`12`) |
| `--day` | `0` | Filter by capture day (`1`–`31`) |
| `--name` | `""` | Filter by file name prefix |
| `--people` | `[]` | Filter by server person cluster ID(s) |
| `--location` | `[]` | Filter by server location ID(s) |
| `--things` | `[]` | Filter by recognized object/scene term(s) |
| `--limit` | `50` | Page size (`1`–`200`) |
| `--offset` | `0` | Starting pagination offset |
| `--sort` | `"created-desc"` | Sort order: `created-desc`, `created-asc` (`createdDate`, cloud upload date), `content-desc`, `content-asc` (`contentDate`, media capture date), `name-asc`, `name-desc` |
| `--all` | `false` | Paginate through all matching results |

```sh
aphotos search --year 2024 --month 8 --sort content-desc
aphotos photos --things beach,sunset --limit 100 --json
aphotos videos --all --json
```

---

### `aphotos ls`

List nodes via `GET /nodes` or folder/album children via `GET /nodes/{id}/children`.

| Flag | Default | Description |
|---|---|---|
| `--parent` | `""` | Parent folder or album node ID |
| `--filter` | `""` | Raw server filter DSL expression |
| `--limit` | `50` | Page size (`1`–`200`) |
| `--offset` | `0` | Starting pagination offset |
| `--sort` | `""` | Sort order: `created-desc`, `created-asc`, `content-desc`, `content-asc`, `modified-desc`, `modified-asc`, `name-asc`, `name-desc` |
| `--all` | `false` | Paginate through all matching nodes (respecting the server `offset + limit <= 9999` window) |

```sh
aphotos ls --filter "kind:FOLDER"
aphotos ls --parent FOLDER_NODE_ID --all
```

---

### `aphotos aggregations`

Query server media aggregations (`GET /search` or `GET /search/aggregation`). Note that querying face clusters uses `--category allPeople` (passing `people` is normalized by the SDK to `allPeople` due to upstream API requirements).

| Flag | Default | Description |
|---|---|---|
| `--category` | `"all"` | Aggregation category: `all`, `allPeople`, `clusterId`, `familyMembers`, `favorite`, `location`, `people`, `things`, `time`, `type` |

```sh
aphotos aggregations --category allPeople --json
aphotos aggregations --category location
```

---

### `aphotos albums`

Manage Amazon Photos albums (`VISUAL_COLLECTION` nodes).

#### `aphotos albums list`

| Flag | Default | Description |
|---|---|---|
| `--limit` | `50` | Page size (`1`–`200`) |
| `--offset` | `0` | Starting pagination offset |

#### `aphotos albums create <name> [node-id...]`

Create a new album and optionally add initial media node IDs.

#### `aphotos albums add <album-id> <node-id...>`

Add one or more media nodes to an existing album.

#### `aphotos albums remove <album-id> <node-id...>`

Remove one or more media nodes from an album.

#### `aphotos albums rename <album-id> <new-name>`

Rename an album.

```sh
aphotos albums list
aphotos albums create "Summer 2026" NODE_1 NODE_2
aphotos albums add ALBUM_ID NODE_3
aphotos albums remove ALBUM_ID NODE_2
aphotos albums rename ALBUM_ID "Summer Trip 2026"
```

---

### `aphotos folders`

Manage Amazon Drive / Photos folders (`FOLDER` nodes).

#### `aphotos folders list`

| Flag | Default | Description |
|---|---|---|
| `--parent` | `""` | Parent folder node ID |
| `--limit` | `50` | Page size (`1`–`200`) |
| `--offset` | `0` | Starting pagination offset |

#### `aphotos folders create <name>`

| Flag | Default | Description |
|---|---|---|
| `--parent` | `""` | Parent folder node ID (defaults to account root) |

```sh
aphotos folders list
aphotos folders create "Backups 2026" --parent ROOT_ID
```

---

### `aphotos upload <file-or-dir...>`

Upload local file(s) or directory contents to Amazon Photos via streaming `POST` to the regional `cdproxy/nodes` endpoint. Uploads are marked `confirmed` only when HTTP 200/201 returns a verifiable remote node ID.

| Flag | Default | Description |
|---|---|---|
| `--parent` | `""` | Target parent folder node ID (defaults to account root) |
| `--album` | `""` | Target album node ID to add confirmed uploaded nodes to |
| `--dry-run` | `false` | Plan files to upload without sending HTTP requests |
| `--on-conflict` | `"fail"` | Conflict resolution policy: `skip`, `fail`, or `rename` |
| `--recursive` | `false` | Recursively traverse directories |
| `--continue-on-error` | `false` | Continue uploading remaining files when an item fails |

```sh
aphotos upload ./IMG_0001.JPG --on-conflict skip
aphotos upload ./DCIM --recursive --album ALBUM_ID --on-conflict skip --json
aphotos upload ./photos --recursive --dry-run
```

---

### `aphotos download <node-id...>`

Download remote media node(s) via `/nodes/{id}/contentRedirection` with atomic temporary-file writes, path-traversal protection, size verification, and MD5 checksum verification.

| Flag | Default | Description |
|---|---|---|
| `--out` | `"."` | Destination directory for downloaded files |
| `--continue-on-error` | `false` | Continue downloading remaining nodes when an item fails |

```sh
aphotos download NODE_ID_1 NODE_ID_2 --out ./downloads
```

---

### `aphotos trash`

List, move to trash, or restore nodes from trash. Permanent purge (`POST /bulk/nodes/purge`) is intentionally omitted in v1 for safety.

#### `aphotos trash list`

| Flag | Default | Description |
|---|---|---|
| `--limit` | `50` | Page size (`1`–`200`) |
| `--offset` | `0` | Starting pagination offset |
| `--filter` | `""` | Raw filter override |
| `--sort` | `""` | Sort order override |
| `--all` | `false` | Paginate through all trashed nodes (up to 9999 limit) |

#### `aphotos trash add <node-id...>`

Move node(s) to trash in chunks of up to 50 IDs per request. Requires `--confirm` (exits with code `6` `CONFIRMATION_REQUIRED` if omitted).

| Flag | Default | Description |
|---|---|---|
| `--confirm` | `false` | Confirm moving node(s) to trash |

#### `aphotos trash restore <node-id...>`

Restore node(s) from trash in chunks of up to 50 IDs per request.

```sh
aphotos trash list
aphotos trash add NODE_ID_1 --confirm
aphotos trash restore NODE_ID_1
```

---

### `aphotos favorite <node-id...>`

Mark node(s) as favorite (`settings.favorite=true`), or unset favorite status with `--undo`.

| Flag | Default | Description |
|---|---|---|
| `--undo` | `false` | Unset favorite status |

```sh
aphotos favorite NODE_ID_1 NODE_ID_2
aphotos favorite NODE_ID_1 --undo
```

---

### `aphotos hide <node-id...>`

Mark node(s) as hidden (`settings.hidden=true`), or unhide with `--undo`.

| Flag | Default | Description |
|---|---|---|
| `--undo` | `false` | Unset hidden status |

```sh
aphotos hide NODE_ID_1
aphotos hide NODE_ID_1 --undo
```

---

### `aphotos version`

Print CLI version, commit, build date, and Go runtime version.

```sh
aphotos version
aphotos version --json
aphotos --version
```

---

### `aphotos completion`

Generate shell completion scripts.

- `aphotos completion bash`
- `aphotos completion zsh`
- `aphotos completion fish`
- `aphotos completion powershell`
