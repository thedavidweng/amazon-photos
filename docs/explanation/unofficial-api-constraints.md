# Unofficial Amazon Photos API Constraints

This page explains the protocol constraints and limitations of the undocumented Amazon Photos / Amazon Drive v1 Web API.

## Discontinuation of the Public Drive API

Amazon shut down the public OAuth-based Amazon Drive REST API in 2023. The web application at `amazon.<tld>/photos` continues to operate over internal `/drive/v1` and regional `cdproxy/nodes` endpoints authenticated by browser session cookies. Because these endpoints are undocumented and private to Amazon's web client, they may change or throttle traffic at any time without notice.

## Regional Host Pairings

Amazon Photos partitions accounts across regional metadata and content hosts:

- **North America (`us`, `ca`)**: Metadata on `www.amazon.com/drive/v1` or `www.amazon.ca/drive/v1`; uploads on `content-na.drive.amazonaws.com/cdproxy/nodes`.
- **Europe (`uk`, `de`, `fr`, `it`, `es`, ...)**: Metadata on `www.amazon.<tld>/drive/v1`; uploads on `content-eu.drive.amazonaws.com/cdproxy/nodes`.
- **Japan (`jp`)**: Metadata on `www.amazon.co.jp/drive/v1`; uploads on `content-jp.drive.amazonaws.com/cdproxy/nodes`.

Sending regional cookies (`ubid-acbuk`, `at-acbuk`) to `.com` fails with `401 Unauthorized`. `pkg/aphotos` pairs every marketplace with its exact metadata, upload, and thumbnail hosts and refuses to guess `.com` when cookie suffixes are ambiguous.

## The `offset + limit <= 9999` Window Ceiling

While `GET /search` and `GET /nodes/{id}/children` allow deep offset pagination, `GET /nodes` and `GET /trash` enforce a strict server window limit of `offset + limit <= 9999`. Requests exceeding `9999` fail with HTTP `400`.

To enumerate large node sets on `/nodes`, `QueryAllNodes` supports `TimeWindowSubdivide: true`, which recursively bisects a `[WindowStart, WindowEnd]` timestamp interval whenever a sub-window's count exceeds `9999`.

## Timestamp Distinction: `contentDate` vs. `createdDate`

Media nodes carry two primary timestamps:

- `contentProperties.contentDate`: The original media capture time parsed from EXIF or video container metadata.
- `createdDate`: The time the node was uploaded to Amazon Photos.

Photos uploaded years after they were taken have a recent `createdDate` and an older `contentDate`. Both the SDK and CLI expose explicit sort orders (`content-desc`/`content-asc` vs. `created-desc`/`created-asc`) so callers never confuse upload time with capture time.

## No Permanent Purge in v1

Amazon's internal web API includes a permanent purge endpoint (`POST /bulk/nodes/purge`), which irreversibly deletes media bypassing trash retention. To prevent accidental data loss, `amazon-photos` intentionally omits permanent purge and provides only reversible trash operations (`MoveToTrash` and `RestoreFromTrash`, with `--confirm` required on `aphotos trash add`).
