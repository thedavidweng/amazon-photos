# Configuration File and Environment Variables

Reference for `aphotos` configuration file paths, schema, secret indirection, and environment variable overrides.

## Configuration File Location

By default, `aphotos` reads and writes configuration at:

1. `--config <path>` flag
2. `APHOTOS_CONFIG` environment variable
3. `$XDG_CONFIG_HOME/aphotos/config.yaml` (when `XDG_CONFIG_HOME` is set)
4. `~/.config/aphotos/config.yaml`

`aphotos auth import` creates the parent directory with `0700` permissions and writes `config.yaml` atomically with `0600` permissions.

## `config.yaml` Schema

```yaml
default_profile: default
profiles:
  default:
    region: us
    cookies:
      session-id: "140-0000000-0000000"
      ubid-main: "130-0000000-0000000"
      at-main: "env:APHOTOS_AT"
  canada:
    region: ca
    drive_url: "https://www.amazon.ca/drive/v1"
    upload_url: "https://content-na.drive.amazonaws.com/cdproxy/nodes"
    thumbnail_url: "https://thumbnails-photos.amazon.ca/v1/thumbnail"
    cookies:
      session-id: "env:CA_SESSION_ID"
      ubid-acbca: "env:CA_UBID"
      at-acbca: "env:CA_AT"
```

### `env:VAR_NAME` Secret Indirection

Any cookie value in `config.yaml` prefixed with `env:` (for example `env:APHOTOS_AT`) is resolved from the process environment at runtime so raw tokens do not need to be stored on disk.

## Environment Variables

| Variable | Description |
|---|---|
| `APHOTOS_CONFIG` | Override path to `config.yaml` |
| `APHOTOS_PROFILE` | Active profile name (default: `default`) |
| `APHOTOS_REGION` | Override Amazon marketplace region (`us`, `ca`, `uk`, `jp`, `de`, ...) |
| `APHOTOS_COOKIES_FILE` | Path to a Netscape `cookies.txt`, JSON array/map, or raw `Cookie` header file |
| `APHOTOS_SESSION_ID` | Override `session-id` cookie value |
| `APHOTOS_UBID` | Override regional `ubid-*` cookie value (`ubid-main` or `ubid-acb*`) |
| `APHOTOS_AT` | Override regional `at-*` cookie value (`at-main` or `at-acb*`) |
| `APHOTOS_JSON` | Enable `--json` mode when set to `1`, `true`, or `yes` |
| `APHOTOS_QUIET` | Enable `--quiet` mode when set to `1`, `true`, or `yes` |
| `APHOTOS_VERBOSE` | Enable `--verbose` mode when set to `1`, `true`, or `yes` |

## Credential Precedence

When constructing a client, `aphotos` resolves credentials in the following order (highest precedence first):

1. Explicit `APHOTOS_SESSION_ID`, `APHOTOS_UBID`, and `APHOTOS_AT` environment variables.
2. Cookie file specified by `APHOTOS_COOKIES_FILE`.
3. Selected profile in `config.yaml` (`--profile` / `APHOTOS_PROFILE` / `default_profile`).
