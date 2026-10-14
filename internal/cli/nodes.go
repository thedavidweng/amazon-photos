package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thedavidweng/amazon-photos/internal/model"
	"github.com/thedavidweng/amazon-photos/internal/output"
	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

type NodeListResult struct {
	Items      []aphotos.Node `json:"items"`
	TotalCount int            `json:"totalCount"`
	Offset     int            `json:"offset"`
	Limit      int            `json:"limit"`
}

type AlbumMembersMutationResult struct {
	AlbumID   string   `json:"albumId"`
	Operation string   `json:"operation"`
	NodeIDs   []string `json:"nodeIds"`
}

type TrashMutationResult struct {
	Operation string   `json:"operation"`
	NodeIDs   []string `json:"nodeIds"`
}

func newAlbumsCmd() *cobra.Command {
	albumsCmd := &cobra.Command{
		Use:   "albums",
		Short: "Manage Amazon Photos albums (visual collections)",
	}

	var listLimit, listOffset int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List albums",
		RunE: withClient("albums list", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			items, count, err := client.ListAlbums(ctx, listLimit, listOffset)
			if err != nil {
				return r.FailError(meta, err)
			}
			if items == nil {
				items = []aphotos.Node{}
			}
			data := NodeListResult{
				Items:      items,
				TotalCount: count,
				Offset:     listOffset,
				Limit:      listLimit,
			}
			return r.Render(meta, data, nil, func() {
				r.Human("Albums: %d total (showing %d)", count, len(items))
				for i := range items {
					a := &items[i]
					r.Human("%s\t%s\tCreatedDate=%s", a.ID, a.Name, formatTime(a.CreatedDate))
				}
			})
		}),
	}
	listCmd.Flags().IntVar(&listLimit, "limit", 50, "page size (1-200)")
	listCmd.Flags().IntVar(&listOffset, "offset", 0, "starting pagination offset")

	createCmd := &cobra.Command{
		Use:   "create <name> [node-id...]",
		Short: "Create a new album and optionally add initial media node IDs",
		Args:  requireMinArgs("albums create", 1, "album <name> is required"),
		RunE: withClient("albums create", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			album, err := client.CreateAlbum(ctx, args[0], args[1:]...)
			if err != nil {
				if album != nil {
					return r.PartialFailure(meta, album, err, nil)
				}
				return r.FailError(meta, err)
			}
			return r.Render(meta, album, nil, func() {
				r.Human("Created album %s (%s)", album.ID, album.Name)
			})
		}),
	}

	addCmd := &cobra.Command{
		Use:   "add <album-id> <node-id...>",
		Short: "Add media nodes to an album",
		Args:  requireMinArgs("albums add", 2, "<album-id> and at least one <node-id> are required"),
		RunE: withClient("albums add", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			if err := client.AddAlbumMembers(ctx, args[0], args[1:]); err != nil {
				return r.FailError(meta, err)
			}
			data := AlbumMembersMutationResult{
				AlbumID:   args[0],
				Operation: "add",
				NodeIDs:   args[1:],
			}
			return r.Render(meta, data, nil, func() {
				r.Human("Added %d node(s) to album %s", len(args[1:]), args[0])
			})
		}),
	}

	removeCmd := &cobra.Command{
		Use:   "remove <album-id> <node-id...>",
		Short: "Remove media nodes from an album",
		Args:  requireMinArgs("albums remove", 2, "<album-id> and at least one <node-id> are required"),
		RunE: withClient("albums remove", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			if err := client.RemoveAlbumMembers(ctx, args[0], args[1:]); err != nil {
				return r.FailError(meta, err)
			}
			data := AlbumMembersMutationResult{
				AlbumID:   args[0],
				Operation: "remove",
				NodeIDs:   args[1:],
			}
			return r.Render(meta, data, nil, func() {
				r.Human("Removed %d node(s) from album %s", len(args[1:]), args[0])
			})
		}),
	}

	renameCmd := &cobra.Command{
		Use:   "rename <album-id> <new-name>",
		Short: "Rename an album",
		Args:  requireExactArgs("albums rename", 2, "<album-id> and <new-name> are required"),
		RunE: withClient("albums rename", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			updated, err := client.RenameNode(ctx, args[0], args[1])
			if err != nil {
				return r.FailError(meta, err)
			}
			return r.Render(meta, updated, nil, func() {
				r.Human("Renamed album %s to %s", updated.ID, updated.Name)
			})
		}),
	}

	albumsCmd.AddCommand(listCmd, createCmd, addCmd, removeCmd, renameCmd)
	return albumsCmd
}

func newFoldersCmd() *cobra.Command {
	foldersCmd := &cobra.Command{
		Use:   "folders",
		Short: "Manage Amazon Drive / Photos folders",
	}

	var parentID string
	var listLimit, listOffset int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List folders",
		RunE: withClient("folders list", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			items, count, err := client.ListFolders(ctx, parentID, listLimit, listOffset)
			if err != nil {
				return r.FailError(meta, err)
			}
			if items == nil {
				items = []aphotos.Node{}
			}
			data := NodeListResult{
				Items:      items,
				TotalCount: count,
				Offset:     listOffset,
				Limit:      listLimit,
			}
			return r.Render(meta, data, nil, func() {
				r.Human("Folders: %d total (showing %d)", count, len(items))
				for i := range items {
					f := &items[i]
					r.Human("%s\t%s\tCreatedDate=%s", f.ID, f.Name, formatTime(f.CreatedDate))
				}
			})
		}),
	}
	listCmd.Flags().StringVar(&parentID, "parent", "", "parent folder node ID")
	listCmd.Flags().IntVar(&listLimit, "limit", 50, "page size (1-200)")
	listCmd.Flags().IntVar(&listOffset, "offset", 0, "starting pagination offset")

	var createParentID string
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a folder (defaults to account root when --parent is omitted)",
		Args:  requireExactArgs("folders create", 1, "folder <name> is required"),
		RunE: withClient("folders create", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			folder, err := client.CreateFolder(ctx, args[0], createParentID)
			if err != nil {
				return r.FailError(meta, err)
			}
			return r.Render(meta, folder, nil, func() {
				r.Human("Created folder %s (%s)", folder.ID, folder.Name)
			})
		}),
	}
	createCmd.Flags().StringVar(&createParentID, "parent", "", "parent folder node ID (defaults to root)")

	foldersCmd.AddCommand(listCmd, createCmd)
	return foldersCmd
}

func newTrashCmd() *cobra.Command {
	trashCmd := &cobra.Command{
		Use:   "trash",
		Short: "List, move to trash, or restore nodes from trash (no permanent purge in v1)",
	}

	var listLimit, listOffset int
	var listFilter, listSort string
	var listAll bool
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List trashed nodes",
		RunE: withClient("trash list", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			if listAll {
				items, err := client.ListAllTrash(ctx)
				if items == nil {
					items = []aphotos.Node{}
				}
				data := NodeListResult{
					Items:      items,
					TotalCount: len(items),
					Offset:     0,
					Limit:      len(items),
				}
				if err != nil {
					return r.PartialFailure(meta, data, err, nil)
				}
				return r.Render(meta, data, nil, func() {
					r.Human("Trash: %d items", len(items))
					for i := range items {
						n := &items[i]
						r.Human("%s\t%s\t%s", n.ID, n.Kind, n.Name)
					}
				})
			}

			items, count, err := client.ListTrash(ctx, aphotos.TrashListOptions{
				Limit:  listLimit,
				Offset: listOffset,
				Sort:   listSort,
				Filter: listFilter,
			})
			if err != nil {
				return r.FailError(meta, err)
			}
			if items == nil {
				items = []aphotos.Node{}
			}
			data := NodeListResult{
				Items:      items,
				TotalCount: count,
				Offset:     listOffset,
				Limit:      listLimit,
			}
			return r.Render(meta, data, nil, func() {
				r.Human("Trash: %d total (showing %d)", count, len(items))
				for i := range items {
					n := &items[i]
					r.Human("%s\t%s\t%s", n.ID, n.Kind, n.Name)
				}
			})
		}),
	}
	listCmd.Flags().IntVar(&listLimit, "limit", 50, "page size (1-200)")
	listCmd.Flags().IntVar(&listOffset, "offset", 0, "starting pagination offset")
	listCmd.Flags().StringVar(&listFilter, "filter", "", "raw filter override")
	listCmd.Flags().StringVar(&listSort, "sort", "", "sort order override")
	listCmd.Flags().BoolVar(&listAll, "all", false, "paginate through all trashed nodes (up to 9999 limit)")

	var confirmAdd bool
	addCmd := &cobra.Command{
		Use:   "add <node-id...>",
		Short: "Move node(s) to trash (requires --confirm)",
		Args:  requireMinArgs("trash add", 1, "at least one <node-id> is required"),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := GetAppContext(cmd.Context())
			if !confirmAdd && !promptConfirmTrashAdd(cmd, app, len(args)) {
				r := newRenderer(app, cmd)
				return r.FailError(metaInput(app, "trash add"), model.NewCommandError(model.ErrConfirmationRequired, "moving nodes to trash requires --confirm"))
			}
			return withClient("trash add", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
				if err := client.MoveToTrash(ctx, args); err != nil {
					return r.FailError(meta, err)
				}
				data := TrashMutationResult{
					Operation: "add",
					NodeIDs:   args,
				}
				return r.Render(meta, data, nil, func() {
					r.Human("Moved %d node(s) to trash", len(args))
				})
			})(cmd, args)
		},
	}
	addCmd.Flags().BoolVar(&confirmAdd, "confirm", false, "confirm moving node(s) to trash")

	restoreCmd := &cobra.Command{
		Use:   "restore <node-id...>",
		Short: "Restore node(s) from trash",
		Args:  requireMinArgs("trash restore", 1, "at least one <node-id> is required"),
		RunE: withClient("trash restore", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			if err := client.RestoreFromTrash(ctx, args); err != nil {
				return r.FailError(meta, err)
			}
			data := TrashMutationResult{
				Operation: "restore",
				NodeIDs:   args,
			}
			return r.Render(meta, data, nil, func() {
				r.Human("Restored %d node(s) from trash", len(args))
			})
		}),
	}

	trashCmd.AddCommand(listCmd, addCmd, restoreCmd)
	return trashCmd
}

func promptConfirmTrashAdd(cmd *cobra.Command, app *AppContext, count int) bool {
	if app == nil || app.JSON {
		return false
	}
	in := app.PromptIn
	if in == nil {
		fi, err := os.Stdin.Stat()
		if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
			return false
		}
		in = os.Stdin
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Move %d node(s) to trash? [y/N]: ", count)
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return ans == "y" || ans == "yes"
}

func newFavoriteCmd() *cobra.Command {
	var undo bool
	cmd := &cobra.Command{
		Use:   "favorite <node-id...>",
		Short: "Mark node(s) as favorite (or remove favorite with --undo)",
		Args:  requireMinArgs("favorite", 1, "at least one <node-id> is required"),
		RunE: withClient("favorite", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			updated, err := client.SetFavorite(ctx, args, !undo)
			if err != nil {
				if len(updated) > 0 {
					return r.PartialFailure(meta, updated, err, nil)
				}
				return r.FailError(meta, err)
			}
			return r.Render(meta, updated, nil, func() {
				r.Human("Updated favorite=%t on %d node(s)", !undo, len(updated))
			})
		}),
	}
	cmd.Flags().BoolVar(&undo, "undo", false, "unset favorite status")
	return cmd
}

func newHideCmd() *cobra.Command {
	var undo bool
	cmd := &cobra.Command{
		Use:   "hide <node-id...>",
		Short: "Mark node(s) as hidden (or unhide with --undo)",
		Args:  requireMinArgs("hide", 1, "at least one <node-id> is required"),
		RunE: withClient("hide", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			updated, err := client.SetHidden(ctx, args, !undo)
			if err != nil {
				if len(updated) > 0 {
					return r.PartialFailure(meta, updated, err, nil)
				}
				return r.FailError(meta, err)
			}
			return r.Render(meta, updated, nil, func() {
				r.Human("Updated hidden=%t on %d node(s)", !undo, len(updated))
			})
		}),
	}
	cmd.Flags().BoolVar(&undo, "undo", false, "unset hidden status")
	return cmd
}

func requireMinArgs(command string, minCount int, msg string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < minCount {
			app := GetAppContext(cmd.Context())
			r := newRenderer(app, cmd)
			return r.FailError(metaInput(app, command), model.NewCommandError(model.ErrValidationFailed, msg))
		}
		for _, a := range args {
			if strings.TrimSpace(a) == "" {
				app := GetAppContext(cmd.Context())
				r := newRenderer(app, cmd)
				return r.FailError(metaInput(app, command), model.NewCommandError(model.ErrValidationFailed, "argument must not be empty"))
			}
		}
		return nil
	}
}

func requireExactArgs(command string, count int, msg string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != count {
			app := GetAppContext(cmd.Context())
			r := newRenderer(app, cmd)
			return r.FailError(metaInput(app, command), model.NewCommandError(model.ErrValidationFailed, msg))
		}
		return nil
	}
}

func init() {
	registerSubcommand(newAlbumsCmd)
	registerSubcommand(newFoldersCmd)
	registerSubcommand(newTrashCmd)
	registerSubcommand(newFavoriteCmd)
	registerSubcommand(newHideCmd)
}
