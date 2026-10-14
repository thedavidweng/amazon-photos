package aphotos

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type favoriteSettingsPatch struct {
	Favorite bool `json:"favorite"`
}

type hiddenSettingsPatch struct {
	Hidden bool `json:"hidden"`
}

type nodeSettingsPatchPayload[T any] struct {
	Settings        T      `json:"settings"`
	ResourceVersion string `json:"resourceVersion"`
	ContentType     string `json:"ContentType"`
}

func (c *Client) SetFavorite(ctx context.Context, nodeIDs []string, favorite bool) ([]Node, error) {
	cleaned, err := sanitizeNodeIDs(nodeIDs)
	if err != nil {
		return nil, err
	}

	payload := nodeSettingsPatchPayload[favoriteSettingsPatch]{
		Settings:        favoriteSettingsPatch{Favorite: favorite},
		ResourceVersion: "V2",
		ContentType:     "JSON",
	}

	updated := make([]Node, 0, len(cleaned))
	for _, id := range cleaned {
		path := fmt.Sprintf("/nodes/%s", url.PathEscape(id))
		var node Node
		if err := c.doMutateJSON(ctx, http.MethodPatch, path, nil, payload, &node); err != nil {
			return updated, err
		}
		if node.ID == "" {
			node.ID = id
		}
		if node.Settings == nil {
			node.Settings = &NodeSettings{Favorite: favorite}
		}
		updated = append(updated, node)
	}
	return updated, nil
}

func (c *Client) SetHidden(ctx context.Context, nodeIDs []string, hidden bool) ([]Node, error) {
	cleaned, err := sanitizeNodeIDs(nodeIDs)
	if err != nil {
		return nil, err
	}

	payload := nodeSettingsPatchPayload[hiddenSettingsPatch]{
		Settings:        hiddenSettingsPatch{Hidden: hidden},
		ResourceVersion: "V2",
		ContentType:     "JSON",
	}

	updated := make([]Node, 0, len(cleaned))
	for _, id := range cleaned {
		path := fmt.Sprintf("/nodes/%s", url.PathEscape(id))
		var node Node
		if err := c.doMutateJSON(ctx, http.MethodPatch, path, nil, payload, &node); err != nil {
			return updated, err
		}
		if node.ID == "" {
			node.ID = id
		}
		if node.Settings == nil {
			node.Settings = &NodeSettings{Hidden: hidden}
		}
		updated = append(updated, node)
	}
	return updated, nil
}
