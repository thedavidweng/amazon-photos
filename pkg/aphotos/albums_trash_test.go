package aphotos_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

func newTestClient(t *testing.T, rt http.RoundTripper) *aphotos.Client {
	t.Helper()
	client, err := aphotos.NewClient(aphotos.Options{
		Region: aphotos.RegionUS,
		Cookies: map[string]string{
			"session-id": "111-2222222-3333333",
			"ubid-main":  "130-0000000-0000000",
			"at-main":    "Atza|synthetic-token",
		},
		Transport:      rt,
		DisableRetries: true,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	return client
}

func jsonResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Request:    req,
	}
}

func TestCreateFolder_WithExplicitAndImplicitRootParent(t *testing.T) {
	t.Run("explicit parentID", func(t *testing.T) {
		var gotMethod, gotPath string
		var gotBody map[string]any

		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotMethod = req.Method
			gotPath = req.URL.Path
			raw, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(raw, &gotBody)
			return jsonResponse(req, http.StatusCreated, `{
				"id": "folder-123",
				"name": "Vacation 2026",
				"kind": "FOLDER",
				"status": "AVAILABLE",
				"parents": ["parent-999"]
			}`), nil
		})

		client := newTestClient(t, rt)
		node, err := client.CreateFolder(context.Background(), "Vacation 2026", "parent-999")
		if err != nil {
			t.Fatalf("CreateFolder returned error: %v", err)
		}
		if gotMethod != http.MethodPost || gotPath != "/drive/v1/nodes" {
			t.Fatalf("got %s %s, want POST /drive/v1/nodes", gotMethod, gotPath)
		}
		if gotBody["kind"] != "FOLDER" || gotBody["name"] != "Vacation 2026" ||
			gotBody["asset"] != "ALL" || gotBody["tempLink"] != "false" ||
			gotBody["resourceVersion"] != "V2" || gotBody["ContentType"] != "JSON" {
			t.Fatalf("unexpected payload: %#v", gotBody)
		}
		parents, ok := gotBody["parents"].([]any)
		if !ok || len(parents) != 1 || parents[0] != "parent-999" {
			t.Fatalf("expected parents [parent-999], got %#v", gotBody["parents"])
		}
		if node.ID != "folder-123" || node.Name != "Vacation 2026" || node.Kind != aphotos.NodeKindFolder {
			t.Fatalf("unexpected returned node: %+v", node)
		}
	})

	t.Run("empty parentID resolves root via GetRoot", func(t *testing.T) {
		var mu sync.Mutex
		var paths []string
		var gotCreateBody map[string]any

		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			defer mu.Unlock()
			paths = append(paths, req.Method+" "+req.URL.Path+"?"+req.URL.RawQuery)
			if req.Method == http.MethodGet && req.URL.Path == "/drive/v1/nodes" {
				return jsonResponse(req, http.StatusOK, `{
					"count": 1,
					"data": [{"id": "root-node-id", "kind": "FOLDER", "isRoot": true}]
				}`), nil
			}
			raw, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(raw, &gotCreateBody)
			return jsonResponse(req, http.StatusCreated, `{
				"info": {"nodeId": "folder-from-info"},
				"name": "Subfolder",
				"kind": "FOLDER"
			}`), nil
		})

		client := newTestClient(t, rt)
		node, err := client.CreateFolder(context.Background(), "Subfolder", "")
		if err != nil {
			t.Fatalf("CreateFolder returned error: %v", err)
		}
		if len(paths) != 2 {
			t.Fatalf("expected 2 HTTP calls (GetRoot + POST /nodes), got %d: %v", len(paths), paths)
		}
		parents, ok := gotCreateBody["parents"].([]any)
		if !ok || len(parents) != 1 || parents[0] != "root-node-id" {
			t.Fatalf("expected parents [root-node-id], got %#v", gotCreateBody["parents"])
		}
		if node.ID != "folder-from-info" {
			t.Fatalf("expected node.ID folder-from-info, got %q", node.ID)
		}
	})
}

func TestListFolders_RootAndParentChildren(t *testing.T) {
	t.Run("lists all folders when parentID is empty", func(t *testing.T) {
		var gotPath, gotFilters, gotLimit, gotOffset string
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path
			q := req.URL.Query()
			gotFilters = q.Get("filters")
			gotLimit = q.Get("limit")
			gotOffset = q.Get("offset")
			return jsonResponse(req, http.StatusOK, `{
				"count": 2,
				"data": [
					{"id": "f1", "name": "Camera Roll", "kind": "FOLDER", "status": "AVAILABLE"},
					{"id": "f2", "name": "Trips", "kind": "FOLDER", "status": "AVAILABLE"}
				]
			}`), nil
		})

		client := newTestClient(t, rt)
		folders, total, err := client.ListFolders(context.Background(), "", 50, 10)
		if err != nil {
			t.Fatalf("ListFolders returned error: %v", err)
		}
		if gotPath != "/drive/v1/nodes" {
			t.Fatalf("got path %q, want /drive/v1/nodes", gotPath)
		}
		if gotFilters != "kind:FOLDER AND status:AVAILABLE" {
			t.Fatalf("got filters %q, want %q", gotFilters, "kind:FOLDER AND status:AVAILABLE")
		}
		if gotLimit != "50" || gotOffset != "10" {
			t.Fatalf("got limit=%q offset=%q, want 50 and 10", gotLimit, gotOffset)
		}
		if total != 2 || len(folders) != 2 || folders[0].ID != "f1" || folders[1].ID != "f2" {
			t.Fatalf("unexpected folders result: total=%d, folders=%+v", total, folders)
		}
	})

	t.Run("lists child folders of explicit parentID", func(t *testing.T) {
		var gotPath, gotFilters, gotLimit, gotOffset string
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path
			q := req.URL.Query()
			gotFilters = q.Get("filters")
			gotLimit = q.Get("limit")
			gotOffset = q.Get("offset")
			return jsonResponse(req, http.StatusOK, `{
				"count": 1,
				"data": [
					{"id": "f-sub", "name": "2026", "kind": "FOLDER", "status": "AVAILABLE", "parents": ["parent-42"]}
				]
			}`), nil
		})

		client := newTestClient(t, rt)
		folders, total, err := client.ListFolders(context.Background(), "parent-42", 0, 0)
		if err != nil {
			t.Fatalf("ListFolders returned error: %v", err)
		}
		if gotPath != "/drive/v1/nodes/parent-42/children" {
			t.Fatalf("got path %q, want /drive/v1/nodes/parent-42/children", gotPath)
		}
		if gotFilters != "kind:FOLDER AND status:AVAILABLE" {
			t.Fatalf("got filters %q, want %q", gotFilters, "kind:FOLDER AND status:AVAILABLE")
		}
		if gotLimit != "200" || gotOffset != "0" {
			t.Fatalf("got limit=%q offset=%q, want default 200 and 0", gotLimit, gotOffset)
		}
		if total != 1 || len(folders) != 1 || folders[0].ID != "f-sub" {
			t.Fatalf("unexpected child folders: total=%d, folders=%+v", total, folders)
		}
	})
}

func TestListAlbums_QueryAndPaginationParams(t *testing.T) {
	var gotPath, gotFilters, gotLimit, gotOffset string
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotPath = req.URL.Path
		q := req.URL.Query()
		gotFilters = q.Get("filters")
		gotLimit = q.Get("limit")
		gotOffset = q.Get("offset")
		return jsonResponse(req, http.StatusOK, `{
			"count": 1,
			"data": [
				{"id": "alb-1", "name": "Family", "kind": "VISUAL_COLLECTION", "status": "AVAILABLE"}
			]
		}`), nil
	})

	client := newTestClient(t, rt)
	albums, total, err := client.ListAlbums(context.Background(), 100, 200)
	if err != nil {
		t.Fatalf("ListAlbums returned error: %v", err)
	}
	if gotPath != "/drive/v1/nodes" {
		t.Fatalf("got path %q, want /drive/v1/nodes", gotPath)
	}
	if gotFilters != "kind:VISUAL_COLLECTION AND status:AVAILABLE" {
		t.Fatalf("got filters %q, want %q", gotFilters, "kind:VISUAL_COLLECTION AND status:AVAILABLE")
	}
	if gotLimit != "100" || gotOffset != "200" {
		t.Fatalf("got limit=%q offset=%q, want 100 and 200", gotLimit, gotOffset)
	}
	if total != 1 || len(albums) != 1 || albums[0].ID != "alb-1" || albums[0].Kind != aphotos.NodeKindVisualCollection {
		t.Fatalf("unexpected albums result: total=%d, albums=%+v", total, albums)
	}
}

func TestCreateAlbum_MembersAndRenameNode(t *testing.T) {
	t.Run("CreateAlbum without initial members sends single POST", func(t *testing.T) {
		var calls int
		var gotMethod, gotPath string
		var gotBody map[string]any

		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			gotMethod = req.Method
			gotPath = req.URL.Path
			raw, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(raw, &gotBody)
			return jsonResponse(req, http.StatusCreated, `{
				"id": "album-100",
				"name": "Summer 2026",
				"kind": "VISUAL_COLLECTION",
				"status": "AVAILABLE"
			}`), nil
		})

		client := newTestClient(t, rt)
		album, err := client.CreateAlbum(context.Background(), "Summer 2026")
		if err != nil {
			t.Fatalf("CreateAlbum returned error: %v", err)
		}
		if calls != 1 {
			t.Fatalf("expected 1 HTTP call, got %d", calls)
		}
		if gotMethod != http.MethodPost || gotPath != "/drive/v1/nodes" {
			t.Fatalf("got %s %s, want POST /drive/v1/nodes", gotMethod, gotPath)
		}
		if gotBody["kind"] != "VISUAL_COLLECTION" || gotBody["name"] != "Summer 2026" ||
			gotBody["resourceVersion"] != "V2" || gotBody["ContentType"] != "JSON" {
			t.Fatalf("unexpected payload: %#v", gotBody)
		}
		if album.ID != "album-100" || album.Name != "Summer 2026" {
			t.Fatalf("unexpected album: %+v", album)
		}
	})

	t.Run("CreateAlbum with initial members calls POST /nodes then PATCH /nodes/{id}/children", func(t *testing.T) {
		type callRecord struct {
			method string
			path   string
			body   map[string]any
		}
		var records []callRecord

		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(req.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			records = append(records, callRecord{
				method: req.Method,
				path:   req.URL.Path,
				body:   body,
			})
			if req.Method == http.MethodPost {
				return jsonResponse(req, http.StatusCreated, `{
					"id": "album-200",
					"name": "Highlights",
					"kind": "VISUAL_COLLECTION"
				}`), nil
			}
			return jsonResponse(req, http.StatusOK, `{}`), nil
		})

		client := newTestClient(t, rt)
		album, err := client.CreateAlbum(context.Background(), "Highlights", "node-1", "node-2")
		if err != nil {
			t.Fatalf("CreateAlbum with members returned error: %v", err)
		}
		if album.ID != "album-200" {
			t.Fatalf("expected album-200, got %q", album.ID)
		}
		if len(records) != 2 {
			t.Fatalf("expected 2 calls, got %d", len(records))
		}
		if records[1].method != http.MethodPatch || records[1].path != "/drive/v1/nodes/album-200/children" {
			t.Fatalf("second call = %s %s, want PATCH /drive/v1/nodes/album-200/children", records[1].method, records[1].path)
		}
		if records[1].body["op"] != "add" || records[1].body["resourceVersion"] != "V2" || records[1].body["ContentType"] != "JSON" {
			t.Fatalf("unexpected AddAlbumMembers payload: %#v", records[1].body)
		}
		vals, ok := records[1].body["value"].([]any)
		if !ok || len(vals) != 2 || vals[0] != "node-1" || vals[1] != "node-2" {
			t.Fatalf("unexpected value array: %#v", records[1].body["value"])
		}
	})

	t.Run("RemoveAlbumMembers sends PATCH op=remove", func(t *testing.T) {
		var gotMethod, gotPath string
		var gotBody map[string]any

		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotMethod = req.Method
			gotPath = req.URL.Path
			raw, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(raw, &gotBody)
			return jsonResponse(req, http.StatusOK, `{}`), nil
		})

		client := newTestClient(t, rt)
		if err := client.RemoveAlbumMembers(context.Background(), "album-300", []string{"n-a", "n-b"}); err != nil {
			t.Fatalf("RemoveAlbumMembers returned error: %v", err)
		}
		if gotMethod != http.MethodPatch || gotPath != "/drive/v1/nodes/album-300/children" {
			t.Fatalf("got %s %s, want PATCH /drive/v1/nodes/album-300/children", gotMethod, gotPath)
		}
		if gotBody["op"] != "remove" || gotBody["resourceVersion"] != "V2" || gotBody["ContentType"] != "JSON" {
			t.Fatalf("unexpected payload: %#v", gotBody)
		}
		vals, ok := gotBody["value"].([]any)
		if !ok || len(vals) != 2 || vals[0] != "n-a" || vals[1] != "n-b" {
			t.Fatalf("unexpected value slice: %#v", gotBody["value"])
		}
	})

	t.Run("RenameNode sends PATCH /nodes/{id}", func(t *testing.T) {
		var gotMethod, gotPath string
		var gotBody map[string]any

		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotMethod = req.Method
			gotPath = req.URL.Path
			raw, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(raw, &gotBody)
			return jsonResponse(req, http.StatusOK, `{
				"id": "album-300",
				"name": "Winter 2026",
				"kind": "VISUAL_COLLECTION"
			}`), nil
		})

		client := newTestClient(t, rt)
		renamed, err := client.RenameNode(context.Background(), "album-300", "Winter 2026")
		if err != nil {
			t.Fatalf("RenameNode returned error: %v", err)
		}
		if gotMethod != http.MethodPatch || gotPath != "/drive/v1/nodes/album-300" {
			t.Fatalf("got %s %s, want PATCH /drive/v1/nodes/album-300", gotMethod, gotPath)
		}
		if gotBody["name"] != "Winter 2026" || gotBody["resourceVersion"] != "V2" || gotBody["ContentType"] != "JSON" {
			t.Fatalf("unexpected RenameNode payload: %#v", gotBody)
		}
		if renamed.ID != "album-300" || renamed.Name != "Winter 2026" {
			t.Fatalf("unexpected renamed node: %+v", renamed)
		}
	})
}

func TestNodeSettings_SetFavoriteAndSetHidden(t *testing.T) {
	t.Run("SetFavorite single and multiple nodes", func(t *testing.T) {
		type patchReq struct {
			path string
			body map[string]any
		}
		var reqs []patchReq

		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPatch {
				t.Fatalf("unexpected method %s", req.Method)
			}
			raw, _ := io.ReadAll(req.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			reqs = append(reqs, patchReq{path: req.URL.Path, body: body})

			nodeID := req.URL.Path[len("/drive/v1/nodes/"):]
			return jsonResponse(req, http.StatusOK, `{
				"id": "`+nodeID+`",
				"kind": "FILE",
				"settings": {"favorite": true, "hidden": false}
			}`), nil
		})

		client := newTestClient(t, rt)
		nodes, err := client.SetFavorite(context.Background(), []string{"node-fav-1", "node-fav-2"}, true)
		if err != nil {
			t.Fatalf("SetFavorite returned error: %v", err)
		}
		if len(reqs) != 2 {
			t.Fatalf("expected 2 PATCH requests, got %d", len(reqs))
		}
		if reqs[0].path != "/drive/v1/nodes/node-fav-1" || reqs[1].path != "/drive/v1/nodes/node-fav-2" {
			t.Fatalf("unexpected paths: %+v", reqs)
		}
		for i, r := range reqs {
			if r.body["resourceVersion"] != "V2" || r.body["ContentType"] != "JSON" {
				t.Fatalf("req[%d] missing V2/JSON fields: %#v", i, r.body)
			}
			settings, ok := r.body["settings"].(map[string]any)
			if !ok || settings["favorite"] != true {
				t.Fatalf("req[%d] unexpected settings: %#v", i, r.body["settings"])
			}
			if _, hasHidden := settings["hidden"]; hasHidden {
				t.Fatalf("req[%d] SetFavorite should not overwrite hidden field: %#v", i, settings)
			}
		}
		if len(nodes) != 2 || nodes[0].ID != "node-fav-1" || nodes[1].ID != "node-fav-2" ||
			nodes[0].Settings == nil || !nodes[0].Settings.Favorite {
			t.Fatalf("unexpected returned nodes: %+v", nodes)
		}
	})

	t.Run("SetHidden true and false across multiple nodes", func(t *testing.T) {
		type patchReq struct {
			path string
			body map[string]any
		}
		var reqs []patchReq

		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(req.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			reqs = append(reqs, patchReq{path: req.URL.Path, body: body})

			nodeID := req.URL.Path[len("/drive/v1/nodes/"):]
			return jsonResponse(req, http.StatusOK, `{
				"id": "`+nodeID+`",
				"kind": "FILE",
				"settings": {"favorite": false, "hidden": true}
			}`), nil
		})

		client := newTestClient(t, rt)
		nodes, err := client.SetHidden(context.Background(), []string{"hide-1", "hide-2", "hide-3"}, true)
		if err != nil {
			t.Fatalf("SetHidden(true) returned error: %v", err)
		}
		if len(reqs) != 3 {
			t.Fatalf("expected 3 PATCH requests, got %d", len(reqs))
		}
		wantPaths := []string{
			"/drive/v1/nodes/hide-1",
			"/drive/v1/nodes/hide-2",
			"/drive/v1/nodes/hide-3",
		}
		for i, wantPath := range wantPaths {
			if reqs[i].path != wantPath {
				t.Fatalf("req[%d].path = %q, want %q", i, reqs[i].path, wantPath)
			}
			settings, ok := reqs[i].body["settings"].(map[string]any)
			if !ok || settings["hidden"] != true {
				t.Fatalf("req[%d] unexpected settings: %#v", i, reqs[i].body["settings"])
			}
			if _, hasFav := settings["favorite"]; hasFav {
				t.Fatalf("req[%d] SetHidden should not overwrite favorite field: %#v", i, settings)
			}
		}
		if len(nodes) != 3 || !nodes[2].Settings.Hidden {
			t.Fatalf("unexpected returned nodes: %+v", nodes)
		}

		reqs = nil
		_, err = client.SetHidden(context.Background(), []string{"hide-1"}, false)
		if err != nil {
			t.Fatalf("SetHidden(false) returned error: %v", err)
		}
		if len(reqs) != 1 || reqs[0].path != "/drive/v1/nodes/hide-1" {
			t.Fatalf("unexpected unhide request: %+v", reqs)
		}
		settings, ok := reqs[0].body["settings"].(map[string]any)
		if !ok || settings["hidden"] != false {
			t.Fatalf("expected hidden=false, got %#v", reqs[0].body["settings"])
		}
	})
}

func TestTrash_ListAndMultiPage(t *testing.T) {
	t.Run("ListTrash uses defaults when TrashListOptions is zero", func(t *testing.T) {
		var gotPath, gotFilters, gotSort, gotLimit, gotOffset string
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path
			q := req.URL.Query()
			gotFilters = q.Get("filters")
			gotSort = q.Get("sort")
			gotLimit = q.Get("limit")
			gotOffset = q.Get("offset")
			return jsonResponse(req, http.StatusOK, `{
				"count": 2,
				"data": [
					{"id": "t-1", "name": "IMG_1.JPG", "kind": "FILE", "status": "TRASH"},
					{"id": "t-2", "name": "IMG_2.JPG", "kind": "FILE", "status": "TRASH"}
				]
			}`), nil
		})

		client := newTestClient(t, rt)
		nodes, total, err := client.ListTrash(context.Background(), aphotos.TrashListOptions{})
		if err != nil {
			t.Fatalf("ListTrash returned error: %v", err)
		}
		if gotPath != "/drive/v1/trash" {
			t.Fatalf("got path %q, want /drive/v1/trash", gotPath)
		}
		if gotFilters != "kind:(FILE* OR FOLDER*) AND status:(TRASH*)" {
			t.Fatalf("got filters %q, want default trash filter", gotFilters)
		}
		if gotSort != "['modifiedDate DESC']" {
			t.Fatalf("got sort %q, want ['modifiedDate DESC']", gotSort)
		}
		if gotLimit != "200" || gotOffset != "0" {
			t.Fatalf("got limit=%q offset=%q, want 200 and 0", gotLimit, gotOffset)
		}
		if total != 2 || len(nodes) != 2 || nodes[0].ID != "t-1" {
			t.Fatalf("unexpected trash nodes: total=%d, nodes=%+v", total, nodes)
		}
	})

	t.Run("ListAllTrash hits /trash on page 2+ and deduplicates without repeating offset 0", func(t *testing.T) {
		var paths []string
		var offsets []string

		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			paths = append(paths, req.URL.Path)
			offset := req.URL.Query().Get("offset")
			offsets = append(offsets, offset)

			switch offset {
			case "0":
				items := make([]map[string]any, 0, 200)
				for i := range 200 {
					items = append(items, map[string]any{
						"id":     fmt.Sprintf("trash-node-%03d", i),
						"name":   fmt.Sprintf("file-%03d.jpg", i),
						"kind":   "FILE",
						"status": "TRASH",
					})
				}
				raw, _ := json.Marshal(map[string]any{"count": 202, "data": items})
				return jsonResponse(req, http.StatusOK, string(raw)), nil
			case "200":
				return jsonResponse(req, http.StatusOK, `{
					"count": 202,
					"data": [
						{"id": "trash-node-199", "name": "file-199.jpg", "kind": "FILE", "status": "TRASH"},
						{"id": "trash-node-200", "name": "file-200.jpg", "kind": "FILE", "status": "TRASH"},
						{"id": "trash-node-201", "name": "file-201.jpg", "kind": "FILE", "status": "TRASH"}
					]
				}`), nil
			default:
				t.Fatalf("unexpected offset %q", offset)
				return nil, nil
			}
		})

		client := newTestClient(t, rt)
		nodes, err := client.ListAllTrash(context.Background())
		if err != nil {
			t.Fatalf("ListAllTrash returned error: %v", err)
		}
		if len(paths) != 2 {
			t.Fatalf("expected 2 page requests, got %d (%v)", len(paths), paths)
		}
		for i, p := range paths {
			if p != "/drive/v1/trash" {
				t.Fatalf("page %d hit %q, want /drive/v1/trash (never /drive/v1/nodes)", i, p)
			}
		}
		if offsets[0] != "0" || offsets[1] != "200" {
			t.Fatalf("expected offsets [0, 200], got %v", offsets)
		}
		if len(nodes) != 202 {
			t.Fatalf("expected 202 deduplicated nodes, got %d", len(nodes))
		}
	})

	t.Run("ListAllTrash respects 9999 offset+limit cap and returns PartialResultError when count exceeds 9999", func(t *testing.T) {
		var calls []string

		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			q := req.URL.Query()
			offsetStr := q.Get("offset")
			limitStr := q.Get("limit")
			calls = append(calls, offsetStr+"+"+limitStr)

			var limit int
			_, _ = fmt.Sscanf(limitStr, "%d", &limit)
			var offset int
			_, _ = fmt.Sscanf(offsetStr, "%d", &offset)
			if offset+limit > 9999 {
				t.Fatalf("violated 9999 cap: offset=%d limit=%d", offset, limit)
			}

			items := make([]map[string]any, 0, limit)
			for i := range limit {
				items = append(items, map[string]any{
					"id":     fmt.Sprintf("t-%d", offset+i),
					"kind":   "FILE",
					"status": "TRASH",
				})
			}
			raw, _ := json.Marshal(map[string]any{"count": 10500, "data": items})
			return jsonResponse(req, http.StatusOK, string(raw)), nil
		})

		client := newTestClient(t, rt)
		nodes, err := client.ListAllTrash(context.Background())
		if err == nil {
			t.Fatalf("expected PartialResultError when total count > 9999, got nil")
		}
		var partialErr *aphotos.PartialResultError
		if !errors.As(err, &partialErr) {
			t.Fatalf("expected *aphotos.PartialResultError, got %T (%v)", err, err)
		}
		if len(nodes) != 9999 {
			t.Fatalf("expected 9999 nodes returned before cap, got %d", len(nodes))
		}
		if calls[len(calls)-1] != "9800+199" {
			t.Fatalf("expected final call to clamp to 9800+199, got %q", calls[len(calls)-1])
		}
	})
}

func TestTrash_BatchMoveAndRestore(t *testing.T) {
	if aphotos.MaxTrashBatchSize != 50 {
		t.Fatalf("MaxTrashBatchSize = %d, want 50", aphotos.MaxTrashBatchSize)
	}

	nodeIDs := make([]string, 120)
	for i := range nodeIDs {
		nodeIDs[i] = fmt.Sprintf("node-%03d", i)
	}

	t.Run("MoveToTrash splits 120 IDs into 50, 50, 20 batches with op=add", func(t *testing.T) {
		var batches [][]string
		var payloads []map[string]any

		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPatch || req.URL.Path != "/drive/v1/trash" {
				t.Fatalf("unexpected %s %s, want PATCH /drive/v1/trash", req.Method, req.URL.Path)
			}
			raw, _ := io.ReadAll(req.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			payloads = append(payloads, body)

			vals, ok := body["value"].([]any)
			if !ok {
				t.Fatalf("missing value array in %#v", body)
			}
			batch := make([]string, 0, len(vals))
			for _, v := range vals {
				s, _ := v.(string)
				batch = append(batch, s)
			}
			batches = append(batches, batch)
			return jsonResponse(req, http.StatusOK, `{}`), nil
		})

		client := newTestClient(t, rt)
		if err := client.MoveToTrash(context.Background(), nodeIDs); err != nil {
			t.Fatalf("MoveToTrash returned error: %v", err)
		}
		if len(batches) != 3 || len(batches[0]) != 50 || len(batches[1]) != 50 || len(batches[2]) != 20 {
			t.Fatalf("unexpected batch sizes: %d, %d, %d", len(batches[0]), len(batches[1]), len(batches[2]))
		}
		for i, p := range payloads {
			if p["recurse"] != "true" || p["op"] != "add" || p["filters"] != "" ||
				p["conflictResolution"] != "RENAME" || p["resourceVersion"] != "V2" || p["ContentType"] != "JSON" {
				t.Fatalf("batch %d unexpected payload: %#v", i, p)
			}
		}
		if batches[0][0] != "node-000" || batches[1][0] != "node-050" || batches[2][19] != "node-119" {
			t.Fatalf("unexpected batch boundaries")
		}
	})

	t.Run("RestoreFromTrash splits 120 IDs into 50, 50, 20 batches with op=remove", func(t *testing.T) {
		var batches [][]string
		var payloads []map[string]any

		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPatch || req.URL.Path != "/drive/v1/trash" {
				t.Fatalf("unexpected %s %s, want PATCH /drive/v1/trash", req.Method, req.URL.Path)
			}
			raw, _ := io.ReadAll(req.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			payloads = append(payloads, body)

			vals, _ := body["value"].([]any)
			batch := make([]string, 0, len(vals))
			for _, v := range vals {
				s, _ := v.(string)
				batch = append(batch, s)
			}
			batches = append(batches, batch)
			return jsonResponse(req, http.StatusOK, `{}`), nil
		})

		client := newTestClient(t, rt)
		if err := client.RestoreFromTrash(context.Background(), nodeIDs); err != nil {
			t.Fatalf("RestoreFromTrash returned error: %v", err)
		}
		if len(batches) != 3 || len(batches[0]) != 50 || len(batches[1]) != 50 || len(batches[2]) != 20 {
			t.Fatalf("unexpected batch sizes: %d, %d, %d", len(batches[0]), len(batches[1]), len(batches[2]))
		}
		for i, p := range payloads {
			if p["recurse"] != "true" || p["op"] != "remove" ||
				p["conflictResolution"] != "RENAME" || p["resourceVersion"] != "V2" || p["ContentType"] != "JSON" {
				t.Fatalf("batch %d unexpected payload: %#v", i, p)
			}
			if _, hasFilters := p["filters"]; hasFilters {
				t.Fatalf("batch %d RestoreFromTrash should not include filters key: %#v", i, p)
			}
		}
	})
}

func TestAlbumsSettingsTrash_ValidationAndPartialFailures(t *testing.T) {
	nopRT := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected network call: %s %s", req.Method, req.URL.Path)
		return nil, nil
	})
	client := newTestClient(t, nopRT)
	ctx := context.Background()

	if _, err := client.CreateFolder(ctx, "   ", "root-1"); err == nil {
		t.Fatalf("expected ConfigError for empty folder name")
	}
	if _, err := client.CreateAlbum(ctx, ""); err == nil {
		t.Fatalf("expected ConfigError for empty album name")
	}
	if err := client.AddAlbumMembers(ctx, "", []string{"n1"}); err == nil {
		t.Fatalf("expected ConfigError for empty album ID")
	}
	if err := client.AddAlbumMembers(ctx, "alb1", nil); err == nil {
		t.Fatalf("expected ConfigError for empty nodeIDs")
	}
	if err := client.RemoveAlbumMembers(ctx, "alb1", []string{"n1", "  "}); err == nil {
		t.Fatalf("expected ConfigError for blank nodeID in slice")
	}
	if _, err := client.RenameNode(ctx, "", "New"); err == nil {
		t.Fatalf("expected ConfigError for empty nodeID in RenameNode")
	}
	if _, err := client.RenameNode(ctx, "n1", "  "); err == nil {
		t.Fatalf("expected ConfigError for empty newName in RenameNode")
	}
	if _, err := client.SetFavorite(ctx, nil, true); err == nil {
		t.Fatalf("expected ConfigError for empty nodeIDs in SetFavorite")
	}
	if _, err := client.SetHidden(ctx, []string{""}, true); err == nil {
		t.Fatalf("expected ConfigError for blank nodeID in SetHidden")
	}
	if err := client.MoveToTrash(ctx, nil); err == nil {
		t.Fatalf("expected ConfigError for empty nodeIDs in MoveToTrash")
	}
	if err := client.RestoreFromTrash(ctx, nil); err == nil {
		t.Fatalf("expected ConfigError for empty nodeIDs in RestoreFromTrash")
	}
	if _, _, err := client.ListTrash(ctx, aphotos.TrashListOptions{Offset: 9999}); err == nil {
		t.Fatalf("expected InvalidFilterError for offset >= 9999 in ListTrash")
	}

	t.Run("SetFavorite returns completed nodes when a subsequent node fails", func(t *testing.T) {
		var call int
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			call++
			if call == 1 {
				return jsonResponse(req, http.StatusOK, `{"id": "n-1", "settings": {"favorite": true}}`), nil
			}
			return jsonResponse(req, http.StatusNotFound, `{"message": "node not found"}`), nil
		})
		c := newTestClient(t, rt)
		nodes, err := c.SetFavorite(ctx, []string{"n-1", "n-2"}, true)
		if err == nil {
			t.Fatalf("expected error on second node, got nil")
		}
		var nfErr *aphotos.NotFoundError
		if !errors.As(err, &nfErr) {
			t.Fatalf("expected *aphotos.NotFoundError, got %T (%v)", err, err)
		}
		if len(nodes) != 1 || nodes[0].ID != "n-1" {
			t.Fatalf("expected first completed node returned, got %+v", nodes)
		}
	})
}
