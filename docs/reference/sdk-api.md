# Go SDK API Reference (`pkg/aphotos`)

Package `github.com/thedavidweng/amazon-photos/pkg/aphotos` is a pure Go, zero-third-party-dependency client for the Amazon Photos / Drive v1 Web API.

## Client Construction

```go
func NewClient(opts Options) (*Client, error)
func (c *Client) Close() error
func (c *Client) RegionConfig() RegionConfig
```

### `Options`

| Field | Type | Default | Description |
|---|---|---|---|
| `Region` | `Region` | Inferred from cookies | Marketplace identifier (`RegionUS`, `RegionCA`, `RegionUK`, `RegionJP`, `RegionDE`, ...) |
| `Cookies` | `map[string]string` | Required | Browser session cookies (`session-id`, `ubid-*`, `at-*`) |
| `HTTPClient` | `*http.Client` | `30s` timeout client | Custom HTTP client |
| `Transport` | `http.RoundTripper` | `http.DefaultTransport` | Injected transport seam for tests or proxies |
| `DriveURL` | `string` | Regional default | Override `/drive/v1` base URL |
| `UploadURL` | `string` | Regional default | Override `/cdproxy/nodes` upload URL |
| `ThumbnailURL` | `string` | Regional default | Override thumbnail base URL |
| `UserAgent` | `string` | Built-in browser UA | Custom `User-Agent` header |
| `MaxMetadataConcurrency` | `int` | `4` (`DefaultMetadataConcurrency`) | Semaphore cap for metadata/download requests |
| `MaxUploadConcurrency` | `int` | `2` (`DefaultUploadConcurrency`) | Semaphore cap for streaming uploads |
| `MaxRetries` | `int` | `3` (`DefaultMaxRetries`) | Read-only retry budget on `429`, `5xx`, and transient network errors |
| `DisableRetries` | `bool` | `false` | Disable read-only retries |
| `BaseBackoff` | `time.Duration` | `100ms` | Initial exponential backoff |
| `MaxBackoff` | `time.Duration` | `5s` | Maximum exponential backoff |
| `MaxRetryAfter` | `time.Duration` | `60s` | Maximum `Retry-After` wait duration |

`NewClient` performs zero network or filesystem I/O.

---

## Account & Node Lookup Methods

```go
func (c *Client) GetRoot(ctx context.Context) (*Node, error)
func (c *Client) GetNode(ctx context.Context, nodeID string) (*Node, error)
func (c *Client) GetUsage(ctx context.Context) (*Usage, error)
```

---

## Search, Node Queries, and Aggregations

```go
func (c *Client) Search(ctx context.Context, opts SearchOptions) (*Page[Node], error)
func (c *Client) SearchAll(ctx context.Context, opts SearchOptions) (*Page[Node], error)
func (c *Client) SearchIter(ctx context.Context, opts SearchOptions) iter.Seq2[Node, error]

func (c *Client) QueryNodes(ctx context.Context, opts NodeQueryOptions) (*Page[Node], error)
func (c *Client) QueryAllNodes(ctx context.Context, opts NodeQueryOptions) (*Page[Node], error)

func (c *Client) ListChildren(ctx context.Context, parentID string, opts ChildrenOptions) (*Page[Node], error)
func (c *Client) ListAllChildren(ctx context.Context, parentID string, opts ChildrenOptions) (*Page[Node], error)

func (c *Client) GetAggregations(ctx context.Context, opts AggregationOptions) (map[string]AggregationBucket, error)
```

Node list methods reject missing/null `count` or `data`, invalid counts, missing Node IDs, and premature empty pages with `*RemoteProtocolError`. Explicit `{"count":0,"data":[]}` is valid; nested `info.nodeId` is supported. Read requests require a final HTTP 2xx response.

Single-page `Page.Complete` describes the requested page, so it can coexist with `HasMore=true`. All-pages helpers retain collected nodes and return `*PartialResultError` with `Complete=false` if pagination stops short of the highest advertised count. An invalid first page returns `*RemoteProtocolError` without a page.

---

## Upload and Download Methods

```go
func (c *Client) Upload(ctx context.Context, req UploadRequest) (*TransferResult, error)
func (c *Client) UploadFile(ctx context.Context, localPath string, opts UploadOptions) (*TransferResult, error)
func (c *Client) UploadBatch(ctx context.Context, paths []string, opts BatchUploadOptions) ([]TransferResult, error)

func (c *Client) Download(ctx context.Context, req DownloadRequest) (*TransferResult, error)
func (c *Client) DownloadBatch(ctx context.Context, nodeIDs []string, opts BatchDownloadOptions) ([]TransferResult, error)
func IsMediaFileName(name string) bool
```

### `TransferStatus` and `ConflictPolicy`

- `TransferConfirmed` (`"confirmed"`): Upload returned HTTP `200`/`201` with a non-empty remote `NodeID`, or download completed and passed size/MD5 checks.
- `TransferSkipped` (`"skipped"`): Upload `--on-conflict skip` verified an existing remote node with matching MD5, or download `--on-conflict skip` preserved an existing local file.
- `TransferUnconfirmed` (`"unconfirmed"`): Upload outcome or conflict match could not be verified (e.g. response missing node ID or remote conflict candidate missing MD5).
- `TransferFailed` (`"failed"`): Transfer failed.

Conflict policies: `ConflictFail` (`"fail"`), `ConflictSkip` (`"skip"`), `ConflictRename` (`"rename"`).

### `TransferResult`

| Field | Type | Description |
|---|---|---|
| `Status` | `TransferStatus` | `confirmed`, `skipped`, `unconfirmed`, or `failed` |
| `NodeID` | `string` | Remote node ID |
| `Node` | `*Node` | Decoded remote `Node` when available |
| `Name` | `string` | File name |
| `LocalPath` | `string` | Local file path |
| `Bytes` | `int64` | Transferred byte count |
| `MD5` | `string` | Lowercase hex MD5 digest of streamed bytes |
| `MD5Verified` | `bool` | `true` when downloaded bytes matched a non-empty expected remote MD5 |
| `ConflictResolved` | `bool` | `true` when a conflict policy (`skip` or `rename`) resolved a collision |
| `Warning` | `string` | Non-fatal verification warning (e.g. remote node lacked MD5 on download) |
| `Error` | `error` | Underlying typed SDK error (`json:"-"`) |
| `ErrorMessage` | `string` | Redacted error string |

---

## Albums, Folders, Trash, and Node Settings

```go
func (c *Client) CreateFolder(ctx context.Context, name, parentID string) (*Node, error)
func (c *Client) ListFolders(ctx context.Context, parentID string, limit, offset int) ([]Node, int, error)

func (c *Client) CreateAlbum(ctx context.Context, name string, initialNodeIDs ...string) (*Node, error)
func (c *Client) ListAlbums(ctx context.Context, limit, offset int) ([]Node, int, error)
func (c *Client) AddAlbumMembers(ctx context.Context, albumID string, nodeIDs []string) error
func (c *Client) RemoveAlbumMembers(ctx context.Context, albumID string, nodeIDs []string) error
func (c *Client) RenameNode(ctx context.Context, nodeID, newName string) (*Node, error)

func (c *Client) ListTrash(ctx context.Context, opts TrashListOptions) ([]Node, int, error)
func (c *Client) ListAllTrash(ctx context.Context) ([]Node, error)
func (c *Client) MoveToTrash(ctx context.Context, nodeIDs []string) error
func (c *Client) RestoreFromTrash(ctx context.Context, nodeIDs []string) error

func (c *Client) SetFavorite(ctx context.Context, nodeIDs []string, favorite bool) ([]Node, error)
func (c *Client) SetHidden(ctx context.Context, nodeIDs []string, hidden bool) ([]Node, error)
```

---

## Typed Error Model

All SDK errors support `errors.As` and scrub sensitive tokens via `Redact(s string) string`:

| Error Type | Meaning |
|---|---|
| `*ConfigError` | Invalid client option, missing cookie, ambiguous region, or invalid local path |
| `*AuthenticationError` | HTTP `401 Unauthorized` (never retried) |
| `*PermissionError` | HTTP `403 Forbidden` |
| `*RateLimitError` | HTTP `429 Too Many Requests` (carries `RetryAfter time.Duration`) |
| `*InvalidFilterError` | HTTP `400 Bad Request` or malformed filter expression |
| `*NotFoundError` | HTTP `404 Not Found` |
| `*ConflictError` | HTTP `409 Conflict` (carries `ConflictingNodeID` when reported by server) or local download file collision |
| `*TransientNetworkError` | Network transport error, premature EOF, or HTTP `5xx`; carries parsed `RetryAfter time.Duration` when supplied by the server |
| `*RemoteProtocolError` | Unexpected final HTTP status, malformed JSON, missing required protocol fields, or download size/MD5 mismatch |
| `*PartialResultError` | Unconfirmed upload, unverified conflict skip, or partial batch/pagination result |

JSON mutations and uploads require final HTTP 2xx status; final 1xx/3xx responses preserve status and request ID in `*RemoteProtocolError`. Existing HTTP 4xx/5xx classifications remain intact. A 2xx upload still requires a verifiable Node ID to be confirmed.

`TransientNetworkError.RetryAfter` preserves a positive `Retry-After` delta-seconds or HTTP-date delay, including HTTP 503 and response-body read failures. Missing, invalid, zero, or expired values yield zero. The exposed delay is not capped by `Options.MaxRetryAfter`, which limits automatic waits. This field does not authorize replaying an uncertain mutation or upload; their existing replay policy is unchanged.
