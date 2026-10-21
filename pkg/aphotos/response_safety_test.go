package aphotos_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

func TestMutationsRequireSuccessStatus(t *testing.T) {
	ctx := context.Background()
	for name, mutate := range map[string]func(*aphotos.Client) error{
		"AddAlbumMembers": func(c *aphotos.Client) error {
			return c.AddAlbumMembers(ctx, "album", []string{"node"})
		},
		"RemoveAlbumMembers": func(c *aphotos.Client) error {
			return c.RemoveAlbumMembers(ctx, "album", []string{"node"})
		},
		"CreateAlbum": func(c *aphotos.Client) error {
			_, err := c.CreateAlbum(ctx, "album")
			return err
		},
		"CreateFolder": func(c *aphotos.Client) error {
			_, err := c.CreateFolder(ctx, "folder", "parent")
			return err
		},
		"RenameNode": func(c *aphotos.Client) error {
			_, err := c.RenameNode(ctx, "node", "renamed")
			return err
		},
		"SetFavorite": func(c *aphotos.Client) error {
			_, err := c.SetFavorite(ctx, []string{"node"}, true)
			return err
		},
		"SetHidden": func(c *aphotos.Client) error {
			_, err := c.SetHidden(ctx, []string{"node"}, true)
			return err
		},
		"MoveToTrash": func(c *aphotos.Client) error {
			return c.MoveToTrash(ctx, []string{"node"})
		},
		"RestoreFromTrash": func(c *aphotos.Client) error {
			return c.RestoreFromTrash(ctx, []string{"node"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, status := range []int{199, 200, 201, 299, 300, 302, 304} {
				t.Run(fmt.Sprint(status), func(t *testing.T) {
					client := newTestClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
						return jsonResponse(req, status, `{"id":"node"}`), nil
					}))
					err := mutate(client)
					if status >= 200 && status < 300 {
						if err != nil {
							t.Fatalf("HTTP %d must succeed: %v", status, err)
						}
						return
					}
					var protocolErr *aphotos.RemoteProtocolError
					if !errors.As(err, &protocolErr) || protocolErr.StatusCode != status {
						t.Fatalf("expected RemoteProtocolError for HTTP %d, got %v", status, err)
					}
				})
			}
		})
	}
}

func TestUploadRequiresSuccessStatus(t *testing.T) {
	for _, status := range []int{199, 200, 201, 299, 300, 302, 304} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			attempts := 0
			client := newTestClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
				attempts++
				return jsonResponse(req, status, `{"id":"node"}`), nil
			}))
			result, err := client.Upload(context.Background(), aphotos.UploadRequest{
				Name: "photo.jpg",
				Body: strings.NewReader("payload"),
			})
			if attempts != 1 {
				t.Fatalf("upload made %d attempts, want one", attempts)
			}
			if status >= 200 && status < 300 {
				if err != nil || result == nil || result.Status != aphotos.TransferConfirmed || result.NodeID != "node" {
					t.Fatalf("expected confirmed upload for HTTP %d, got result=%+v err=%v", status, result, err)
				}
				return
			}
			var protocolErr *aphotos.RemoteProtocolError
			if !errors.As(err, &protocolErr) || protocolErr.StatusCode != status {
				t.Fatalf("expected RemoteProtocolError for HTTP %d, got result=%+v err=%v", status, result, err)
			}
			if result == nil || result.Status != aphotos.TransferFailed || result.NodeID != "" || result.Error != err {
				t.Fatalf("expected failed upload preserving error, got %+v", result)
			}
		})
	}
}

func TestServiceUnavailablePreservesRetryAfter(t *testing.T) {
	ctx := context.Background()
	for name, request := range map[string]func(*aphotos.Client) error{
		"QueryNodes": func(c *aphotos.Client) error {
			_, err := c.QueryNodes(ctx, aphotos.NodeQueryOptions{})
			return err
		},
		"AddAlbumMembers": func(c *aphotos.Client) error {
			return c.AddAlbumMembers(ctx, "album", []string{"node"})
		},
		"Upload": func(c *aphotos.Client) error {
			result, err := c.Upload(ctx, aphotos.UploadRequest{Name: "photo.jpg", Body: strings.NewReader("payload")})
			if result == nil || result.Status != aphotos.TransferFailed || result.Error != err {
				t.Fatalf("expected failed upload preserving error, got result=%+v err=%v", result, err)
			}
			return err
		},
		"Download": func(c *aphotos.Client) error {
			_, err := c.Download(ctx, aphotos.DownloadRequest{NodeID: "node", OwnerID: "owner", DestDir: t.TempDir()})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				header string
				min    time.Duration
				max    time.Duration
			}{
				{name: "seconds above retry cap", header: "120", min: 120 * time.Second, max: 120 * time.Second},
				{name: "HTTP date", header: time.Now().Add(5 * time.Minute).UTC().Format(http.TimeFormat), min: 4 * time.Minute, max: 5 * time.Minute},
				{name: "absent"},
				{name: "invalid", header: "later"},
				{name: "zero", header: "0"},
				{name: "expired", header: "Wed, 21 Oct 2015 07:28:00 GMT"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					for _, brokenBody := range []bool{false, true} {
						t.Run(fmt.Sprintf("brokenBody=%t", brokenBody), func(t *testing.T) {
							attempts := 0
							client := newTestClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
								attempts++
								resp := jsonResponse(req, http.StatusServiceUnavailable, `{"message":"unavailable"}`)
								resp.Header.Set("Retry-After", tc.header)
								resp.Header.Set("X-Amzn-Requestid", "req-503")
								if brokenBody {
									resp.Body = io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF))
								}
								return resp, nil
							}))
							err := request(client)
							var transientErr *aphotos.TransientNetworkError
							if !errors.As(err, &transientErr) || transientErr.StatusCode != 503 || transientErr.RequestID != "req-503" {
								t.Fatalf("expected TransientNetworkError preserving response metadata, got %v", err)
							}
							if transientErr.RetryAfter < tc.min || transientErr.RetryAfter > tc.max {
								t.Fatalf("RetryAfter = %s, want %s..%s", transientErr.RetryAfter, tc.min, tc.max)
							}
							if attempts != 1 {
								t.Fatalf("made %d attempts with retries disabled, want one", attempts)
							}
						})
					}
				})
			}
		})
	}
}
