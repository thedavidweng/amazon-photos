# Regions and Verification Status

`amazon-photos` pairs each Amazon marketplace region with its exact `drive/v1` metadata URL, `cdproxy/nodes` upload URL, thumbnail host, required browser session cookies, and evidence grade.

## Regional Endpoint Table

| Region Code | Marketplace | Drive API (`DriveURL`) | Upload Host (`UploadURL`) | Thumbnail Host (`ThumbnailURL`) | Required Canonical Cookies | Evidence Status |
|---|---|---|---|---|---|---|
| `us` (`com`) | United States | `https://www.amazon.com/drive/v1` | `https://content-na.drive.amazonaws.com/cdproxy/nodes` | `https://thumbnails-photos.amazon.com/v1/thumbnail` | `session-id`, `ubid-main`, `at-main` | `verified` |
| `ca` | Canada | `https://www.amazon.ca/drive/v1` | `https://content-na.drive.amazonaws.com/cdproxy/nodes` | `https://thumbnails-photos.amazon.ca/v1/thumbnail` | `session-id`, `ubid-acbca`, `at-acbca` | `experimental` |
| `uk` (`co.uk`) | United Kingdom | `https://www.amazon.co.uk/drive/v1` | `https://content-eu.drive.amazonaws.com/cdproxy/nodes` | `https://thumbnails-photos.amazon.co.uk/v1/thumbnail` | `session-id`, `ubid-acbuk`, `at-acbuk` | `experimental` |
| `jp` (`co.jp`) | Japan | `https://www.amazon.co.jp/drive/v1` | `https://content-jp.drive.amazonaws.com/cdproxy/nodes` | `https://thumbnails-photos.amazon.co.jp/v1/thumbnail` | `session-id`, `ubid-acbjp`, `at-acbjp` | `experimental` |
| `de` | Germany | `https://www.amazon.de/drive/v1` | `https://content-eu.drive.amazonaws.com/cdproxy/nodes` | `https://thumbnails-photos.amazon.de/v1/thumbnail` | `session-id`, `ubid-acbde`, `at-acbde` | `experimental` |
| `fr` | France | `https://www.amazon.fr/drive/v1` | `https://content-eu.drive.amazonaws.com/cdproxy/nodes` | `https://thumbnails-photos.amazon.fr/v1/thumbnail` | `session-id`, `ubid-acbfr`, `at-acbfr` | `experimental` |
| `it` | Italy | `https://www.amazon.it/drive/v1` | `https://content-eu.drive.amazonaws.com/cdproxy/nodes` | `https://thumbnails-photos.amazon.it/v1/thumbnail` | `session-id`, `ubid-acbit`, `at-acbit` | `experimental` |
| `es` | Spain | `https://www.amazon.es/drive/v1` | `https://content-eu.drive.amazonaws.com/cdproxy/nodes` | `https://thumbnails-photos.amazon.es/v1/thumbnail` | `session-id`, `ubid-acbes`, `at-acbes` | `experimental` |
| `nl`, `se`, `pl`, `be`, `tr` | EU TLDs | `https://www.amazon.{tld}/drive/v1` | `https://content-eu.drive.amazonaws.com/cdproxy/nodes` | `https://thumbnails-photos.amazon.{tld}/v1/thumbnail` | `session-id`, `ubid-acb{tld}`, `at-acb{tld}` | `experimental` |
| `au` (`com.au`) | Australia | `https://www.amazon.com.au/drive/v1` | `https://content-na.drive.amazonaws.com/cdproxy/nodes` | `https://thumbnails-photos.amazon.com.au/v1/thumbnail` | `session-id`, `ubid-acbau`, `at-acbau` | `experimental` |

## Evidence Grading Policy

- **`verified`**: Backed by recorded live account verification against the production marketplace (`us`).
- **`experimental`**: Derived from static protocol routing and community pull requests (`trevorhobenshield/amazon_photos` PRs #19, #20, #27, #35). A region is never promoted from `experimental` to `verified` without live end-to-end verification on a real account in that marketplace.

## Cookie Normalization Rules

- Input maps and cookie files may use either underscore (`session_id`, `ubid_main`, `at_main`, `ubid_acb*`, `at_acb*`) or hyphen (`session-id`, `ubid-main`, `at-main`, `ubid-acb*`, `at-acb*`) keys.
- `CanonicalCookieKey` normalizes all recognized auth cookie keys to hyphenated browser wire format.
- If both underscore and hyphen variants are supplied with different values, `NewClient` fails fast with `*ConfigError`.
