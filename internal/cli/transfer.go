package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thedavidweng/amazon-photos/internal/output"
	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

type TransferSummaryData struct {
	Operation   string                   `json:"operation"`
	DryRun      bool                     `json:"dry_run,omitempty"`
	ParentID    string                   `json:"parent_id,omitempty"`
	AlbumID     string                   `json:"album_id,omitempty"`
	AlbumAdded  []string                 `json:"album_added,omitempty"`
	Total       int                      `json:"total"`
	Confirmed   int                      `json:"confirmed"`
	Skipped     int                      `json:"skipped"`
	Unconfirmed int                      `json:"unconfirmed"`
	Failed      int                      `json:"failed"`
	Items       []aphotos.TransferResult `json:"items"`
}

func newUploadCmd() *cobra.Command {
	var parentID string
	var albumID string
	var dryRun bool
	var onConflict string
	var recursive bool
	var continueOnError bool

	cmd := &cobra.Command{
		Use:   "upload <file-or-dir...>",
		Short: "Upload local file(s) or directory contents to Amazon Photos",
		Args:  requireMinArgs("upload", 1, "at least one <file-or-dir> path is required"),
		RunE: withClient("upload", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			policy := aphotos.ConflictPolicy(strings.ToLower(strings.TrimSpace(onConflict)))
			if policy == "" {
				policy = aphotos.ConflictFail
			}

			results, err := client.UploadBatch(ctx, args, aphotos.BatchUploadOptions{
				ParentNodeID:    parentID,
				AutoResolveRoot: strings.TrimSpace(parentID) == "" && !dryRun,
				ConflictPolicy:  policy,
				Recursive:       recursive,
				DryRun:          dryRun,
				ContinueOnError: continueOnError,
				Progress: func(p aphotos.TransferProgress) {
					if p.Done > 0 && p.TotalItems > 0 {
						r.Progressf("[%d/%d] %s (%d/%d bytes)", p.Done, p.TotalItems, p.Name, p.BytesTransferred, p.TotalBytes)
					}
				},
			})

			summary := summarizeTransfers("upload", results, dryRun, parentID, albumID)

			trimmedAlbum := strings.TrimSpace(albumID)
			if trimmedAlbum != "" && !dryRun {
				var confirmedIDs []string
				for i := range results {
					res := &results[i]
					if res.Status == aphotos.TransferConfirmed && strings.TrimSpace(res.NodeID) != "" {
						confirmedIDs = append(confirmedIDs, strings.TrimSpace(res.NodeID))
					}
				}
				if len(confirmedIDs) > 0 {
					if albErr := client.AddAlbumMembers(ctx, trimmedAlbum, confirmedIDs); albErr != nil {
						return r.PartialFailure(meta, summary, albErr, nil)
					}
					summary.AlbumAdded = confirmedIDs
				}
			}

			if err != nil {
				if summary.Confirmed > 0 || summary.Skipped > 0 || continueOnError {
					return r.PartialFailure(meta, summary, err, nil)
				}
				return r.FailWithData(meta, summary, err, nil)
			}

			return r.Render(meta, summary, nil, func() {
				renderTransferSummaryHuman(r, summary)
			})
		}),
	}

	cmd.Flags().StringVar(&parentID, "parent", "", "target parent folder node ID (defaults to account root)")
	cmd.Flags().StringVar(&albumID, "album", "", "target album node ID to add confirmed uploaded nodes to")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "plan files to upload without sending HTTP requests")
	cmd.Flags().StringVar(&onConflict, "on-conflict", "fail", "conflict resolution policy: skip, fail, or rename")
	cmd.Flags().BoolVar(&recursive, "recursive", false, "recursively traverse directories")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", false, "continue uploading remaining files when an item fails")
	return cmd
}

func newDownloadCmd() *cobra.Command {
	var outDir string
	var continueOnError bool

	cmd := &cobra.Command{
		Use:   "download <node-id...>",
		Short: "Download remote media node(s) with size and MD5 verification",
		Args:  requireMinArgs("download", 1, "at least one <node-id> is required"),
		RunE: withClient("download", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			dest := strings.TrimSpace(outDir)
			if dest == "" {
				dest = "."
			}

			results, err := client.DownloadBatch(ctx, args, aphotos.BatchDownloadOptions{
				DestDir:         dest,
				ContinueOnError: continueOnError,
				Progress: func(p aphotos.TransferProgress) {
					if p.Done > 0 && p.TotalItems > 0 {
						r.Progressf("[%d/%d] %s (%d bytes)", p.Done, p.TotalItems, p.Name, p.BytesTransferred)
					}
				},
			})

			summary := summarizeTransfers("download", results, false, "", "")
			if err != nil {
				if summary.Confirmed > 0 || summary.Skipped > 0 || continueOnError {
					return r.PartialFailure(meta, summary, err, nil)
				}
				return r.FailWithData(meta, summary, err, nil)
			}

			return r.Render(meta, summary, nil, func() {
				renderTransferSummaryHuman(r, summary)
			})
		}),
	}

	cmd.Flags().StringVar(&outDir, "out", ".", "destination directory for downloaded files")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", false, "continue downloading remaining nodes when an item fails")
	return cmd
}

func summarizeTransfers(op string, items []aphotos.TransferResult, dryRun bool, parentID, albumID string) TransferSummaryData {
	if items == nil {
		items = []aphotos.TransferResult{}
	}
	s := TransferSummaryData{
		Operation: op,
		DryRun:    dryRun,
		ParentID:  strings.TrimSpace(parentID),
		AlbumID:   strings.TrimSpace(albumID),
		Total:     len(items),
		Items:     items,
	}
	for i := range items {
		switch items[i].Status {
		case aphotos.TransferConfirmed:
			s.Confirmed++
		case aphotos.TransferSkipped:
			s.Skipped++
		case aphotos.TransferUnconfirmed:
			s.Unconfirmed++
		case aphotos.TransferFailed:
			s.Failed++
		}
	}
	return s
}

func renderTransferSummaryHuman(r *output.Renderer, s TransferSummaryData) {
	r.Human("%s summary: total=%d confirmed=%d skipped=%d unconfirmed=%d failed=%d",
		s.Operation, s.Total, s.Confirmed, s.Skipped, s.Unconfirmed, s.Failed)
	for i := range s.Items {
		it := &s.Items[i]
		r.Human("  [%s] name=%s id=%s path=%s bytes=%d", it.Status, it.Name, it.NodeID, it.LocalPath, it.Bytes)
	}
}

func init() {
	registerSubcommand(newUploadCmd)
	registerSubcommand(newDownloadCmd)
}
