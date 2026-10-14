package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/thedavidweng/amazon-photos/internal/model"
	"gopkg.in/yaml.v3"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonHTTPResponse(status int, body string) *http.Response {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("x-amzn-RequestId", "req-mock-123")
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func execCLI(t *testing.T, rt http.RoundTripper, args ...string) (stdout, stderr string, exitCode int, err error) {
	t.Helper()
	cmd := NewRootCommand(RootOptions{
		Transport: rt,
	})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)

	err = cmd.Execute()
	exitCode = 0
	if err != nil {
		var cmdErr *model.CommandError
		if errors.As(err, &cmdErr) {
			exitCode = cmdErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	return outBuf.String(), errBuf.String(), exitCode, err
}

func TestVersionAndHelpWorkOfflineWithoutCredentials(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "nonexistent", "config.yaml")

	t.Run("--help", func(t *testing.T) {
		stdout, _, code, err := execCLI(t, nil, "--config", cfgPath, "--help")
		if err != nil || code != 0 {
			t.Fatalf("--help failed: code=%d err=%v", code, err)
		}
		if !strings.Contains(stdout, "aphotos") {
			t.Errorf("expected help output, got %q", stdout)
		}
	})

	t.Run("--version", func(t *testing.T) {
		stdout, _, code, err := execCLI(t, nil, "--config", cfgPath, "--version")
		if err != nil || code != 0 {
			t.Fatalf("--version failed: code=%d err=%v", code, err)
		}
		if !strings.Contains(stdout, "dev") {
			t.Errorf("expected version output, got %q", stdout)
		}
	})

	t.Run("version --json", func(t *testing.T) {
		stdout, stderr, code, err := execCLI(t, nil, "--config", cfgPath, "--json", "version")
		if err != nil || code != 0 {
			t.Fatalf("version --json failed: code=%d err=%v stderr=%s", code, err, stderr)
		}
		var env model.Envelope
		if uErr := json.Unmarshal([]byte(stdout), &env); uErr != nil {
			t.Fatalf("invalid JSON %q: %v", stdout, uErr)
		}
		if !env.OK || env.Meta.Command != "version" {
			t.Errorf("unexpected version envelope: %+v", env)
		}
	})

	t.Run("completion bash", func(t *testing.T) {
		stdout, _, code, err := execCLI(t, nil, "--config", cfgPath, "completion", "bash")
		if err != nil || code != 0 || !strings.Contains(stdout, "aphotos") {
			t.Fatalf("completion bash failed: code=%d err=%v", code, err)
		}
	})
}

func TestAuthImportStatusLogoutE2E(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "aphotos", "config.yaml")
	cookieFile := filepath.Join(tmpDir, "cookies.json")
	cookieContent := `{"session-id":"140-1111111-2222222","ubid-main":"130-3333333-4444444","at-main":"Atza|test-secret-cookie"}`
	if err := os.WriteFile(cookieFile, []byte(cookieContent), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stdout, stderr, code, err := execCLI(t, nil, "--config", cfgPath, "--json", "auth", "import", cookieFile)
	if err != nil || code != 0 {
		t.Fatalf("auth import failed: code=%d err=%v stderr=%s", code, err, stderr)
	}
	if strings.Contains(stdout, "Atza|test-secret-cookie") || strings.Contains(stdout, "140-1111111-2222222") {
		t.Fatalf("auth import leaked raw cookie values on stdout: %s", stdout)
	}
	var importEnv model.Envelope
	if uErr := json.Unmarshal([]byte(stdout), &importEnv); uErr != nil || !importEnv.OK {
		t.Fatalf("invalid auth import envelope: %v (%s)", uErr, stdout)
	}

	stdout, stderr, code, err = execCLI(t, nil, "--config", cfgPath, "--json", "auth", "status")
	if err != nil || code != 0 {
		t.Fatalf("auth status failed: code=%d err=%v stderr=%s", code, err, stderr)
	}
	var statusEnv model.Envelope
	if uErr := json.Unmarshal([]byte(stdout), &statusEnv); uErr != nil || !statusEnv.OK {
		t.Fatalf("invalid auth status envelope: %v (%s)", uErr, stdout)
	}
	if statusEnv.Meta.Region != "us" {
		t.Errorf("Meta.Region = %q, want us", statusEnv.Meta.Region)
	}

	_, stderr, code, err = execCLI(t, nil, "--config", cfgPath, "--json", "auth", "logout")
	if err != nil || code != 0 {
		t.Fatalf("auth logout failed: code=%d err=%v stderr=%s", code, err, stderr)
	}

	_, _, code, _ = execCLI(t, nil, "--config", cfgPath, "--json", "auth", "status")
	if code != 2 && code != 8 {
		t.Errorf("expected auth/config exit code after logout, got %d", code)
	}
}

func writeTestConfig(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cookieFile := filepath.Join(tmpDir, "cookies.json")
	if err := os.WriteFile(cookieFile, []byte(`{"session-id":"140-1111111-2222222","ubid-main":"130-3333333-4444444","at-main":"Atza|test-secret-cookie"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, _, code, err := execCLI(t, nil, "--config", cfgPath, "auth", "import", cookieFile)
	if err != nil || code != 0 {
		t.Fatalf("auth import setup failed: code=%d err=%v", code, err)
	}
	return cfgPath
}

func TestUsageAndDoctorE2E(t *testing.T) {
	cfgPath := writeTestConfig(t)

	t.Run("usage --json", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/drive/v1/account/usage" {
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}
			return jsonHTTPResponse(200, `{
				"lastCalculated": "2026-02-21T12:00:00.000Z",
				"photo": {"billable": {"bytes": 0, "count": 0}, "total": {"bytes": 1024000, "count": 42}},
				"video": {"billable": {"bytes": 2048000, "count": 3}, "total": {"bytes": 2048000, "count": 3}}
			}`), nil
		})

		stdout, stderr, code, err := execCLI(t, rt, "--config", cfgPath, "--json", "usage")
		if err != nil || code != 0 {
			t.Fatalf("usage failed: code=%d err=%v stderr=%s", code, err, stderr)
		}
		var env model.Envelope
		if uErr := json.Unmarshal([]byte(stdout), &env); uErr != nil || !env.OK {
			t.Fatalf("invalid usage envelope: %v (%s)", uErr, stdout)
		}
	})

	t.Run("doctor unconfigured with --allow-unconfigured succeeds in CI", func(t *testing.T) {
		missingCfg := filepath.Join(t.TempDir(), "missing", "config.yaml")
		stdout, stderr, code, err := execCLI(t, nil, "--config", missingCfg, "--json", "doctor", "--allow-unconfigured")
		if err != nil || code != 0 {
			t.Fatalf("doctor --allow-unconfigured failed: code=%d err=%v stderr=%s", code, err, stderr)
		}
		var env model.Envelope
		if uErr := json.Unmarshal([]byte(stdout), &env); uErr != nil || !env.OK {
			t.Fatalf("invalid doctor envelope: %v (%s)", uErr, stdout)
		}
	})

	t.Run("doctor --write-probe without --confirm returns exit 6", func(t *testing.T) {
		stdout, _, code, _ := execCLI(t, nil, "--config", cfgPath, "--json", "doctor", "--write-probe")
		if code != 6 {
			t.Fatalf("expected exit code 6 for --write-probe without --confirm, got %d (%s)", code, stdout)
		}
	})

	t.Run("doctor graded read and write-probe with --confirm", func(t *testing.T) {
		var createdProbe, trashedProbe bool
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Method == http.MethodGet && req.URL.Path == "/drive/v1/nodes":
				return jsonHTTPResponse(200, `{"count":1,"data":[{"id":"root-1","ownerId":"owner-1","kind":"FOLDER","isRoot":true,"status":"AVAILABLE"}]}`), nil
			case req.Method == http.MethodGet && req.URL.Path == "/drive/v1/account/usage":
				return jsonHTTPResponse(200, `{"lastCalculated":"2026-02-21T12:00:00.000Z","photo":{"billable":{"bytes":0,"count":0},"total":{"bytes":100,"count":1}}}`), nil
			case req.Method == http.MethodGet && req.URL.Path == "/drive/v1/search":
				return jsonHTTPResponse(200, `{"count":1,"data":[{"id":"photo-1","name":"a.jpg","kind":"FILE","status":"AVAILABLE"}]}`), nil
			case req.Method == http.MethodPost && req.URL.Path == "/drive/v1/nodes":
				createdProbe = true
				return jsonHTTPResponse(201, `{"id":"probe-folder-1","name":".aphotos-doctor-probe","kind":"FOLDER","status":"AVAILABLE"}`), nil
			case req.Method == http.MethodPatch && req.URL.Path == "/drive/v1/trash":
				trashedProbe = true
				return jsonHTTPResponse(200, `{}`), nil
			default:
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		})

		stdout, stderr, code, err := execCLI(t, rt, "--config", cfgPath, "--json", "doctor", "--write-probe", "--confirm")
		if err != nil || code != 0 {
			t.Fatalf("doctor --write-probe --confirm failed: code=%d err=%v stderr=%s stdout=%s", code, err, stderr, stdout)
		}
		if !createdProbe || !trashedProbe {
			t.Errorf("expected write probe to create and trash probe folder: created=%v trashed=%v", createdProbe, trashedProbe)
		}
		var env model.Envelope
		if uErr := json.Unmarshal([]byte(stdout), &env); uErr != nil || !env.OK {
			t.Fatalf("invalid doctor envelope: %v (%s)", uErr, stdout)
		}
	})
}

func TestSearchLsPhotosVideosAggregationsE2E(t *testing.T) {
	cfgPath := writeTestConfig(t)

	t.Run("search with typed flags and sort content-desc", func(t *testing.T) {
		var gotFilter, gotSort string
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/drive/v1/search" {
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}
			gotFilter = req.URL.Query().Get("filters")
			gotSort = req.URL.Query().Get("sort")
			return jsonHTTPResponse(200, `{
				"count": 1,
				"data": [{
					"id": "node-img-1",
					"name": "IMG_0001.JPG",
					"kind": "FILE",
					"status": "AVAILABLE",
					"createdDate": "2026-01-10T10:00:00.000Z",
					"contentProperties": {
						"size": 12345,
						"contentType": "image/jpeg",
						"contentDate": "2023-08-14T15:30:00.000Z"
					}
				}]
			}`), nil
		})

		stdout, stderr, code, err := execCLI(t, rt,
			"--config", cfgPath,
			"search",
			"--type", "photos",
			"--year", "2023",
			"--month", "8",
			"--day", "14",
			"--name", "IMG_",
			"--people", "cluster-1",
			"--location", "CAN#BC#Vancouver",
			"--things", "beach",
			"--sort", "content-desc",
			"--limit", "10",
		)
		if err != nil || code != 0 {
			t.Fatalf("search failed: code=%d err=%v stderr=%s", code, err, stderr)
		}
		if gotSort != "['contentProperties.contentDate DESC']" {
			t.Errorf("sort = %q, want ['contentProperties.contentDate DESC']", gotSort)
		}
		for _, wantClause := range []string{
			"type:(PHOTOS)",
			"timeYear:(2023)",
			"timeMonth:(8)",
			"timeDay:(14)",
			"name:IMG_*",
			"people:(cluster-1)",
			"location:(CAN#BC#Vancouver)",
			"things:(beach)",
		} {
			if !strings.Contains(gotFilter, wantClause) {
				t.Errorf("filter %q missing %q", gotFilter, wantClause)
			}
		}
		if !strings.Contains(stdout, "ContentDate") || !strings.Contains(stdout, "CreatedDate") {
			t.Errorf("expected human output to clearly label ContentDate and CreatedDate, got:\n%s", stdout)
		}
	})

	t.Run("photos and videos convenience commands", func(t *testing.T) {
		var filters []string
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			filters = append(filters, req.URL.Query().Get("filters"))
			return jsonHTTPResponse(200, `{"count":0,"data":[]}`), nil
		})

		_, _, code, err := execCLI(t, rt, "--config", cfgPath, "--json", "photos", "--year", "2024")
		if err != nil || code != 0 {
			t.Fatalf("photos failed: code=%d err=%v", code, err)
		}
		_, _, code, err = execCLI(t, rt, "--config", cfgPath, "--json", "videos", "--year", "2024")
		if err != nil || code != 0 {
			t.Fatalf("videos failed: code=%d err=%v", code, err)
		}
		if len(filters) != 2 || !strings.Contains(filters[0], "type:(PHOTOS)") || !strings.Contains(filters[1], "type:(VIDEOS)") {
			t.Errorf("unexpected filters for photos/videos: %v", filters)
		}
	})

	t.Run("ls and ls --parent", func(t *testing.T) {
		var paths []string
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			paths = append(paths, req.URL.Path)
			return jsonHTTPResponse(200, `{"count":1,"data":[{"id":"n1","name":"file.jpg","kind":"FILE","status":"AVAILABLE"}]}`), nil
		})

		stdout, _, code, err := execCLI(t, rt, "--config", cfgPath, "--json", "ls", "--limit", "5")
		if err != nil || code != 0 {
			t.Fatalf("ls failed: code=%d err=%v (%s)", code, err, stdout)
		}
		_, _, code, err = execCLI(t, rt, "--config", cfgPath, "--json", "ls", "--parent", "folder-123")
		if err != nil || code != 0 {
			t.Fatalf("ls --parent failed: code=%d err=%v", code, err)
		}
		if len(paths) != 2 || paths[0] != "/drive/v1/nodes" || paths[1] != "/drive/v1/nodes/folder-123/children" {
			t.Errorf("unexpected ls paths: %v", paths)
		}
	})

	t.Run("aggregations", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/drive/v1/search/aggregation" || req.URL.Query().Get("category") != "allPeople" {
				t.Fatalf("unexpected aggregation request: %s", req.URL.String())
			}
			return jsonHTTPResponse(200, `{"aggregations":{"allPeople":[{"value":"p1","count":12,"searchData":{"clusterName":"Alice"}}]}}`), nil
		})

		stdout, _, code, err := execCLI(t, rt, "--config", cfgPath, "--json", "aggregations", "--category", "allPeople")
		if err != nil || code != 0 {
			t.Fatalf("aggregations failed: code=%d err=%v (%s)", code, err, stdout)
		}
		var env model.Envelope
		if uErr := json.Unmarshal([]byte(stdout), &env); uErr != nil || !env.OK {
			t.Fatalf("invalid aggregations JSON: %v", uErr)
		}
	})
}

func TestAlbumsFoldersTrashFavoriteHideE2E(t *testing.T) {
	cfgPath := writeTestConfig(t)

	t.Run("albums list, create, add, remove, rename", func(t *testing.T) {
		var calls []string
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls = append(calls, req.Method+" "+req.URL.Path)
			switch {
			case req.Method == http.MethodGet && req.URL.Path == "/drive/v1/nodes":
				return jsonHTTPResponse(200, `{"count":1,"data":[{"id":"alb-1","name":"Trip","kind":"VISUAL_COLLECTION","status":"AVAILABLE"}]}`), nil
			case req.Method == http.MethodPost && req.URL.Path == "/drive/v1/nodes":
				return jsonHTTPResponse(201, `{"id":"alb-new","name":"Summer","kind":"VISUAL_COLLECTION","status":"AVAILABLE"}`), nil
			case req.Method == http.MethodPatch && req.URL.Path == "/drive/v1/nodes/alb-1/children":
				return jsonHTTPResponse(200, `{}`), nil
			case req.Method == http.MethodPatch && req.URL.Path == "/drive/v1/nodes/alb-1":
				return jsonHTTPResponse(200, `{"id":"alb-1","name":"Renamed","kind":"VISUAL_COLLECTION","status":"AVAILABLE"}`), nil
			default:
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
				return nil, nil
			}
		})

		for _, cmdArgs := range [][]string{
			{"--config", cfgPath, "--json", "albums", "list"},
			{"--config", cfgPath, "--json", "albums", "create", "Summer"},
			{"--config", cfgPath, "--json", "albums", "add", "alb-1", "node-1", "node-2"},
			{"--config", cfgPath, "--json", "albums", "remove", "alb-1", "node-1"},
			{"--config", cfgPath, "--json", "albums", "rename", "alb-1", "Renamed"},
		} {
			stdout, stderr, code, err := execCLI(t, rt, cmdArgs...)
			if err != nil || code != 0 {
				t.Fatalf("command %v failed: code=%d err=%v stderr=%s stdout=%s", cmdArgs, code, err, stderr, stdout)
			}
			var env model.Envelope
			if uErr := json.Unmarshal([]byte(stdout), &env); uErr != nil || !env.OK {
				t.Fatalf("invalid JSON for %v: %v (%s)", cmdArgs, uErr, stdout)
			}
		}
		if len(calls) != 5 {
			t.Errorf("expected 5 API calls, got %d (%v)", len(calls), calls)
		}
	})

	t.Run("folders list and create", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Method == http.MethodGet && req.URL.Path == "/drive/v1/nodes":
				if strings.Contains(req.URL.Query().Get("filters"), "isRoot:true") {
					return jsonHTTPResponse(200, `{"count":1,"data":[{"id":"root-1","ownerId":"own-1","kind":"FOLDER","isRoot":true}]}`), nil
				}
				return jsonHTTPResponse(200, `{"count":1,"data":[{"id":"fld-1","name":"Docs","kind":"FOLDER","status":"AVAILABLE"}]}`), nil
			case req.Method == http.MethodPost && req.URL.Path == "/drive/v1/nodes":
				return jsonHTTPResponse(201, `{"id":"fld-new","name":"Archive","kind":"FOLDER","status":"AVAILABLE"}`), nil
			default:
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
				return nil, nil
			}
		})

		stdout, _, code, err := execCLI(t, rt, "--config", cfgPath, "--json", "folders", "list")
		if err != nil || code != 0 {
			t.Fatalf("folders list failed: code=%d err=%v (%s)", code, err, stdout)
		}
		stdout, _, code, err = execCLI(t, rt, "--config", cfgPath, "--json", "folders", "create", "Archive")
		if err != nil || code != 0 {
			t.Fatalf("folders create failed: code=%d err=%v (%s)", code, err, stdout)
		}
	})

	t.Run("trash list, trash add requires --confirm (exit 6), trash restore", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Method == http.MethodGet && req.URL.Path == "/drive/v1/trash":
				return jsonHTTPResponse(200, `{"count":1,"data":[{"id":"tr-1","name":"old.jpg","kind":"FILE","status":"TRASH"}]}`), nil
			case req.Method == http.MethodPatch && req.URL.Path == "/drive/v1/trash":
				return jsonHTTPResponse(200, `{}`), nil
			default:
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
				return nil, nil
			}
		})

		stdout, _, code, err := execCLI(t, rt, "--config", cfgPath, "--json", "trash", "list")
		if err != nil || code != 0 {
			t.Fatalf("trash list failed: code=%d err=%v (%s)", code, err, stdout)
		}

		stdout, _, code, _ = execCLI(t, rt, "--config", cfgPath, "--json", "trash", "add", "node-1")
		if code != 6 {
			t.Fatalf("expected exit code 6 for trash add without --confirm, got %d (%s)", code, stdout)
		}
		var errEnv model.Envelope
		if uErr := json.Unmarshal([]byte(stdout), &errEnv); uErr != nil || errEnv.OK || errEnv.Error == nil || errEnv.Error.Code != model.ErrConfirmationRequired {
			t.Fatalf("expected CONFIRMATION_REQUIRED envelope, got %s", stdout)
		}

		stdout, _, code, err = execCLI(t, rt, "--config", cfgPath, "--json", "trash", "add", "--confirm", "node-1")
		if err != nil || code != 0 {
			t.Fatalf("trash add --confirm failed: code=%d err=%v (%s)", code, err, stdout)
		}

		stdout, _, code, err = execCLI(t, rt, "--config", cfgPath, "--json", "trash", "restore", "node-1")
		if err != nil || code != 0 {
			t.Fatalf("trash restore failed: code=%d err=%v (%s)", code, err, stdout)
		}
	})

	t.Run("favorite and hide with --undo", func(t *testing.T) {
		var bodies []string
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(req.Body)
			bodies = append(bodies, string(raw))
			return jsonHTTPResponse(200, `{"id":"node-1","name":"pic.jpg","kind":"FILE"}`), nil
		})

		for _, cmdArgs := range [][]string{
			{"--config", cfgPath, "--json", "favorite", "node-1"},
			{"--config", cfgPath, "--json", "favorite", "--undo", "node-1"},
			{"--config", cfgPath, "--json", "hide", "node-1"},
			{"--config", cfgPath, "--json", "hide", "--undo", "node-1"},
		} {
			_, _, code, err := execCLI(t, rt, cmdArgs...)
			if err != nil || code != 0 {
				t.Fatalf("command %v failed: code=%d err=%v", cmdArgs, code, err)
			}
		}
		if len(bodies) != 4 || !strings.Contains(bodies[0], `"favorite":true`) || !strings.Contains(bodies[1], `"favorite":false`) ||
			!strings.Contains(bodies[2], `"hidden":true`) || !strings.Contains(bodies[3], `"hidden":false`) {
			t.Errorf("unexpected settings bodies: %v", bodies)
		}
	})
}

func TestUploadAndDownloadE2E(t *testing.T) {
	cfgPath := writeTestConfig(t)
	srcDir := t.TempDir()
	fileA := filepath.Join(srcDir, "a.jpg")
	fileB := filepath.Join(srcDir, "b.jpg")
	if err := os.WriteFile(fileA, []byte("jpeg-bytes-a"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("jpeg-bytes-b"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Run("upload --dry-run makes zero HTTP requests", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Fatalf("unexpected HTTP request during --dry-run: %s %s", req.Method, req.URL.String())
			return nil, nil
		})

		stdout, stderr, code, err := execCLI(t, rt, "--config", cfgPath, "--json", "upload", "--dry-run", fileA, fileB)
		if err != nil || code != 0 {
			t.Fatalf("upload --dry-run failed: code=%d err=%v stderr=%s", code, err, stderr)
		}
		var env model.Envelope
		if uErr := json.Unmarshal([]byte(stdout), &env); uErr != nil || !env.OK {
			t.Fatalf("invalid dry-run envelope: %v (%s)", uErr, stdout)
		}
	})

	t.Run("upload with --album adds confirmed uploaded node IDs to album", func(t *testing.T) {
		var albumPatchBody string
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Method == http.MethodGet && req.URL.Path == "/drive/v1/nodes":
				return jsonHTTPResponse(200, `{"count":1,"data":[{"id":"root-1","ownerId":"own-1","kind":"FOLDER","isRoot":true}]}`), nil
			case req.Method == http.MethodPost && req.URL.Path == "/cdproxy/nodes":
				name := req.URL.Query().Get("name")
				return jsonHTTPResponse(201, `{"id":"up-`+name+`","name":"`+name+`","kind":"FILE","status":"AVAILABLE"}`), nil
			case req.Method == http.MethodPatch && req.URL.Path == "/drive/v1/nodes/alb-99/children":
				raw, _ := io.ReadAll(req.Body)
				albumPatchBody = string(raw)
				return jsonHTTPResponse(200, `{}`), nil
			default:
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		})

		stdout, stderr, code, err := execCLI(t, rt, "--config", cfgPath, "--json", "upload", "--album", "alb-99", fileA)
		if err != nil || code != 0 {
			t.Fatalf("upload --album failed: code=%d err=%v stderr=%s stdout=%s", code, err, stderr, stdout)
		}
		if !strings.Contains(albumPatchBody, `"up-a.jpg"`) {
			t.Errorf("expected album patch to include uploaded node ID up-a.jpg, got %s", albumPatchBody)
		}
	})

	t.Run("upload partial failure with --continue-on-error returns exit code 5", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Method == http.MethodGet && req.URL.Path == "/drive/v1/nodes":
				return jsonHTTPResponse(200, `{"count":1,"data":[{"id":"root-1","ownerId":"own-1","kind":"FOLDER","isRoot":true}]}`), nil
			case req.Method == http.MethodPost && req.URL.Path == "/cdproxy/nodes":
				name := req.URL.Query().Get("name")
				if name == "a.jpg" {
					return jsonHTTPResponse(201, `{"id":"up-a","name":"a.jpg","kind":"FILE","status":"AVAILABLE"}`), nil
				}
				return jsonHTTPResponse(409, `{"message":"Node already exists"}`), nil
			default:
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		})

		stdout, _, code, _ := execCLI(t, rt, "--config", cfgPath, "--json", "upload", "--on-conflict", "fail", "--continue-on-error", fileA, fileB)
		if code != 5 {
			t.Fatalf("expected exit code 5 for partial upload failure, got %d (%s)", code, stdout)
		}
		var env model.Envelope
		if uErr := json.Unmarshal([]byte(stdout), &env); uErr != nil || env.OK || env.Error == nil || env.Error.Code != model.ErrPartialSuccess {
			t.Fatalf("expected PARTIAL_SUCCESS envelope, got %s (err=%v)", stdout, uErr)
		}
	})

	t.Run("download single and partial failure", func(t *testing.T) {
		outDir := t.TempDir()
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Method == http.MethodGet && req.URL.Path == "/drive/v1/nodes":
				return jsonHTTPResponse(200, `{"count":1,"data":[{"id":"root-1","ownerId":"own-1","kind":"FOLDER","isRoot":true}]}`), nil
			case req.Method == http.MethodGet && req.URL.Path == "/drive/v1/nodes/node-ok/contentRedirection":
				h := make(http.Header)
				h.Set("Content-Disposition", `attachment; filename="downloaded.jpg"`)
				h.Set("Content-Length", "10")
				return &http.Response{
					StatusCode: 200,
					Header:     h,
					Body:       io.NopCloser(strings.NewReader("0123456789")),
				}, nil
			case req.Method == http.MethodGet && req.URL.Path == "/drive/v1/nodes/node-missing/contentRedirection":
				return jsonHTTPResponse(404, `{"message":"Node not found"}`), nil
			default:
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		})

		stdout, stderr, code, err := execCLI(t, rt, "--config", cfgPath, "--json", "download", "--out", outDir, "node-ok")
		if err != nil || code != 0 {
			t.Fatalf("download failed: code=%d err=%v stderr=%s stdout=%s", code, err, stderr, stdout)
		}
		gotBytes, rErr := os.ReadFile(filepath.Join(outDir, "downloaded.jpg"))
		if rErr != nil || string(gotBytes) != "0123456789" {
			t.Fatalf("downloaded file mismatch: err=%v content=%q", rErr, string(gotBytes))
		}

		stdout, _, code, _ = execCLI(t, rt, "--config", cfgPath, "--json", "download", "--out", t.TempDir(), "--continue-on-error", "node-ok", "node-missing")
		if code != 5 {
			t.Fatalf("expected exit code 5 for partial download failure, got %d (%s)", code, stdout)
		}
	})
}

func TestUploadConflictPoliciesSkipAndRenameE2E(t *testing.T) {
	cfgPath := writeTestConfig(t)
	srcDir := t.TempDir()
	fileA := filepath.Join(srcDir, "dup.jpg")
	if err := os.WriteFile(fileA, []byte("same-bytes"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Run("--on-conflict skip resolves matching remote node", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Method == http.MethodGet && req.URL.Path == "/drive/v1/nodes":
				f := req.URL.Query().Get("filters")
				if strings.Contains(f, "isRoot:true") {
					return jsonHTTPResponse(200, `{"count":1,"data":[{"id":"root-1","ownerId":"own-1","kind":"FOLDER","isRoot":true}]}`), nil
				}
				return jsonHTTPResponse(200, `{"count":1,"data":[{"id":"existing-node-1","name":"dup.jpg","kind":"FILE","status":"AVAILABLE","contentProperties":{"size":10,"md5":"9c3c31d4cf9b117761f89598d702418a"}}]}`), nil
			case req.Method == http.MethodPost && req.URL.Path == "/cdproxy/nodes":
				return jsonHTTPResponse(409, `{"message":"Node already exists"}`), nil
			default:
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		})

		stdout, stderr, code, err := execCLI(t, rt, "--config", cfgPath, "--json", "upload", "--on-conflict", "skip", fileA)
		if err != nil || code != 0 {
			t.Fatalf("upload --on-conflict skip failed: code=%d err=%v stderr=%s stdout=%s", code, err, stderr, stdout)
		}
		if !strings.Contains(stdout, `"skipped":1`) {
			t.Errorf("expected skipped:1 in upload summary, got %s", stdout)
		}
	})

	t.Run("--on-conflict rename retries with disambiguated filename", func(t *testing.T) {
		var uploadedNames []string
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Method == http.MethodGet && req.URL.Path == "/drive/v1/nodes":
				return jsonHTTPResponse(200, `{"count":1,"data":[{"id":"root-1","ownerId":"own-1","kind":"FOLDER","isRoot":true}]}`), nil
			case req.Method == http.MethodPost && req.URL.Path == "/cdproxy/nodes":
				name := req.URL.Query().Get("name")
				uploadedNames = append(uploadedNames, name)
				if name == "dup.jpg" {
					return jsonHTTPResponse(409, `{"message":"Node already exists"}`), nil
				}
				return jsonHTTPResponse(201, `{"id":"renamed-node-1","name":"`+name+`","kind":"FILE","status":"AVAILABLE"}`), nil
			default:
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		})

		stdout, stderr, code, err := execCLI(t, rt, "--config", cfgPath, "--json", "upload", "--on-conflict", "rename", fileA)
		if err != nil || code != 0 {
			t.Fatalf("upload --on-conflict rename failed: code=%d err=%v stderr=%s stdout=%s", code, err, stderr, stdout)
		}
		if len(uploadedNames) < 2 || uploadedNames[0] != "dup.jpg" || uploadedNames[1] != "dup (1).jpg" {
			t.Errorf("unexpected uploaded names: %v", uploadedNames)
		}
	})
}

func TestDocumentationAndReleaseContractsSync(t *testing.T) {
	repoRoot := filepath.Join("..", "..")

	readRepoFile := func(t *testing.T, rel string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatalf("failed to read %s: %v", rel, err)
		}
		return string(data)
	}

	t.Run("COMMANDS.md documents every command and flag and contentDate vs createdDate", func(t *testing.T) {
		commandsDoc := readRepoFile(t, "COMMANDS.md")

		if !strings.Contains(commandsDoc, "contentDate") || !strings.Contains(commandsDoc, "createdDate") {
			t.Errorf("COMMANDS.md must document contentDate (capture date) vs createdDate (upload date)")
		}

		root := NewRootCommand(RootOptions{})
		var walk func(cmd *cobra.Command, prefix string)
		walk = func(cmd *cobra.Command, prefix string) {
			fullPath := strings.TrimSpace(prefix + " " + cmd.Name())
			if cmd != root {
				if !strings.Contains(commandsDoc, fullPath) {
					t.Errorf("COMMANDS.md missing command %q", fullPath)
				}
			}
			cmd.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
				if f.Hidden || f.Name == "help" || f.Name == "version" {
					return
				}
				flagToken := "--" + f.Name
				if !strings.Contains(commandsDoc, flagToken) {
					t.Errorf("COMMANDS.md missing flag %q for command %q", flagToken, fullPath)
				}
			})
			cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
				if f.Hidden || f.Name == "help" || f.Name == "version" {
					return
				}
				flagToken := "--" + f.Name
				if !strings.Contains(commandsDoc, flagToken) {
					t.Errorf("COMMANDS.md missing persistent flag %q", flagToken)
				}
			})
			for _, sub := range cmd.Commands() {
				if sub.Hidden || sub.Name() == "help" {
					continue
				}
				walk(sub, fullPath)
			}
		}
		walk(root, "")
	})

	t.Run("JSON_SCHEMA.md documents envelope fields, error codes, categories, and exit codes", func(t *testing.T) {
		schemaDoc := readRepoFile(t, "JSON_SCHEMA.md")

		for _, field := range []string{`"ok"`, `"data"`, `"error"`, `"meta"`, `"schema_version"`, `"duration_ms"`, `"request_id"`} {
			if !strings.Contains(schemaDoc, field) {
				t.Errorf("JSON_SCHEMA.md missing envelope field %s", field)
			}
		}

		allCodes := []string{
			model.ErrValidationFailed,
			model.ErrInvalidFilter,
			model.ErrAuthRequired,
			model.ErrAuthFailed,
			model.ErrPermissionDenied,
			model.ErrAPIError,
			model.ErrRemoteProtocolError,
			model.ErrResourceNotFound,
			model.ErrConflict,
			model.ErrNetworkError,
			model.ErrRateLimited,
			model.ErrPartialSuccess,
			model.ErrConfirmationRequired,
			model.ErrReadOnlyViolation,
			model.ErrFilesystemError,
			model.ErrConfigError,
			model.ErrInterrupted,
		}
		for _, code := range allCodes {
			if !strings.Contains(schemaDoc, code) {
				t.Errorf("JSON_SCHEMA.md missing error code %q", code)
			}
			cat := model.CategoryForCode(code)
			if !strings.Contains(schemaDoc, cat) {
				t.Errorf("JSON_SCHEMA.md missing category %q for code %q", cat, code)
			}
		}
		for _, exitStr := range []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "130"} {
			if !strings.Contains(schemaDoc, exitStr) {
				t.Errorf("JSON_SCHEMA.md missing exit code %s", exitStr)
			}
		}
	})

	t.Run("README.md contains disclaimer, regions, SDK/CLI usage, exit codes, credits, and MIT license", func(t *testing.T) {
		readme := readRepoFile(t, "README.md")

		requiredSnippets := []string{
			"Unofficial client; not affiliated with, endorsed by, or sponsored by Amazon.com, Inc.",
			"verified",
			"experimental",
			"content-na.drive.amazonaws.com",
			"content-eu.drive.amazonaws.com",
			"content-jp.drive.amazonaws.com",
			"go get github.com/thedavidweng/amazon-photos/pkg/aphotos",
			"auth import",
			"doctor",
			"usage",
			"search",
			"upload",
			"download",
			"albums",
			"trash",
			"130",
			"trevorhobenshield/amazon_photos",
			"kpfitzgerald/amazon-photos-mcp",
			"vanessa/immich-amazon-sync",
			"MIT",
		}
		for _, snippet := range requiredSnippets {
			if !strings.Contains(readme, snippet) {
				t.Errorf("README.md missing required content %q", snippet)
			}
		}
	})

	t.Run("governance and release files exist and are valid YAML", func(t *testing.T) {
		for _, docFile := range []string{"CHANGELOG.md", "CONTRIBUTING.md", "SECURITY.md", "CONTEXT.md", "GLOSSARY.md"} {
			content := readRepoFile(t, docFile)
			if strings.TrimSpace(content) == "" {
				t.Errorf("%s is empty", docFile)
			}
		}

		goreleaserRaw := readRepoFile(t, ".goreleaser.yaml")
		var gr map[string]any
		if err := yaml.Unmarshal([]byte(goreleaserRaw), &gr); err != nil {
			t.Fatalf(".goreleaser.yaml invalid YAML: %v", err)
		}
		for _, token := range []string{
			"./cmd/aphotos",
			"aphotos",
			"github.com/thedavidweng/amazon-photos/internal/cli.Version",
			"github.com/thedavidweng/amazon-photos/internal/cli.Commit",
			"github.com/thedavidweng/amazon-photos/internal/cli.Date",
			"linux",
			"darwin",
			"windows",
			"amd64",
			"arm64",
			"checksums.txt",
			"sha256",
			"homebrew-tap",
		} {
			if !strings.Contains(goreleaserRaw, token) {
				t.Errorf(".goreleaser.yaml missing %q", token)
			}
		}

		ciRaw := readRepoFile(t, ".github/workflows/ci.yml")
		var ciMap map[string]any
		if err := yaml.Unmarshal([]byte(ciRaw), &ciMap); err != nil {
			t.Fatalf(".github/workflows/ci.yml invalid YAML: %v", err)
		}
		for _, token := range []string{"mise run check", "ubuntu-latest", "macos-latest", "windows-latest", "go test ./...", "go build ./cmd/aphotos"} {
			if !strings.Contains(ciRaw, token) {
				t.Errorf(".github/workflows/ci.yml missing %q", token)
			}
		}

		relRaw := readRepoFile(t, ".github/workflows/release.yml")
		var relMap map[string]any
		if err := yaml.Unmarshal([]byte(relRaw), &relMap); err != nil {
			t.Fatalf(".github/workflows/release.yml invalid YAML: %v", err)
		}
		for _, token := range []string{"v*", "goreleaser"} {
			if !strings.Contains(relRaw, token) {
				t.Errorf(".github/workflows/release.yml missing %q", token)
			}
		}
	})
}
