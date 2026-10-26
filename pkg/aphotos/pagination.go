package aphotos

import (
	"context"
	"fmt"
	"time"
)

const (
	MaxPageLimit        = 200
	MaxNodeOffsetWindow = 9999
)

type Page[T any] struct {
	Items                  []T                           `json:"items"`
	TotalCount             int                           `json:"totalCount"`
	Offset                 int                           `json:"offset"`
	Limit                  int                           `json:"limit"`
	NextOffset             int                           `json:"nextOffset"`
	HasMore                bool                          `json:"hasMore"`
	Complete               bool                          `json:"complete"`
	TruncatedByOffsetLimit bool                          `json:"truncatedByOffsetLimit,omitempty"`
	Warnings               []string                      `json:"warnings,omitempty"`
	Aggregations           map[string][]AggregationEntry `json:"aggregations,omitempty"`
}

func normalizeOffsetAndLimit(offset, limit int) (normOffset, normLimit int, err error) {
	if offset < 0 {
		return 0, 0, &InvalidFilterError{
			Message: fmt.Sprintf("offset must be >= 0, got %d", offset),
		}
	}
	if limit < 0 {
		return 0, 0, &InvalidFilterError{
			Message: fmt.Sprintf("limit must be >= 0, got %d", limit),
		}
	}
	if limit > MaxPageLimit {
		return 0, 0, &InvalidFilterError{
			Message: fmt.Sprintf("limit %d exceeds maximum page limit %d", limit, MaxPageLimit),
		}
	}
	if limit == 0 {
		limit = MaxPageLimit
	}
	return offset, limit, nil
}

func normalizeBoundedOffsetAndLimit(endpoint string, offset, limit int) (normOffset, normLimit int, err error) {
	if limit > MaxPageLimit {
		limit = MaxPageLimit
	}
	normOffset, normLimit, err = normalizeOffsetAndLimit(offset, limit)
	if err != nil {
		return 0, 0, err
	}
	if normOffset >= MaxNodeOffsetWindow {
		return 0, 0, &InvalidFilterError{
			Endpoint: endpoint,
			Message:  fmt.Sprintf("offset %d exceeds maximum node offset window %d", normOffset, MaxNodeOffsetWindow),
		}
	}
	if normOffset+normLimit > MaxNodeOffsetWindow {
		normLimit = MaxNodeOffsetWindow - normOffset
	}
	return normOffset, normLimit, nil
}

func buildSinglePage(items []Node, totalCount, offset, limit int, aggs map[string][]AggregationEntry) *Page[Node] {
	if items == nil {
		items = []Node{}
	}
	nextOffset := offset + limit
	var hasMore bool
	if totalCount > 0 {
		hasMore = offset+limit < totalCount && len(items) > 0
	} else {
		hasMore = len(items) == limit && limit > 0
	}
	return &Page[Node]{
		Items:        items,
		TotalCount:   totalCount,
		Offset:       offset,
		Limit:        limit,
		NextOffset:   nextOffset,
		HasMore:      hasMore,
		Complete:     true,
		Aggregations: aggs,
	}
}

func paginateUnbounded(
	ctx context.Context,
	endpoint string,
	startOffset int,
	pageLimit int,
	fetchPage func(ctx context.Context, offset, limit int) (*Page[Node], error),
) (*Page[Node], error) {
	firstPage, err := fetchPage(ctx, startOffset, pageLimit)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(firstPage.Items))
	merged := make([]Node, 0, max(len(firstPage.Items), firstPage.TotalCount))
	dupCount := appendUniqueNodes(&merged, firstPage.Items, seen)

	totalCount := firstPage.TotalCount
	currentOffset := startOffset + pageLimit
	hasMore := firstPage.HasMore
	var warnings []string

	for hasMore {
		if totalCount > 0 && currentOffset >= totalCount {
			break
		}
		p, pErr := fetchPage(ctx, currentOffset, pageLimit)
		if pErr != nil {
			warnings = duplicateWarnings(dupCount)
			warnings = append(warnings, fmt.Sprintf("pagination interrupted at offset %d: %s", currentOffset, Redact(pErr.Error())))
			partial := &Page[Node]{
				Items:        merged,
				TotalCount:   totalCount,
				Offset:       startOffset,
				Limit:        pageLimit,
				NextOffset:   currentOffset,
				HasMore:      true,
				Complete:     false,
				Warnings:     warnings,
				Aggregations: firstPage.Aggregations,
			}
			return partial, &PartialResultError{
				Endpoint: endpoint,
				Message:  fmt.Sprintf("pagination incomplete at offset %d", currentOffset),
				Warnings: warnings,
				Cause:    pErr,
			}
		}

		if p.TotalCount > totalCount {
			totalCount = p.TotalCount
		}
		if len(p.Items) == 0 {
			break
		}
		pageDuplicates := appendUniqueNodes(&merged, p.Items, seen)
		dupCount += pageDuplicates
		if pageDuplicates == len(p.Items) {
			break
		}
		currentOffset += pageLimit
		hasMore = p.HasMore
	}

	warnings = duplicateWarnings(dupCount)
	return completePagination(endpoint, &Page[Node]{
		Items:        merged,
		TotalCount:   max(totalCount, len(merged)),
		Offset:       startOffset,
		Limit:        pageLimit,
		NextOffset:   currentOffset,
		HasMore:      false,
		Complete:     true,
		Warnings:     warnings,
		Aggregations: firstPage.Aggregations,
	})
}

func paginateBounded9999(
	ctx context.Context,
	endpoint string,
	startOffset int,
	pageLimit int,
	fetchPage func(ctx context.Context, offset, limit int) (*Page[Node], error),
) (*Page[Node], error) {
	if startOffset >= MaxNodeOffsetWindow {
		return nil, &InvalidFilterError{
			Endpoint: endpoint,
			Message:  fmt.Sprintf("offset %d exceeds maximum node offset window %d", startOffset, MaxNodeOffsetWindow),
		}
	}
	firstLimit := min(pageLimit, MaxNodeOffsetWindow-startOffset)
	firstPage, err := fetchPage(ctx, startOffset, firstLimit)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(firstPage.Items))
	merged := make([]Node, 0, min(max(len(firstPage.Items), firstPage.TotalCount), MaxNodeOffsetWindow))
	dupCount := appendUniqueNodes(&merged, firstPage.Items, seen)

	totalCount := firstPage.TotalCount
	currentOffset := startOffset + firstLimit
	hasMore := firstPage.HasMore
	var warnings []string

	for hasMore && currentOffset < MaxNodeOffsetWindow {
		if totalCount > 0 && currentOffset >= totalCount {
			hasMore = false
			break
		}
		stepLimit := min(pageLimit, MaxNodeOffsetWindow-currentOffset)
		p, pErr := fetchPage(ctx, currentOffset, stepLimit)
		if pErr != nil {
			warnings = duplicateWarnings(dupCount)
			warnings = append(warnings, fmt.Sprintf("pagination interrupted at offset %d: %s", currentOffset, Redact(pErr.Error())))
			partial := &Page[Node]{
				Items:        merged,
				TotalCount:   totalCount,
				Offset:       startOffset,
				Limit:        pageLimit,
				NextOffset:   currentOffset,
				HasMore:      true,
				Complete:     false,
				Warnings:     warnings,
				Aggregations: firstPage.Aggregations,
			}
			return partial, &PartialResultError{
				Endpoint: endpoint,
				Message:  fmt.Sprintf("pagination incomplete at offset %d", currentOffset),
				Warnings: warnings,
				Cause:    pErr,
			}
		}

		if p.TotalCount > totalCount {
			totalCount = p.TotalCount
		}
		if len(p.Items) == 0 {
			hasMore = false
			break
		}
		pageDuplicates := appendUniqueNodes(&merged, p.Items, seen)
		dupCount += pageDuplicates
		if pageDuplicates == len(p.Items) {
			break
		}
		currentOffset += stepLimit
		hasMore = p.HasMore
	}

	warnings = duplicateWarnings(dupCount)

	if totalCount > MaxNodeOffsetWindow || (hasMore && currentOffset >= MaxNodeOffsetWindow) {
		truncMsg := fmt.Sprintf("result count %d exceeds %s offset+limit window %d; enable TimeWindowSubdivide to avoid truncation", totalCount, endpoint, MaxNodeOffsetWindow)
		warnings = append(warnings, truncMsg)
		partial := &Page[Node]{
			Items:                  merged,
			TotalCount:             totalCount,
			Offset:                 startOffset,
			Limit:                  pageLimit,
			NextOffset:             currentOffset,
			HasMore:                true,
			Complete:               false,
			TruncatedByOffsetLimit: true,
			Warnings:               warnings,
			Aggregations:           firstPage.Aggregations,
		}
		return partial, &PartialResultError{
			Endpoint: endpoint,
			Message:  truncMsg,
			Warnings: warnings,
		}
	}

	return completePagination(endpoint, &Page[Node]{
		Items:        merged,
		TotalCount:   max(totalCount, len(merged)),
		Offset:       startOffset,
		Limit:        pageLimit,
		NextOffset:   currentOffset,
		HasMore:      false,
		Complete:     true,
		Warnings:     warnings,
		Aggregations: firstPage.Aggregations,
	})
}

func completePagination(endpoint string, page *Page[Node]) (*Page[Node], error) {
	if page.Offset+len(page.Items) < page.TotalCount {
		page.Complete = false
		page.HasMore = true
		message := fmt.Sprintf("pagination returned %d unique nodes from offset %d of advertised count %d", len(page.Items), page.Offset, page.TotalCount)
		page.Warnings = append(page.Warnings, message)
		return page, &PartialResultError{
			Endpoint: endpoint,
			Message:  message,
			Warnings: page.Warnings,
		}
	}
	return page, nil
}

func duplicateWarnings(dupCount int) []string {
	if dupCount > 0 {
		return []string{fmt.Sprintf("deduplicated %d duplicate node(s) across page boundaries", dupCount)}
	}
	return nil
}

func appendUniqueNodes(dst *[]Node, src []Node, seen map[string]struct{}) int {
	dups := 0
	for i := range src {
		id := src[i].ID
		if id != "" {
			if _, exists := seen[id]; exists {
				dups++
				continue
			}
			seen[id] = struct{}{}
		}
		*dst = append(*dst, src[i])
	}
	return dups
}

type timeWindow struct {
	low           time.Time
	high          time.Time
	inclusiveLow  bool
	inclusiveHigh bool
}

func subdivideByTimeWindows(
	ctx context.Context,
	endpoint string,
	baseFilter string,
	dateField string,
	start, end time.Time,
	pageLimit int,
	fetchWithFilter func(ctx context.Context, filter string, offset, limit int) (*Page[Node], error),
) (*Page[Node], error) {
	if dateField == "" {
		dateField = "createdDate"
	}
	now := time.Now().UTC()
	if start.IsZero() {
		start = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	} else {
		start = start.UTC()
	}
	if end.IsZero() {
		end = now.Add(24 * time.Hour)
	} else {
		end = end.UTC()
	}
	if start.After(end) {
		return nil, &InvalidFilterError{
			Endpoint: endpoint,
			Message:  "WindowStart cannot be after WindowEnd",
		}
	}

	seen := make(map[string]struct{})
	var merged []Node
	var warnings []string
	dupCount := 0

	stack := []timeWindow{
		{low: start, high: end, inclusiveLow: true, inclusiveHigh: true},
	}

	for len(stack) > 0 {
		win := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		winFilter, err := composeWindowFilter(baseFilter, dateField, win)
		if err != nil {
			return nil, err
		}

		firstPage, err := fetchWithFilter(ctx, winFilter, 0, pageLimit)
		if err != nil {
			warnings = duplicateWarnings(dupCount)
			partial := &Page[Node]{
				Items:      merged,
				TotalCount: len(merged),
				Offset:     0,
				Limit:      pageLimit,
				HasMore:    true,
				Complete:   false,
				Warnings:   warnings,
			}
			return partial, &PartialResultError{
				Endpoint: endpoint,
				Message:  "time-window subdivision interrupted",
				Warnings: warnings,
				Cause:    err,
			}
		}

		if firstPage.TotalCount > MaxNodeOffsetWindow {
			span := win.high.Sub(win.low)
			if span <= time.Second {
				truncMsg := fmt.Sprintf("single-second time window still exceeds %d nodes (count=%d)", MaxNodeOffsetWindow, firstPage.TotalCount)
				warnings = duplicateWarnings(dupCount)
				warnings = append(warnings, truncMsg)
				partial := &Page[Node]{
					Items:                  merged,
					TotalCount:             firstPage.TotalCount,
					Offset:                 0,
					Limit:                  pageLimit,
					HasMore:                true,
					Complete:               false,
					TruncatedByOffsetLimit: true,
					Warnings:               warnings,
				}
				return partial, &PartialResultError{
					Endpoint: endpoint,
					Message:  truncMsg,
					Warnings: warnings,
				}
			}
			mid := win.low.Add(span / 2).UTC().Truncate(time.Second)
			if !mid.After(win.low) {
				mid = win.low.Add(time.Second)
			}
			// Push second half first so first half is popped next (chronological traversal).
			stack = append(stack,
				timeWindow{low: mid, high: win.high, inclusiveLow: true, inclusiveHigh: win.inclusiveHigh},
				timeWindow{low: win.low, high: mid, inclusiveLow: win.inclusiveLow, inclusiveHigh: false},
			)
			continue
		}

		winPage, err := paginateBounded9999(ctx, endpoint, 0, pageLimit, func(ctx context.Context, offset, limit int) (*Page[Node], error) {
			if offset == 0 && limit == pageLimit {
				return firstPage, nil
			}
			return fetchWithFilter(ctx, winFilter, offset, limit)
		})
		if err != nil {
			dupCount += appendUniqueNodes(&merged, winPage.Items, seen)
			warnings = duplicateWarnings(dupCount)
			warnings = append(warnings, winPage.Warnings...)
			partial := &Page[Node]{
				Items:                  merged,
				TotalCount:             len(merged),
				Offset:                 0,
				Limit:                  pageLimit,
				HasMore:                true,
				Complete:               false,
				TruncatedByOffsetLimit: winPage.TruncatedByOffsetLimit,
				Warnings:               warnings,
			}
			return partial, err
		}

		dupCount += appendUniqueNodes(&merged, winPage.Items, seen)
	}

	warnings = duplicateWarnings(dupCount)
	return &Page[Node]{
		Items:      merged,
		TotalCount: len(merged),
		Offset:     0,
		Limit:      pageLimit,
		NextOffset: len(merged),
		HasMore:    false,
		Complete:   true,
		Warnings:   warnings,
	}, nil
}

func composeWindowFilter(baseFilter, dateField string, win timeWindow) (string, error) {
	rangeExpr, err := NewFilter().DateRange(dateField, win.low, win.high, win.inclusiveLow, win.inclusiveHigh).Build()
	if err != nil {
		return "", err
	}
	if baseFilter == "" {
		return rangeExpr, nil
	}
	return fmt.Sprintf("(%s) AND %s", baseFilter, rangeExpr), nil
}
