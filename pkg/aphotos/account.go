package aphotos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type nodesListResponse struct {
	Count        *int            `json:"count"`
	Data         []Node          `json:"data"`
	Aggregations json.RawMessage `json:"aggregations,omitempty"`
}

func (c *Client) readNodesJSON(ctx context.Context, endpoint string, params url.Values, resp *nodesListResponse) error {
	if err := c.doReadJSON(ctx, endpoint, params, resp); err != nil {
		return err
	}
	if resp.Count == nil || resp.Data == nil {
		return &RemoteProtocolError{
			Endpoint: endpoint,
			Message:  "node list response requires count and a data array",
		}
	}
	if *resp.Count < len(resp.Data) {
		return &RemoteProtocolError{
			Endpoint: endpoint,
			Message:  "node list count is less than the number of returned nodes",
		}
	}
	offset, _ := strconv.Atoi(params.Get("offset"))
	if len(resp.Data) == 0 && offset < *resp.Count {
		return &RemoteProtocolError{
			Endpoint: endpoint,
			Message:  fmt.Sprintf("empty node list at offset %d before advertised count %d", offset, *resp.Count),
		}
	}
	for i := range resp.Data {
		if strings.TrimSpace(resp.Data[i].ID) == "" {
			return &RemoteProtocolError{
				Endpoint: endpoint,
				Message:  fmt.Sprintf("node list item %d has no node ID", i),
			}
		}
	}
	return nil
}

type UsageBucket struct {
	Bytes int64 `json:"bytes"`
	Count int64 `json:"count"`
}

func (ub *UsageBucket) UnmarshalJSON(data []byte) error {
	var raw struct {
		Bytes flexInt64 `json:"bytes"`
		Count flexInt64 `json:"count"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	ub.Bytes = int64(raw.Bytes)
	ub.Count = int64(raw.Count)
	return nil
}

type UsageCategory struct {
	Billable UsageBucket `json:"billable"`
	Total    UsageBucket `json:"total"`
}

type Usage struct {
	LastCalculated time.Time                `json:"lastCalculated"`
	Photo          UsageCategory            `json:"photo"`
	Video          UsageCategory            `json:"video"`
	Other          UsageCategory            `json:"other"`
	Doc            UsageCategory            `json:"doc"`
	Categories     map[string]UsageCategory `json:"categories,omitempty"`
	Raw            json.RawMessage          `json:"-"`
}

func (u *Usage) UnmarshalJSON(data []byte) error {
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawMap); err != nil {
		return err
	}
	u.Categories = make(map[string]UsageCategory)
	for k, v := range rawMap {
		if k == "lastCalculated" {
			var ts string
			if err := json.Unmarshal(v, &ts); err == nil {
				u.LastCalculated = parseOptionalUTCTime(ts)
			}
			continue
		}
		var cat UsageCategory
		if err := json.Unmarshal(v, &cat); err != nil {
			continue
		}
		u.Categories[k] = cat
		switch k {
		case "photo":
			u.Photo = cat
		case "video":
			u.Video = cat
		case "other":
			u.Other = cat
		case "doc":
			u.Doc = cat
		}
	}
	u.Raw = append(json.RawMessage(nil), data...)
	return nil
}

func (c *Client) GetRoot(ctx context.Context) (*Node, error) {
	params := baseQueryParams()
	params.Set("filters", "isRoot:true")

	var resp nodesListResponse
	if err := c.doReadJSON(ctx, "/nodes", params, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 || strings.TrimSpace(resp.Data[0].ID) == "" {
		return nil, &RemoteProtocolError{
			Endpoint: "/nodes",
			Message:  "root node response contained no valid root node",
		}
	}
	root := resp.Data[0]
	return &root, nil
}

func (c *Client) GetNode(ctx context.Context, nodeID string) (*Node, error) {
	trimmedID := strings.TrimSpace(nodeID)
	if trimmedID == "" {
		return nil, &ConfigError{
			Field:   "node_id",
			Message: "node ID is required",
		}
	}
	path := fmt.Sprintf("/nodes/%s", url.PathEscape(trimmedID))
	var node Node
	if err := c.doReadJSON(ctx, path, baseQueryParams(), &node); err != nil {
		var nfErr *NotFoundError
		if errors.As(err, &nfErr) {
			nfErr.NodeID = trimmedID
		}
		return nil, err
	}
	if strings.TrimSpace(node.ID) == "" {
		return nil, &RemoteProtocolError{Endpoint: path, Message: "node response contained no node ID"}
	}
	return &node, nil
}

func (c *Client) GetUsage(ctx context.Context) (*Usage, error) {
	var usage Usage
	if err := c.doReadJSON(ctx, "/account/usage", baseQueryParams(), &usage); err != nil {
		return nil, err
	}
	return &usage, nil
}

func baseQueryParams() url.Values {
	q := make(url.Values, 4)
	q.Set("asset", "ALL")
	q.Set("tempLink", "false")
	q.Set("resourceVersion", "V2")
	q.Set("ContentType", "JSON")
	return q
}

type flexInt64 int64

func (fi *flexInt64) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "" || s == "null" {
		*fi = 0
		return nil
	}
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		s = strings.TrimSpace(strings.Trim(s, `"`))
		if s == "" {
			*fi = 0
			return nil
		}
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid integer value %q: %w", string(data), err)
	}
	*fi = flexInt64(v)
	return json.Unmarshal([]byte(strconv.FormatInt(v, 10)), (*int64)(fi))
}
