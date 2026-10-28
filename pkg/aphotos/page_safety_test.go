package aphotos_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

func TestNodePagesRequireExplicitListEnvelope(t *testing.T) {
	for name, fetch := range map[string]func(*aphotos.Client) (*aphotos.Page[aphotos.Node], error){
		"QueryNodes": func(c *aphotos.Client) (*aphotos.Page[aphotos.Node], error) {
			return c.QueryNodes(context.Background(), aphotos.NodeQueryOptions{})
		},
		"Search": func(c *aphotos.Client) (*aphotos.Page[aphotos.Node], error) {
			return c.Search(context.Background(), aphotos.SearchOptions{})
		},
		"ListChildren": func(c *aphotos.Client) (*aphotos.Page[aphotos.Node], error) {
			return c.ListChildren(context.Background(), "parent", aphotos.ChildrenOptions{})
		},
		"QueryAllNodes": func(c *aphotos.Client) (*aphotos.Page[aphotos.Node], error) {
			return c.QueryAllNodes(context.Background(), aphotos.NodeQueryOptions{})
		},
		"SearchAll": func(c *aphotos.Client) (*aphotos.Page[aphotos.Node], error) {
			return c.SearchAll(context.Background(), aphotos.SearchOptions{})
		},
		"ListAllChildren": func(c *aphotos.Client) (*aphotos.Page[aphotos.Node], error) {
			return c.ListAllChildren(context.Background(), "parent", aphotos.ChildrenOptions{})
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name string
				body string
				id   string
				bad  bool
			}{
				{name: "missing envelope", body: `{}`, bad: true},
				{name: "missing data", body: `{"count":0}`, bad: true},
				{name: "null data", body: `{"count":0,"data":null}`, bad: true},
				{name: "missing count", body: `{"data":[]}`, bad: true},
				{name: "null count", body: `{"count":null,"data":[]}`, bad: true},
				{name: "negative count", body: `{"count":-1,"data":[]}`, bad: true},
				{name: "zero count with nodes", body: `{"count":0,"data":[{"id":"a"}]}`, bad: true},
				{name: "missing node ID", body: `{"count":1,"data":[{}]}`, bad: true},
				{name: "null node", body: `{"count":1,"data":[null]}`, bad: true},
				{name: "blank node ID", body: `{"count":1,"data":[{"id":" "}]}`, bad: true},
				{name: "empty before advertised end", body: `{"count":1,"data":[]}`, bad: true},
				{name: "explicit zero", body: `{"count":0,"data":[]}`},
				{name: "nested node ID", body: `{"count":1,"data":[{"info":{"nodeId":"nested-node"}}]}`, id: "nested-node"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					client := newTestUSClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(tc.body)),
							Request:    req,
						}, nil
					}), 0)
					page, err := fetch(client)
					if tc.bad {
						var protocolErr *aphotos.RemoteProtocolError
						if !errors.As(err, &protocolErr) || page != nil {
							t.Fatalf("expected RemoteProtocolError and no page, got page=%+v err=%v", page, err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if tc.id != "" {
						if len(page.Items) != 1 || page.Items[0].ID != tc.id {
							t.Fatalf("expected nested node %q, got %+v", tc.id, page)
						}
					} else if page.Items == nil || len(page.Items) != 0 || page.TotalCount != 0 || page.HasMore || !page.Complete {
						t.Fatalf("explicit zero must be a complete empty page: %+v", page)
					}
				})
			}
		})
	}
}

func TestAllNodePagesRejectCountTruncation(t *testing.T) {
	for name, fetch := range map[string]func(*aphotos.Client) (*aphotos.Page[aphotos.Node], error){
		"QueryAllNodes": func(c *aphotos.Client) (*aphotos.Page[aphotos.Node], error) {
			return c.QueryAllNodes(context.Background(), aphotos.NodeQueryOptions{Limit: 2})
		},
		"SearchAll": func(c *aphotos.Client) (*aphotos.Page[aphotos.Node], error) {
			return c.SearchAll(context.Background(), aphotos.SearchOptions{Limit: 2})
		},
		"ListAllChildren": func(c *aphotos.Client) (*aphotos.Page[aphotos.Node], error) {
			return c.ListAllChildren(context.Background(), "parent", aphotos.ChildrenOptions{Limit: 2})
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				first  string
				second string
				items  int
				total  int
			}{
				{name: "initial empty", first: `{"count":3,"data":[]}`, items: 0, total: 3},
				{name: "premature empty", first: `{"count":3,"data":[{"id":"a"},{"id":"b"}]}`, second: `{"count":3,"data":[]}`, items: 2, total: 3},
				{name: "count shrinks to zero", first: `{"count":4,"data":[{"id":"a"},{"id":"b"}]}`, second: `{"count":0,"data":[]}`, items: 2, total: 4},
				{name: "malformed later envelope", first: `{"count":4,"data":[{"id":"a"},{"id":"b"}]}`, second: `{}`, items: 2, total: 4},
				{name: "short final page", first: `{"count":4,"data":[{"id":"a"},{"id":"b"}]}`, second: `{"count":4,"data":[{"id":"c"}]}`, items: 3, total: 4},
				{name: "duplicate final page", first: `{"count":4,"data":[{"id":"a"},{"id":"b"}]}`, second: `{"count":4,"data":[{"id":"a"},{"id":"b"}]}`, items: 2, total: 4},
				{name: "nonprogressing page", first: `{"count":6,"data":[{"id":"a"},{"id":"b"}]}`, second: `{"count":6,"data":[{"id":"a"},{"id":"b"}]}`, items: 2, total: 6},
			} {
				t.Run(tc.name, func(t *testing.T) {
					client := newTestUSClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
						body := tc.first
						offset := req.URL.Query().Get("offset")
						if offset != "0" && offset != "2" {
							t.Fatalf("pagination continued after a page with no new nodes: offset=%s", offset)
						}
						if offset != "0" {
							body = tc.second
						}
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(body)),
							Request:    req,
						}, nil
					}), 0)
					page, err := fetch(client)
					if tc.items == 0 {
						var protocolErr *aphotos.RemoteProtocolError
						if !errors.As(err, &protocolErr) || page != nil {
							t.Fatalf("expected RemoteProtocolError for initial truncated page, got page=%+v err=%v", page, err)
						}
						return
					}
					if _, ok := errors.AsType[*aphotos.PartialResultError](err); !ok {
						t.Fatalf("expected PartialResultError, got page=%+v err=%v", page, err)
					}
					if page == nil || page.Complete || !page.HasMore || len(page.Items) != tc.items || page.TotalCount != tc.total {
						t.Fatalf("expected %d partial nodes of %d, got %+v", tc.items, tc.total, page)
					}
				})
			}
		})
	}
}

func TestSearchAllCompletesFromNonzeroOffset(t *testing.T) {
	client := newTestUSClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"count":3,"data":[{"id":"c"}]}`)),
			Request:    req,
		}, nil
	}), 0)
	page, err := client.SearchAll(context.Background(), aphotos.SearchOptions{Offset: 2, Limit: 2})
	if err != nil || page == nil || !page.Complete || page.HasMore || len(page.Items) != 1 || page.Items[0].ID != "c" {
		t.Fatalf("expected complete remaining range, got page=%+v err=%v", page, err)
	}
}

func TestNodeReadsRejectNonSuccessStatus(t *testing.T) {
	for _, status := range []int{199, http.StatusMultipleChoices, http.StatusFound, http.StatusNotModified} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := newTestUSClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Body:       io.NopCloser(strings.NewReader(`{"count":0,"data":[]}`)),
					Request:    req,
				}, nil
			}), 0)
			page, err := client.QueryNodes(context.Background(), aphotos.NodeQueryOptions{})
			var protocolErr *aphotos.RemoteProtocolError
			if !errors.As(err, &protocolErr) || protocolErr.StatusCode != status || page != nil {
				t.Fatalf("expected RemoteProtocolError for HTTP %d, got page=%+v err=%v", status, page, err)
			}
		})
	}
}
