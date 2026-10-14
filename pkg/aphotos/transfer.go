package aphotos

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

type ConflictPolicy string

const (
	ConflictFail   ConflictPolicy = "fail"
	ConflictSkip   ConflictPolicy = "skip"
	ConflictRename ConflictPolicy = "rename"
)

type TransferStatus string

const (
	TransferConfirmed   TransferStatus = "confirmed"
	TransferUnconfirmed TransferStatus = "unconfirmed"
	TransferSkipped     TransferStatus = "skipped"
	TransferFailed      TransferStatus = "failed"
)

type TransferProgress struct {
	SourcePath       string `json:"sourcePath,omitempty"`
	Name             string `json:"name,omitempty"`
	BytesTransferred int64  `json:"bytesTransferred"`
	TotalBytes       int64  `json:"totalBytes,omitempty"`
	Done             int    `json:"done,omitempty"`
	TotalItems       int    `json:"totalItems,omitempty"`
}

type ProgressFunc func(TransferProgress)

type TransferResult struct {
	Status           TransferStatus `json:"status"`
	NodeID           string         `json:"nodeId,omitempty"`
	Node             *Node          `json:"node,omitempty"`
	Name             string         `json:"name,omitempty"`
	LocalPath        string         `json:"localPath,omitempty"`
	Bytes            int64          `json:"bytes"`
	MD5              string         `json:"md5,omitempty"`
	ConflictResolved bool           `json:"conflictResolved,omitempty"`
	Error            error          `json:"-"`
	ErrorMessage     string         `json:"error,omitempty"`
}

type BatchUploadOptions struct {
	ParentNodeID    string
	AutoResolveRoot bool
	ConflictPolicy  ConflictPolicy
	Recursive       bool
	MediaOnly       bool
	DryRun          bool
	ContinueOnError bool
	Concurrency     int
	Progress        ProgressFunc
}

type BatchDownloadOptions struct {
	DestDir         string
	OwnerID         string
	ContinueOnError bool
	Concurrency     int
	Progress        ProgressFunc
}

type localFileEntry struct {
	path string
	name string
	size int64
}

func (c *Client) UploadBatch(ctx context.Context, paths []string, opts BatchUploadOptions) ([]TransferResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := normalizeConflictPolicy(opts.ConflictPolicy); err != nil {
		return nil, err
	}

	files, err := collectUploadFiles(paths, opts.Recursive, opts.MediaOnly)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	if opts.DryRun {
		results := make([]TransferResult, len(files))
		for i, f := range files {
			results[i] = TransferResult{
				Status:    TransferSkipped,
				Name:      f.name,
				LocalPath: f.path,
				Bytes:     f.size,
			}
			if opts.Progress != nil {
				opts.Progress(TransferProgress{
					SourcePath:       f.path,
					Name:             f.name,
					BytesTransferred: 0,
					TotalBytes:       f.size,
					Done:             i + 1,
					TotalItems:       len(files),
				})
			}
		}
		return results, nil
	}

	parentID := strings.TrimSpace(opts.ParentNodeID)
	if parentID == "" && opts.AutoResolveRoot {
		root, rootErr := c.GetRoot(ctx)
		if rootErr != nil {
			return nil, rootErr
		}
		parentID = root.ID
	}

	workerCount := opts.Concurrency
	if workerCount <= 0 {
		workerCount = cap(c.uploadSem)
		if workerCount <= 0 {
			workerCount = DefaultUploadConcurrency
		}
	}
	if workerCount > len(files) {
		workerCount = len(files)
	}

	return runBatchWorkers(ctx, len(files), workerCount, opts.ContinueOnError, "/cdproxy/nodes", func(workCtx context.Context, idx int, doneCount *atomic.Int32) (TransferResult, error) {
		f := files[idx]
		var itemProgress ProgressFunc
		if opts.Progress != nil {
			itemProgress = func(p TransferProgress) {
				p.Done = int(doneCount.Load())
				p.TotalItems = len(files)
				opts.Progress(p)
			}
		}
		res, upErr := c.UploadFile(workCtx, f.path, UploadOptions{
			Name:           f.name,
			ParentNodeID:   parentID,
			ConflictPolicy: opts.ConflictPolicy,
			Progress:       itemProgress,
		})
		doneNow := int(doneCount.Add(1))
		if opts.Progress != nil {
			opts.Progress(TransferProgress{
				SourcePath:       f.path,
				Name:             f.name,
				BytesTransferred: f.size,
				TotalBytes:       f.size,
				Done:             doneNow,
				TotalItems:       len(files),
			})
		}
		if res == nil {
			return *failedTransferResult(f.name, f.path, 0, "", upErr), upErr
		}
		return *res, upErr
	})
}

func (c *Client) DownloadBatch(ctx context.Context, nodeIDs []string, opts BatchDownloadOptions) ([]TransferResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cleanedIDs := make([]string, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			cleanedIDs = append(cleanedIDs, trimmed)
		}
	}
	if len(cleanedIDs) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(opts.DestDir) == "" {
		return nil, &ConfigError{Field: "dest_dir", Message: "download destination directory is required"}
	}

	ownerID := strings.TrimSpace(opts.OwnerID)
	if ownerID == "" {
		root, err := c.GetRoot(ctx)
		if err != nil {
			return nil, err
		}
		ownerID = strings.TrimSpace(root.OwnerID)
	}

	workerCount := opts.Concurrency
	if workerCount <= 0 {
		workerCount = cap(c.metaSem)
		if workerCount <= 0 {
			workerCount = DefaultMetadataConcurrency
		}
	}
	if workerCount > len(cleanedIDs) {
		workerCount = len(cleanedIDs)
	}

	return runBatchWorkers(ctx, len(cleanedIDs), workerCount, opts.ContinueOnError, "/nodes/contentRedirection", func(workCtx context.Context, idx int, doneCount *atomic.Int32) (TransferResult, error) {
		nodeID := cleanedIDs[idx]
		var itemProgress ProgressFunc
		if opts.Progress != nil {
			itemProgress = func(p TransferProgress) {
				p.Done = int(doneCount.Load())
				p.TotalItems = len(cleanedIDs)
				opts.Progress(p)
			}
		}
		res, dlErr := c.Download(workCtx, DownloadRequest{
			NodeID:   nodeID,
			OwnerID:  ownerID,
			DestDir:  opts.DestDir,
			Progress: itemProgress,
		})
		doneNow := int(doneCount.Add(1))
		if opts.Progress != nil && res != nil {
			opts.Progress(TransferProgress{
				SourcePath:       res.LocalPath,
				Name:             res.Name,
				BytesTransferred: res.Bytes,
				TotalBytes:       res.Bytes,
				Done:             doneNow,
				TotalItems:       len(cleanedIDs),
			})
		}
		if res == nil {
			return *failedDownloadResult(nodeID, "", 0, "", dlErr), dlErr
		}
		return *res, dlErr
	})
}

func runBatchWorkers(
	ctx context.Context,
	totalItems, workerCount int,
	continueOnError bool,
	endpoint string,
	fn func(workCtx context.Context, idx int, doneCount *atomic.Int32) (TransferResult, error),
) ([]TransferResult, error) {
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]TransferResult, totalItems)
	errs := make([]error, totalItems)

	var nextIdx atomic.Int32
	var doneCount atomic.Int32
	var wg sync.WaitGroup

	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				idx := int(nextIdx.Add(1) - 1)
				if idx >= totalItems {
					return
				}
				if err := workCtx.Err(); err != nil {
					results[idx] = TransferResult{
						Status:       TransferFailed,
						Error:        err,
						ErrorMessage: Redact(err.Error()),
					}
					errs[idx] = err
					continue
				}
				res, err := fn(workCtx, idx, &doneCount)
				results[idx] = res
				errs[idx] = err
				if err != nil && !continueOnError {
					cancel()
				}
			}
		}()
	}

	wg.Wait()

	var firstErr error
	var failCount int
	var warnings []string
	for i, err := range errs {
		if err == nil {
			continue
		}
		failCount++
		if firstErr == nil {
			firstErr = err
		}
		label := results[i].Name
		if label == "" {
			label = results[i].NodeID
		}
		warnings = append(warnings, fmt.Sprintf("%s: %s", label, Redact(err.Error())))
	}

	if failCount == 0 {
		return results, nil
	}
	if !continueOnError {
		return results, firstErr
	}
	return results, &PartialResultError{
		Endpoint: endpoint,
		Message:  fmt.Sprintf("%d of %d transfers failed or were unconfirmed", failCount, totalItems),
		Warnings: warnings,
		Cause:    firstErr,
	}
}

func collectUploadFiles(paths []string, recursive, mediaOnly bool) ([]localFileEntry, error) {
	seen := make(map[string]bool)
	var files []localFileEntry

	for _, rawPath := range paths {
		p := strings.TrimSpace(rawPath)
		if p == "" {
			continue
		}
		cleaned := filepath.Clean(p)
		info, err := os.Stat(cleaned)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			if mediaOnly && !IsMediaFileName(info.Name()) {
				continue
			}
			if !seen[cleaned] {
				seen[cleaned] = true
				files = append(files, localFileEntry{
					path: cleaned,
					name: info.Name(),
					size: info.Size(),
				})
			}
			continue
		}

		if recursive {
			walkErr := filepath.WalkDir(cleaned, func(entryPath string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					if entryPath != cleaned && strings.HasPrefix(d.Name(), ".") {
						return filepath.SkipDir
					}
					return nil
				}
				if strings.HasPrefix(d.Name(), ".") || !d.Type().IsRegular() {
					return nil
				}
				if mediaOnly && !IsMediaFileName(d.Name()) {
					return nil
				}
				fi, statErr := d.Info()
				if statErr != nil {
					return statErr
				}
				normEntry := filepath.Clean(entryPath)
				if !seen[normEntry] {
					seen[normEntry] = true
					files = append(files, localFileEntry{
						path: normEntry,
						name: fi.Name(),
						size: fi.Size(),
					})
				}
				return nil
			})
			if walkErr != nil {
				return nil, walkErr
			}
		} else {
			dirEntries, readErr := os.ReadDir(cleaned)
			if readErr != nil {
				return nil, readErr
			}
			for _, d := range dirEntries {
				if d.IsDir() || strings.HasPrefix(d.Name(), ".") || !d.Type().IsRegular() {
					continue
				}
				if mediaOnly && !IsMediaFileName(d.Name()) {
					continue
				}
				fi, statErr := d.Info()
				if statErr != nil {
					return nil, statErr
				}
				entryPath := filepath.Join(cleaned, d.Name())
				if !seen[entryPath] {
					seen[entryPath] = true
					files = append(files, localFileEntry{
						path: entryPath,
						name: fi.Name(),
						size: fi.Size(),
					})
				}
			}
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].path < files[j].path
	})
	return files, nil
}

var supportedMediaExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
	".heic": true,
	".heif": true,
	".avif": true,
	".bmp":  true,
	".tif":  true,
	".tiff": true,
	".dng":  true,
	".cr2":  true,
	".cr3":  true,
	".nef":  true,
	".arw":  true,
	".orf":  true,
	".rw2":  true,
	".raf":  true,
	".mp4":  true,
	".mov":  true,
	".m4v":  true,
	".avi":  true,
	".mkv":  true,
	".webm": true,
	".3gp":  true,
	".mts":  true,
	".m2ts": true,
}

func IsMediaFileName(name string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
	return supportedMediaExtensions[ext]
}
