# amazon-photos Domain Glossary

## Core Creed

**`amazon-photos` provides a zero-side-effect, strongly typed Go SDK (`pkg/aphotos`) and automation-friendly CLI (`aphotos`) for the unofficial Amazon Photos / Drive v1 Web API**, prioritizing verification, explicit regional routing, bounded concurrency, and credential safety over speculative features.

## Core Concepts

- **aphotos** — Both the public Go SDK package (`github.com/thedavidweng/amazon-photos/pkg/aphotos`) and the installed CLI binary (`cmd/aphotos`).
- **Node** — A remote entity in Amazon Photos / Drive (`FILE`, `FOLDER`, or `VISUAL_COLLECTION`), identified by an opaque node ID and carrying ownership (`ownerId`), status (`AVAILABLE`, `TRASH`), parent IDs, timestamps, settings, content properties, and preserved raw JSON (`Raw`).
- **Root Node** — The account's top-level folder (`isRoot: true`), resolved via `GET /nodes?filters=isRoot:true` and supplying the account's `ownerId`.
- **Visual Collection (Album)** — A `Node` with `kind: "VISUAL_COLLECTION"` representing a photo/video album whose members are managed via `/nodes/{id}/children`.
- **Content Date vs. Created Date** — `contentProperties.contentDate` is the media capture timestamp (EXIF/video creation time); `createdDate` is the cloud upload/node creation timestamp. The SDK and CLI preserve both in UTC and never conflate them.
- **Regional Route (`RegionConfig`)** — The paired set of regional endpoints (`DriveURL`, `UploadURL` / `cdproxy`, `ThumbnailURL`), canonical cookie names (`session-id`, `ubid-*`, `at-*`), and `EvidenceStatus` (`verified` vs. `experimental`) for an Amazon marketplace (`us`, `ca`, `uk`, `jp`, or European TLDs).
- **Evidence Status** — Whether a regional route or endpoint has prior recorded live account verification (`verified`, e.g., US `amazon.com`) or is derived from static source/PR evidence (`experimental`, e.g., CA, UK, JP, EU TLDs).
- **Canonical Wire Cookies** — Hyphenated browser cookie names (`session-id`, `ubid-main`, `at-main`, `ubid-acb<tld>`, `at-acb<tld>`) sent on outgoing requests. Input aliases using underscores (`session_id`, `ubid_main`, `at_main`, `ubid_acb<tld>`, `at_acb<tld>`) are normalized at construction and never duplicated on the wire.
- **Session ID Header** — The required `x-amzn-sessionid` HTTP request header populated from the normalized `session-id` cookie value.
- **Redaction (`Redact`)** — Central scrubbing of sensitive authentication cookies, session IDs, tokens, and pre-signed `contentRedirection` URLs/queries before any error string, log line, or JSON envelope is emitted.
- **Confirmed vs. Unconfirmed Upload** — An upload via `cdproxy/nodes` is `confirmed` only when the server returns HTTP 200/201 with a verifiable remote node ID; otherwise it is `unconfirmed` and requires reconciliation rather than blind replay or false success reporting.
- **Envelope** — The versioned CLI `--json` output wrapper (`ok`, `data`, `error`, `meta`) paired with deterministic process exit codes.

## Architectural Sources of Truth

- Architecture & retry/routing decisions: `docs/adr/0001-sdk-architecture.md`
- CLI contract, credential store & JSON envelope: `docs/adr/0002-cli-and-json-contract.md`
- Agent & repository rules: `AGENTS.md`
