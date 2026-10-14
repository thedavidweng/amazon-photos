package aphotos

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type nodeSettingsPatchPayload struct {
	Settings        map[string]bool `json:"settings"`
	ResourceVersion string          `json:"resourceVersion"`
	ContentType     string          `json:"ContentType"`
}

func (c *Client) SetFavorite(ctx context.Context, nodeIDs []string, favorite bool) ([]Node, error) {
	return c.patchNodesSettings(ctx, nodeIDs, map[string]bool{"favorite": favorite}, &NodeSettings{Favorite: favorite})
}

func (c *Client) SetHidden(ctx context.Context, nodeIDs []string, hidden bool) ([]Node, error) {
	return c.patchNodesSettings(ctx, nodeIDs, map[string]bool{"hidden": hidden}, &NodeSettings{Hidden: hidden})
}

func (c *Client) patchNodesSettings(ctx context.Context, nodeIDs []string, settingsMap map[string]bool, fallback *NodeSettings) ([]Node, error) {
	cleaned, err := sanitizeNodeIDs(nodeIDs)
	if err != nil {
		return nil, err
	}

	payload := nodeSettingsPatchPayload{
		Settings:        settingsMap,
		ResourceVersion: "V2",
		ContentType:     "JSON",
	}

	updated := make([]Node, 0, len(cleaned))
	for _, id := range cleaned {
		path := fmt.Sprintf("/nodes/%s", url.PathEscape(id))
		var node Node
		if err := c.doMutateJSON(ctx, http.MethodPatch, path, payload, &node); err != nil {
			return updated, err
		}
		if node.ID == "" {
			node.ID = id
		}
		if node.Settings == nil && fallback != nil {
			cp := *fallback
			node.Settings = &cp
		}
		updated = append(updated, node)
	}
	return updated, nil
}
