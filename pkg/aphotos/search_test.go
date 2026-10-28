package aphotos_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

func TestFilterBuilder_BuildAndEscaping(t *testing.T) {
	t.Run("composite search filter matching protocol example", func(t *testing.T) {
		f, err := aphotos.NewFilter().
			MediaType(aphotos.MediaPhotosAndVideos).
			Things([]string{"plant", "beach"}, []string{"moon"}).
			Year(2023).
			Month(8).
			Day(14).
			Locations("CAN#BC#Vancouver").
			Build()
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		want := "type:(PHOTOS OR VIDEOS) AND things:(plant AND beach OR moon) AND timeYear:(2023) AND timeMonth:(8) AND timeDay:(14) AND location:(CAN#BC#Vancouver)"
		if f != want {
			t.Errorf("Build() =\n  %s\nwant:\n  %s", f, want)
		}
	})

	t.Run("node filters and escaping of spaces, quotes, and injection attempts", func(t *testing.T) {
		f, err := aphotos.NewFilter().
			Kind(aphotos.NodeKindFile).
			Status(aphotos.NodeStatusAvailable).
			IsRoot(false).
			NameExact(`my "vacation" photo (1).jpg`).
			NamePrefix(`summer 2026`).
			ParentID("root-123").
			ContentTypePrefix("image").
			MD5("d41d8cd98f00b204e9800998ecf8427e").
			PeopleIDs("person-1", "person-2").
			ClusterIDs("cluster-abc").
			Build()
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		for _, wantSub := range []string{
			"kind:FILE",
			"status:AVAILABLE",
			"isRoot:false",
			`name:"my \"vacation\" photo (1).jpg"`,
			`name:"summer 2026"*`,
			"parents:root-123",
			"contentProperties.contentType:image*",
			"contentProperties.md5:d41d8cd98f00b204e9800998ecf8427e",
			"people:(person-1 OR person-2)",
			"clusterId:(cluster-abc)",
		} {
			if !strings.Contains(f, wantSub) {
				t.Errorf("expected filter %q to contain %q", f, wantSub)
			}
		}
	})

	t.Run("injection attempt in NameExact cannot break out of quotes", func(t *testing.T) {
		f, err := aphotos.NewFilter().
			NameExact(`foo" OR isRoot:true OR name:"bar`).
			Build()
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		want := `name:"foo\" OR isRoot:true OR name:\"bar"`
		if f != want {
			t.Errorf("Build() = %s, want %s", f, want)
		}
	})

	t.Run("date range inclusive, exclusive, and open bounds", func(t *testing.T) {
		low := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		high := time.Date(2023, 12, 31, 23, 59, 59, 0, time.UTC)

		inc, err := aphotos.NewFilter().
			DateRange("createdDate", low, high, true, true).
			Build()
		if err != nil {
			t.Fatalf("Build inclusive date range failed: %v", err)
		}
		wantInc := `createdDate:["2023-01-01T00:00:00Z" TO "2023-12-31T23:59:59Z"]`
		if inc != wantInc {
			t.Errorf("inclusive DateRange = %s, want %s", inc, wantInc)
		}

		exc, err := aphotos.NewFilter().
			DateRange("contentProperties.contentDate", low, time.Time{}, false, true).
			Build()
		if err != nil {
			t.Fatalf("Build open-high date range failed: %v", err)
		}
		wantExc := `contentProperties.contentDate:{"2023-01-01T00:00:00Z" TO *]`
		if exc != wantExc {
			t.Errorf("open-high DateRange = %s, want %s", exc, wantExc)
		}
	})

	t.Run("RawFilter and AndRaw", func(t *testing.T) {
		rf, err := aphotos.RawFilter("  kind:VISUAL_COLLECTION  ").Build()
		if err != nil {
			t.Fatalf("RawFilter Build failed: %v", err)
		}
		if rf != "kind:VISUAL_COLLECTION" {
			t.Errorf("RawFilter = %q, want %q", rf, "kind:VISUAL_COLLECTION")
		}

		combined, err := aphotos.NewFilter().
			MediaType(aphotos.MediaPhotos).
			AndRaw("favorite:(true)").
			Build()
		if err != nil {
			t.Fatalf("AndRaw Build failed: %v", err)
		}
		if combined != "type:(PHOTOS) AND (favorite:(true))" {
			t.Errorf("AndRaw = %q, want %q", combined, "type:(PHOTOS) AND (favorite:(true))")
		}
	})

	t.Run("validation errors return InvalidFilterError", func(t *testing.T) {
		badCases := []struct {
			name string
			fb   *aphotos.FilterBuilder
		}{
			{
				name: "invalid month",
				fb:   aphotos.NewFilter().Month(13),
			},
			{
				name: "invalid day",
				fb:   aphotos.NewFilter().Day(0),
			},
			{
				name: "reversed date range",
				fb: aphotos.NewFilter().DateRange(
					"createdDate",
					time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
					time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
					true, true,
				),
			},
			{
				name: "empty name exact",
				fb:   aphotos.NewFilter().NameExact(""),
			},
			{
				name: "control character in value",
				fb:   aphotos.NewFilter().NameExact("bad\nname"),
			},
			{
				name: "empty raw filter",
				fb:   aphotos.RawFilter("   "),
			},
		}

		for _, tc := range badCases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := tc.fb.Build()
				if err == nil {
					t.Fatalf("expected error for %s, got nil", tc.name)
				}
				var filterErr *aphotos.InvalidFilterError
				if !errors.As(err, &filterErr) {
					t.Fatalf("expected *InvalidFilterError for %s, got %T (%v)", tc.name, err, err)
				}
			})
		}
	})
}

func TestGetAggregations_AllAndSingleCategoryShapes(t *testing.T) {
	t.Run("all category hits /search with limit=1 and searchContext=all and parses map", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet {
				t.Fatalf("Method = %s, want GET", req.Method)
			}
			if req.URL.Path != "/drive/v1/search" {
				t.Fatalf("Path = %s, want /drive/v1/search", req.URL.Path)
			}
			q := req.URL.Query()
			wantParams := map[string]string{
				"asset":           "ALL",
				"tempLink":        "false",
				"resourceVersion": "V2",
				"ContentType":     "JSON",
				"limit":           "1",
				"lowResThumbnail": "true",
				"searchContext":   "all",
				"groupByForTime":  "year",
			}
			for k, want := range wantParams {
				if got := q.Get(k); got != want {
					t.Errorf("query[%q] = %q, want %q", k, got, want)
				}
			}
			body := `{
				"count": 27599,
				"data": [],
				"aggregations": {
					"type": [
						{"value": "PHOTOS", "count": 26000},
						{"value": "VIDEOS", "count": 1599}
					],
					"things": [
						{"value": "beach", "count": 420}
					]
				}
			}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(body)),
				Request:    req,
			}, nil
		})

		client := newTestUSClient(t, rt, 0)
		aggs, err := client.GetAggregations(context.Background(), aphotos.AggAll)
		if err != nil {
			t.Fatalf("GetAggregations(AggAll) failed: %v", err)
		}
		if len(aggs["type"]) != 2 || aggs["type"][0].Value != "PHOTOS" || aggs["type"][0].Count != 26000 {
			t.Fatalf("unexpected type aggregations: %+v", aggs["type"])
		}
		if len(aggs["things"]) != 1 || aggs["things"][0].Value != "beach" || aggs["things"][0].Count != 420 {
			t.Fatalf("unexpected things aggregations: %+v", aggs["things"])
		}
		if len(aggs["things"][0].Raw) == 0 {
			t.Error("expected Raw JSON to be preserved on AggregationEntry")
		}
	})

	t.Run("single category hits /search/aggregation and handles both map and slice shapes", func(t *testing.T) {
		for _, shape := range []string{"map", "slice"} {
			t.Run(shape, func(t *testing.T) {
				rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if req.URL.Path != "/drive/v1/search/aggregation" {
						t.Fatalf("Path = %s, want /drive/v1/search/aggregation", req.URL.Path)
					}
					q := req.URL.Query()
					if q.Get("aggregationContext") != "all" {
						t.Errorf("aggregationContext = %q, want all", q.Get("aggregationContext"))
					}
					if q.Get("category") != string(aphotos.AggAllPeople) {
						t.Errorf("category = %q, want %q", q.Get("category"), aphotos.AggAllPeople)
					}
					if q.Get("resourceVersion") != "V2" || q.Get("ContentType") != "JSON" {
						t.Errorf("unexpected base query: %v", q)
					}

					var body string
					if shape == "map" {
						body = `{
							"aggregations": {
								"allPeople": [
									{
										"value": "CyChdySYdfj7DHsjdSHdy",
										"count": 4183,
										"searchData": {
											"clusterName": "Lara",
											"nodeId": " HeMReF-vvJiTTkdPIeWuoP"
										}
									}
								]
							}
						}`
					} else {
						body = `{
							"aggregations": [
								{
									"value": "CyChdySYdfj7DHsjdSHdy",
									"count": "4183",
									"searchData": {
										"clusterName": "Lara",
										"nodeId": " HeMReF-vvJiTTkdPIeWuoP"
									}
								}
							]
						}`
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewBufferString(body)),
						Request:    req,
					}, nil
				})

				client := newTestUSClient(t, rt, 0)
				aggs, err := client.GetAggregations(context.Background(), aphotos.AggAllPeople)
				if err != nil {
					t.Fatalf("GetAggregations(AggAllPeople) [%s] failed: %v", shape, err)
				}
				entries := aggs["allPeople"]
				if len(entries) != 1 {
					t.Fatalf("len(entries) = %d, want 1", len(entries))
				}
				if entries[0].Value != "CyChdySYdfj7DHsjdSHdy" || entries[0].Count != 4183 {
					t.Errorf("unexpected entry: %+v", entries[0])
				}
				if entries[0].SearchData == nil || entries[0].SearchData.ClusterName != "Lara" || entries[0].SearchData.NodeID != "HeMReF-vvJiTTkdPIeWuoP" {
					t.Errorf("unexpected SearchData: %+v", entries[0].SearchData)
				}
			})
		}
	})

	t.Run("unsupported category returns InvalidFilterError without HTTP call", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Fatal("unexpected HTTP call for invalid category")
			return nil, nil
		})
		client := newTestUSClient(t, rt, 0)
		_, err := client.GetAggregations(context.Background(), "dates")
		if err == nil {
			t.Fatal("expected error for unsupported category 'dates', got nil")
		}
		var filterErr *aphotos.InvalidFilterError
		if !errors.As(err, &filterErr) {
			t.Fatalf("expected *InvalidFilterError, got %T (%v)", err, err)
		}
	})
}

func TestSearch_SinglePageAnd400InvalidFilter(t *testing.T) {
	t.Run("default options populate filters, limit, offset, lowResThumbnail, searchContext, sort, and aggregations", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet || req.URL.Path != "/drive/v1/search" {
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
			}
			q := req.URL.Query()
			want := map[string]string{
				"asset":           "ALL",
				"tempLink":        "false",
				"resourceVersion": "V2",
				"ContentType":     "JSON",
				"filters":         "type:(PHOTOS OR VIDEOS)",
				"limit":           "200",
				"offset":          "0",
				"lowResThumbnail": "true",
				"searchContext":   "customer",
				"sort":            "['createdDate DESC']",
			}
			for k, w := range want {
				if got := q.Get(k); got != w {
					t.Errorf("query[%q] = %q, want %q", k, got, w)
				}
			}
			body := `{
				"count": 500,
				"data": [
					{
						"id": "node-1",
						"name": "IMG_0001.JPG",
						"kind": "FILE",
						"status": "AVAILABLE",
						"ownerId": "owner-1",
						"parents": ["root-1"],
						"createdDate": "2024-01-10T10:00:00.000Z",
						"modifiedDate": "2024-01-10T10:00:00.000Z",
						"contentProperties": {
							"size": 12345,
							"md5": "d41d8cd98f00b204e9800998ecf8427e",
							"contentType": "image/jpeg",
							"extension": "jpg",
							"contentDate": "2023-08-14T15:30:00.000Z"
						}
					}
				],
				"aggregations": {
					"type": [{"value": "PHOTOS", "count": 500}]
				}
			}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(body)),
				Request:    req,
			}, nil
		})

		client := newTestUSClient(t, rt, 0)
		page, err := client.Search(context.Background(), aphotos.SearchOptions{
			Sort: aphotos.SortCreatedDesc,
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if page.TotalCount != 500 || len(page.Items) != 1 {
			t.Fatalf("unexpected page: TotalCount=%d, len(Items)=%d", page.TotalCount, len(page.Items))
		}
		if page.Offset != 0 || page.Limit != aphotos.MaxPageLimit || page.NextOffset != 200 {
			t.Errorf("Offsets = (%d, %d, %d), want (0, 200, 200)", page.Offset, page.Limit, page.NextOffset)
		}
		if !page.HasMore || !page.Complete {
			t.Errorf("HasMore=%v, Complete=%v, want true, true", page.HasMore, page.Complete)
		}
		if len(page.Aggregations["type"]) != 1 {
			t.Errorf("expected aggregations to be populated, got %+v", page.Aggregations)
		}
	})

	t.Run("400 bad filter maps to InvalidFilterError with Filter preserved", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(bytes.NewBufferString(`{"message":"Invalid filter expression"}`)),
				Request:    req,
			}, nil
		})
		client := newTestUSClient(t, rt, 0)
		_, err := client.Search(context.Background(), aphotos.SearchOptions{
			Filters: "bad_field:(123)",
		})
		if err == nil {
			t.Fatal("expected error on HTTP 400, got nil")
		}
		var invErr *aphotos.InvalidFilterError
		if !errors.As(err, &invErr) {
			t.Fatalf("expected *InvalidFilterError, got %T (%v)", err, err)
		}
		if invErr.Filter != "bad_field:(123)" {
			t.Errorf("Filter = %q, want %q", invErr.Filter, "bad_field:(123)")
		}
	})
}

func TestQueryNodes_AndListChildren_SinglePageContract(t *testing.T) {
	t.Run("QueryNodes rejects offset + limit > 9999 before network call", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Fatal("unexpected HTTP call when offset + limit > 9999")
			return nil, nil
		})
		client := newTestUSClient(t, rt, 0)
		_, err := client.QueryNodes(context.Background(), aphotos.NodeQueryOptions{
			Offset: 9800,
			Limit:  200,
		})
		if err == nil {
			t.Fatal("expected InvalidFilterError when offset + limit > 9999, got nil")
		}
		var invErr *aphotos.InvalidFilterError
		if !errors.As(err, &invErr) {
			t.Fatalf("expected *InvalidFilterError, got %T (%v)", err, err)
		}
	})

	t.Run("QueryNodes sends filters, limit, offset, sort", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/drive/v1/nodes" {
				t.Fatalf("Path = %s, want /drive/v1/nodes", req.URL.Path)
			}
			q := req.URL.Query()
			if q.Get("filters") != "kind:VISUAL_COLLECTION" {
				t.Errorf("filters = %q, want kind:VISUAL_COLLECTION", q.Get("filters"))
			}
			if q.Get("limit") != "50" || q.Get("offset") != "100" {
				t.Errorf("limit=%q offset=%q, want 50, 100", q.Get("limit"), q.Get("offset"))
			}
			if q.Get("sort") != string(aphotos.SortModifiedDesc) {
				t.Errorf("sort = %q, want %q", q.Get("sort"), aphotos.SortModifiedDesc)
			}
			body := `{"count": 105, "data": [{"id": "album-1", "name": "Summer", "kind": "VISUAL_COLLECTION", "status": "AVAILABLE"}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(body)),
				Request:    req,
			}, nil
		})
		client := newTestUSClient(t, rt, 0)
		page, err := client.QueryNodes(context.Background(), aphotos.NodeQueryOptions{
			Filter: aphotos.NewFilter().Kind(aphotos.NodeKindVisualCollection),
			Limit:  50,
			Offset: 100,
			Sort:   aphotos.SortModifiedDesc,
		})
		if err != nil {
			t.Fatalf("QueryNodes failed: %v", err)
		}
		if page.TotalCount != 105 || len(page.Items) != 1 || page.Items[0].ID != "album-1" {
			t.Fatalf("unexpected page: %+v", page)
		}
		if page.HasMore {
			t.Errorf("HasMore = true, want false (100 + 50 >= 105)")
		}
	})

	t.Run("ListChildren sends kind:FOLDER when FoldersOnly is true", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/drive/v1/nodes/parent-folder-123/children" {
				t.Fatalf("Path = %s, want /drive/v1/nodes/parent-folder-123/children", req.URL.Path)
			}
			q := req.URL.Query()
			if q.Get("filters") != "kind:FOLDER" {
				t.Errorf("filters = %q, want kind:FOLDER", q.Get("filters"))
			}
			body := `{"count": 1, "data": [{"id": "child-1", "name": "Subfolder", "kind": "FOLDER", "status": "AVAILABLE"}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(body)),
				Request:    req,
			}, nil
		})
		client := newTestUSClient(t, rt, 0)
		page, err := client.ListChildren(context.Background(), "parent-folder-123", aphotos.ChildrenOptions{
			FoldersOnly: true,
		})
		if err != nil {
			t.Fatalf("ListChildren failed: %v", err)
		}
		if len(page.Items) != 1 || page.Items[0].ID != "child-1" {
			t.Fatalf("unexpected children: %+v", page.Items)
		}
	})
}

func TestPagination_MultiPageDeduplicationAndNoDuplicateOffsetZero(t *testing.T) {
	t.Run("SearchAll fetches page 0 then page 1 at offset=200 without repeating offset=0 and deduplicates shifting IDs", func(t *testing.T) {
		var offsets []string
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			q := req.URL.Query()
			off := q.Get("offset")
			offsets = append(offsets, off)

			var body string
			switch off {
			case "0":
				// 200 items: node-001 .. node-200
				items := make([]string, 0, 200)
				for i := 1; i <= 200; i++ {
					items = append(items, fmt.Sprintf(`{"id":"node-%03d","name":"f%03d.jpg","kind":"FILE","status":"AVAILABLE"}`, i, i))
				}
				body = fmt.Sprintf(`{"count": 205, "data": [%s]}`, strings.Join(items, ","))
			case "200":
				// Shifting list: node-200 appears again at start of page 2, plus node-201..node-205
				items := []string{
					`{"id":"node-200","name":"f200.jpg","kind":"FILE","status":"AVAILABLE"}`,
					`{"id":"node-201","name":"f201.jpg","kind":"FILE","status":"AVAILABLE"}`,
					`{"id":"node-202","name":"f202.jpg","kind":"FILE","status":"AVAILABLE"}`,
					`{"id":"node-203","name":"f203.jpg","kind":"FILE","status":"AVAILABLE"}`,
					`{"id":"node-204","name":"f204.jpg","kind":"FILE","status":"AVAILABLE"}`,
					`{"id":"node-205","name":"f205.jpg","kind":"FILE","status":"AVAILABLE"}`,
				}
				body = fmt.Sprintf(`{"count": 205, "data": [%s]}`, strings.Join(items, ","))
			default:
				t.Fatalf("unexpected offset %q", off)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(body)),
				Request:    req,
			}, nil
		})

		client := newTestUSClient(t, rt, 0)
		page, err := client.SearchAll(context.Background(), aphotos.SearchOptions{})
		if err != nil {
			t.Fatalf("SearchAll failed: %v", err)
		}
		if len(offsets) != 2 || offsets[0] != "0" || offsets[1] != "200" {
			t.Fatalf("offsets requested = %v, want [0 200] (never repeating offset=0)", offsets)
		}
		if len(page.Items) != 205 {
			t.Fatalf("deduplicated len(Items) = %d, want 205", len(page.Items))
		}
		if !page.Complete || page.HasMore {
			t.Errorf("Complete=%v HasMore=%v, want true, false", page.Complete, page.HasMore)
		}
		if len(page.Warnings) == 0 {
			t.Error("expected warning noting duplicate node IDs were deduplicated across shifting pages")
		}
	})

	t.Run("QueryAllNodes surfaces 9999 offset boundary with PartialResultError and TruncatedByOffsetLimit when not subdividing", func(t *testing.T) {
		var requestedOffsets []int
		var requestedLimits []int
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			q := req.URL.Query()
			off, _ := strconv.Atoi(q.Get("offset"))
			lim, _ := strconv.Atoi(q.Get("limit"))
			if off+lim > aphotos.MaxNodeOffsetWindow {
				t.Fatalf("request exceeded 9999 window: offset=%d limit=%d (sum=%d)", off, lim, off+lim)
			}
			requestedOffsets = append(requestedOffsets, off)
			requestedLimits = append(requestedLimits, lim)

			items := make([]string, 0, lim)
			for i := range lim {
				id := off + i + 1
				items = append(items, fmt.Sprintf(`{"id":"n-%d","name":"n-%d.jpg","kind":"FILE","status":"AVAILABLE"}`, id, id))
			}
			body := fmt.Sprintf(`{"count": 12000, "data": [%s]}`, strings.Join(items, ","))
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(body)),
				Request:    req,
			}, nil
		})

		client := newTestUSClient(t, rt, 0)
		page, err := client.QueryAllNodes(context.Background(), aphotos.NodeQueryOptions{
			Filter: aphotos.NewFilter().Kind(aphotos.NodeKindFile),
		})
		if err == nil {
			t.Fatal("expected PartialResultError when TotalCount > 9999 without TimeWindowSubdivide, got nil")
		}
		var partialErr *aphotos.PartialResultError
		if !errors.As(err, &partialErr) {
			t.Fatalf("expected *PartialResultError, got %T (%v)", err, err)
		}
		if page == nil {
			t.Fatal("expected partial page to be returned alongside PartialResultError")
		}
		if !page.TruncatedByOffsetLimit || page.Complete || !page.HasMore {
			t.Errorf("page flags: TruncatedByOffsetLimit=%v Complete=%v HasMore=%v, want true, false, true",
				page.TruncatedByOffsetLimit, page.Complete, page.HasMore)
		}
		if len(page.Items) != aphotos.MaxNodeOffsetWindow {
			t.Errorf("len(page.Items) = %d, want %d", len(page.Items), aphotos.MaxNodeOffsetWindow)
		}
		lastIdx := len(requestedOffsets) - 1
		if requestedOffsets[lastIdx] != 9800 || requestedLimits[lastIdx] != 199 {
			t.Errorf("last page request = (offset=%d, limit=%d), want (9800, 199)", requestedOffsets[lastIdx], requestedLimits[lastIdx])
		}
	})

	t.Run("QueryAllNodes with TimeWindowSubdivide safely subdivides >9999 results without truncation or duplicates", func(t *testing.T) {
		tStart := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		tMid := tStart.Add(tEndDelta() / 2)
		tEnd := tStart.Add(tEndDelta())

		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			q := req.URL.Query()
			filters := q.Get("filters")
			off, _ := strconv.Atoi(q.Get("offset"))
			if off > 0 {
				t.Fatalf("expected subdivided windows to fit in single page, got offset=%d", off)
			}

			// Root window [2023-01-01T00:00:00Z TO 2023-01-03T00:00:00Z] reports count 12000 (>9999)
			fullRange := fmt.Sprintf(`createdDate:[%q TO %q]`, tStart.Format(time.RFC3339), tEnd.Format(time.RFC3339))
			firstHalf := fmt.Sprintf(`createdDate:[%q TO %q}`, tStart.Format(time.RFC3339), tMid.Format(time.RFC3339))
			secondHalf := fmt.Sprintf(`createdDate:[%q TO %q]`, tMid.Format(time.RFC3339), tEnd.Format(time.RFC3339))

			switch {
			case strings.Contains(filters, fullRange):
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBufferString(`{"count": 12000, "data": [{"id":"w1-1","name":"a.jpg","kind":"FILE","status":"AVAILABLE"}]}`)),
					Request:    req,
				}, nil
			case strings.Contains(filters, firstHalf):
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{"count": 2, "data": [
						{"id":"w1-1","name":"a.jpg","kind":"FILE","status":"AVAILABLE"},
						{"id":"w1-2","name":"b.jpg","kind":"FILE","status":"AVAILABLE"}
					]}`)),
					Request: req,
				}, nil
			case strings.Contains(filters, secondHalf):
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{"count": 2, "data": [
						{"id":"w1-2","name":"b.jpg","kind":"FILE","status":"AVAILABLE"},
						{"id":"w2-1","name":"c.jpg","kind":"FILE","status":"AVAILABLE"}
					]}`)),
					Request: req,
				}, nil
			default:
				t.Fatalf("unexpected filter in subdivision: %s", filters)
				return nil, nil
			}
		})

		client := newTestUSClient(t, rt, 0)
		page, err := client.QueryAllNodes(context.Background(), aphotos.NodeQueryOptions{
			Filter:              aphotos.NewFilter().Kind(aphotos.NodeKindFile),
			TimeWindowSubdivide: true,
			WindowStart:         tStart,
			WindowEnd:           tEnd,
		})
		if err != nil {
			t.Fatalf("QueryAllNodes with TimeWindowSubdivide failed: %v", err)
		}
		if !page.Complete || page.TruncatedByOffsetLimit || page.HasMore {
			t.Errorf("unexpected flags: Complete=%v Truncated=%v HasMore=%v", page.Complete, page.TruncatedByOffsetLimit, page.HasMore)
		}
		if len(page.Items) != 3 {
			t.Fatalf("len(page.Items) = %d, want 3 deduplicated items (w1-1, w1-2, w2-1)", len(page.Items))
		}
	})
}

func TestPagination_IteratorsAndPartialFailure(t *testing.T) {
	t.Run("SearchIter yields deduplicated nodes and stops early when caller breaks", func(t *testing.T) {
		var offsets []string
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			off := req.URL.Query().Get("offset")
			offsets = append(offsets, off)
			var body string
			switch off {
			case "0":
				body = `{"count": 4, "data": [{"id":"n1","name":"1.jpg"},{"id":"n2","name":"2.jpg"}]}`
			case "2":
				body = `{"count": 4, "data": [{"id":"n2","name":"2.jpg"},{"id":"n3","name":"3.jpg"}]}`
			default:
				t.Fatalf("unexpected offset %s", off)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(body)),
				Request:    req,
			}, nil
		})

		client := newTestUSClient(t, rt, 0)
		var gotIDs []string
		for node, err := range client.SearchIter(context.Background(), aphotos.SearchOptions{Limit: 2}) {
			if err != nil {
				t.Fatalf("SearchIter yielded unexpected error: %v", err)
			}
			gotIDs = append(gotIDs, node.ID)
		}
		if strings.Join(gotIDs, ",") != "n1,n2,n3" {
			t.Fatalf("gotIDs = %v, want [n1 n2 n3]", gotIDs)
		}
		if strings.Join(offsets, ",") != "0,2" {
			t.Fatalf("offsets = %v, want [0 2]", offsets)
		}
	})

	t.Run("SearchAll mid-pagination failure returns partial Page with Complete=false and PartialResultError", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			off := req.URL.Query().Get("offset")
			if off == "0" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBufferString(`{"count": 4, "data": [{"id":"n1","name":"1.jpg"},{"id":"n2","name":"2.jpg"}]}`)),
					Request:    req,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Body:       io.NopCloser(bytes.NewBufferString(`{"message":"Access denied on page 2"}`)),
				Request:    req,
			}, nil
		})

		client := newTestUSClient(t, rt, 0)
		page, err := client.SearchAll(context.Background(), aphotos.SearchOptions{Limit: 2})
		if err == nil {
			t.Fatal("expected PartialResultError on page 2 failure, got nil")
		}
		var partialErr *aphotos.PartialResultError
		if !errors.As(err, &partialErr) {
			t.Fatalf("expected *PartialResultError, got %T (%v)", err, err)
		}
		var permErr *aphotos.PermissionError
		if !errors.As(err, &permErr) {
			t.Fatalf("expected wrapped *PermissionError cause, got %v", errors.Unwrap(partialErr))
		}
		if page == nil || page.Complete || !page.HasMore || len(page.Items) != 2 {
			t.Fatalf("unexpected partial page: %+v", page)
		}
	})
}

func tEndDelta() time.Duration {
	return 48 * time.Hour
}
