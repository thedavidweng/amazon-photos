# amazon-photos (`aphotos`)

<p align="center">
  <a href="https://github.com/thedavidweng/amazon-photos/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/thedavidweng/amazon-photos/ci.yml?branch=main&style=flat-square&label=ci" alt="CI"></a>
  <a href="https://github.com/thedavidweng/amazon-photos/releases"><img src="https://img.shields.io/github/v/release/thedavidweng/amazon-photos?style=flat-square" alt="Release"></a>
  <a href="https://github.com/thedavidweng/amazon-photos/blob/main/LICENSE"><img src="https://img.shields.io/github/license/thedavidweng/amazon-photos?style=flat-square" alt="License"></a>
  <img src="https://img.shields.io/badge/go-%3E%3D1.27.3-blue?style=flat-square" alt="Go">
</p>

> **Disclaimer**: Unofficial client; not affiliated with, endorsed by, or sponsored by Amazon.com, Inc.

`amazon-photos` provides a pure Go SDK (`github.com/thedavidweng/amazon-photos/pkg/aphotos`) and single-binary CLI (`aphotos`) for Amazon Photos / Amazon Drive v1, with structured JSON output, explicit regional routing (`content-na.drive.amazonaws.com`, `content-eu.drive.amazonaws.com`, `content-jp.drive.amazonaws.com`), and verified media transfers.

## Related projects

Three sibling projects share one Amazon Photos foundation:

| Project | What it is | Relation |
| --- | --- | --- |
| [`amazon-photos`](https://github.com/thedavidweng/amazon-photos) | Go SDK (`pkg/aphotos`) and `aphotos` CLI | This project; the foundation both apps below import |
| [`rainforest`](https://github.com/thedavidweng/rainforest) | Cross-platform desktop app for one-time upload, one-way folder backup, and recovery download | End-user client on this SDK |
| [`immich-amazon-sync`](https://github.com/thedavidweng/immich-amazon-sync) | Self-hosted container service | Server-side Immich → Amazon Photos backup on this SDK |

## Highlights

- **Pure Go SDK (`pkg/aphotos`)**: Zero third-party dependencies, zero network/disk I/O on `NewClient`, injectable `http.RoundTripper`, and bounded concurrency semaphores.
- **Agent-First CLI (`aphotos`)**: Versioned `--json` envelope (`schema_version: "v1"`), thread-safe `stderr` progress, central credential redaction (`aphotos.Redact`), and deterministic exit codes (`0`–`8`, `130`).
- **Verified Transfers**: Streaming uploads require a verified remote node ID or matching MD5 before reporting `confirmed` or `skipped`; downloads verify remote byte size and MD5 and never overwrite existing local files.
- **Explicit Regional Routing**: Supports `us` (`verified`) and `ca`, `uk`, `jp`, and European TLDs (`experimental`) with fail-fast cookie region inference.

## Quickstart

### Install `aphotos` CLI

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

### First CLI Commands

```shell
aphotos auth login
# For existing cookie files: aphotos auth import ./cookies.json
aphotos doctor
aphotos usage --json
aphotos search --year 2025 --sort content-desc --json
aphotos upload ./IMG_1024.HEIC --on-conflict skip --json
aphotos download NODE_ID --out ./downloads --json
aphotos albums list --json
aphotos trash list --json
```

### Install Go SDK

```shell
go get github.com/thedavidweng/amazon-photos/pkg/aphotos
```

## Documentation

Organized following the [Diátaxis](https://diataxis.fr/) framework:

### Tutorials (Learning-Oriented)

- [First-Time CLI Setup, Authentication, Upload, and Verification](docs/tutorials/getting-started-cli.md)
- [Build Your First Client with the Go SDK](docs/tutorials/first-sdk-client.md)

### How-To Guides (Task-Oriented)

- [Batch Upload and Download](docs/how-to/batch-transfers.md)
- [Configure Amazon Regions](docs/how-to/configure-regions.md)
- [Automate Workflows with AI Agents](docs/how-to/agent-automation.md)
- [Troubleshoot Authentication and Transfers](docs/how-to/troubleshooting.md)

### Reference (Information-Oriented)

- [CLI Command Reference](docs/reference/cli-commands.md)
- [Go SDK API Reference (`pkg/aphotos`)](docs/reference/sdk-api.md)
- [JSON Schema and Exit Codes](docs/reference/json-schema.md)
- [Search Filter DSL, Pagination, and Aggregations](docs/reference/search-filters-and-pagination.md)
- [Regions and Verification Status](docs/reference/regions.md)
- [Configuration File and Environment Variables](docs/reference/configuration.md)

### Explanation (Understanding-Oriented)

- [SDK and CLI Architecture](docs/explanation/architecture.md)
- [Unofficial Amazon Photos API Constraints](docs/explanation/unofficial-api-constraints.md)
- [Authentication and Credential Security Model](docs/explanation/security-model.md)
- [Upload Confirmation, Retries, Idempotency, and Conflict Handling](docs/explanation/transfer-integrity-and-conflicts.md)

### Project & Architecture Records

- [ADR 0001: SDK Architecture](docs/adr/0001-sdk-architecture.md)
- [ADR 0002: CLI and JSON Contract](docs/adr/0002-cli-and-json-contract.md)
- [Domain Context](CONTEXT.md) & [Glossary](GLOSSARY.md)
- [Contributing](CONTRIBUTING.md) & [Security Policy](SECURITY.md)
- [Changelog](CHANGELOG.md)

## Credits & Acknowledgments

Protocol research and edge-case behaviors are informed by prior open-source work in the Amazon Photos community:

- [`trevorhobenshield/amazon_photos`](https://github.com/trevorhobenshield/amazon_photos) (MIT) by Trevor Hobenshield and community PR contributors (#18, #19, #20, #27, #28, #29, #30, #31, #33, #34, #35)
- [`kpfitzgerald/amazon-photos-mcp`](https://github.com/kpfitzgerald/amazon-photos-mcp)
- [`vanessa/immich-amazon-sync`](https://github.com/vanessa/immich-amazon-sync) (MIT)

## License

Released under the [MIT License](LICENSE).
