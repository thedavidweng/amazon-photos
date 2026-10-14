package aphotos

import (
	"context"
	"fmt"
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
	if opts.Offset < 0 {
		return nil, 0, &ConfigError{
			Field:   "offset",
			Message: "offset cannot be negative",
		}
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 200
	} else if limit > 200 {
		limit = 200
	}
	if opts.Offset >= 9999 {
		return nil, 0, &InvalidFilterError{
			StatusCode: http.StatusBadRequest,
			Endpoint:   "/trash",
			Message:    "Offset + limit cannot be greater than 9999",
		}
	}
	if opts.Offset+limit > 9999 {
		limit = 9999 - opts.Offset
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
	params.Set("limit", strconv.Itoa(limit))
	params.Set("offset", strconv.Itoa(opts.Offset))

	var resp nodesListResponse
	if err := c.doReadJSON(ctx, "/trash", params, &resp); err != nil {
		return nil, 0, err
	}
	return resp.Data, resp.Count, nil
}

func (c *Client) ListAllTrash(ctx context.Context) ([]Node, error) {
	const pageSize = 200
	var all []Node
	seen := make(map[string]struct{})
	offset := 0

	for offset < 9999 {
		limit := min(pageSize, 9999-offset)
		page, total, err := c.ListTrash(ctx, TrashListOptions{
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			if len(all) > 0 {
				return all, &PartialResultError{
					Endpoint: "/trash",
					Message:  fmt.Sprintf("failed fetching trash page at offset %d after collecting %d items", offset, len(all)),
					Cause:    err,
				}
			}
			return nil, err
		}

		for i := range page {
			n := &page[i]
			if n.ID != "" {
				if _, exists := seen[n.ID]; exists {
					continue
				}
				seen[n.ID] = struct{}{}
			}
			all = append(all, *n)
		}

		nextOffset := offset + len(page)
		if len(page) == 0 || (total > 0 && nextOffset >= total) || len(page) < limit {
			break
		}

		if offset+limit >= 9999 && total > 9999 {
			return all, &PartialResultError{
				Endpoint: "/trash",
				Message:  fmt.Sprintf("trash pagination reached the 9999 offset limit (total reported: %d)", total),
				Warnings: []string{"Amazon Drive /trash API enforces offset + limit <= 9999"},
			}
		}

		offset += limit
	}

	return all, nil
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
		if err := c.doMutateJSON(ctx, http.MethodPatch, "/trash", nil, payload, nil); err != nil {
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
		if err := c.doMutateJSON(ctx, http.MethodPatch, "/trash", nil, payload, nil); err != nil {
			return err
		}
	}
	return nil
}
