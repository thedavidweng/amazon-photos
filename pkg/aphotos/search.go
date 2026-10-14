package aphotos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type SearchOptions struct {
	Filter             *FilterBuilder
	Filters            string
	Limit              int
	Offset             int
	Sort               SortOrder
	SearchContext      string
	GroupByForTime     string
	DisableLowResThumb bool
}

type NodeQueryOptions struct {
	Filter              *FilterBuilder
	Filters             string
	Limit               int
	Offset              int
	Sort                SortOrder
	TimeWindowSubdivide bool
	TimeWindowField     string
	WindowStart         time.Time
	WindowEnd           time.Time
}

type ChildrenOptions struct {
	Filter      *FilterBuilder
	Filters     string
	FoldersOnly bool
	Limit       int
	Offset      int
	Sort        SortOrder
}

type searchResponse struct {
	Count        int             `json:"count"`
	Data         []Node          `json:"data"`
	Aggregations json.RawMessage `json:"aggregations,omitempty"`
}

func (c *Client) Search(ctx context.Context, opts SearchOptions) (*Page[Node], error) {
	offset, limit, err := normalizeOffsetAndLimit(opts.Offset, opts.Limit)
	if err != nil {
		return nil, err
	}

	filterExpr, err := resolveFilterExpr(opts.Filter, opts.Filters, fmt.Sprintf("type:(%s)", string(MediaPhotosAndVideos)))
	if err != nil {
		return nil, err
	}

	searchCtx := strings.TrimSpace(opts.SearchContext)
	if searchCtx == "" {
		searchCtx = "customer"
	}

	params := baseQueryParams()
	params.Set("filters", filterExpr)
	params.Set("limit", strconv.Itoa(limit))
	params.Set("offset", strconv.Itoa(offset))
	if !opts.DisableLowResThumb {
		params.Set("lowResThumbnail", "true")
	} else {
		params.Set("lowResThumbnail", "false")
	}
	params.Set("searchContext", searchCtx)
	if strings.TrimSpace(string(opts.Sort)) != "" {
		params.Set("sort", strings.TrimSpace(string(opts.Sort)))
	}
	if strings.TrimSpace(opts.GroupByForTime) != "" {
		params.Set("groupByForTime", strings.TrimSpace(opts.GroupByForTime))
	}

	var resp searchResponse
	if err := c.doReadJSON(ctx, "/search", params, &resp); err != nil {
		return nil, attachFilterToError(err, filterExpr)
	}

	aggs, aggErr := parseAggregationsField(resp.Aggregations, string(AggAll))
	if aggErr != nil {
		return nil, aggErr
	}

	return buildSinglePage(resp.Data, resp.Count, offset, limit, aggs), nil
}

func (c *Client) SearchAll(ctx context.Context, opts SearchOptions) (*Page[Node], error) {
	offset, limit, err := normalizeOffsetAndLimit(opts.Offset, opts.Limit)
	if err != nil {
		return nil, err
	}
	return paginateUnbounded(ctx, "/search", offset, limit, func(ctx context.Context, pageOffset, pageLimit int) (*Page[Node], error) {
		reqOpts := opts
		reqOpts.Offset = pageOffset
		reqOpts.Limit = pageLimit
		return c.Search(ctx, reqOpts)
	})
}

func (c *Client) SearchIter(ctx context.Context, opts SearchOptions) iter.Seq2[Node, error] {
	return func(yield func(Node, error) bool) {
		offset, limit, err := normalizeOffsetAndLimit(opts.Offset, opts.Limit)
		if err != nil {
			yield(Node{}, err)
			return
		}
		seen := make(map[string]struct{})
		currentOffset := offset
		for {
			reqOpts := opts
			reqOpts.Offset = currentOffset
			reqOpts.Limit = limit
			p, pErr := c.Search(ctx, reqOpts)
			if pErr != nil {
				yield(Node{}, pErr)
				return
			}
			if len(p.Items) == 0 {
				return
			}
			for i := range p.Items {
				id := p.Items[i].ID
				if id != "" {
					if _, exists := seen[id]; exists {
						continue
					}
					seen[id] = struct{}{}
				}
				if !yield(p.Items[i], nil) {
					return
				}
			}
			if !p.HasMore {
				return
			}
			currentOffset += limit
			if p.TotalCount > 0 && currentOffset >= p.TotalCount {
				return
			}
		}
	}
}

func (c *Client) QueryNodes(ctx context.Context, opts NodeQueryOptions) (*Page[Node], error) {
	offset, limit, err := normalizeOffsetAndLimit(opts.Offset, opts.Limit)
	if err != nil {
		return nil, err
	}
	if offset+limit > MaxNodeOffsetWindow {
		return nil, &InvalidFilterError{
			Endpoint: "/nodes",
			Message:  fmt.Sprintf("offset (%d) + limit (%d) = %d exceeds /nodes maximum window %d", offset, limit, offset+limit, MaxNodeOffsetWindow),
		}
	}

	filterExpr, err := resolveFilterExpr(opts.Filter, opts.Filters, "")
	if err != nil {
		return nil, err
	}

	params := baseQueryParams()
	if filterExpr != "" {
		params.Set("filters", filterExpr)
	}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("offset", strconv.Itoa(offset))
	if strings.TrimSpace(string(opts.Sort)) != "" {
		params.Set("sort", strings.TrimSpace(string(opts.Sort)))
	}

	var resp nodesListResponse
	if err := c.doReadJSON(ctx, "/nodes", params, &resp); err != nil {
		return nil, attachFilterToError(err, filterExpr)
	}

	return buildSinglePage(resp.Data, resp.Count, offset, limit, nil), nil
}

func (c *Client) QueryAllNodes(ctx context.Context, opts NodeQueryOptions) (*Page[Node], error) {
	offset, limit, err := normalizeOffsetAndLimit(opts.Offset, opts.Limit)
	if err != nil {
		return nil, err
	}
	filterExpr, err := resolveFilterExpr(opts.Filter, opts.Filters, "")
	if err != nil {
		return nil, err
	}

	if opts.TimeWindowSubdivide {
		return subdivideByTimeWindows(
			ctx,
			"/nodes",
			filterExpr,
			strings.TrimSpace(opts.TimeWindowField),
			opts.WindowStart,
			opts.WindowEnd,
			limit,
			func(ctx context.Context, subFilter string, pageOffset, pageLimit int) (*Page[Node], error) {
				return c.QueryNodes(ctx, NodeQueryOptions{
					Filters: subFilter,
					Offset:  pageOffset,
					Limit:   pageLimit,
					Sort:    opts.Sort,
				})
			},
		)
	}

	return paginateBounded9999(ctx, "/nodes", offset, limit, func(ctx context.Context, pageOffset, pageLimit int) (*Page[Node], error) {
		return c.QueryNodes(ctx, NodeQueryOptions{
			Filters: filterExpr,
			Offset:  pageOffset,
			Limit:   pageLimit,
			Sort:    opts.Sort,
		})
	})
}

func (c *Client) ListChildren(ctx context.Context, parentID string, opts ChildrenOptions) (*Page[Node], error) {
	cleanParentID := strings.TrimSpace(parentID)
	if cleanParentID == "" {
		return nil, &InvalidFilterError{
			Endpoint: "/nodes/{id}/children",
			Message:  "parent node ID must not be empty",
		}
	}

	offset, limit, err := normalizeOffsetAndLimit(opts.Offset, opts.Limit)
	if err != nil {
		return nil, err
	}

	defaultFilter := ""
	if opts.FoldersOnly {
		defaultFilter = "kind:FOLDER"
	}
	filterExpr, err := resolveFilterExpr(opts.Filter, opts.Filters, defaultFilter)
	if err != nil {
		return nil, err
	}

	params := baseQueryParams()
	if filterExpr != "" {
		params.Set("filters", filterExpr)
	}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("offset", strconv.Itoa(offset))
	if strings.TrimSpace(string(opts.Sort)) != "" {
		params.Set("sort", strings.TrimSpace(string(opts.Sort)))
	}

	endpoint := fmt.Sprintf("/nodes/%s/children", url.PathEscape(cleanParentID))
	var resp nodesListResponse
	if err := c.doReadJSON(ctx, endpoint, params, &resp); err != nil {
		return nil, attachFilterToError(err, filterExpr)
	}

	return buildSinglePage(resp.Data, resp.Count, offset, limit, nil), nil
}

func (c *Client) ListAllChildren(ctx context.Context, parentID string, opts ChildrenOptions) (*Page[Node], error) {
	cleanParentID := strings.TrimSpace(parentID)
	if cleanParentID == "" {
		return nil, &InvalidFilterError{
			Endpoint: "/nodes/{id}/children",
			Message:  "parent node ID must not be empty",
		}
	}
	offset, limit, err := normalizeOffsetAndLimit(opts.Offset, opts.Limit)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("/nodes/%s/children", url.PathEscape(cleanParentID))
	return paginateUnbounded(ctx, endpoint, offset, limit, func(ctx context.Context, pageOffset, pageLimit int) (*Page[Node], error) {
		reqOpts := opts
		reqOpts.Offset = pageOffset
		reqOpts.Limit = pageLimit
		return c.ListChildren(ctx, cleanParentID, reqOpts)
	})
}

func resolveFilterExpr(fb *FilterBuilder, rawFilter, defaultFilter string) (string, error) {
	var built string
	if fb != nil {
		var err error
		built, err = fb.Build()
		if err != nil {
			return "", err
		}
	}
	raw := strings.TrimSpace(rawFilter)
	if raw != "" {
		if hasControlChars(raw) {
			return "", &InvalidFilterError{
				Filter:  raw,
				Message: "filter contains invalid control characters",
			}
		}
		if built != "" {
			return fmt.Sprintf("(%s) AND (%s)", built, raw), nil
		}
		return raw, nil
	}
	if built != "" {
		return built, nil
	}
	return defaultFilter, nil
}

func attachFilterToError(err error, filterExpr string) error {
	var inv *InvalidFilterError
	if errors.As(err, &inv) && inv.Filter == "" {
		inv.Filter = filterExpr
	}
	return err
}
