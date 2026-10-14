package aphotos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultMetadataConcurrency = 4
	DefaultUploadConcurrency   = 2
	DefaultMaxRetries          = 3
	DefaultBaseBackoff         = 100 * time.Millisecond
	DefaultMaxBackoff          = 5 * time.Second
	DefaultMaxRetryAfter       = 60 * time.Second
	defaultUserAgent           = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

type Options struct {
	Region                 Region
	Cookies                map[string]string
	HTTPClient             *http.Client
	Transport              http.RoundTripper
	DriveURL               string
	UploadURL              string
	ThumbnailURL           string
	UserAgent              string
	MaxMetadataConcurrency int
	MaxUploadConcurrency   int
	MaxRetries             int
	DisableRetries         bool
	BaseBackoff            time.Duration
	MaxBackoff             time.Duration
	MaxRetryAfter          time.Duration
}

type Client struct {
	httpClient    *http.Client
	regionCfg     RegionConfig
	cookies       map[string]string
	cookieHeader  string
	sessionID     string
	userAgent     string
	metaSem       chan struct{}
	uploadSem     chan struct{}
	maxRetries    int
	baseBackoff   time.Duration
	maxBackoff    time.Duration
	maxRetryAfter time.Duration
	closeOnce     sync.Once
	closed        chan struct{}
}

func NewClient(opts Options) (*Client, error) {
	normCookies, err := normalizeAndValidateCookies(opts.Cookies)
	if err != nil {
		return nil, err
	}

	region := opts.Region
	if strings.TrimSpace(string(region)) == "" {
		inferred, inferErr := InferRegionFromCookies(normCookies)
		if inferErr != nil {
			return nil, inferErr
		}
		region = inferred
	}

	regionCfg, err := ResolveRegion(region)
	if err != nil {
		return nil, err
	}

	for _, reqCookie := range regionCfg.RequiredCookies {
		if strings.TrimSpace(normCookies[reqCookie]) == "" {
			return nil, &ConfigError{
				Field:   "cookies",
				Message: fmt.Sprintf("missing required cookie %q for region %q", reqCookie, regionCfg.Region),
			}
		}
	}

	if opts.DriveURL != "" {
		u, uErr := validateOverrideURL("drive_url", opts.DriveURL)
		if uErr != nil {
			return nil, uErr
		}
		regionCfg.DriveURL = u
	}
	if opts.UploadURL != "" {
		u, uErr := validateOverrideURL("upload_url", opts.UploadURL)
		if uErr != nil {
			return nil, uErr
		}
		regionCfg.UploadURL = u
	}
	if opts.ThumbnailURL != "" {
		u, uErr := validateOverrideURL("thumbnail_url", opts.ThumbnailURL)
		if uErr != nil {
			return nil, uErr
		}
		regionCfg.ThumbnailURL = u
	}

	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	} else {
		cloned := *hc
		hc = &cloned
	}
	if opts.Transport != nil {
		hc.Transport = opts.Transport
	}

	ua := strings.TrimSpace(opts.UserAgent)
	if ua == "" {
		ua = defaultUserAgent
	}

	metaConc := opts.MaxMetadataConcurrency
	if metaConc <= 0 {
		metaConc = DefaultMetadataConcurrency
	}
	uploadConc := opts.MaxUploadConcurrency
	if uploadConc <= 0 {
		uploadConc = DefaultUploadConcurrency
	}

	maxRetries := opts.MaxRetries
	if opts.DisableRetries || maxRetries < 0 {
		maxRetries = 0
	} else if maxRetries == 0 {
		maxRetries = DefaultMaxRetries
	}

	baseBackoff := opts.BaseBackoff
	if baseBackoff <= 0 {
		baseBackoff = DefaultBaseBackoff
	}
	maxBackoff := opts.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = DefaultMaxBackoff
	}
	maxRetryAfter := opts.MaxRetryAfter
	if maxRetryAfter <= 0 {
		maxRetryAfter = DefaultMaxRetryAfter
	}

	return &Client{
		httpClient:    hc,
		regionCfg:     regionCfg,
		cookies:       normCookies,
		cookieHeader:  formatCookieHeader(normCookies, regionCfg.RequiredCookies),
		sessionID:     normCookies["session-id"],
		userAgent:     ua,
		metaSem:       make(chan struct{}, metaConc),
		uploadSem:     make(chan struct{}, uploadConc),
		maxRetries:    maxRetries,
		baseBackoff:   baseBackoff,
		maxBackoff:    maxBackoff,
		maxRetryAfter: maxRetryAfter,
		closed:        make(chan struct{}),
	}, nil
}

func (c *Client) RegionConfig() RegionConfig {
	rc := c.regionCfg
	rc.RequiredCookies = append([]string(nil), c.regionCfg.RequiredCookies...)
	return rc
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		close(c.closed)
		if c.httpClient != nil {
			c.httpClient.CloseIdleConnections()
		}
	})
	return nil
}

func normalizeAndValidateCookies(raw map[string]string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, &ConfigError{
			Field:   "cookies",
			Message: "authentication cookies are required",
		}
	}
	norm := make(map[string]string, len(raw))
	for k, v := range raw {
		val := strings.TrimSpace(v)
		if val == "" {
			continue
		}
		ck := canonicalCookieKey(k)
		if existing, exists := norm[ck]; exists && existing != val {
			return nil, &ConfigError{
				Field:   "cookies",
				Message: fmt.Sprintf("conflicting values for cookie alias %q", ck),
			}
		}
		norm[ck] = val
	}
	if strings.TrimSpace(norm["session-id"]) == "" {
		return nil, &ConfigError{
			Field:   "cookies",
			Message: "missing required cookie \"session-id\"",
		}
	}
	return norm, nil
}

func formatCookieHeader(cookies map[string]string, requiredOrder []string) string {
	seen := make(map[string]bool, len(cookies))
	parts := make([]string, 0, len(cookies))
	for _, k := range requiredOrder {
		if v, ok := cookies[k]; ok && v != "" {
			parts = append(parts, k+"="+v)
			seen[k] = true
		}
	}
	extra := make([]string, 0, len(cookies))
	for k, v := range cookies {
		if !seen[k] && v != "" {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		parts = append(parts, k+"="+cookies[k])
	}
	return strings.Join(parts, "; ")
}

func (c *Client) acquireSem(ctx context.Context, sem chan struct{}) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, errors.New("aphotos: client is closed")
	case sem <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() { <-sem })
		}, nil
	}
}

func (c *Client) doReadJSON(ctx context.Context, path string, query url.Values, out any) error {
	normPath := ensureLeadingSlash(path)
	fullURL := c.regionCfg.DriveURL + normPath
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		release, err := c.acquireSem(ctx, c.metaSem)
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, http.NoBody)
		if err != nil {
			release()
			return &ConfigError{
				Field:   "endpoint",
				Message: "failed to construct HTTP request",
				Cause:   err,
			}
		}
		c.setCommonHeaders(req)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			release()
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			lastErr = &TransientNetworkError{
				Endpoint: normPath,
				Message:  Redact(err.Error()),
				Cause:    err,
			}
			if attempt < c.maxRetries {
				if wErr := c.waitRetry(ctx, attempt, 0); wErr != nil {
					return wErr
				}
				continue
			}
			return lastErr
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		release()

		if readErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			lastErr = &TransientNetworkError{
				StatusCode: resp.StatusCode,
				Endpoint:   normPath,
				RequestID:  extractRequestID(resp.Header),
				Message:    Redact(readErr.Error()),
				Cause:      readErr,
			}
			if attempt < c.maxRetries {
				if wErr := c.waitRetry(ctx, attempt, 0); wErr != nil {
					return wErr
				}
				continue
			}
			return lastErr
		}

		if resp.StatusCode >= 400 {
			retryAfter := parseRetryAfter(resp.Header)
			httpErr := classifyHTTPError(normPath, resp, body, retryAfter)
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
				lastErr = httpErr
				if attempt < c.maxRetries {
					if wErr := c.waitRetry(ctx, attempt, retryAfter); wErr != nil {
						return wErr
					}
					continue
				}
				return lastErr
			}
			return httpErr
		}

		if out != nil && resp.StatusCode != http.StatusNoContent {
			if err := json.Unmarshal(body, out); err != nil {
				return &RemoteProtocolError{
					StatusCode: resp.StatusCode,
					Endpoint:   normPath,
					RequestID:  extractRequestID(resp.Header),
					Message:    "invalid JSON in response",
					Cause:      err,
				}
			}
		}
		return nil
	}

	return lastErr
}

func (c *Client) DoMutateJSON(ctx context.Context, method, path string, query url.Values, payload, out any) error {
	return c.doMutateJSON(ctx, method, path, query, payload, out)
}

func (c *Client) doMutateJSON(ctx context.Context, method, path string, query url.Values, payload, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	normPath := ensureLeadingSlash(path)
	fullURL := c.regionCfg.DriveURL + normPath
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	var bodyReader io.Reader = http.NoBody
	var contentLen int64
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return &ConfigError{
				Field:   "payload",
				Message: "failed to marshal JSON request body",
				Cause:   err,
			}
		}
		bodyReader = bytes.NewReader(encoded)
		contentLen = int64(len(encoded))
	}

	release, err := c.acquireSem(ctx, c.metaSem)
	if err != nil {
		return err
	}
	defer release()

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return &ConfigError{
			Field:   "endpoint",
			Message: "failed to construct HTTP request",
			Cause:   err,
		}
	}
	c.setCommonHeaders(req)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = contentLen
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return &TransientNetworkError{
			Endpoint: normPath,
			Message:  Redact(err.Error()),
			Cause:    err,
		}
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return &TransientNetworkError{
			StatusCode: resp.StatusCode,
			Endpoint:   normPath,
			RequestID:  extractRequestID(resp.Header),
			Message:    Redact(readErr.Error()),
			Cause:      readErr,
		}
	}

	if resp.StatusCode >= 400 {
		return classifyHTTPError(normPath, resp, body, parseRetryAfter(resp.Header))
	}

	if out != nil && resp.StatusCode != http.StatusNoContent && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return &RemoteProtocolError{
				StatusCode: resp.StatusCode,
				Endpoint:   normPath,
				RequestID:  extractRequestID(resp.Header),
				Message:    "invalid JSON in response",
				Cause:      err,
			}
		}
	}
	return nil
}

func (c *Client) waitRetry(ctx context.Context, attempt int, retryAfter time.Duration) error {
	wait := c.computeBackoff(attempt)
	if retryAfter > 0 {
		capped := min(retryAfter, c.maxRetryAfter)
		if capped > wait {
			wait = capped
		}
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return errors.New("aphotos: client is closed")
	case <-timer.C:
		return nil
	}
}

func (c *Client) computeBackoff(attempt int) time.Duration {
	shift := min(max(attempt, 0), 10)
	delay := min(c.baseBackoff*(1<<uint(shift)), c.maxBackoff)
	if delay <= 1 {
		return delay
	}
	half := int64(delay) / 2
	jitter := rand.Int64N(half + 1)
	return time.Duration(half + jitter)
}

func parseRetryAfter(h http.Header) time.Duration {
	if h == nil {
		return 0
	}
	val := strings.TrimSpace(h.Get("Retry-After"))
	if val == "" {
		for k, vals := range h {
			if strings.EqualFold(k, "retry-after") && len(vals) > 0 {
				val = strings.TrimSpace(vals[0])
				break
			}
		}
	}
	if val == "" {
		return 0
	}
	if secs, err := strconv.Atoi(val); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(val); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func (c *Client) setCommonHeaders(req *http.Request) {
	req.Header.Set("x-amzn-sessionid", c.sessionID)
	req.Header.Set("Cookie", c.cookieHeader)
	req.Header.Set("User-Agent", c.userAgent)
}

func ensureLeadingSlash(p string) string {
	if p == "" || p[0] != '/' {
		return "/" + p
	}
	return p
}
