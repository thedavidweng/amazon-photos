package aphotos

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type AggregationCategory string

const (
	AggAll           AggregationCategory = "all"
	AggAllPeople     AggregationCategory = "allPeople"
	AggClusterID     AggregationCategory = "clusterId"
	AggFamilyMembers AggregationCategory = "familyMembers"
	AggFavorite      AggregationCategory = "favorite"
	AggLocation      AggregationCategory = "location"
	AggPeople        AggregationCategory = "people"
	AggThings        AggregationCategory = "things"
	AggTime          AggregationCategory = "time"
	AggType          AggregationCategory = "type"
)

type AggregationSearchData struct {
	ClusterName string `json:"clusterName,omitempty"`
	NodeID      string `json:"nodeId,omitempty"`
}

func (sd *AggregationSearchData) UnmarshalJSON(data []byte) error {
	var raw struct {
		ClusterName string `json:"clusterName,omitempty"`
		NodeID      string `json:"nodeId,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	sd.ClusterName = strings.TrimSpace(raw.ClusterName)
	sd.NodeID = strings.TrimSpace(raw.NodeID)
	return nil
}

type AggregationEntry struct {
	Value      string                 `json:"value"`
	Count      int                    `json:"count"`
	SearchData *AggregationSearchData `json:"searchData,omitempty"`
	Raw        json.RawMessage        `json:"-"`
}

func (ae *AggregationEntry) UnmarshalJSON(data []byte) error {
	var raw struct {
		Value      string                 `json:"value"`
		Count      flexInt64              `json:"count"`
		SearchData *AggregationSearchData `json:"searchData,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	ae.Value = raw.Value
	ae.Count = int(raw.Count)
	ae.SearchData = raw.SearchData
	ae.Raw = append(json.RawMessage(nil), data...)
	return nil
}

func (c *Client) GetAggregations(ctx context.Context, category AggregationCategory) (map[string][]AggregationEntry, error) {
	cat := AggregationCategory(strings.TrimSpace(string(category)))
	if cat == "" {
		cat = AggAll
	}
	if !isValidAggregationCategory(cat) {
		return nil, &InvalidFilterError{
			Endpoint: "/search/aggregation",
			Filter:   string(category),
			Message:  fmt.Sprintf("unsupported aggregation category %q", category),
		}
	}

	if cat == AggAll {
		params := baseQueryParams()
		params.Set("limit", "1")
		params.Set("lowResThumbnail", "true")
		params.Set("searchContext", "all")
		params.Set("groupByForTime", "year")

		var resp struct {
			Aggregations json.RawMessage `json:"aggregations"`
		}
		if err := c.doReadJSON(ctx, "/search", params, &resp); err != nil {
			return nil, err
		}
		return parseAggregationsField(resp.Aggregations, string(AggAll))
	}

	params := make(url.Values, 4)
	params.Set("aggregationContext", "all")
	params.Set("category", string(cat))
	params.Set("resourceVersion", "V2")
	params.Set("ContentType", "JSON")

	var resp struct {
		Aggregations json.RawMessage `json:"aggregations"`
	}
	if err := c.doReadJSON(ctx, "/search/aggregation", params, &resp); err != nil {
		return nil, err
	}
	return parseAggregationsField(resp.Aggregations, string(cat))
}

func isValidAggregationCategory(cat AggregationCategory) bool {
	switch cat {
	case AggAll,
		AggAllPeople,
		AggClusterID,
		AggFamilyMembers,
		AggFavorite,
		AggLocation,
		AggPeople,
		AggThings,
		AggTime,
		AggType:
		return true
	default:
		return false
	}
}

func parseAggregationsField(raw json.RawMessage, defaultKey string) (map[string][]AggregationEntry, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return map[string][]AggregationEntry{}, nil
	}

	switch trimmed[0] {
	case '{':
		var objMap map[string]json.RawMessage
		if err := json.Unmarshal(raw, &objMap); err != nil {
			return nil, &RemoteProtocolError{
				Endpoint: "/search/aggregation",
				Message:  "invalid aggregations object in response",
				Cause:    err,
			}
		}
		out := make(map[string][]AggregationEntry, len(objMap))
		for k, v := range objMap {
			vTrimmed := strings.TrimSpace(string(v))
			if vTrimmed == "" || vTrimmed == "null" {
				out[k] = nil
				continue
			}
			var entries []AggregationEntry
			if err := json.Unmarshal(v, &entries); err != nil {
				return nil, &RemoteProtocolError{
					Endpoint: "/search/aggregation",
					Message:  fmt.Sprintf("invalid aggregation entries for category %q", k),
					Cause:    err,
				}
			}
			out[k] = entries
		}
		return out, nil

	case '[':
		var entries []AggregationEntry
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, &RemoteProtocolError{
				Endpoint: "/search/aggregation",
				Message:  "invalid aggregations array in response",
				Cause:    err,
			}
		}
		key := defaultKey
		if key == "" {
			key = string(AggAll)
		}
		return map[string][]AggregationEntry{
			key: entries,
		}, nil

	default:
		return nil, &RemoteProtocolError{
			Endpoint: "/search/aggregation",
			Message:  "unexpected JSON type for aggregations field",
		}
	}
}
