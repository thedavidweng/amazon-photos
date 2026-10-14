package aphotos

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type createFolderPayload struct {
	Kind            string   `json:"kind"`
	Name            string   `json:"name"`
	Parents         []string `json:"parents"`
	Asset           string   `json:"asset"`
	TempLink        string   `json:"tempLink"`
	ResourceVersion string   `json:"resourceVersion"`
	ContentType     string   `json:"ContentType"`
}

func (c *Client) CreateFolder(ctx context.Context, name, parentID string) (*Node, error) {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return nil, &ConfigError{
			Field:   "name",
			Message: "folder name is required",
		}
	}

	resolvedParent := strings.TrimSpace(parentID)
	if resolvedParent == "" {
		root, err := c.GetRoot(ctx)
		if err != nil {
			return nil, err
		}
		resolvedParent = root.ID
	}

	payload := createFolderPayload{
		Kind:            string(NodeKindFolder),
		Name:            trimmedName,
		Parents:         []string{resolvedParent},
		Asset:           "ALL",
		TempLink:        "false",
		ResourceVersion: "V2",
		ContentType:     "JSON",
	}

	var created Node
	if err := c.doMutateJSON(ctx, http.MethodPost, "/nodes", nil, payload, &created); err != nil {
		return nil, err
	}
	if strings.TrimSpace(created.ID) == "" {
		return nil, &RemoteProtocolError{
			Endpoint: "/nodes",
			Message:  "create folder response did not include node ID",
		}
	}
	if created.Name == "" {
		created.Name = trimmedName
	}
	if created.Kind == "" {
		created.Kind = NodeKindFolder
	}
	if len(created.Parents) == 0 {
		created.Parents = []string{resolvedParent}
	}
	return &created, nil
}

func normalizeListBounds(limit, offset int) (normLimit, normOffset int, err error) {
	if offset < 0 {
		return 0, 0, &ConfigError{
			Field:   "offset",
			Message: "offset cannot be negative",
		}
	}
	if limit <= 0 {
		limit = 200
	} else if limit > 200 {
		limit = 200
	}
	if offset+limit > 9999 {
		if offset >= 9999 {
			return 0, 0, &InvalidFilterError{
				StatusCode: http.StatusBadRequest,
				Endpoint:   "/nodes",
				Message:    "Offset + limit cannot be greater than 9999",
			}
		}
		limit = 9999 - offset
	}
	return limit, offset, nil
}

func (c *Client) ListFolders(ctx context.Context, parentID string, limit, offset int) ([]Node, int, error) {
	normLimit, normOffset, err := normalizeListBounds(limit, offset)
	if err != nil {
		return nil, 0, err
	}

	params := baseQueryParams()
	params.Set("filters", "kind:FOLDER AND status:AVAILABLE")
	params.Set("limit", strconv.Itoa(normLimit))
	params.Set("offset", strconv.Itoa(normOffset))

	path := "/nodes"
	if trimmedParent := strings.TrimSpace(parentID); trimmedParent != "" {
		path = fmt.Sprintf("/nodes/%s/children", url.PathEscape(trimmedParent))
	}

	var resp nodesListResponse
	if err := c.doReadJSON(ctx, path, params, &resp); err != nil {
		return nil, 0, err
	}
	return resp.Data, resp.Count, nil
}

func (c *Client) ListAlbums(ctx context.Context, limit, offset int) ([]Node, int, error) {
	normLimit, normOffset, err := normalizeListBounds(limit, offset)
	if err != nil {
		return nil, 0, err
	}

	params := baseQueryParams()
	params.Set("filters", "kind:VISUAL_COLLECTION AND status:AVAILABLE")
	params.Set("limit", strconv.Itoa(normLimit))
	params.Set("offset", strconv.Itoa(normOffset))

	var resp nodesListResponse
	if err := c.doReadJSON(ctx, "/nodes", params, &resp); err != nil {
		return nil, 0, err
	}
	return resp.Data, resp.Count, nil
}

type createAlbumPayload struct {
	Kind            string `json:"kind"`
	Name            string `json:"name"`
	ResourceVersion string `json:"resourceVersion"`
	ContentType     string `json:"ContentType"`
}

type albumChildrenPatchPayload struct {
	Op              string   `json:"op"`
	Value           []string `json:"value"`
	ResourceVersion string   `json:"resourceVersion"`
	ContentType     string   `json:"ContentType"`
}

type renameNodePayload struct {
	Name            string `json:"name"`
	ResourceVersion string `json:"resourceVersion"`
	ContentType     string `json:"ContentType"`
}

func (c *Client) CreateAlbum(ctx context.Context, name string, initialNodeIDs ...string) (*Node, error) {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return nil, &ConfigError{
			Field:   "name",
			Message: "album name is required",
		}
	}

	payload := createAlbumPayload{
		Kind:            string(NodeKindVisualCollection),
		Name:            trimmedName,
		ResourceVersion: "V2",
		ContentType:     "JSON",
	}

	var created Node
	if err := c.doMutateJSON(ctx, http.MethodPost, "/nodes", nil, payload, &created); err != nil {
		return nil, err
	}
	if strings.TrimSpace(created.ID) == "" {
		return nil, &RemoteProtocolError{
			Endpoint: "/nodes",
			Message:  "create album response did not include node ID",
		}
	}
	if created.Name == "" {
		created.Name = trimmedName
	}
	if created.Kind == "" {
		created.Kind = NodeKindVisualCollection
	}

	if len(initialNodeIDs) > 0 {
		if err := c.AddAlbumMembers(ctx, created.ID, initialNodeIDs); err != nil {
			return &created, err
		}
	}
	return &created, nil
}

func (c *Client) AddAlbumMembers(ctx context.Context, albumID string, nodeIDs []string) error {
	return c.patchAlbumMembers(ctx, albumID, "add", nodeIDs)
}

func (c *Client) RemoveAlbumMembers(ctx context.Context, albumID string, nodeIDs []string) error {
	return c.patchAlbumMembers(ctx, albumID, "remove", nodeIDs)
}

func (c *Client) patchAlbumMembers(ctx context.Context, albumID, op string, nodeIDs []string) error {
	trimmedAlbumID := strings.TrimSpace(albumID)
	if trimmedAlbumID == "" {
		return &ConfigError{
			Field:   "album_id",
			Message: "album ID is required",
		}
	}
	cleaned, err := sanitizeNodeIDs(nodeIDs)
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/nodes/%s/children", url.PathEscape(trimmedAlbumID))
	payload := albumChildrenPatchPayload{
		Op:              op,
		Value:           cleaned,
		ResourceVersion: "V2",
		ContentType:     "JSON",
	}
	return c.doMutateJSON(ctx, http.MethodPatch, path, nil, payload, nil)
}

func (c *Client) RenameNode(ctx context.Context, nodeID, newName string) (*Node, error) {
	trimmedID := strings.TrimSpace(nodeID)
	if trimmedID == "" {
		return nil, &ConfigError{
			Field:   "node_id",
			Message: "node ID is required",
		}
	}
	trimmedName := strings.TrimSpace(newName)
	if trimmedName == "" {
		return nil, &ConfigError{
			Field:   "name",
			Message: "new node name is required",
		}
	}

	path := fmt.Sprintf("/nodes/%s", url.PathEscape(trimmedID))
	payload := renameNodePayload{
		Name:            trimmedName,
		ResourceVersion: "V2",
		ContentType:     "JSON",
	}

	var updated Node
	if err := c.doMutateJSON(ctx, http.MethodPatch, path, nil, payload, &updated); err != nil {
		return nil, err
	}
	if updated.ID == "" {
		updated.ID = trimmedID
	}
	if updated.Name == "" {
		updated.Name = trimmedName
	}
	return &updated, nil
}

func sanitizeNodeIDs(nodeIDs []string) ([]string, error) {
	if len(nodeIDs) == 0 {
		return nil, &ConfigError{
			Field:   "node_ids",
			Message: "at least one node ID is required",
		}
	}
	cleaned := make([]string, 0, len(nodeIDs))
	for _, raw := range nodeIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			return nil, &ConfigError{
				Field:   "node_ids",
				Message: "node ID cannot be empty",
			}
		}
		cleaned = append(cleaned, id)
	}
	return cleaned, nil
}
