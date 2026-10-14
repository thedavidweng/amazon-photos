# amazon-photos (`aphotos`)

<p align="center">
  <a href="https://github.com/thedavidweng/amazon-photos/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/thedavidweng/amazon-photos/ci.yml?branch=main&style=flat-square&label=ci" alt="CI"></a>
  <a href="https://github.com/thedavidweng/amazon-photos/releases"><img src="https://img.shields.io/github/v/release/thedavidweng/amazon-photos?style=flat-square" alt="Release"></a>
  <a href="https://github.com/thedavidweng/amazon-photos/blob/main/LICENSE"><img src="https://img.shields.io/github/license/thedavidweng/amazon-photos?style=flat-square" alt="License"></a>
  <img src="https://img.shields.io/badge/go-%3E%3D1.26-blue?style=flat-square" alt="Go">
</p>

> **Disclaimer**: Unofficial client; not affiliated with, endorsed by, or sponsored by Amazon.com, Inc.

`amazon-photos` provides a pure Go SDK (`github.com/thedavidweng/amazon-photos/pkg/aphotos`) and single-binary CLI (`aphotos`) for Amazon Photos / Amazon Drive v1.

---

## Important Notices: Private API Instability & Credential Security

- **Private API Instability**: Amazon discontinued the public Amazon Drive REST API in 2023. This project communicates with the undocumented Amazon Photos Web API (`/drive/v1` and regional `cdproxy/nodes` endpoints), which may change, throttle, or break at any time without notice.
- **Verified Transfers Only**: Uploads via `cdproxy/nodes` are reported as `confirmed` only when the server returns HTTP `200`/`201` with a verifiable remote node ID. Ambiguous or interrupted transfers are marked `unconfirmed` and never blindly replayed as duplicate uploads. HTTP `409 Conflict` is never treated as unconditional success without verifying the existing remote node.
- **Credential Security**: Authentication relies on browser session cookies (`session-id`, `ubid-*`, `at-*`).
  - Never commit or paste raw cookie values into repositories, issues, logs, or shell history.
  - `aphotos auth import` writes credentials to `~/.config/aphotos/config.yaml` with `0600` file permissions inside a `0700` directory.
  - You can use `env:VAR_NAME` indirection inside `config.yaml` or set `APHOTOS_COOKIES_FILE`, `APHOTOS_SESSION_ID`, `APHOTOS_UBID`, `APHOTOS_AT`, and `APHOTOS_REGION` for stateless runs.
  - All SDK errors, CLI `--verbose` stderr logs, and `--json` envelopes pass through `aphotos.Redact` so cookies, `x-amzn-sessionid`, tokens, and pre-signed `contentRedirection` URLs are never printed.

---

## Regional Routing & Verification Status

`amazon-photos` pairs each marketplace region with its exact `drive/v1` domain, `cdproxy` upload host, thumbnail host, and canonical browser wire cookies. When region is not explicitly passed, it is inferred only when cookie suffixes unambiguously identify a single region; ambiguous or unknown cookies fail fast and never silently fall back to `.com`.

| Region | Drive API (`DriveURL`) | Upload Host (`UploadURL`) | Thumbnail Host (`ThumbnailURL`) | Canonical Wire Cookies | Status |
|---|---|---|---|---|---|
| **US** (`us` / `com`) | `https://www.amazon.com/drive/v1` | `https://content-na.drive.amazonaws.com/cdproxy/nodes` | `https://thumbnails-photos.amazon.com/v1/thumbnail` | `session-id`, `ubid-main`, `at-main` | `verified` |
| **CA** (`ca`) | `https://www.amazon.ca/drive/v1` | `https://content-na.drive.amazonaws.com/cdproxy/nodes` | `https://thumbnails-photos.amazon.ca/v1/thumbnail` | `session-id`, `ubid-acbca`, `at-acbca` | `experimental` |
| **UK** (`uk` / `co.uk`) | `https://www.amazon.co.uk/drive/v1` | `https://content-eu.drive.amazonaws.com/cdproxy/nodes` | `https://thumbnails-photos.amazon.co.uk/v1/thumbnail` | `session-id`, `ubid-acbuk`, `at-acbuk` | `experimental` |
| **JP** (`jp` / `co.jp`) | `https://www.amazon.co.jp/drive/v1` | `https://content-jp.drive.amazonaws.com/cdproxy/nodes` | `https://thumbnails-photos.amazon.co.jp/v1/thumbnail` | `session-id`, `ubid-acbjp`, `at-acbjp` | `experimental` |
| **EU** (`de`, `fr`, `it`, `es`, `nl`, `se`, `pl`, ...) | `https://www.amazon.{tld}/drive/v1` | `https://content-eu.drive.amazonaws.com/cdproxy/nodes` | `https://thumbnails-photos.amazon.{tld}/v1/thumbnail` | `session-id`, `ubid-acb{tld}`, `at-acb{tld}` | `experimental` |

*Note*: Both underscore (`session_id`, `ubid_main`, `at_main`, `ubid_acb*`, `at_acb*`) and hyphen input keys are accepted and normalized to canonical hyphenated wire names.

---

## Go SDK Quickstart

Install the zero-dependency SDK package:

```shell
go get github.com/thedavidweng/amazon-photos/pkg/aphotos
```

`aphotos.NewClient` performs zero network or disk I/O on construction and accepts an optional custom `*http.Client` or `http.RoundTripper`:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

func main() {
	client, err := aphotos.NewClient(aphotos.Options{
		Region: aphotos.RegionUS,
		Cookies: map[string]string{
			"session-id": os.Getenv("APHOTOS_SESSION_ID"),
			"ubid-main":  os.Getenv("APHOTOS_UBID"),
			"at-main":    os.Getenv("APHOTOS_AT"),
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	ctx := context.Background()

	usage, err := client.GetUsage(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Photos: %d items (%d bytes)\n", usage.Photo.Total.Count, usage.Photo.Total.Bytes)

	page, err := client.Search(ctx, aphotos.SearchOptions{
		Filter: aphotos.NewFilter().MediaType(aphotos.MediaTypePhotos).Year(2025),
		Limit:  50,
		Sort:   aphotos.SortContentDateDesc,
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, node := range page.Items {
		fmt.Printf("%s  %s  capture=%v  upload=%v\n",
			node.ID, node.Name, node.ContentProperties.ContentDate, node.CreatedDate)
	}
}
```

---

## `aphotos` CLI Quickstart

### Installation

**Homebrew (macOS / Linux):**

```shell
brew tap thedavidweng/homebrew-tap
brew install aphotos
```

**Go Install:**

```shell
go install github.com/thedavidweng/amazon-photos/cmd/aphotos@latest
```

**GitHub Releases:**

Download prebuilt archives for `darwin`, `linux`, or `windows` (`amd64` / `arm64`) with `checksums.txt` from [GitHub Releases](https://github.com/thedavidweng/amazon-photos/releases).

### Usage Examples

```shell
# 1. Import browser cookies (Netscape cookies.txt, JSON array/map, or Cookie header)
aphotos auth import ./cookies.json
aphotos auth status

# 2. Run graded diagnostics (config, region, root/usage, search probe)
aphotos doctor
aphotos doctor --write-probe --confirm

# 3. Check account storage usage
aphotos usage
aphotos usage --json

# 4. Search media (contentDate = capture date, createdDate = cloud upload date)
aphotos search --year 2024 --month 8 --sort content-desc
aphotos photos --things beach,sunset --limit 50 --json
aphotos videos --all --json

# 5. Upload files or directories with conflict verification
aphotos upload ./IMG_1024.HEIC --on-conflict skip
aphotos upload ./Vacation2026 --recursive --album ALBUM_NODE_ID --on-conflict skip --json

# 6. Download original media with size and MD5 verification
aphotos download NODE_ID_1 NODE_ID_2 --out ./downloads

# 7. Manage albums and folders
aphotos albums list
aphotos albums create "Summer 2026" NODE_ID_1 NODE_ID_2
aphotos albums add ALBUM_NODE_ID NODE_ID_3
aphotos albums remove ALBUM_NODE_ID NODE_ID_2
aphotos albums rename ALBUM_NODE_ID "Summer Trip 2026"
aphotos folders list

# 8. Inspect, trash, or restore nodes (no permanent purge in v1)
aphotos trash list
aphotos trash add NODE_ID_1 --confirm
aphotos trash restore NODE_ID_1
```

See [COMMANDS.md](COMMANDS.md) for all commands and flags, and [JSON_SCHEMA.md](JSON_SCHEMA.md) for the `--json` output contract.

---

## Machine Error Codes & Exit Codes

Every CLI failure maps deterministically to a process exit code and machine-readable error code:

| Exit Code | Error Code(s) | Category | Retryable | Description |
|---|---|---|---|---|
| `0` | — | — | — | Command succeeded (`"ok": true`) |
| `1` | `VALIDATION_FAILED`, `INVALID_FILTER` | `validation` | `false` | Invalid flags, arguments, or server filter DSL (`400`) |
| `2` | `AUTH_REQUIRED`, `AUTH_FAILED`, `PERMISSION_DENIED` | `auth` | `false` | Missing credentials, expired cookies (`401`), or forbidden (`403`) |
| `3` | `API_ERROR`, `REMOTE_PROTOCOL_ERROR`, `RESOURCE_NOT_FOUND`, `CONFLICT` | `api` | `false` | Remote API error, missing node (`404`), conflict (`409`), or checksum mismatch |
| `4` | `NETWORK_ERROR`, `RATE_LIMITED` | `network` | `true` | Transient network error, timeout, or HTTP `429` rate limit |
| `5` | `PARTIAL_SUCCESS` | `api` | `false` | Batch transfer or multi-page query completed with partial results |
| `6` | `CONFIRMATION_REQUIRED`, `READ_ONLY_VIOLATION` | `safety` | `false` | Missing `--confirm` on `trash add` or `doctor --write-probe` |
| `7` | `FILESYSTEM_ERROR` | `filesystem` | `false` | Local file read/write/permission error |
| `8` | `CONFIG_ERROR` | `config` | `false` | Missing/ambiguous region, missing cookie key, or malformed config |
| `130` | `INTERRUPTED` | `interrupted` | `false` | Cancelled via `SIGINT` / `SIGTERM` |

---

## Documentation

- [COMMANDS.md](COMMANDS.md) — Complete CLI command and flag reference
- [JSON_SCHEMA.md](JSON_SCHEMA.md) — Normative `--json` envelope schema and error table
- [CONTEXT.md](CONTEXT.md) & [GLOSSARY.md](GLOSSARY.md) — Domain model and terminology
- [docs/adr/0001-sdk-architecture.md](docs/adr/0001-sdk-architecture.md) — SDK architecture, regional routing, and retry rules
- [docs/adr/0002-cli-and-json-contract.md](docs/adr/0002-cli-and-json-contract.md) — CLI credential store and JSON contract
- [CONTRIBUTING.md](CONTRIBUTING.md) — Development setup and quality gates
- [SECURITY.md](SECURITY.md) — Vulnerability reporting and credential protection design

---

## Credits & Acknowledgments

Protocol research and edge-case behaviors are informed by prior open-source work in the Amazon Photos community:

- [`trevorhobenshield/amazon_photos`](https://github.com/trevorhobenshield/amazon_photos) (MIT) by Trevor Hobenshield — foundational Python reverse-engineering of the Amazon Photos / Drive v1 endpoints and filter DSL, along with community PR contributors:
  - PR [#18](https://github.com/trevorhobenshield/amazon_photos/pull/18) (`folder_Valid` / `os.makedirs` fix)
  - PR [#19](https://github.com/trevorhobenshield/amazon_photos/pull/19) (`.co.uk` regional domain mapping)
  - PR [#20](https://github.com/trevorhobenshield/amazon_photos/pull/20) (`at-acbjp` -> `co.jp` + `content-jp` upload host mapping)
  - PR [#27](https://github.com/trevorhobenshield/amazon_photos/pull/27) & PR [#35](https://github.com/trevorhobenshield/amazon_photos/pull/35) (underscore vs. hyphen cookie name normalization)
  - PR [#28](https://github.com/trevorhobenshield/amazon_photos/pull/28) (Windows filename sanitization on download)
  - PR [#29](https://github.com/trevorhobenshield/amazon_photos/pull/29) (`account/usage` `lastCalculated` field handling)
  - PR [#30](https://github.com/trevorhobenshield/amazon_photos/pull/30) (duplicate page-0 pagination offset fix)
  - PR [#31](https://github.com/trevorhobenshield/amazon_photos/pull/31) (`trashed` empty-list return fix)
  - PR [#33](https://github.com/trevorhobenshield/amazon_photos/pull/33) (empty search response guard)
  - PR [#34](https://github.com/trevorhobenshield/amazon_photos/pull/34) (`favorite`/`unfavorite` `PATCH /nodes/{id}` method fix)
- [`kpfitzgerald/amazon-photos-mcp`](https://github.com/kpfitzgerald/amazon-photos-mcp) — live US endpoint verification notes, `allPeople` aggregation category behavior, and cookie bridging insights.
- [`vanessa/immich-amazon-sync`](https://github.com/vanessa/immich-amazon-sync) (MIT) — additional protocol observations on upload conflict and trash semantics.

---

## License

Released under the [MIT License](LICENSE).
