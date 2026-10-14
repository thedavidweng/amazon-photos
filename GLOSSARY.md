# amazon-photos Domain Glossary

See `CONTEXT.md` for full context and architectural pointers.

## Terms

- **aphotos** — Public Go SDK package (`pkg/aphotos`) and CLI binary (`cmd/aphotos`).
- **Client** — Stateless, concurrency-bounded Amazon Photos API client created via `aphotos.NewClient` with zero I/O side effects on construction.
- **Region** — Explicit Amazon marketplace identifier (`us`, `ca`, `uk`, `jp`, or EU TLD such as `de`, `fr`, `it`, `es`) determining `DriveURL`, `UploadURL` (`cdproxy`), `ThumbnailURL`, and canonical regional cookies.
- **EvidenceStatus** — `verified` (backed by recorded account verification, US) or `experimental` (static/PR protocol route).
- **Node** — Remote Amazon Photos / Drive object (`FILE`, `FOLDER`, `VISUAL_COLLECTION`) with preserved `Raw` JSON.
- **ContentDate** — Media capture timestamp (`contentProperties.contentDate`), distinct from cloud node `CreatedDate`.
- **Usage** — Account storage breakdown across categories (`photo`, `video`, `other`, `doc`) with `billable` and `total` byte/file counts and `lastCalculated`.
- **Redact** — Central sanitizer that strips cookies, `x-amzn-sessionid`, tokens, and pre-signed query strings from errors and logs.
