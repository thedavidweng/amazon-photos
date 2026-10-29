package aphotos_test

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

func newTestTransferClient(t *testing.T, rt http.RoundTripper) *aphotos.Client {
	t.Helper()
	client, err := aphotos.NewClient(aphotos.Options{
		Region: aphotos.RegionUS,
		Cookies: map[string]string{
			"session-id": "111-2222222-3333333",
			"ubid-main":  "130-0000000-0000000",
			"at-main":    "Atza|synthetic-token-value",
		},
		Transport:   rt,
		MaxRetries:  2,
		BaseBackoff: 1,
		MaxBackoff:  5,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

type syntheticStream struct {
	remaining int64
	pattern   byte
}

func (s *syntheticStream) Read(p []byte) (int, error) {
	if s.remaining <= 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), s.remaining)
	for i := range n {
		p[i] = s.pattern
	}
	s.remaining -= n
	return int(n), nil
}

func TestUpload_StreamingSuccessAndSizes(t *testing.T) {
	cases := []struct {
		name         string
		fileName     string
		parentNodeID string
		autoRoot     bool
		size         int64
		body         func() io.Reader
		wantMD5      string
	}{
		{
			name:         "empty 0-byte stream",
			fileName:     "empty.txt",
			parentNodeID: "ROOT123",
			size:         0,
			body:         func() io.Reader { return bytes.NewReader(nil) },
			wantMD5:      "d41d8cd98f00b204e9800998ecf8427e",
		},
		{
			name:         "1-byte stream",
			fileName:     "one.bin",
			parentNodeID: "ROOT123",
			size:         1,
			body:         func() io.Reader { return bytes.NewReader([]byte{'A'}) },
			wantMD5:      "7fc56270e7a70fa81a5935b72eacbe29",
		},
		{
			name:     "large synthetic stream with AutoResolveRoot",
			fileName: "video.mp4",
			autoRoot: true,
			size:     2 * 1024 * 1024,
			body: func() io.Reader {
				return &syntheticStream{remaining: 2 * 1024 * 1024, pattern: 'Z'}
			},
			wantMD5: func() string {
				h := md5.New()
				_, _ = io.Copy(h, &syntheticStream{remaining: 2 * 1024 * 1024, pattern: 'Z'})
				return hex.EncodeToString(h.Sum(nil))
			}(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotBytes int64
			client := newTestTransferClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method == http.MethodGet && req.URL.Path == "/drive/v1/nodes" {
					if req.URL.Query().Get("filters") != "isRoot:true" {
						t.Fatalf("unexpected GET /nodes filters: %q", req.URL.Query().Get("filters"))
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(`{"count":1,"data":[{"id":"RESOLVED_ROOT","ownerId":"OWN1","kind":"FOLDER","isRoot":true}]}`)),
					}, nil
				}
				if req.Method != http.MethodPost {
					t.Fatalf("method = %s, want POST", req.Method)
				}
				if req.URL.Host != "content-na.drive.amazonaws.com" || req.URL.Path != "/cdproxy/nodes" {
					t.Fatalf("unexpected upload URL: %s", req.URL.String())
				}
				q := req.URL.Query()
				if q.Get("name") != tc.fileName {
					t.Errorf("query name = %q, want %q", q.Get("name"), tc.fileName)
				}
				if q.Get("kind") != "FILE" {
					t.Errorf("query kind = %q, want FILE", q.Get("kind"))
				}
				wantParent := tc.parentNodeID
				if tc.autoRoot {
					wantParent = "RESOLVED_ROOT"
				}
				if q.Get("parentNodeId") != wantParent {
					t.Errorf("query parentNodeId = %q, want %q", q.Get("parentNodeId"), wantParent)
				}
				if req.Header.Get("x-amzn-sessionid") != "111-2222222-3333333" {
					t.Errorf("missing x-amzn-sessionid header")
				}
				if !strings.Contains(req.Header.Get("Cookie"), "session-id=111-2222222-3333333") {
					t.Errorf("missing session-id in Cookie header")
				}
				n, err := io.Copy(io.Discard, req.Body)
				if err != nil {
					return nil, err
				}
				gotBytes = n
				bodyJSON := fmt.Sprintf(`{"id":"NODE_%s","name":%q,"kind":"FILE","status":"AVAILABLE"}`, tc.fileName, tc.fileName)
				return &http.Response{
					StatusCode: http.StatusCreated,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(bodyJSON)),
				}, nil
			}))

			var beforeMem runtime.MemStats
			if tc.size >= 1024*1024 {
				runtime.GC()
				runtime.ReadMemStats(&beforeMem)
			}
			var lastProgress aphotos.TransferProgress
			var progressCalls int
			res, err := client.Upload(context.Background(), aphotos.UploadRequest{
				Name:            tc.fileName,
				ParentNodeID:    tc.parentNodeID,
				AutoResolveRoot: tc.autoRoot,
				Body:            tc.body(),
				Size:            tc.size,
				Progress: func(p aphotos.TransferProgress) {
					progressCalls++
					lastProgress = p
				},
			})
			if tc.size >= 1024*1024 {
				var afterMem runtime.MemStats
				runtime.ReadMemStats(&afterMem)
				var heapDelta int64
				if afterMem.TotalAlloc > beforeMem.TotalAlloc {
					heapDelta = int64(afterMem.TotalAlloc - beforeMem.TotalAlloc)
				}
				if heapDelta > tc.size/2 {
					t.Errorf("streaming upload allocated %d bytes for %d-byte stream (expected bounded buffer)", heapDelta, tc.size)
				}
			}
			if err != nil {
				t.Fatalf("Upload returned unexpected error: %v", err)
			}
			if res == nil {
				t.Fatal("Upload returned nil result")
			}
			if res.Status != aphotos.TransferConfirmed {
				t.Errorf("Status = %q, want %q", res.Status, aphotos.TransferConfirmed)
			}
			if res.NodeID != "NODE_"+tc.fileName {
				t.Errorf("NodeID = %q, want %q", res.NodeID, "NODE_"+tc.fileName)
			}
			if res.Node == nil || res.Node.ID != res.NodeID {
				t.Errorf("Node = %+v, want matching Node", res.Node)
			}
			if gotBytes != tc.size || res.Bytes != tc.size {
				t.Errorf("bytes = %d (server %d), want %d", res.Bytes, gotBytes, tc.size)
			}
			if res.MD5 != tc.wantMD5 {
				t.Errorf("MD5 = %q, want %q", res.MD5, tc.wantMD5)
			}
			if progressCalls == 0 || lastProgress.BytesTransferred != tc.size {
				t.Errorf("progress calls = %d, lastBytes = %d, want %d", progressCalls, lastProgress.BytesTransferred, tc.size)
			}
		})
	}
}

func TestUpload_201WithoutNodeIDReturnsUnconfirmed(t *testing.T) {
	client := newTestTransferClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		_, _ = io.Copy(io.Discard, req.Body)
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"name":"mystery.jpg","kind":"FILE"}`)),
		}, nil
	}))

	res, err := client.Upload(context.Background(), aphotos.UploadRequest{
		Name:         "mystery.jpg",
		ParentNodeID: "ROOT1",
		Body:         strings.NewReader("payload"),
	})
	if res == nil {
		t.Fatal("expected non-nil TransferResult on unconfirmed upload")
	}
	if res.Status != aphotos.TransferUnconfirmed {
		t.Fatalf("Status = %q, want %q", res.Status, aphotos.TransferUnconfirmed)
	}
	if _, ok := errors.AsType[*aphotos.PartialResultError](err); !ok {
		t.Fatalf("expected *PartialResultError on unconfirmed 201, got %T: %v", err, err)
	}
}

func TestUpload_NonReplayableReaderNeverAutoRetries(t *testing.T) {
	var attempts atomic.Int32
	client := newTestTransferClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts.Add(1)
		_, _ = io.Copy(io.Discard, req.Body)
		return nil, errors.New("connection reset by peer")
	}))

	// Wrap in io.Reader only (not io.Seeker)
	nonSeekReader := struct{ io.Reader }{strings.NewReader("one-shot stream")}
	res, err := client.Upload(context.Background(), aphotos.UploadRequest{
		Name:         "oneshot.jpg",
		ParentNodeID: "ROOT1",
		Body:         nonSeekReader,
	})
	if err == nil {
		t.Fatal("expected error on network failure, got nil")
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want 1 (must not retry non-replayable reader)", attempts.Load())
	}
	if res == nil || res.Status != aphotos.TransferFailed {
		t.Fatalf("expected TransferFailed result, got %+v", res)
	}
}

func TestUpload_ConflictPolicies(t *testing.T) {
	t.Run("ConflictFail default returns ConflictError and never marks confirmed", func(t *testing.T) {
		client := newTestTransferClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			_, _ = io.Copy(io.Discard, req.Body)
			return &http.Response{
				StatusCode: http.StatusConflict,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"message":"NODE_EXISTS","info":{"nodeId":"EXISTING_NODE_99"}}`)),
			}, nil
		}))

		res, err := client.Upload(context.Background(), aphotos.UploadRequest{
			Name:         "dup.jpg",
			ParentNodeID: "ROOT1",
			Body:         strings.NewReader("photo-bytes"),
		})
		var conflictErr *aphotos.ConflictError
		if !errors.As(err, &conflictErr) {
			t.Fatalf("expected *ConflictError, got %T: %v", err, err)
		}
		if conflictErr.ConflictingNodeID != "EXISTING_NODE_99" {
			t.Errorf("ConflictingNodeID = %q, want EXISTING_NODE_99", conflictErr.ConflictingNodeID)
		}
		if res == nil || res.Status != aphotos.TransferFailed {
			t.Fatalf("expected TransferFailed result, got %+v", res)
		}
	})

	t.Run("ConflictSkip verifies existing remote node by name and MD5", func(t *testing.T) {
		payload := "duplicate-content-bytes"
		sum := md5.Sum([]byte(payload))
		wantMD5 := hex.EncodeToString(sum[:])

		var queriedNodes bool
		client := newTestTransferClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodPost {
				_, _ = io.Copy(io.Discard, req.Body)
				return &http.Response{
					StatusCode: http.StatusConflict,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"message":"Node already exists"}`)),
				}, nil
			}
			if req.Method == http.MethodGet && req.URL.Path == "/drive/v1/nodes" {
				queriedNodes = true
				body := fmt.Sprintf(`{"count":1,"data":[{"id":"VERIFIED_NODE_42","name":"dup.jpg","kind":"FILE","status":"AVAILABLE","contentProperties":{"md5":%q,"size":%d}}]}`, wantMD5, len(payload))
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			}
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
			return nil, nil
		}))

		res, err := client.Upload(context.Background(), aphotos.UploadRequest{
			Name:           "dup.jpg",
			ParentNodeID:   "ROOT1",
			Body:           strings.NewReader(payload),
			ConflictPolicy: aphotos.ConflictSkip,
		})
		if err != nil {
			t.Fatalf("ConflictSkip returned unexpected error: %v", err)
		}
		if !queriedNodes {
			t.Fatal("expected GET /nodes query to verify conflicting remote node")
		}
		if res == nil || res.Status != aphotos.TransferSkipped {
			t.Fatalf("Status = %+v, want TransferSkipped", res)
		}
		if res.NodeID != "VERIFIED_NODE_42" {
			t.Errorf("NodeID = %q, want VERIFIED_NODE_42", res.NodeID)
		}
		if !res.ConflictResolved {
			t.Errorf("ConflictResolved = false, want true")
		}
	})

	t.Run("ConflictSkip fails when remote node has same name but different MD5", func(t *testing.T) {
		client := newTestTransferClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodPost {
				_, _ = io.Copy(io.Discard, req.Body)
				return &http.Response{
					StatusCode: http.StatusConflict,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"message":"NODE_EXISTS","info":{"nodeId":"DIFFERENT_NODE_77"}}`)),
				}, nil
			}
			if req.Method == http.MethodGet && req.URL.Path == "/drive/v1/nodes" {
				body := `{"count":1,"data":[{"id":"DIFFERENT_NODE_77","name":"dup.jpg","kind":"FILE","status":"AVAILABLE","contentProperties":{"md5":"00000000000000000000000000000000","size":999}}]}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			}
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
			return nil, nil
		}))

		res, err := client.Upload(context.Background(), aphotos.UploadRequest{
			Name:           "dup.jpg",
			ParentNodeID:   "ROOT1",
			Body:           strings.NewReader("local-content-with-different-md5"),
			ConflictPolicy: aphotos.ConflictSkip,
		})
		var conflictErr *aphotos.ConflictError
		if !errors.As(err, &conflictErr) {
			t.Fatalf("expected *ConflictError when remote MD5 differs, got %T: %v", err, err)
		}
		if conflictErr.ConflictingNodeID != "DIFFERENT_NODE_77" {
			t.Errorf("ConflictingNodeID = %q, want DIFFERENT_NODE_77", conflictErr.ConflictingNodeID)
		}
		if res == nil || res.Status != aphotos.TransferFailed {
			t.Fatalf("expected TransferFailed, got %+v", res)
		}
	})

	t.Run("ConflictRename replays ReadSeeker once with disambiguated filename", func(t *testing.T) {
		payload := "rename-me-bytes"
		var names []string
		var bodies []string
		client := newTestTransferClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			b, _ := io.ReadAll(req.Body)
			names = append(names, req.URL.Query().Get("name"))
			bodies = append(bodies, string(b))
			if len(names) == 1 {
				return &http.Response{
					StatusCode: http.StatusConflict,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"message":"Conflict"}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusCreated,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"id":"RENAMED_NODE_1","name":"photo (1).jpg","kind":"FILE"}`)),
			}, nil
		}))

		res, err := client.Upload(context.Background(), aphotos.UploadRequest{
			Name:           "photo.jpg",
			ParentNodeID:   "ROOT1",
			Body:           strings.NewReader(payload),
			ConflictPolicy: aphotos.ConflictRename,
		})
		if err != nil {
			t.Fatalf("ConflictRename returned unexpected error: %v", err)
		}
		if len(names) != 2 || names[0] != "photo.jpg" || names[1] != "photo (1).jpg" {
			t.Fatalf("upload names = %v, want [photo.jpg, photo (1).jpg]", names)
		}
		if bodies[0] != payload || bodies[1] != payload {
			t.Fatalf("replayed body mismatch: %v", bodies)
		}
		if res.Status != aphotos.TransferConfirmed || res.NodeID != "RENAMED_NODE_1" || !res.ConflictResolved || res.Name != "photo (1).jpg" {
			t.Fatalf("unexpected ConflictRename result: %+v", res)
		}
	})

	t.Run("ConflictRename fails on non-replayable reader", func(t *testing.T) {
		client := newTestTransferClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			_, _ = io.Copy(io.Discard, req.Body)
			return &http.Response{
				StatusCode: http.StatusConflict,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"message":"Conflict"}`)),
			}, nil
		}))

		nonSeek := struct{ io.Reader }{strings.NewReader("cannot-seek")}
		res, err := client.Upload(context.Background(), aphotos.UploadRequest{
			Name:           "photo.jpg",
			ParentNodeID:   "ROOT1",
			Body:           nonSeek,
			ConflictPolicy: aphotos.ConflictRename,
		})
		if _, ok := errors.AsType[*aphotos.ConflictError](err); !ok {
			t.Fatalf("expected *ConflictError when reader cannot be replayed, got %T: %v", err, err)
		}
		if res == nil || res.Status != aphotos.TransferFailed {
			t.Fatalf("expected TransferFailed, got %+v", res)
		}
	})
}

func TestUpload_ConflictSkipFailsWhenUnverified(t *testing.T) {
	t.Run("409 without node ID and empty remote query returns ConflictError", func(t *testing.T) {
		client := newTestTransferClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodPost {
				_, _ = io.Copy(io.Discard, req.Body)
				return &http.Response{
					StatusCode: http.StatusConflict,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"message":"Conflict without node ID"}`)),
				}, nil
			}
			if req.Method == http.MethodGet && req.URL.Path == "/drive/v1/nodes" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"count":0,"data":[]}`)),
				}, nil
			}
			return nil, errors.New("unexpected request")
		}))

		res, err := client.Upload(context.Background(), aphotos.UploadRequest{
			Name:           "ghost.jpg",
			ParentNodeID:   "ROOT1",
			Body:           strings.NewReader("ghost-data"),
			ConflictPolicy: aphotos.ConflictSkip,
		})
		if _, ok := errors.AsType[*aphotos.ConflictError](err); !ok {
			t.Fatalf("expected *ConflictError when 409 has no node ID and remote query finds nothing, got %T: %v", err, err)
		}
		if res == nil || res.Status != aphotos.TransferFailed {
			t.Fatalf("expected TransferFailed, got %+v", res)
		}
	})

	t.Run("409 with hint node ID or same-name remote node lacking MD5 returns unconfirmed and never skips", func(t *testing.T) {
		client := newTestTransferClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodPost {
				_, _ = io.Copy(io.Discard, req.Body)
				return &http.Response{
					StatusCode: http.StatusConflict,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"message":"NODE_EXISTS","info":{"nodeId":"NO_MD5_NODE_1"}}`)),
				}, nil
			}
			if req.Method == http.MethodGet && req.URL.Path == "/drive/v1/nodes" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"count":1,"data":[{"id":"NO_MD5_NODE_1","name":"dup.jpg","kind":"FILE","status":"AVAILABLE","contentProperties":{"size":10}}]}`)),
				}, nil
			}
			return nil, errors.New("unexpected request")
		}))

		res, err := client.Upload(context.Background(), aphotos.UploadRequest{
			Name:           "dup.jpg",
			ParentNodeID:   "ROOT1",
			Body:           strings.NewReader("0123456789"),
			ConflictPolicy: aphotos.ConflictSkip,
		})
		if err == nil {
			t.Fatal("expected error when remote node lacks MD5, got nil")
		}
		if _, ok := errors.AsType[*aphotos.PartialResultError](err); !ok {
			t.Fatalf("expected *PartialResultError when remote node lacks MD5, got %T: %v", err, err)
		}
		if res == nil || res.Status != aphotos.TransferUnconfirmed {
			t.Fatalf("expected TransferUnconfirmed, got %+v", res)
		}
	})
}

func TestUploadFile_StreamsFromLocalDiskAndSupportsConflictRename(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "IMG_0001.JPG")
	content := []byte("real-local-file-bytes-123456")
	if err := os.WriteFile(filePath, content, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var attempts int
	var gotNames []string
	client := newTestTransferClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		gotNames = append(gotNames, req.URL.Query().Get("name"))
		b, _ := io.ReadAll(req.Body)
		if !bytes.Equal(b, content) {
			t.Errorf("uploaded body = %q, want %q", string(b), string(content))
		}
		if attempts == 1 {
			return &http.Response{
				StatusCode: http.StatusConflict,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"message":"NODE_EXISTS"}`)),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"id":"LOCAL_FILE_NODE","name":"IMG_0001 (1).JPG","kind":"FILE"}`)),
		}, nil
	}))

	res, err := client.UploadFile(context.Background(), filePath, aphotos.UploadOptions{
		ParentNodeID:   "ROOT1",
		ConflictPolicy: aphotos.ConflictRename,
	})
	if err != nil {
		t.Fatalf("UploadFile failed: %v", err)
	}
	if res.Status != aphotos.TransferConfirmed || res.NodeID != "LOCAL_FILE_NODE" || !res.ConflictResolved {
		t.Fatalf("unexpected UploadFile result: %+v", res)
	}
	if len(gotNames) != 2 || gotNames[0] != "IMG_0001.JPG" || gotNames[1] != "IMG_0001 (1).JPG" {
		t.Fatalf("gotNames = %v, want [IMG_0001.JPG IMG_0001 (1).JPG]", gotNames)
	}
}

func TestDownload_RedirectAndVerifiedAtomicWrite(t *testing.T) {
	payload := []byte("verified-heic-binary-payload-0123456789")
	sum := md5.Sum(payload)
	wantMD5 := hex.EncodeToString(sum[:])

	var cdnCookieHeader string
	var cdnSessionHeader string
	cdnSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cdnCookieHeader = r.Header.Get("Cookie")
		cdnSessionHeader = r.Header.Get("x-amzn-sessionid")
		w.Header().Set("Content-Disposition", `attachment; filename="IMG_1234.HEIC"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer cdnSrv.Close()

	var gotQuerySuffix string
	var gotOwnerID string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/drive/v1/nodes" && r.URL.Query().Get("filters") == "isRoot:true" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"count":1,"data":[{"id":"ROOT1","ownerId":"OWNER_RESOLVED","kind":"FOLDER","isRoot":true}]}`))
			return
		}
		if r.URL.Path == "/drive/v1/nodes/NODE_DL_1/contentRedirection" {
			gotQuerySuffix = r.URL.Query().Get("querySuffix")
			gotOwnerID = r.URL.Query().Get("ownerId")
			http.Redirect(w, r, cdnSrv.URL+"/cdproxy/templink/secret-token-xyz?X-Amz-Signature=supersecret", http.StatusTemporaryRedirect)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiSrv.Close()

	client, err := aphotos.NewClient(aphotos.Options{
		Region:   aphotos.RegionUS,
		DriveURL: apiSrv.URL + "/drive/v1",
		Cookies: map[string]string{
			"session-id": "111-2222222-3333333",
			"ubid-main":  "130-0000000-0000000",
			"at-main":    "Atza|synthetic-token-value",
		},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	destDir := t.TempDir()
	var lastProgress aphotos.TransferProgress
	res, err := client.Download(context.Background(), aphotos.DownloadRequest{
		NodeID:       "NODE_DL_1",
		DestDir:      destDir,
		ExpectedSize: int64(len(payload)),
		ExpectedMD5:  wantMD5,
		Progress: func(p aphotos.TransferProgress) {
			lastProgress = p
		},
	})
	if err != nil {
		t.Fatalf("Download returned error: %v", err)
	}
	if gotQuerySuffix != "?download=true" {
		t.Errorf("querySuffix = %q, want ?download=true", gotQuerySuffix)
	}
	if gotOwnerID != "OWNER_RESOLVED" {
		t.Errorf("ownerId = %q, want OWNER_RESOLVED", gotOwnerID)
	}
	if cdnCookieHeader != "" || cdnSessionHeader != "" {
		t.Errorf("expected credentials stripped on cross-host redirect, got Cookie=%q x-amzn-sessionid=%q", cdnCookieHeader, cdnSessionHeader)
	}
	if res.Status != aphotos.TransferConfirmed {
		t.Errorf("Status = %q, want %q", res.Status, aphotos.TransferConfirmed)
	}
	if res.Name != "IMG_1234.HEIC" {
		t.Errorf("Name = %q, want IMG_1234.HEIC", res.Name)
	}
	wantPath := filepath.Join(destDir, "IMG_1234.HEIC")
	if res.LocalPath != wantPath {
		t.Errorf("LocalPath = %q, want %q", res.LocalPath, wantPath)
	}
	gotBytes, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", wantPath, err)
	}
	if !bytes.Equal(gotBytes, payload) {
		t.Errorf("file content = %q, want %q", string(gotBytes), string(payload))
	}
	if res.MD5 != wantMD5 || res.Bytes != int64(len(payload)) {
		t.Errorf("res MD5=%q Bytes=%d, want MD5=%q Bytes=%d", res.MD5, res.Bytes, wantMD5, len(payload))
	}
	if lastProgress.BytesTransferred != int64(len(payload)) {
		t.Errorf("progress BytesTransferred = %d, want %d", lastProgress.BytesTransferred, len(payload))
	}
	entries, _ := os.ReadDir(destDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Errorf("leftover temp file found: %s", e.Name())
		}
	}
}

func TestDownload_PathTraversalPrevention(t *testing.T) {
	maliciousDispositions := []struct {
		disp     string
		fileName string
		wantSafe string
	}{
		{
			disp:     `attachment; filename="../../etc/passwd"`,
			wantSafe: "passwd",
		},
		{
			disp:     `attachment; filename="..\..\..\..\Windows\System32\cmd.exe"`,
			wantSafe: "cmd.exe",
		},
		{
			fileName: "../../../outside.jpg",
			wantSafe: "outside.jpg",
		},
		{
			disp:     `attachment; filename=".."`,
			wantSafe: "NODE_TRAVERSAL",
		},
	}

	for i, tc := range maliciousDispositions {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.disp != "" {
					w.Header().Set("Content-Disposition", tc.disp)
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("safe-bytes"))
			}))
			defer srv.Close()

			client, err := aphotos.NewClient(aphotos.Options{
				Region:   aphotos.RegionUS,
				DriveURL: srv.URL + "/drive/v1",
				Cookies: map[string]string{
					"session-id": "111-2222222-3333333",
					"ubid-main":  "130-0000000-0000000",
					"at-main":    "Atza|synthetic-token-value",
				},
			})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			defer func() { _ = client.Close() }()

			destDir := t.TempDir()
			res, err := client.Download(context.Background(), aphotos.DownloadRequest{
				NodeID:   "NODE_TRAVERSAL",
				OwnerID:  "OWN1",
				DestDir:  destDir,
				FileName: tc.fileName,
			})
			if err != nil {
				t.Fatalf("Download returned error: %v", err)
			}
			if filepath.Dir(res.LocalPath) != filepath.Clean(destDir) {
				t.Fatalf("LocalPath %q escaped DestDir %q", res.LocalPath, destDir)
			}
			if res.Name != tc.wantSafe {
				t.Errorf("Name = %q, want %q", res.Name, tc.wantSafe)
			}
		})
	}
}

func TestDownload_VerificationFailuresAndCleanup(t *testing.T) {
	t.Run("size mismatch removes temp file and fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("short"))
		}))
		defer srv.Close()

		client, _ := aphotos.NewClient(aphotos.Options{
			Region:   aphotos.RegionUS,
			DriveURL: srv.URL + "/drive/v1",
			Cookies: map[string]string{
				"session-id": "111-2222222-3333333",
				"ubid-main":  "130-0000000-0000000",
				"at-main":    "Atza|token",
			},
		})
		defer func() { _ = client.Close() }()

		destDir := t.TempDir()
		res, err := client.Download(context.Background(), aphotos.DownloadRequest{
			NodeID:       "NODE_SHORT",
			OwnerID:      "OWN1",
			DestDir:      destDir,
			FileName:     "short.jpg",
			ExpectedSize: 100,
		})
		if err == nil {
			t.Fatal("expected error on ExpectedSize mismatch, got nil")
		}
		if res == nil || res.Status != aphotos.TransferFailed {
			t.Fatalf("expected TransferFailed, got %+v", res)
		}
		entries, _ := os.ReadDir(destDir)
		if len(entries) != 0 {
			t.Fatalf("expected empty destDir after failed download, found %d files", len(entries))
		}
	})

	t.Run("MD5 mismatch removes temp file and fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("corrupted-bytes"))
		}))
		defer srv.Close()

		client, _ := aphotos.NewClient(aphotos.Options{
			Region:   aphotos.RegionUS,
			DriveURL: srv.URL + "/drive/v1",
			Cookies: map[string]string{
				"session-id": "111-2222222-3333333",
				"ubid-main":  "130-0000000-0000000",
				"at-main":    "Atza|token",
			},
		})
		defer func() { _ = client.Close() }()

		destDir := t.TempDir()
		res, err := client.Download(context.Background(), aphotos.DownloadRequest{
			NodeID:      "NODE_BAD_MD5",
			OwnerID:     "OWN1",
			DestDir:     destDir,
			FileName:    "corrupt.jpg",
			ExpectedMD5: "00000000000000000000000000000000",
		})
		if err == nil {
			t.Fatal("expected error on ExpectedMD5 mismatch, got nil")
		}
		if _, ok := errors.AsType[*aphotos.RemoteProtocolError](err); !ok {
			t.Fatalf("expected *RemoteProtocolError on MD5 mismatch, got %T: %v", err, err)
		}
		if res == nil || res.Status != aphotos.TransferFailed {
			t.Fatalf("expected TransferFailed, got %+v", res)
		}
		entries, _ := os.ReadDir(destDir)
		if len(entries) != 0 {
			t.Fatalf("expected empty destDir after MD5 mismatch, found %d files", len(entries))
		}
	})

	t.Run("premature EOF and context cancellation clean up temp file", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "10000")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("partial-chunk"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			cancel()
		}))
		defer srv.Close()

		client, _ := aphotos.NewClient(aphotos.Options{
			Region:         aphotos.RegionUS,
			DriveURL:       srv.URL + "/drive/v1",
			DisableRetries: true,
			Cookies: map[string]string{
				"session-id": "111-2222222-3333333",
				"ubid-main":  "130-0000000-0000000",
				"at-main":    "Atza|token",
			},
		})
		defer func() { _ = client.Close() }()

		destDir := t.TempDir()
		_, err := client.Download(ctx, aphotos.DownloadRequest{
			NodeID:   "NODE_CANCEL",
			OwnerID:  "OWN1",
			DestDir:  destDir,
			FileName: "partial.jpg",
		})
		if err == nil {
			t.Fatal("expected error on truncated/canceled download, got nil")
		}
		entries, _ := os.ReadDir(destDir)
		if len(entries) != 0 {
			t.Fatalf("expected empty destDir after canceled download, found %d files", len(entries))
		}
	})
}

func TestDownload_OutlivesClientTimeoutAndHonorsContext(t *testing.T) {
	payload := bytes.Repeat([]byte("v"), 4*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for chunk := range slices.Chunk(payload, 1024) {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			_, _ = w.Write(chunk)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	client, err := aphotos.NewClient(aphotos.Options{
		Region:         aphotos.RegionUS,
		DriveURL:       srv.URL + "/drive/v1",
		HTTPClient:     &http.Client{Timeout: 150 * time.Millisecond},
		DisableRetries: true,
		Cookies: map[string]string{
			"session-id": "111-2222222-3333333",
			"ubid-main":  "130-0000000-0000000",
			"at-main":    "Atza|token",
		},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	t.Run("body slower than HTTPClient.Timeout completes", func(t *testing.T) {
		destDir := t.TempDir()
		res, err := client.Download(context.Background(), aphotos.DownloadRequest{
			NodeID:       "NODE_SLOW",
			OwnerID:      "OWN1",
			DestDir:      destDir,
			FileName:     "slow.mp4",
			ExpectedSize: int64(len(payload)),
		})
		if err != nil {
			t.Fatalf("Download returned error: %v", err)
		}
		if res.Status != aphotos.TransferConfirmed || res.Bytes != int64(len(payload)) {
			t.Fatalf("Status=%q Bytes=%d, want %q %d", res.Status, res.Bytes, aphotos.TransferConfirmed, len(payload))
		}
		got, err := os.ReadFile(filepath.Join(destDir, "slow.mp4"))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("file content length = %d, want %d", len(got), len(payload))
		}
	})

	t.Run("context deadline still cancels and cleans up", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		destDir := t.TempDir()
		_, err := client.Download(ctx, aphotos.DownloadRequest{
			NodeID:   "NODE_SLOW",
			OwnerID:  "OWN1",
			DestDir:  destDir,
			FileName: "slow.mp4",
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want context.DeadlineExceeded", err)
		}
		entries, _ := os.ReadDir(destDir)
		if len(entries) != 0 {
			t.Fatalf("expected empty destDir after deadline, found %d files", len(entries))
		}
	})
}

func TestUploadBatch_DirectoryWalkDryRunAndErrorPropagation(t *testing.T) {
	srcDir := t.TempDir()
	subDir := filepath.Join(srcDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "a.jpg"), []byte("img-a"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "notes.txt"), []byte("note"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "b.mp4"), []byte("vid-b"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Run("DryRun performs zero HTTP calls and filters media when MediaOnly is true", func(t *testing.T) {
		var calls atomic.Int32
		client := newTestTransferClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, errors.New("unexpected network call during DryRun")
		}))

		results, err := client.UploadBatch(context.Background(), []string{srcDir}, aphotos.BatchUploadOptions{
			ParentNodeID: "ROOT1",
			Recursive:    true,
			MediaOnly:    true,
			DryRun:       true,
		})
		if err != nil {
			t.Fatalf("UploadBatch DryRun error: %v", err)
		}
		if calls.Load() != 0 {
			t.Fatalf("expected 0 HTTP calls in DryRun, got %d", calls.Load())
		}
		if len(results) != 2 {
			t.Fatalf("len(results) = %d, want 2 (a.jpg and b.mp4)", len(results))
		}
		if results[0].Name != "a.jpg" || results[1].Name != "b.mp4" {
			t.Fatalf("unexpected DryRun names: %+v", results)
		}
	})

	t.Run("ContinueOnError returns PartialResultError when one file fails", func(t *testing.T) {
		client := newTestTransferClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			_, _ = io.Copy(io.Discard, req.Body)
			name := req.URL.Query().Get("name")
			if name == "notes.txt" {
				return &http.Response{
					StatusCode: http.StatusConflict,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"message":"Conflict"}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusCreated,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(fmt.Sprintf(`{"id":"ID_%s","name":%q,"kind":"FILE"}`, name, name))),
			}, nil
		}))

		results, err := client.UploadBatch(context.Background(), []string{srcDir}, aphotos.BatchUploadOptions{
			ParentNodeID:    "ROOT1",
			Recursive:       true,
			ContinueOnError: true,
			Concurrency:     2,
		})
		if _, ok := errors.AsType[*aphotos.PartialResultError](err); !ok {
			t.Fatalf("expected *PartialResultError, got %T: %v", err, err)
		}
		if len(results) != 3 {
			t.Fatalf("len(results) = %d, want 3", len(results))
		}
		var confirmed, failed int
		for _, r := range results {
			switch r.Status {
			case aphotos.TransferConfirmed:
				confirmed++
			case aphotos.TransferFailed:
				failed++
			}
		}
		if confirmed != 2 || failed != 1 {
			t.Fatalf("confirmed=%d failed=%d, want 2 and 1", confirmed, failed)
		}
	})

	t.Run("FailFast cancels remaining workers cleanly without deadlock", func(t *testing.T) {
		manyDir := t.TempDir()
		for i := range 20 {
			p := filepath.Join(manyDir, fmt.Sprintf("img_%02d.jpg", i))
			if err := os.WriteFile(p, []byte("data"), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
		}

		client := newTestTransferClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			_, _ = io.Copy(io.Discard, req.Body)
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"message":"Unauthorized"}`)),
			}, nil
		}))

		done := make(chan struct{})
		var batchErr error
		go func() {
			defer close(done)
			_, batchErr = client.UploadBatch(context.Background(), []string{manyDir}, aphotos.BatchUploadOptions{
				ParentNodeID:    "ROOT1",
				Recursive:       true,
				ContinueOnError: false,
				Concurrency:     3,
			})
		}()

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("UploadBatch deadlocked on worker failure")
		}
		if _, ok := errors.AsType[*aphotos.AuthenticationError](batchErr); !ok {
			t.Fatalf("expected *AuthenticationError, got %T: %v", batchErr, batchErr)
		}
	})
}

func TestDownloadBatch_ConcurrentNodeOwnersWithoutRootOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ready := make(chan struct{})
	var metadataRequests atomic.Int32
	client := newTestTransferClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/drive/v1/nodes" {
			return jsonResponse(req, http.StatusOK, `{"data":[{"id":"ROOT","kind":"FOLDER","isRoot":true}]}`), nil
		}
		nodeID, _, _ := strings.Cut(strings.TrimPrefix(req.URL.Path, "/drive/v1/nodes/"), "/")
		if nodeID != "NODE_A" && nodeID != "NODE_B" {
			return nil, fmt.Errorf("unexpected request: %s", req.URL.Path)
		}
		if strings.HasSuffix(req.URL.Path, "/contentRedirection") {
			if got := req.URL.Query().Get("ownerId"); got != "OWNER_"+nodeID {
				t.Errorf("%s ownerId = %q, want %q", nodeID, got, "OWNER_"+nodeID)
			}
			return jsonResponse(req, http.StatusOK, nodeID), nil
		}
		if metadataRequests.Add(1) == 2 {
			close(ready)
		}
		select {
		case <-ready:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		return jsonResponse(req, http.StatusOK, fmt.Sprintf(`{"id":%q,"name":%q,"kind":"FILE","ownerId":%q}`, nodeID, nodeID+".jpg", "OWNER_"+nodeID)), nil
	}))
	destDir := t.TempDir()
	results, err := client.DownloadBatch(ctx, []string{"NODE_A", "NODE_B"}, aphotos.BatchDownloadOptions{
		DestDir:     destDir,
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("DownloadBatch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	for i, nodeID := range []string{"NODE_A", "NODE_B"} {
		if results[i].Status != aphotos.TransferConfirmed {
			t.Errorf("%s status = %s, want confirmed", nodeID, results[i].Status)
		}
		content, err := os.ReadFile(filepath.Join(destDir, nodeID+".jpg"))
		if err != nil || string(content) != nodeID {
			t.Errorf("%s downloaded content = %q, err = %v", nodeID, content, err)
		}
	}
}

func TestDownloadBatch_PartialAndSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "NODE_BAD") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not found"}`))
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="ok.jpg"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok-content"))
	}))
	defer srv.Close()

	client, err := aphotos.NewClient(aphotos.Options{
		Region:   aphotos.RegionUS,
		DriveURL: srv.URL + "/drive/v1",
		Cookies: map[string]string{
			"session-id": "111-2222222-3333333",
			"ubid-main":  "130-0000000-0000000",
			"at-main":    "Atza|token",
		},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	destDir := t.TempDir()
	results, err := client.DownloadBatch(context.Background(), []string{"NODE_OK", "NODE_BAD"}, aphotos.BatchDownloadOptions{
		DestDir:         destDir,
		OwnerID:         "OWN1",
		ContinueOnError: true,
		Concurrency:     2,
	})
	if _, ok := errors.AsType[*aphotos.PartialResultError](err); !ok {
		t.Fatalf("expected *PartialResultError, got %T: %v", err, err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].Status != aphotos.TransferConfirmed || results[1].Status != aphotos.TransferFailed {
		t.Fatalf("unexpected batch download statuses: %+v", results)
	}
}

func TestDownload_RFC5987FilenameStarAndSignedURLRedaction(t *testing.T) {
	t.Run("parses RFC 5987 filename* UTF-8 percent-encoded disposition", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''Vacaciones%202026%20%281%29.jpg")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("utf8-file"))
		}))
		defer srv.Close()

		client, err := aphotos.NewClient(aphotos.Options{
			Region:   aphotos.RegionUS,
			DriveURL: srv.URL + "/drive/v1",
			Cookies: map[string]string{
				"session-id": "111-2222222-3333333",
				"ubid-main":  "130-0000000-0000000",
				"at-main":    "Atza|token",
			},
		})
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		defer func() { _ = client.Close() }()

		destDir := t.TempDir()
		res, err := client.Download(context.Background(), aphotos.DownloadRequest{
			NodeID:  "NODE_UTF8",
			OwnerID: "OWN1",
			DestDir: destDir,
		})
		if err != nil {
			t.Fatalf("Download: %v", err)
		}
		if res.Name != "Vacaciones 2026 (1).jpg" {
			t.Fatalf("Name = %q, want %q", res.Name, "Vacaciones 2026 (1).jpg")
		}
	})

	t.Run("redacts signed templink URLs when redirected download fails", func(t *testing.T) {
		apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://127.0.0.1:1/cdproxy/templink/SECRET_TEMPLINK_TOKEN?X-Amz-Signature=SECRET_SIG_VAL", http.StatusTemporaryRedirect)
		}))
		defer apiSrv.Close()

		client, err := aphotos.NewClient(aphotos.Options{
			Region:         aphotos.RegionUS,
			DriveURL:       apiSrv.URL + "/drive/v1",
			DisableRetries: true,
			Cookies: map[string]string{
				"session-id": "111-2222222-3333333",
				"ubid-main":  "130-0000000-0000000",
				"at-main":    "Atza|token",
			},
		})
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		defer func() { _ = client.Close() }()

		_, err = client.Download(context.Background(), aphotos.DownloadRequest{
			NodeID:  "NODE_REDIRECT_ERR",
			OwnerID: "OWN1",
			DestDir: t.TempDir(),
		})
		if err == nil {
			t.Fatal("expected error when redirect target fails, got nil")
		}
		msg := err.Error()
		if strings.Contains(msg, "SECRET_TEMPLINK_TOKEN") || strings.Contains(msg, "SECRET_SIG_VAL") {
			t.Fatalf("error message leaked signed templink URL secrets: %s", msg)
		}
	})
}

func TestDownload_LocalConflictPoliciesAndConcurrentSafety(t *testing.T) {
	payload := []byte("remote-photo-bytes-v2")
	sum := md5.Sum(payload)
	payloadMD5 := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="conflict.jpg"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	client, err := aphotos.NewClient(aphotos.Options{
		Region:   aphotos.RegionUS,
		DriveURL: srv.URL + "/drive/v1",
		Cookies: map[string]string{
			"session-id": "111-2222222-3333333",
			"ubid-main":  "130-0000000-0000000",
			"at-main":    "Atza|token",
		},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	t.Run("default ConflictFail never overwrites existing local file", func(t *testing.T) {
		destDir := t.TempDir()
		existingPath := filepath.Join(destDir, "conflict.jpg")
		original := []byte("original-precious-local-file")
		if err := os.WriteFile(existingPath, original, 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		res, err := client.Download(context.Background(), aphotos.DownloadRequest{
			NodeID:      "NODE_1",
			OwnerID:     "OWN1",
			DestDir:     destDir,
			ExpectedMD5: payloadMD5,
		})
		if _, ok := errors.AsType[*aphotos.ConflictError](err); !ok {
			t.Fatalf("expected *ConflictError when destination file exists, got %T: %v", err, err)
		}
		if res == nil || res.Status != aphotos.TransferFailed {
			t.Fatalf("expected TransferFailed, got %+v", res)
		}
		after, _ := os.ReadFile(existingPath)
		if !bytes.Equal(after, original) {
			t.Fatalf("existing file was overwritten: got %q, want %q", string(after), string(original))
		}
	})

	t.Run("ConflictSkip preserves existing local file and returns skipped", func(t *testing.T) {
		destDir := t.TempDir()
		existingPath := filepath.Join(destDir, "conflict.jpg")
		original := []byte("original-precious-local-file")
		if err := os.WriteFile(existingPath, original, 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		res, err := client.Download(context.Background(), aphotos.DownloadRequest{
			NodeID:         "NODE_1",
			OwnerID:        "OWN1",
			DestDir:        destDir,
			ConflictPolicy: aphotos.ConflictSkip,
		})
		if err != nil {
			t.Fatalf("ConflictSkip returned unexpected error: %v", err)
		}
		if res == nil || res.Status != aphotos.TransferSkipped || !res.ConflictResolved {
			t.Fatalf("expected TransferSkipped with ConflictResolved=true, got %+v", res)
		}
		after, _ := os.ReadFile(existingPath)
		if !bytes.Equal(after, original) {
			t.Fatalf("existing file was modified: got %q, want %q", string(after), string(original))
		}
	})

	t.Run("ConflictRename disambiguates filename without overwriting", func(t *testing.T) {
		destDir := t.TempDir()
		existingPath := filepath.Join(destDir, "conflict.jpg")
		original := []byte("original-precious-local-file")
		if err := os.WriteFile(existingPath, original, 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		res, err := client.Download(context.Background(), aphotos.DownloadRequest{
			NodeID:         "NODE_1",
			OwnerID:        "OWN1",
			DestDir:        destDir,
			ExpectedMD5:    payloadMD5,
			ConflictPolicy: aphotos.ConflictRename,
		})
		if err != nil {
			t.Fatalf("ConflictRename returned error: %v", err)
		}
		if res == nil || res.Status != aphotos.TransferConfirmed || res.Name != "conflict (1).jpg" || !res.ConflictResolved || !res.MD5Verified {
			t.Fatalf("unexpected ConflictRename result: %+v", res)
		}
		afterOrig, _ := os.ReadFile(existingPath)
		if !bytes.Equal(afterOrig, original) {
			t.Fatalf("original file modified: %q", string(afterOrig))
		}
		renamedBytes, err := os.ReadFile(filepath.Join(destDir, "conflict (1).jpg"))
		if err != nil || !bytes.Equal(renamedBytes, payload) {
			t.Fatalf("renamed file content mismatch: err=%v content=%q", err, string(renamedBytes))
		}
	})

	t.Run("concurrent downloads of same filename with ConflictRename never overwrite each other", func(t *testing.T) {
		destDir := t.TempDir()
		const workers = 6
		errs := make(chan error, workers)
		paths := make(chan string, workers)
		for i := range workers {
			go func(idx int) {
				res, dlErr := client.Download(context.Background(), aphotos.DownloadRequest{
					NodeID:         fmt.Sprintf("NODE_%d", idx),
					OwnerID:        "OWN1",
					DestDir:        destDir,
					ExpectedMD5:    payloadMD5,
					ConflictPolicy: aphotos.ConflictRename,
				})
				if dlErr != nil {
					errs <- dlErr
					return
				}
				paths <- res.LocalPath
				errs <- nil
			}(i)
		}
		seen := make(map[string]bool)
		for range workers {
			if e := <-errs; e != nil {
				t.Fatalf("concurrent download failed: %v", e)
			}
		}
		close(paths)
		for p := range paths {
			if seen[p] {
				t.Fatalf("duplicate output path written by concurrent workers: %s", p)
			}
			seen[p] = true
		}
		if len(seen) != workers {
			t.Fatalf("expected %d distinct files, got %d", workers, len(seen))
		}
	})
}

func TestDownloadBatch_MetadataVerificationAndMissingMD5Warning(t *testing.T) {
	goodPayload := []byte("verified-batch-content-12345")
	goodSum := md5.Sum(goodPayload)
	goodMD5 := hex.EncodeToString(goodSum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/drive/v1/nodes/NODE_VERIFIED":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"id":"NODE_VERIFIED","name":"verified.jpg","kind":"FILE","contentProperties":{"size":%d,"md5":%q}}`, len(goodPayload), goodMD5)
		case "/drive/v1/nodes/NODE_VERIFIED/contentRedirection":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(goodPayload)
		case "/drive/v1/nodes/NODE_CORRUPT":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"id":"NODE_CORRUPT","name":"corrupt.jpg","kind":"FILE","contentProperties":{"size":%d,"md5":"ffffffffffffffffffffffffffffffff"}}`, len(goodPayload))
		case "/drive/v1/nodes/NODE_CORRUPT/contentRedirection":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(goodPayload)
		case "/drive/v1/nodes/NODE_NO_MD5":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"id":"NODE_NO_MD5","name":"nomd5.jpg","kind":"FILE","contentProperties":{"size":%d}}`, len(goodPayload))
		case "/drive/v1/nodes/NODE_NO_MD5/contentRedirection":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(goodPayload)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client, err := aphotos.NewClient(aphotos.Options{
		Region:   aphotos.RegionUS,
		DriveURL: srv.URL + "/drive/v1",
		Cookies: map[string]string{
			"session-id": "111-2222222-3333333",
			"ubid-main":  "130-0000000-0000000",
			"at-main":    "Atza|token",
		},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	destDir := t.TempDir()
	results, err := client.DownloadBatch(context.Background(), []string{"NODE_VERIFIED", "NODE_NO_MD5", "NODE_CORRUPT"}, aphotos.BatchDownloadOptions{
		DestDir:         destDir,
		OwnerID:         "OWN1",
		ContinueOnError: true,
		Concurrency:     2,
	})
	if _, ok := errors.AsType[*aphotos.PartialResultError](err); !ok {
		t.Fatalf("expected *PartialResultError when one item fails MD5 verification, got %T: %v", err, err)
	}
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}
	if results[0].Status != aphotos.TransferConfirmed || !results[0].MD5Verified || results[0].Warning != "" {
		t.Errorf("expected NODE_VERIFIED confirmed with MD5Verified=true and no warning, got %+v", results[0])
	}
	if results[1].Status != aphotos.TransferConfirmed || results[1].MD5Verified || results[1].Warning == "" {
		t.Errorf("expected NODE_NO_MD5 confirmed with MD5Verified=false and explicit warning, got %+v", results[1])
	}
	if results[2].Status != aphotos.TransferFailed {
		t.Errorf("expected NODE_CORRUPT to fail MD5 verification, got %+v", results[2])
	}
}
