package aphotos

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type UploadRequest struct {
	Name            string
	ParentNodeID    string
	AutoResolveRoot bool
	Body            io.Reader
	Size            int64
	ContentType     string
	LocalPath       string
	ConflictPolicy  ConflictPolicy
	Progress        ProgressFunc
}

type UploadOptions struct {
	Name            string
	ParentNodeID    string
	AutoResolveRoot bool
	ContentType     string
	ConflictPolicy  ConflictPolicy
	Progress        ProgressFunc
}

type uploadStreamReader struct {
	r          io.Reader
	hasher     hash.Hash
	progress   ProgressFunc
	sourcePath string
	name       string
	totalBytes int64
	readBytes  int64
}

func (u *uploadStreamReader) Read(p []byte) (int, error) {
	n, err := u.r.Read(p)
	if n > 0 {
		_, _ = u.hasher.Write(p[:n])
		u.readBytes += int64(n)
		if u.progress != nil {
			u.progress(TransferProgress{
				SourcePath:       u.sourcePath,
				Name:             u.name,
				BytesTransferred: u.readBytes,
				TotalBytes:       u.totalBytes,
			})
		}
	}
	return n, err
}

func (c *Client) UploadFile(ctx context.Context, localPath string, opts UploadOptions) (*TransferResult, error) {
	cleaned := strings.TrimSpace(localPath)
	if cleaned == "" {
		err := &ConfigError{Field: "local_path", Message: "upload local file path is required"}
		return &TransferResult{
			Status:       TransferFailed,
			Error:        err,
			ErrorMessage: err.Error(),
		}, err
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		return &TransferResult{
			Status:       TransferFailed,
			LocalPath:    cleaned,
			Error:        err,
			ErrorMessage: Redact(err.Error()),
		}, err
	}
	if info.IsDir() {
		dirErr := &ConfigError{Field: "local_path", Message: fmt.Sprintf("path %q is a directory", cleaned)}
		return &TransferResult{
			Status:       TransferFailed,
			LocalPath:    cleaned,
			Error:        dirErr,
			ErrorMessage: dirErr.Error(),
		}, dirErr
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = filepath.Base(cleaned)
	}
	return c.Upload(ctx, UploadRequest{
		Name:            name,
		ParentNodeID:    opts.ParentNodeID,
		AutoResolveRoot: opts.AutoResolveRoot,
		Size:            info.Size(),
		ContentType:     opts.ContentType,
		LocalPath:       cleaned,
		ConflictPolicy:  opts.ConflictPolicy,
		Progress:        opts.Progress,
	})
}

func (c *Client) Upload(ctx context.Context, req UploadRequest) (*TransferResult, error) {
	if err := ctx.Err(); err != nil {
		return failedTransferResult(req.Name, req.LocalPath, 0, "", err), err
	}

	name := strings.TrimSpace(req.Name)
	if name == "" && strings.TrimSpace(req.LocalPath) != "" {
		name = filepath.Base(strings.TrimSpace(req.LocalPath))
	}
	if name == "" {
		err := &ConfigError{Field: "name", Message: "upload file name is required"}
		return failedTransferResult(name, req.LocalPath, 0, "", err), err
	}
	if req.Body == nil && strings.TrimSpace(req.LocalPath) == "" {
		err := &ConfigError{Field: "body", Message: "upload body reader or local path is required"}
		return failedTransferResult(name, req.LocalPath, 0, "", err), err
	}

	policy, err := normalizeConflictPolicy(req.ConflictPolicy)
	if err != nil {
		return failedTransferResult(name, req.LocalPath, 0, "", err), err
	}

	parentID := strings.TrimSpace(req.ParentNodeID)
	if parentID == "" && req.AutoResolveRoot {
		root, rootErr := c.GetRoot(ctx)
		if rootErr != nil {
			return failedTransferResult(name, req.LocalPath, 0, "", rootErr), rootErr
		}
		parentID = root.ID
	}

	var seeker io.ReadSeeker
	var seekerStart int64
	if rs, ok := req.Body.(io.ReadSeeker); ok {
		if pos, seekErr := rs.Seek(0, io.SeekCurrent); seekErr == nil {
			seeker = rs
			seekerStart = pos
		}
	}

	res, err := c.uploadOnce(ctx, req, name, parentID, seeker, seekerStart, false)
	if err == nil {
		return res, nil
	}

	var conflictErr *ConflictError
	if !errors.As(err, &conflictErr) {
		return res, err
	}

	switch policy {
	case ConflictFail:
		return res, err
	case ConflictSkip:
		return c.handleConflictSkip(ctx, res, conflictErr, name, parentID)
	case ConflictRename:
		return c.handleConflictRename(ctx, req, res, conflictErr, name, parentID, seeker, seekerStart)
	default:
		return res, err
	}
}

func (c *Client) uploadOnce(
	ctx context.Context,
	req UploadRequest,
	name, parentID string,
	seeker io.ReadSeeker,
	seekerStart int64,
	replay bool,
) (*TransferResult, error) {
	var bodyReader io.Reader
	var closer io.Closer
	size := req.Size

	if replay {
		switch {
		case seeker != nil:
			if _, err := seeker.Seek(seekerStart, io.SeekStart); err != nil {
				return failedTransferResult(name, req.LocalPath, 0, "", err), err
			}
			bodyReader = seeker
		case strings.TrimSpace(req.LocalPath) != "":
			f, info, err := openLocalUploadFile(req.LocalPath)
			if err != nil {
				return failedTransferResult(name, req.LocalPath, 0, "", err), err
			}
			bodyReader = f
			closer = f
			if size <= 0 {
				size = info.Size()
			}
		default:
			err := &ConflictError{
				StatusCode: http.StatusConflict,
				Endpoint:   "/cdproxy/nodes",
				Message:    "cannot rename on conflict: upload body is not replayable",
			}
			return failedTransferResult(name, req.LocalPath, 0, "", err), err
		}
	} else {
		if req.Body != nil {
			bodyReader = req.Body
		} else {
			f, info, err := openLocalUploadFile(req.LocalPath)
			if err != nil {
				return failedTransferResult(name, req.LocalPath, 0, "", err), err
			}
			bodyReader = f
			closer = f
			if size <= 0 {
				size = info.Size()
			}
		}
	}
	if closer != nil {
		defer func() { _ = closer.Close() }()
	}

	release, err := c.acquireSem(ctx, c.uploadSem)
	if err != nil {
		return failedTransferResult(name, req.LocalPath, 0, "", err), err
	}
	defer release()

	u, err := url.Parse(c.regionCfg.UploadURL)
	if err != nil {
		cfgErr := &ConfigError{Field: "upload_url", Message: "invalid upload URL", Cause: err}
		return failedTransferResult(name, req.LocalPath, 0, "", cfgErr), cfgErr
	}
	q := u.Query()
	q.Set("name", name)
	q.Set("kind", string(NodeKindFile))
	if parentID != "" {
		q.Set("parentNodeId", parentID)
	}
	u.RawQuery = q.Encode()

	stream := &uploadStreamReader{
		r:          bodyReader,
		hasher:     md5.New(),
		progress:   req.Progress,
		sourcePath: req.LocalPath,
		name:       name,
		totalBytes: size,
	}

	if size == 0 && req.Progress != nil {
		req.Progress(TransferProgress{
			SourcePath:       req.LocalPath,
			Name:             name,
			BytesTransferred: 0,
			TotalBytes:       0,
		})
	}

	reqBody := struct{ io.Reader }{stream}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), reqBody)
	if err != nil {
		cfgErr := &ConfigError{Field: "upload_url", Message: "failed to construct upload request", Cause: err}
		return failedTransferResult(name, req.LocalPath, 0, "", cfgErr), cfgErr
	}
	c.setCommonHeaders(httpReq)
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", detectUploadContentType(req.ContentType, name))
	if size > 0 {
		httpReq.ContentLength = size
	}

	resp, err := c.httpClient.Do(httpReq)
	if err == nil && resp != nil && resp.StatusCode >= 400 && (stream.readBytes < size || (size == 0 && stream.readBytes == 0)) {
		_, _ = io.Copy(io.Discard, stream)
	}
	computedMD5 := hex.EncodeToString(stream.hasher.Sum(nil))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return failedTransferResult(name, req.LocalPath, stream.readBytes, computedMD5, ctxErr), ctxErr
		}
		netErr := &TransientNetworkError{
			Endpoint: "/cdproxy/nodes",
			Message:  Redact(err.Error()),
			Cause:    err,
		}
		return failedTransferResult(name, req.LocalPath, stream.readBytes, computedMD5, netErr), netErr
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return failedTransferResult(name, req.LocalPath, stream.readBytes, computedMD5, ctxErr), ctxErr
		}
		netErr := &TransientNetworkError{
			StatusCode: resp.StatusCode,
			Endpoint:   "/cdproxy/nodes",
			RequestID:  extractRequestID(resp.Header),
			Message:    Redact(readErr.Error()),
			Cause:      readErr,
		}
		return failedTransferResult(name, req.LocalPath, stream.readBytes, computedMD5, netErr), netErr
	}

	if resp.StatusCode >= 400 {
		httpErr := classifyHTTPError("/cdproxy/nodes", resp, respBody, parseRetryAfter(resp.Header))
		return failedTransferResult(name, req.LocalPath, stream.readBytes, computedMD5, httpErr), httpErr
	}

	var node Node
	if len(bytesTrimSpace(respBody)) > 0 {
		if err := json.Unmarshal(respBody, &node); err != nil {
			unconfErr := &PartialResultError{
				Endpoint: "/cdproxy/nodes",
				Message:  "upload returned HTTP success but response JSON could not be verified",
				Cause:    err,
			}
			return &TransferResult{
				Status:       TransferUnconfirmed,
				Name:         name,
				LocalPath:    req.LocalPath,
				Bytes:        stream.readBytes,
				MD5:          computedMD5,
				Error:        unconfErr,
				ErrorMessage: unconfErr.Error(),
			}, unconfErr
		}
	}

	if strings.TrimSpace(node.ID) == "" {
		unconfErr := &PartialResultError{
			Endpoint: "/cdproxy/nodes",
			Message:  fmt.Sprintf("upload HTTP %d returned without verifiable node ID", resp.StatusCode),
		}
		return &TransferResult{
			Status:       TransferUnconfirmed,
			Name:         name,
			LocalPath:    req.LocalPath,
			Bytes:        stream.readBytes,
			MD5:          computedMD5,
			Error:        unconfErr,
			ErrorMessage: unconfErr.Error(),
		}, unconfErr
	}

	return &TransferResult{
		Status:    TransferConfirmed,
		NodeID:    node.ID,
		Node:      &node,
		Name:      name,
		LocalPath: req.LocalPath,
		Bytes:     stream.readBytes,
		MD5:       computedMD5,
	}, nil
}

func (c *Client) handleConflictSkip(
	ctx context.Context,
	failedRes *TransferResult,
	conflictErr *ConflictError,
	name, parentID string,
) (*TransferResult, error) {
	verifiedNode, verifyErr := c.findConflictingRemoteNode(ctx, name, parentID, failedRes.MD5, conflictErr)
	if verifyErr != nil {
		return failedRes, verifyErr
	}
	if verifiedNode == nil {
		return failedRes, conflictErr
	}
	return &TransferResult{
		Status:           TransferSkipped,
		NodeID:           verifiedNode.ID,
		Node:             verifiedNode,
		Name:             name,
		LocalPath:        failedRes.LocalPath,
		Bytes:            failedRes.Bytes,
		MD5:              failedRes.MD5,
		ConflictResolved: true,
	}, nil
}

func (c *Client) findConflictingRemoteNode(
	ctx context.Context,
	name, parentID, md5Hex string,
	conflictErr *ConflictError,
) (*Node, error) {
	params := baseQueryParams()
	params.Set("limit", "200")
	params.Set("offset", "0")

	fb := NewFilter().Kind(NodeKindFile).NameExact(name)
	if parentID != "" {
		fb.ParentID(parentID)
	}
	filterExpr, err := fb.Build()
	if err != nil {
		return nil, err
	}
	params.Set("filters", filterExpr)

	var resp nodesListResponse
	if err := c.doReadJSON(ctx, "/nodes", params, &resp); err != nil {
		return nil, err
	}

	hintNodeID := ""
	if conflictErr != nil {
		hintNodeID = strings.TrimSpace(conflictErr.ConflictingNodeID)
	}

	var nameMatchNoMD5 *Node
	var mismatchedNode *Node
	for i := range resp.Data {
		n := resp.Data[i]
		if strings.TrimSpace(n.ID) == "" {
			continue
		}
		remoteMD5 := ""
		if n.ContentProperties != nil {
			remoteMD5 = strings.TrimSpace(n.ContentProperties.MD5)
		}
		if md5Hex != "" && remoteMD5 != "" {
			if strings.EqualFold(remoteMD5, md5Hex) {
				return &n, nil
			}
			if mismatchedNode == nil || (hintNodeID != "" && n.ID == hintNodeID) {
				cp := n
				mismatchedNode = &cp
			}
			continue
		}
		if hintNodeID != "" && n.ID == hintNodeID {
			return &n, nil
		}
		if n.Name == name && nameMatchNoMD5 == nil {
			cp := n
			nameMatchNoMD5 = &cp
		}
	}

	if nameMatchNoMD5 != nil {
		return nameMatchNoMD5, nil
	}
	if mismatchedNode != nil {
		return nil, &ConflictError{
			StatusCode:        http.StatusConflict,
			Endpoint:          "/cdproxy/nodes",
			ConflictingNodeID: mismatchedNode.ID,
			Message:           fmt.Sprintf("remote node %q (%s) exists with different MD5 (%s != %s)", name, mismatchedNode.ID, mismatchedNode.ContentProperties.MD5, md5Hex),
		}
	}
	return nil, nil
}

func (c *Client) handleConflictRename(
	ctx context.Context,
	req UploadRequest,
	failedRes *TransferResult,
	conflictErr *ConflictError,
	name, parentID string,
	seeker io.ReadSeeker,
	seekerStart int64,
) (*TransferResult, error) {
	if seeker == nil && strings.TrimSpace(req.LocalPath) == "" {
		return failedRes, conflictErr
	}
	renamed := disambiguateFileName(name, 1)
	res, err := c.uploadOnce(ctx, req, renamed, parentID, seeker, seekerStart, true)
	if err != nil {
		return res, err
	}
	res.ConflictResolved = true
	return res, nil
}

func disambiguateFileName(name string, index int) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if stem == "" {
		return fmt.Sprintf("%s (%d)", name, index)
	}
	return fmt.Sprintf("%s (%d)%s", stem, index, ext)
}

func normalizeConflictPolicy(p ConflictPolicy) (ConflictPolicy, error) {
	trimmed := ConflictPolicy(strings.ToLower(strings.TrimSpace(string(p))))
	switch trimmed {
	case "", ConflictFail:
		return ConflictFail, nil
	case ConflictSkip:
		return ConflictSkip, nil
	case ConflictRename:
		return ConflictRename, nil
	default:
		return "", &ConfigError{
			Field:   "conflict_policy",
			Message: fmt.Sprintf("unsupported conflict policy %q (must be fail, skip, or rename)", p),
		}
	}
}

func detectUploadContentType(explicit, fileName string) string {
	ct := strings.TrimSpace(explicit)
	if ct != "" {
		return ct
	}
	if ext := filepath.Ext(fileName); ext != "" {
		if guessed := mime.TypeByExtension(ext); guessed != "" {
			return guessed
		}
	}
	return "application/octet-stream"
}

func openLocalUploadFile(localPath string) (*os.File, os.FileInfo, error) {
	cleaned := strings.TrimSpace(localPath)
	f, err := os.Open(cleaned)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, nil, &ConfigError{
			Field:   "local_path",
			Message: fmt.Sprintf("path %q is a directory", cleaned),
		}
	}
	return f, info, nil
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func failedTransferResult(name, localPath string, nBytes int64, md5Hex string, err error) *TransferResult {
	msg := ""
	if err != nil {
		msg = Redact(err.Error())
	}
	return &TransferResult{
		Status:       TransferFailed,
		Name:         name,
		LocalPath:    localPath,
		Bytes:        nBytes,
		MD5:          md5Hex,
		Error:        err,
		ErrorMessage: msg,
	}
}
