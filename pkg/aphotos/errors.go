package aphotos

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type ConfigError struct {
	Field   string
	Message string
	Cause   error
}

func (e *ConfigError) Error() string {
	msg := Redact(e.Message)
	if e.Field != "" {
		return fmt.Sprintf("aphotos config error (%s): %s", e.Field, msg)
	}
	return fmt.Sprintf("aphotos config error: %s", msg)
}

func (e *ConfigError) Unwrap() error {
	return e.Cause
}

type AuthenticationError struct {
	StatusCode int
	Endpoint   string
	RequestID  string
	Message    string
	Cause      error
}

func (e *AuthenticationError) Error() string {
	return formatHTTPError("authentication error", e.Endpoint, e.StatusCode, e.RequestID, e.Message)
}

func (e *AuthenticationError) Unwrap() error {
	return e.Cause
}

type PermissionError struct {
	StatusCode int
	Endpoint   string
	RequestID  string
	Message    string
	Cause      error
}

func (e *PermissionError) Error() string {
	return formatHTTPError("permission error", e.Endpoint, e.StatusCode, e.RequestID, e.Message)
}

func (e *PermissionError) Unwrap() error {
	return e.Cause
}

type RateLimitError struct {
	StatusCode int
	Endpoint   string
	RequestID  string
	RetryAfter time.Duration
	Message    string
	Cause      error
}

func (e *RateLimitError) Error() string {
	base := formatHTTPError("rate limit error", e.Endpoint, e.StatusCode, e.RequestID, e.Message)
	if e.RetryAfter > 0 {
		return fmt.Sprintf("%s (retry after %s)", base, e.RetryAfter)
	}
	return base
}

func (e *RateLimitError) Unwrap() error {
	return e.Cause
}

type InvalidFilterError struct {
	StatusCode int
	Endpoint   string
	RequestID  string
	Filter     string
	Message    string
	Cause      error
}

func (e *InvalidFilterError) Error() string {
	return formatHTTPError("invalid filter or request error", e.Endpoint, e.StatusCode, e.RequestID, e.Message)
}

func (e *InvalidFilterError) Unwrap() error {
	return e.Cause
}

type NotFoundError struct {
	StatusCode int
	Endpoint   string
	RequestID  string
	NodeID     string
	Message    string
	Cause      error
}

func (e *NotFoundError) Error() string {
	return formatHTTPError("not found error", e.Endpoint, e.StatusCode, e.RequestID, e.Message)
}

func (e *NotFoundError) Unwrap() error {
	return e.Cause
}

type ConflictError struct {
	StatusCode        int
	Endpoint          string
	RequestID         string
	ConflictingNodeID string
	Message           string
	Cause             error
}

func (e *ConflictError) Error() string {
	return formatHTTPError("conflict error", e.Endpoint, e.StatusCode, e.RequestID, e.Message)
}

func (e *ConflictError) Unwrap() error {
	return e.Cause
}

type TransientNetworkError struct {
	StatusCode int
	Endpoint   string
	RequestID  string
	RetryAfter time.Duration
	Message    string
	Cause      error
}

func (e *TransientNetworkError) Error() string {
	return formatHTTPError("transient network error", e.Endpoint, e.StatusCode, e.RequestID, e.Message)
}

func (e *TransientNetworkError) Unwrap() error {
	return e.Cause
}

type RemoteProtocolError struct {
	StatusCode int
	Endpoint   string
	RequestID  string
	Message    string
	Cause      error
}

func (e *RemoteProtocolError) Error() string {
	return formatHTTPError("remote protocol error", e.Endpoint, e.StatusCode, e.RequestID, e.Message)
}

func (e *RemoteProtocolError) Unwrap() error {
	return e.Cause
}

type PartialResultError struct {
	Endpoint string
	Message  string
	Warnings []string
	Cause    error
}

func (e *PartialResultError) Error() string {
	msg := Redact(e.Message)
	if e.Cause != nil {
		return fmt.Sprintf("aphotos partial result on %s: %s: %s", Redact(e.Endpoint), msg, Redact(e.Cause.Error()))
	}
	return fmt.Sprintf("aphotos partial result on %s: %s", Redact(e.Endpoint), msg)
}

func (e *PartialResultError) Unwrap() error {
	return e.Cause
}

func formatHTTPError(kind, endpoint string, status int, reqID, message string) string {
	ep := Redact(endpoint)
	msg := Redact(message)
	if msg == "" && status > 0 {
		msg = http.StatusText(status)
	}
	if status > 0 && reqID != "" {
		return fmt.Sprintf("aphotos %s on %s (HTTP %d, request %s): %s", kind, ep, status, Redact(reqID), msg)
	}
	if status > 0 {
		return fmt.Sprintf("aphotos %s on %s (HTTP %d): %s", kind, ep, status, msg)
	}
	return fmt.Sprintf("aphotos %s on %s: %s", kind, ep, msg)
}

var (
	reCookieKV        = regexp.MustCompile(`(?i)\b(session[-_]id|ubid[-_][a-z0-9]+|at[-_][a-z0-9]+|x-amz-Cf-Id)\s*=\s*([^\s;&"',]+)`)
	reHeaderLine      = regexp.MustCompile(`(?i)\b(x-amzn-sessionid|cookie|set-cookie|authorization)\s*:\s*([^\r\n"]+)`)
	reAtzaToken       = regexp.MustCompile(`Atza\|[^\s;&"',]+`)
	reTemplinkPath    = regexp.MustCompile(`(?i)(/cdproxy/templink/)[^\s?"']+`)
	reSignedQueryParm = regexp.MustCompile(`(?i)([?&](?:X-Amz-[A-Za-z0-9_-]+|AWSAccessKeyId|Signature|sig|token|session[-_]id)=)([^&#\s"']+)`)
)

func Redact(s string) string {
	if s == "" {
		return ""
	}
	out := reTemplinkPath.ReplaceAllString(s, "${1}[REDACTED]")
	out = reSignedQueryParm.ReplaceAllString(out, "${1}[REDACTED]")
	out = reAtzaToken.ReplaceAllString(out, "[REDACTED]")
	out = reCookieKV.ReplaceAllString(out, "${1}=[REDACTED]")
	out = reHeaderLine.ReplaceAllString(out, "${1}: [REDACTED]")
	return out
}

func extractRequestID(h http.Header) string {
	if h == nil {
		return ""
	}
	for _, key := range []string{"x-amzn-RequestId", "x-amzn-requestid", "X-Amz-Request-Id"} {
		if v := strings.TrimSpace(h.Get(key)); v != "" {
			return v
		}
	}
	for k, vals := range h {
		if strings.EqualFold(k, "x-amzn-requestid") || strings.EqualFold(k, "x-amz-request-id") {
			if len(vals) > 0 && strings.TrimSpace(vals[0]) != "" {
				return strings.TrimSpace(vals[0])
			}
		}
	}
	return ""
}

func classifyHTTPError(endpoint string, resp *http.Response, body []byte, retryAfter time.Duration) error {
	reqID := extractRequestID(resp.Header)
	msg, conflictNodeID := parseErrorPayload(body)
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}

	switch resp.StatusCode {
	case http.StatusBadRequest:
		return &InvalidFilterError{
			StatusCode: resp.StatusCode,
			Endpoint:   endpoint,
			RequestID:  reqID,
			Message:    msg,
		}
	case http.StatusUnauthorized:
		return &AuthenticationError{
			StatusCode: resp.StatusCode,
			Endpoint:   endpoint,
			RequestID:  reqID,
			Message:    msg,
		}
	case http.StatusForbidden:
		return &PermissionError{
			StatusCode: resp.StatusCode,
			Endpoint:   endpoint,
			RequestID:  reqID,
			Message:    msg,
		}
	case http.StatusNotFound:
		return &NotFoundError{
			StatusCode: resp.StatusCode,
			Endpoint:   endpoint,
			RequestID:  reqID,
			Message:    msg,
		}
	case http.StatusConflict:
		return &ConflictError{
			StatusCode:        resp.StatusCode,
			Endpoint:          endpoint,
			RequestID:         reqID,
			ConflictingNodeID: conflictNodeID,
			Message:           msg,
		}
	case http.StatusTooManyRequests:
		return &RateLimitError{
			StatusCode: resp.StatusCode,
			Endpoint:   endpoint,
			RequestID:  reqID,
			RetryAfter: retryAfter,
			Message:    msg,
		}
	default:
		if resp.StatusCode >= 500 {
			return &TransientNetworkError{
				StatusCode: resp.StatusCode,
				Endpoint:   endpoint,
				RequestID:  reqID,
				RetryAfter: retryAfter,
				Message:    msg,
			}
		}
		return &RemoteProtocolError{
			StatusCode: resp.StatusCode,
			Endpoint:   endpoint,
			RequestID:  reqID,
			Message:    msg,
		}
	}
}

func parseErrorPayload(body []byte) (message, conflictNodeID string) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "", ""
	}
	var payload struct {
		Message string `json:"message"`
		Code    string `json:"code"`
		ID      string `json:"id"`
		Info    *struct {
			NodeID string `json:"nodeId"`
		} `json:"info"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		conflictNodeID = strings.TrimSpace(payload.ID)
		if conflictNodeID == "" && payload.Info != nil {
			conflictNodeID = strings.TrimSpace(payload.Info.NodeID)
		}
		if payload.Message != "" {
			return Redact(payload.Message), conflictNodeID
		}
		if payload.Code != "" {
			return Redact(payload.Code), conflictNodeID
		}
	}
	if len(trimmed) > 512 {
		trimmed = trimmed[:512]
	}
	return Redact(trimmed), ""
}
