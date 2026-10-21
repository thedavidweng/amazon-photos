package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thedavidweng/amazon-photos/internal/output"
	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

type searchFlags struct {
	filter    string
	mediaType string
	year      int
	month     int
	day       int
	name      string
	people    []string
	locations []string
	things    []string
	limit     int
	offset    int
	sortOrder string
	all       bool
}

func newSearchCmd() *cobra.Command {
	sf := &searchFlags{}
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search media nodes via Amazon Photos /search (ContentDate = capture date, CreatedDate = upload date)",
		Long:  "Search media in Amazon Photos. Note: contentDate (content-desc/content-asc) is the EXIF/media capture timestamp, whereas createdDate (created-desc/created-asc) is the cloud upload/node creation timestamp.",
		RunE: withClient("search", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			return runSearchCommand(ctx, r, meta, client, sf, "")
		}),
	}
	bindSearchFlags(cmd, sf, true)
	return cmd
}

func newPhotosCmd() *cobra.Command {
	sf := &searchFlags{}
	cmd := &cobra.Command{
		Use:   "photos",
		Short: "Search photo nodes (ContentDate = capture date, CreatedDate = upload date)",
		RunE: withClient("photos", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			return runSearchCommand(ctx, r, meta, client, sf, "photos")
		}),
	}
	bindSearchFlags(cmd, sf, false)
	return cmd
}

func newVideosCmd() *cobra.Command {
	sf := &searchFlags{}
	cmd := &cobra.Command{
		Use:   "videos",
		Short: "Search video nodes (ContentDate = capture date, CreatedDate = upload date)",
		RunE: withClient("videos", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			return runSearchCommand(ctx, r, meta, client, sf, "videos")
		}),
	}
	bindSearchFlags(cmd, sf, false)
	return cmd
}

func bindSearchFlags(cmd *cobra.Command, sf *searchFlags, includeType bool) {
	cmd.Flags().StringVar(&sf.filter, "filter", "", "raw server filter DSL expression")
	if includeType {
		cmd.Flags().StringVar(&sf.mediaType, "type", "all", "media type filter: photos, videos, or all")
	}
	cmd.Flags().IntVar(&sf.year, "year", 0, "filter by capture year (e.g. 2023)")
	cmd.Flags().IntVar(&sf.month, "month", 0, "filter by capture month (1-12)")
	cmd.Flags().IntVar(&sf.day, "day", 0, "filter by capture day (1-31)")
	cmd.Flags().StringVar(&sf.name, "name", "", "filter by file name prefix")
	cmd.Flags().StringSliceVar(&sf.people, "people", nil, "filter by server person cluster ID(s)")
	cmd.Flags().StringSliceVar(&sf.locations, "location", nil, "filter by server location ID(s)")
	cmd.Flags().StringSliceVar(&sf.things, "things", nil, "filter by recognized object/scene term(s)")
	cmd.Flags().IntVar(&sf.limit, "limit", 50, "page size (1-200)")
	cmd.Flags().IntVar(&sf.offset, "offset", 0, "starting pagination offset")
	cmd.Flags().StringVar(&sf.sortOrder, "sort", "created-desc", "sort order: created-desc, created-asc (cloud upload date), content-desc, content-asc (media capture date), name-asc, name-desc")
	cmd.Flags().BoolVar(&sf.all, "all", false, "paginate through all matching results; fail with partial results if enumeration is incomplete")
}

func runSearchCommand(ctx context.Context, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client, sf *searchFlags, fixedType string) error {
	sortVal, err := parseSortFlag(sf.sortOrder)
	if err != nil {
		return r.FailError(meta, err)
	}

	mediaTypeStr := sf.mediaType
	if fixedType != "" {
		mediaTypeStr = fixedType
	}
	var fb *aphotos.FilterBuilder
	mt, err := parseMediaTypeFlag(mediaTypeStr)
	if err != nil {
		return r.FailError(meta, err)
	}

	hasTypedFilter := fixedType != "" ||
		(strings.TrimSpace(sf.mediaType) != "" && strings.ToLower(strings.TrimSpace(sf.mediaType)) != "all") ||
		sf.year != 0 || sf.month != 0 || sf.day != 0 ||
		strings.TrimSpace(sf.name) != "" || len(sf.people) > 0 || len(sf.locations) > 0 || len(sf.things) > 0

	if hasTypedFilter || strings.TrimSpace(sf.filter) == "" {
		fb = aphotos.NewFilter().MediaType(mt)
		if sf.year != 0 {
			fb.Year(sf.year)
		}
		if sf.month != 0 {
			fb.Month(sf.month)
		}
		if sf.day != 0 {
			fb.Day(sf.day)
		}
		if strings.TrimSpace(sf.name) != "" {
			fb.NamePrefix(sf.name)
		}
		if len(sf.people) > 0 {
			fb.PeopleIDs(sf.people...)
		}
		if len(sf.locations) > 0 {
			fb.Locations(sf.locations...)
		}
		if len(sf.things) > 0 {
			fb.Things(sf.things, nil)
		}
	}

	opts := aphotos.SearchOptions{
		Filter:  fb,
		Filters: sf.filter,
		Limit:   sf.limit,
		Offset:  sf.offset,
		Sort:    sortVal,
	}

	var page *aphotos.Page[aphotos.Node]
	if sf.all {
		page, err = client.SearchAll(ctx, opts)
	} else {
		page, err = client.Search(ctx, opts)
	}
	if err != nil {
		if page != nil {
			return r.PartialFailure(meta, page, err, page.Warnings)
		}
		return r.FailError(meta, err)
	}

	return r.Render(meta, page, page.Warnings, func() {
		renderNodePageHuman(r, page)
	})
}

func newLsCmd() *cobra.Command {
	var parentID string
	var filter string
	var limit int
	var offset int
	var sortStr string
	var all bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List nodes or folder children (ContentDate = capture date, CreatedDate = upload date)",
		RunE: withClient("ls", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			sortVal, err := parseSortFlag(sortStr)
			if err != nil {
				return r.FailError(meta, err)
			}

			var page *aphotos.Page[aphotos.Node]
			if strings.TrimSpace(parentID) != "" {
				cOpts := aphotos.ChildrenOptions{
					Filters: filter,
					Limit:   limit,
					Offset:  offset,
					Sort:    sortVal,
				}
				if all {
					page, err = client.ListAllChildren(ctx, parentID, cOpts)
				} else {
					page, err = client.ListChildren(ctx, parentID, cOpts)
				}
			} else {
				qOpts := aphotos.NodeQueryOptions{
					Filters: filter,
					Limit:   limit,
					Offset:  offset,
					Sort:    sortVal,
				}
				if all {
					page, err = client.QueryAllNodes(ctx, qOpts)
				} else {
					page, err = client.QueryNodes(ctx, qOpts)
				}
			}
			if err != nil {
				if page != nil {
					return r.PartialFailure(meta, page, err, page.Warnings)
				}
				return r.FailError(meta, err)
			}

			return r.Render(meta, page, page.Warnings, func() {
				renderNodePageHuman(r, page)
			})
		}),
	}

	cmd.Flags().StringVar(&parentID, "parent", "", "parent folder or album node ID")
	cmd.Flags().StringVar(&filter, "filter", "", "raw server filter DSL expression")
	cmd.Flags().IntVar(&limit, "limit", 50, "page size (1-200)")
	cmd.Flags().IntVar(&offset, "offset", 0, "starting pagination offset")
	cmd.Flags().StringVar(&sortStr, "sort", "", "sort order: created-desc, created-asc, content-desc, content-asc, modified-desc, modified-asc, name-asc, name-desc")
	cmd.Flags().BoolVar(&all, "all", false, "paginate through all matching nodes")
	return cmd
}

func newAggregationsCmd() *cobra.Command {
	var category string
	cmd := &cobra.Command{
		Use:   "aggregations",
		Short: "Query server media aggregations (all, allPeople, location, things, time, type, ...)",
		RunE: withClient("aggregations", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			aggs, err := client.GetAggregations(ctx, aphotos.AggregationCategory(category))
			if err != nil {
				return r.FailError(meta, err)
			}
			return r.Render(meta, aggs, nil, func() {
				for cat, entries := range aggs {
					r.Human("Category: %s (%d entries)", cat, len(entries))
					for _, e := range entries {
						label := e.Value
						if e.SearchData != nil && e.SearchData.ClusterName != "" {
							label = fmt.Sprintf("%s (%s)", e.Value, e.SearchData.ClusterName)
						}
						r.Human("  %s: %d", label, e.Count)
					}
				}
			})
		}),
	}
	cmd.Flags().StringVar(&category, "category", "all", "aggregation category: all, allPeople, clusterId, familyMembers, favorite, location, people, things, time, type")
	return cmd
}

func parseMediaTypeFlag(raw string) (aphotos.MediaTypeFilter, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "all", "photos-and-videos":
		return aphotos.MediaPhotosAndVideos, nil
	case "photos", "photo":
		return aphotos.MediaPhotos, nil
	case "videos", "video":
		return aphotos.MediaVideos, nil
	default:
		return "", &aphotos.InvalidFilterError{
			Filter:  raw,
			Message: fmt.Sprintf("unsupported --type %q (expected photos, videos, or all)", raw),
		}
	}
}

func parseSortFlag(raw string) (aphotos.SortOrder, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	switch strings.ToLower(s) {
	case "created-desc":
		return aphotos.SortCreatedDesc, nil
	case "created-asc":
		return aphotos.SortCreatedAsc, nil
	case "content-desc":
		return aphotos.SortContentDateDesc, nil
	case "content-asc":
		return aphotos.SortContentDateAsc, nil
	case "modified-desc":
		return aphotos.SortModifiedDesc, nil
	case "modified-asc":
		return aphotos.SortModifiedAsc, nil
	case "name-asc":
		return aphotos.SortNameAsc, nil
	case "name-desc":
		return aphotos.SortNameDesc, nil
	}
	switch aphotos.SortOrder(s) {
	case aphotos.SortCreatedDesc,
		aphotos.SortCreatedAsc,
		aphotos.SortContentDateDesc,
		aphotos.SortContentDateAsc,
		aphotos.SortModifiedDesc,
		aphotos.SortModifiedAsc,
		aphotos.SortNameAsc,
		aphotos.SortNameDesc:
		return aphotos.SortOrder(s), nil
	default:
		return "", &aphotos.InvalidFilterError{
			Filter:  raw,
			Message: fmt.Sprintf("unsupported --sort %q (expected created-desc, created-asc, content-desc, content-asc, modified-desc, modified-asc, name-asc, or name-desc)", raw),
		}
	}
}

func renderNodePageHuman(r *output.Renderer, page *aphotos.Page[aphotos.Node]) {
	r.Human("Total: %d (showing %d, offset %d, hasMore=%t)", page.TotalCount, len(page.Items), page.Offset, page.HasMore)
	for i := range page.Items {
		n := &page.Items[i]
		var size int64
		var contentDate string
		if n.ContentProperties != nil {
			size = n.ContentProperties.Size
			contentDate = formatTime(n.ContentProperties.ContentDate)
		} else {
			contentDate = "-"
		}
		r.Human("%s\t%s\t%s\t%dB\tContentDate(capture)=%s\tCreatedDate(upload)=%s",
			n.ID, n.Kind, n.Name, size, contentDate, formatTime(n.CreatedDate))
	}
}

func init() {
	registerSubcommand(newSearchCmd)
	registerSubcommand(newPhotosCmd)
	registerSubcommand(newVideosCmd)
	registerSubcommand(newLsCmd)
	registerSubcommand(newAggregationsCmd)
}
