package aphotos

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type DownloadRequest struct {
	NodeID       string
	OwnerID      string
	DestDir      string
	FileName     string
	ExpectedSize int64
	ExpectedMD5  string
	Progress     ProgressFunc
}

func (c *Client) Download(ctx context.Context, req DownloadRequest) (*TransferResult, error) {
	if err := ctx.Err(); err != nil {
		return failedDownloadResult(req.NodeID, req.FileName, 0, "", err), err
	}

	nodeID := strings.TrimSpace(req.NodeID)
	if nodeID == "" {
		err := &ConfigError{Field: "node_id", Message: "download node ID is required"}
		return failedDownloadResult("", req.FileName, 0, "", err), err
	}
	destDir := strings.TrimSpace(req.DestDir)
	if destDir == "" {
		err := &ConfigError{Field: "dest_dir", Message: "download destination directory is required"}
		return failedDownloadResult(nodeID, req.FileName, 0, "", err), err
	}

	cleanDestDir := filepath.Clean(destDir)
	if err := os.MkdirAll(cleanDestDir, 0o755); err != nil {
		return failedDownloadResult(nodeID, req.FileName, 0, "", err), err
	}

	ownerID := strings.TrimSpace(req.OwnerID)
	if ownerID == "" {
		root, err := c.GetRoot(ctx)
		if err != nil {
			return failedDownloadResult(nodeID, req.FileName, 0, "", err), err
		}
		ownerID = strings.TrimSpace(root.OwnerID)
		if ownerID == "" {
			err := &RemoteProtocolError{
				Endpoint: "/nodes",
				Message:  "root node response missing ownerId required for download",
			}
			return failedDownloadResult(nodeID, req.FileName, 0, "", err), err
		}
	}

	endpointPath := fmt.Sprintf("/nodes/%s/contentRedirection", url.PathEscape(nodeID))
	q := make(url.Values, 2)
	q.Set("querySuffix", "?download=true")
	q.Set("ownerId", ownerID)
	fullURL := c.regionCfg.DriveURL + endpointPath + "?" + q.Encode()

	dlClient := c.downloadHTTPClient()

	var lastResult *TransferResult
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return failedDownloadResult(nodeID, req.FileName, 0, "", err), err
		}

		res, retryable, err := c.downloadAttempt(ctx, dlClient, endpointPath, fullURL, cleanDestDir, nodeID, req)
		if err == nil {
			return res, nil
		}
		lastResult = res
		lastErr = err
		if !retryable || attempt >= c.maxRetries {
			return lastResult, lastErr
		}
		var retryAfter time.Duration
		var rlErr *RateLimitError
		if errors.As(err, &rlErr) {
			retryAfter = rlErr.RetryAfter
		}
		if wErr := c.waitRetry(ctx, attempt, retryAfter); wErr != nil {
			return failedDownloadResult(nodeID, req.FileName, 0, "", wErr), wErr
		}
	}

	return lastResult, lastErr
}

func (c *Client) downloadHTTPClient() *http.Client {
	cloned := *c.httpClient
	origCheck := c.httpClient.CheckRedirect
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if len(via) > 0 && !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
			req.Header.Del("Cookie")
			req.Header.Del("x-amzn-sessionid")
			req.Header.Del("Authorization")
		}
		if origCheck != nil {
			return origCheck(req, via)
		}
		return nil
	}
	return &cloned
}

func (c *Client) downloadAttempt(
	ctx context.Context,
	dlClient *http.Client,
	endpointPath, fullURL, cleanDestDir, nodeID string,
	req DownloadRequest,
) (*TransferResult, bool, error) {
	release, err := c.acquireSem(ctx, c.metaSem)
	if err != nil {
		return failedDownloadResult(nodeID, req.FileName, 0, "", err), false, err
	}
	defer release()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, http.NoBody)
	if err != nil {
		cfgErr := &ConfigError{Field: "endpoint", Message: "failed to construct download request", Cause: err}
		return failedDownloadResult(nodeID, req.FileName, 0, "", cfgErr), false, cfgErr
	}
	c.setCommonHeaders(httpReq)

	resp, err := dlClient.Do(httpReq)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return failedDownloadResult(nodeID, req.FileName, 0, "", ctxErr), false, ctxErr
		}
		netErr := &TransientNetworkError{
			Endpoint: endpointPath,
			Message:  Redact(err.Error()),
			Cause:    err,
		}
		return failedDownloadResult(nodeID, req.FileName, 0, "", netErr), true, netErr
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		retryAfter := parseRetryAfter(resp.Header)
		httpErr := classifyHTTPError(endpointPath, resp, errBody, retryAfter)
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return failedDownloadResult(nodeID, req.FileName, 0, "", httpErr), retryable, httpErr
	}
	if resp.StatusCode != http.StatusOK {
		protoErr := &RemoteProtocolError{
			StatusCode: resp.StatusCode,
			Endpoint:   endpointPath,
			RequestID:  extractRequestID(resp.Header),
			Message:    fmt.Sprintf("unexpected HTTP status %d on download", resp.StatusCode),
		}
		return failedDownloadResult(nodeID, req.FileName, 0, "", protoErr), false, protoErr
	}

	safeName := resolveDownloadFileName(req.FileName, resp.Header.Get("Content-Disposition"), nodeID)
	finalPath, err := joinWithinDir(cleanDestDir, safeName)
	if err != nil {
		return failedDownloadResult(nodeID, safeName, 0, "", err), false, err
	}

	totalBytes := req.ExpectedSize
	var contentLen int64 = -1
	if clStr := strings.TrimSpace(resp.Header.Get("Content-Length")); clStr != "" {
		if parsedCL, parseErr := strconv.ParseInt(clStr, 10, 64); parseErr == nil && parsedCL >= 0 {
			contentLen = parsedCL
			if totalBytes <= 0 {
				totalBytes = parsedCL
			}
		}
	}

	tmpFile, err := os.CreateTemp(cleanDestDir, ".aphotos-dl-*.part")
	if err != nil {
		return failedDownloadResult(nodeID, safeName, 0, "", err), false, err
	}
	tmpPath := tmpFile.Name()
	cleanupTmp := func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}

	hasher := md5.New()
	stream := &uploadStreamReader{
		r:          resp.Body,
		hasher:     hasher,
		progress:   req.Progress,
		sourcePath: finalPath,
		name:       safeName,
		totalBytes: totalBytes,
	}

	if totalBytes == 0 && req.Progress != nil {
		req.Progress(TransferProgress{
			SourcePath:       finalPath,
			Name:             safeName,
			BytesTransferred: 0,
			TotalBytes:       0,
		})
	}

	written, copyErr := io.Copy(tmpFile, stream)
	computedMD5 := hex.EncodeToString(hasher.Sum(nil))
	if copyErr != nil {
		cleanupTmp()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return failedDownloadResult(nodeID, safeName, written, computedMD5, ctxErr), false, ctxErr
		}
		netErr := &TransientNetworkError{
			StatusCode: resp.StatusCode,
			Endpoint:   endpointPath,
			RequestID:  extractRequestID(resp.Header),
			Message:    Redact(copyErr.Error()),
			Cause:      copyErr,
		}
		return failedDownloadResult(nodeID, safeName, written, computedMD5, netErr), true, netErr
	}

	if err := ctx.Err(); err != nil {
		cleanupTmp()
		return failedDownloadResult(nodeID, safeName, written, computedMD5, err), false, err
	}

	if contentLen >= 0 && written != contentLen {
		cleanupTmp()
		truncErr := &TransientNetworkError{
			StatusCode: resp.StatusCode,
			Endpoint:   endpointPath,
			RequestID:  extractRequestID(resp.Header),
			Message:    fmt.Sprintf("truncated download body: got %d bytes, Content-Length was %d", written, contentLen),
		}
		return failedDownloadResult(nodeID, safeName, written, computedMD5, truncErr), true, truncErr
	}

	if req.ExpectedSize > 0 && written != req.ExpectedSize {
		cleanupTmp()
		sizeErr := &RemoteProtocolError{
			StatusCode: resp.StatusCode,
			Endpoint:   endpointPath,
			RequestID:  extractRequestID(resp.Header),
			Message:    fmt.Sprintf("downloaded size mismatch: got %d bytes, expected %d", written, req.ExpectedSize),
		}
		return failedDownloadResult(nodeID, safeName, written, computedMD5, sizeErr), false, sizeErr
	}

	if expectedMD5 := strings.TrimSpace(req.ExpectedMD5); expectedMD5 != "" {
		if !strings.EqualFold(computedMD5, expectedMD5) {
			cleanupTmp()
			md5Err := &RemoteProtocolError{
				StatusCode: resp.StatusCode,
				Endpoint:   endpointPath,
				RequestID:  extractRequestID(resp.Header),
				Message:    fmt.Sprintf("downloaded MD5 mismatch: got %s, expected %s", computedMD5, expectedMD5),
			}
			return failedDownloadResult(nodeID, safeName, written, computedMD5, md5Err), false, md5Err
		}
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return failedDownloadResult(nodeID, safeName, written, computedMD5, err), false, err
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return failedDownloadResult(nodeID, safeName, written, computedMD5, err), false, err
	}

	return &TransferResult{
		Status:    TransferConfirmed,
		NodeID:    nodeID,
		Name:      safeName,
		LocalPath: finalPath,
		Bytes:     written,
		MD5:       computedMD5,
	}, false, nil
}

func resolveDownloadFileName(explicitName, contentDisposition, nodeID string) string {
	for _, candidate := range []string{
		strings.TrimSpace(explicitName),
		parseDispositionFileName(contentDisposition),
		strings.TrimSpace(nodeID),
	} {
		if safe := sanitizeSingleFileName(candidate); safe != "" {
			return safe
		}
	}
	return "download.bin"
}

func parseDispositionFileName(headerVal string) string {
	h := strings.TrimSpace(headerVal)
	if h == "" {
		return ""
	}
	if _, params, err := mime.ParseMediaType(h); err == nil {
		if fn := strings.TrimSpace(params["filename"]); fn != "" {
			return fn
		}
		if fnStar := strings.TrimSpace(params["filename*"]); fnStar != "" {
			return fnStar
		}
	}
	for _, part := range strings.Split(h, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		val := strings.Trim(strings.TrimSpace(kv[1]), `"'`)
		if key == "filename*" {
			if idx := strings.Index(val, "''"); idx >= 0 {
				val = val[idx+2:]
			}
			if decoded, err := url.PathUnescape(val); err == nil && strings.TrimSpace(decoded) != "" {
				return decoded
			}
		}
		if key == "filename" && val != "" {
			return val
		}
	}
	return ""
}

func sanitizeSingleFileName(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	s = strings.ReplaceAll(s, `\`, `/`)
	if len(s) >= 2 && ((s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z')) && s[1] == ':' {
		s = s[2:]
	}
	parts := strings.Split(s, "/")
	base := ""
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		if p != "" && p != "." && p != ".." {
			base = p
			break
		}
	}
	base = filepath.Base(base)
	if base == "" || base == "." || base == ".." || base == "/" || base == `\` {
		return ""
	}
	return base
}

func joinWithinDir(cleanDestDir, safeName string) (string, error) {
	if safeName == "" || safeName == "." || safeName == ".." || strings.ContainsAny(safeName, `/\`) {
		return "", &ConfigError{
			Field:   "file_name",
			Message: fmt.Sprintf("unsafe download file name %q", safeName),
		}
	}
	joined := filepath.Join(cleanDestDir, safeName)
	rel, err := filepath.Rel(cleanDestDir, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.Dir(joined) != cleanDestDir {
		return "", &ConfigError{
			Field:   "file_name",
			Message: fmt.Sprintf("download path %q escapes destination directory", safeName),
		}
	}
	return joined, nil
}

func failedDownloadResult(nodeID, name string, nBytes int64, md5Hex string, err error) *TransferResult {
	msg := ""
	if err != nil {
		msg = Redact(err.Error())
	}
	return &TransferResult{
		Status:       TransferFailed,
		NodeID:       nodeID,
		Name:         name,
		Bytes:        nBytes,
		MD5:          md5Hex,
		Error:        err,
		ErrorMessage: msg,
	}
}
