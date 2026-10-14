# Search Filter DSL, Pagination, and Aggregations

Reference for the Amazon Photos / Drive v1 search filter DSL, `FilterBuilder`, pagination windows, and server aggregations.

## `FilterBuilder` Methods

`aphotos.NewFilter()` constructs escaped, conjunction-joined (`AND`) filter strings:

| Method | Emitted Clause | Notes |
|---|---|---|
| `MediaType(MediaPhotosAndVideos)` | `type:(PHOTOS OR VIDEOS)` | Also accepts `MediaPhotos` (`PHOTOS`) and `MediaVideos` (`VIDEOS`) |
| `Kind(NodeKindFile)` | `kind:FILE` | Accepts `FILE`, `FOLDER`, `VISUAL_COLLECTION` |
| `Status(NodeKindAvailable)` | `status:AVAILABLE` | Accepts `AVAILABLE`, `TRASH` |
| `IsRoot(true)` | `isRoot:true` | Account root folder lookup |
| `NameExact("IMG_0001.JPG")` | `name:IMG_0001.JPG` | Quotes values containing spaces or special characters |
| `NamePrefix("IMG_")` | `name:IMG_*` | Prefix match |
| `ParentID(parentID)` | `parents:<id>` | Filter by parent folder/album ID |
| `ContentTypePrefix("image/")` | `contentProperties.contentType:image/*` | MIME prefix |
| `MD5(md5Hex)` | `contentProperties.md5:<32-hex>` | Validates 32-character hex digest |
| `Year(2025)` | `timeYear:(2025)` | `1900`–`2200` |
| `Month(8)` | `timeMonth:(8)` | `1`–`12` |
| `Day(15)` | `timeDay:(15)` | `1`–`31` |
| `DateRange(field, low, high, incLow, incHigh)` | `field:["2025-01-01T00:00:00Z" TO "2025-12-31T23:59:59Z"]` | Fields: `createdDate`, `modifiedDate`, `contentProperties.contentDate` |
| `PeopleIDs(ids...)` | `people:(id1 OR id2)` | Person cluster IDs |
| `ClusterIDs(ids...)` | `clusterId:(id1 OR id2)` | Face cluster IDs |
| `Locations(locs...)` | `location:(loc1 OR loc2)` | Location terms |
| `Things(andTerms, orTerms)` | `things:(a AND b OR c)` | Object/scene recognition terms |
| `AndRaw(raw)` | `(<raw>)` | Appends raw server filter clause (rejects control characters) |

## Sort Orders (`SortOrder`)

| Constant | CLI `--sort` Value | Server Expression | Timestamp / Field |
|---|---|---|---|
| `SortCreatedDesc` | `created-desc` | `['createdDate DESC']` | Cloud upload date (newest first) |
| `SortCreatedAsc` | `created-asc` | `['createdDate ASC']` | Cloud upload date (oldest first) |
| `SortContentDateDesc` | `content-desc` | `['contentProperties.contentDate DESC']` | EXIF/video capture date (newest first) |
| `SortContentDateAsc` | `content-asc` | `['contentProperties.contentDate ASC']` | EXIF/video capture date (oldest first) |
| `SortModifiedDesc` | `modified-desc` | `['modifiedDate DESC']` | Node modification date (newest first) |
| `SortModifiedAsc` | `modified-asc` | `['modifiedDate ASC']` | Node modification date (oldest first) |
| `SortNameAsc` | `name-asc` | `['name ASC']` | File/folder name A–Z |
| `SortNameDesc` | `name-desc` | `['name DESC']` | File/folder name Z–A |

## Pagination Constraints

- **Page size (`limit`)**: `1`–`200` (`MaxPageLimit = 200`, `DefaultPageLimit = 50`).
- **Offset advancement**: Pages advance strictly by `offset + limit` and deduplicate items by `Node.ID`.
- **`/search` and `/nodes/{id}/children`**: Support unbounded offset pagination via `SearchAll`, `SearchIter`, and `ListAllChildren`.
- **`/nodes` and `/trash` `9999` window limit**: Amazon's `/nodes` and `/trash` endpoints enforce `offset + limit <= 9999` (`MaxNodeOffsetWindow = 9999`). `QueryAllNodes` stops at the `9999` ceiling (with a warning in `Page.Warnings`) unless `TimeWindowSubdivide: true` is set with `WindowStart` and `WindowEnd` to recursively bisect time ranges.

## Aggregations

`GetAggregations` and `aphotos aggregations --category <cat>` support the following server categories:

- `all` (`AggAll`)
- `allPeople` (`AggAllPeople` — primary category for named/unnamed face clusters)
- `clusterId` (`AggClusterID`)
- `familyMembers` (`AggFamilyMembers`)
- `favorite` (`AggFavorite`)
- `location` (`AggLocation`)
- `people` (`AggPeople`)
- `things` (`AggThings`)
- `time` (`AggTime`)
- `type` (`AggType`)
