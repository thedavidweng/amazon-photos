package aphotos

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

const (
	MaxTrashBatchSize  = 50
	defaultTrashFilter = "kind:(FILE* OR FOLDER*) AND status:(TRASH*)"
	defaultTrashSort   = "['modifiedDate DESC']"
)

type TrashListOptions struct {
	Limit  int
	Offset int
	Sort   string
	Filter string
}

type moveToTrashPayload struct {
	Recurse            string   `json:"recurse"`
	Op                 string   `json:"op"`
	Filters            string   `json:"filters"`
	ConflictResolution string   `json:"conflictResolution"`
	Value              []string `json:"value"`
	ResourceVersion    string   `json:"resourceVersion"`
	ContentType        string   `json:"ContentType"`
}

type restoreFromTrashPayload struct {
	Recurse            string   `json:"recurse"`
	Op                 string   `json:"op"`
	ConflictResolution string   `json:"conflictResolution"`
	Value              []string `json:"value"`
	ResourceVersion    string   `json:"resourceVersion"`
	ContentType        string   `json:"ContentType"`
}

func (c *Client) ListTrash(ctx context.Context, opts TrashListOptions) ([]Node, int, error) {
	normOffset, normLimit, err := normalizeBoundedOffsetAndLimit("/trash", opts.Offset, opts.Limit)
	if err != nil {
		return nil, 0, err
	}

	filter := strings.TrimSpace(opts.Filter)
	if filter == "" {
		filter = defaultTrashFilter
	}
	sortExpr := strings.TrimSpace(opts.Sort)
	if sortExpr == "" {
		sortExpr = defaultTrashSort
	}

	params := baseQueryParams()
	params.Set("filters", filter)
	params.Set("sort", sortExpr)
	params.Set("limit", strconv.Itoa(normLimit))
	params.Set("offset", strconv.Itoa(normOffset))

	var resp nodesListResponse
	if err := c.doReadJSON(ctx, "/trash", params, &resp); err != nil {
		return nil, 0, err
	}
	return resp.Data, resp.Count, nil
}

func (c *Client) ListAllTrash(ctx context.Context) ([]Node, error) {
	page, err := paginateBounded9999(ctx, "/trash", 0, MaxPageLimit, func(ctx context.Context, offset, limit int) (*Page[Node], error) {
		items, total, pErr := c.ListTrash(ctx, TrashListOptions{
			Limit:  limit,
			Offset: offset,
		})
		if pErr != nil {
			return nil, pErr
		}
		return buildSinglePage(items, total, offset, limit, nil), nil
	})
	if page != nil {
		return page.Items, err
	}
	return nil, err
}

func (c *Client) MoveToTrash(ctx context.Context, nodeIDs []string) error {
	cleaned, err := sanitizeNodeIDs(nodeIDs)
	if err != nil {
		return err
	}

	for start := 0; start < len(cleaned); start += MaxTrashBatchSize {
		end := min(start+MaxTrashBatchSize, len(cleaned))
		batch := cleaned[start:end]
		payload := moveToTrashPayload{
			Recurse:            "true",
			Op:                 "add",
			Filters:            "",
			ConflictResolution: "RENAME",
			Value:              batch,
			ResourceVersion:    "V2",
			ContentType:        "JSON",
		}
		if err := c.doMutateJSON(ctx, http.MethodPatch, "/trash", payload, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) RestoreFromTrash(ctx context.Context, nodeIDs []string) error {
	cleaned, err := sanitizeNodeIDs(nodeIDs)
	if err != nil {
		return err
	}

	for start := 0; start < len(cleaned); start += MaxTrashBatchSize {
		end := min(start+MaxTrashBatchSize, len(cleaned))
		batch := cleaned[start:end]
		payload := restoreFromTrashPayload{
			Recurse:            "true",
			Op:                 "remove",
			ConflictResolution: "RENAME",
			Value:              batch,
			ResourceVersion:    "V2",
			ContentType:        "JSON",
		}
		if err := c.doMutateJSON(ctx, http.MethodPatch, "/trash", payload, nil); err != nil {
			return err
		}
	}
	return nil
}
