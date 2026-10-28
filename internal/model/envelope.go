package model

import (
	"context"
	"errors"
	"io/fs"

	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

const SchemaVersion = "v1"

const (
	ErrValidationFailed     = "VALIDATION_FAILED"
	ErrInvalidFilter        = "INVALID_FILTER"
	ErrAuthRequired         = "AUTH_REQUIRED"
	ErrAuthFailed           = "AUTH_FAILED"
	ErrPermissionDenied     = "PERMISSION_DENIED"
	ErrAPIError             = "API_ERROR"
	ErrRemoteProtocolError  = "REMOTE_PROTOCOL_ERROR"
	ErrResourceNotFound     = "RESOURCE_NOT_FOUND"
	ErrConflict             = "CONFLICT"
	ErrNetworkError         = "NETWORK_ERROR"
	ErrRateLimited          = "RATE_LIMITED"
	ErrPartialSuccess       = "PARTIAL_SUCCESS"
	ErrConfirmationRequired = "CONFIRMATION_REQUIRED"
	ErrReadOnlyViolation    = "READ_ONLY_VIOLATION"
	ErrFilesystemError      = "FILESYSTEM_ERROR"
	ErrConfigError          = "CONFIG_ERROR"
	ErrInterrupted          = "INTERRUPTED"
)

type Envelope struct {
	OK    bool       `json:"ok"`
	Data  any        `json:"data,omitempty"`
	Error *ErrorBody `json:"error,omitempty"`
	Meta  Meta       `json:"meta"`
}

type ErrorBody struct {
	Code         string         `json:"code"`
	Message      string         `json:"message"`
	Category     string         `json:"category,omitempty"`
	Retryable    bool           `json:"retryable,omitempty"`
	RetryAfterMS int64          `json:"retry_after_ms,omitempty"`
	Details      map[string]any `json:"details,omitempty"`
}

type Meta struct {
	Command       string   `json:"command,omitempty"`
	Profile       string   `json:"profile,omitempty"`
	Region        string   `json:"region,omitempty"`
	DurationMS    int64    `json:"duration_ms"`
	SchemaVersion string   `json:"schema_version"`
	RequestID     string   `json:"request_id,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
}

func ExitCode(code string) int {
	switch code {
	case "":
		return 0
	case ErrValidationFailed, ErrInvalidFilter:
		return 1
	case ErrAuthRequired, ErrAuthFailed, ErrPermissionDenied:
		return 2
	case ErrAPIError, ErrRemoteProtocolError, ErrResourceNotFound, ErrConflict:
		return 3
	case ErrNetworkError, ErrRateLimited:
		return 4
	case ErrPartialSuccess:
		return 5
	case ErrConfirmationRequired, ErrReadOnlyViolation:
		return 6
	case ErrFilesystemError:
		return 7
	case ErrConfigError:
		return 8
	case ErrInterrupted:
		return 130
	default:
		return 1
	}
}

func CategoryForCode(code string) string {
	switch code {
	case ErrValidationFailed, ErrInvalidFilter:
		return "validation"
	case ErrAuthRequired, ErrAuthFailed, ErrPermissionDenied:
		return "auth"
	case ErrAPIError, ErrRemoteProtocolError, ErrResourceNotFound, ErrConflict, ErrPartialSuccess:
		return "api"
	case ErrNetworkError, ErrRateLimited:
		return "network"
	case ErrConfirmationRequired, ErrReadOnlyViolation:
		return "safety"
	case ErrFilesystemError:
		return "filesystem"
	case ErrConfigError:
		return "config"
	case ErrInterrupted:
		return "interrupted"
	default:
		return "validation"
	}
}

func IsRetryableCode(code string) bool {
	return code == ErrNetworkError || code == ErrRateLimited
}

type CommandError struct {
	Code    string
	Message string
	Body    ErrorBody
	Cause   error
}

func (e *CommandError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return aphotos.Redact(e.Message)
	}
	return aphotos.Redact(e.Body.Message)
}

func (e *CommandError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *CommandError) ExitCode() int {
	if e == nil {
		return 0
	}
	return ExitCode(e.Code)
}

func NewCommandError(code, message string) *CommandError {
	redacted := aphotos.Redact(message)
	return &CommandError{
		Code:    code,
		Message: redacted,
		Body: ErrorBody{
			Code:      code,
			Message:   redacted,
			Category:  CategoryForCode(code),
			Retryable: IsRetryableCode(code),
		},
	}
}

func NewCommandErrorWithDetails(code, message string, details map[string]any) *CommandError {
	ce := NewCommandError(code, message)
	ce.Body.Details = sanitizeDetails(details)
	return ce
}

func FromError(err error) *CommandError {
	if err == nil {
		return nil
	}

	if existing, ok := errors.AsType[*CommandError](err); ok {
		if existing.Body.Code == "" {
			existing.Body.Code = existing.Code
		}
		if existing.Body.Message == "" {
			existing.Body.Message = aphotos.Redact(existing.Message)
		}
		if existing.Body.Category == "" {
			existing.Body.Category = CategoryForCode(existing.Code)
		}
		return existing
	}

	if errors.Is(err, context.Canceled) {
		return NewCommandError(ErrInterrupted, "operation interrupted")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NewCommandError(ErrNetworkError, aphotos.Redact(err.Error()))
	}

	if cfgErr, ok := errors.AsType[*aphotos.ConfigError](err); ok {
		var details map[string]any
		if cfgErr.Field != "" {
			details = map[string]any{"field": aphotos.Redact(cfgErr.Field)}
		}
		ce := NewCommandErrorWithDetails(ErrConfigError, cfgErr.Error(), details)
		ce.Cause = err
		return ce
	}

	if authErr, ok := errors.AsType[*aphotos.AuthenticationError](err); ok {
		details := httpDetails(authErr.Endpoint, authErr.StatusCode, authErr.RequestID)
		ce := NewCommandErrorWithDetails(ErrAuthFailed, authErr.Error(), details)
		ce.Cause = err
		return ce
	}

	if permErr, ok := errors.AsType[*aphotos.PermissionError](err); ok {
		details := httpDetails(permErr.Endpoint, permErr.StatusCode, permErr.RequestID)
		ce := NewCommandErrorWithDetails(ErrPermissionDenied, permErr.Error(), details)
		ce.Cause = err
		return ce
	}

	if rlErr, ok := errors.AsType[*aphotos.RateLimitError](err); ok {
		details := httpDetails(rlErr.Endpoint, rlErr.StatusCode, rlErr.RequestID)
		ce := NewCommandErrorWithDetails(ErrRateLimited, rlErr.Error(), details)
		if rlErr.RetryAfter > 0 {
			ce.Body.RetryAfterMS = rlErr.RetryAfter.Milliseconds()
		}
		ce.Cause = err
		return ce
	}

	if filterErr, ok := errors.AsType[*aphotos.InvalidFilterError](err); ok {
		details := httpDetails(filterErr.Endpoint, filterErr.StatusCode, filterErr.RequestID)
		if filterErr.Filter != "" {
			if details == nil {
				details = map[string]any{}
			}
			details["filter"] = aphotos.Redact(filterErr.Filter)
		}
		ce := NewCommandErrorWithDetails(ErrInvalidFilter, filterErr.Error(), details)
		ce.Cause = err
		return ce
	}

	if nfErr, ok := errors.AsType[*aphotos.NotFoundError](err); ok {
		details := httpDetails(nfErr.Endpoint, nfErr.StatusCode, nfErr.RequestID)
		if nfErr.NodeID != "" {
			if details == nil {
				details = map[string]any{}
			}
			details["node_id"] = aphotos.Redact(nfErr.NodeID)
		}
		ce := NewCommandErrorWithDetails(ErrResourceNotFound, nfErr.Error(), details)
		ce.Cause = err
		return ce
	}

	if conflictErr, ok := errors.AsType[*aphotos.ConflictError](err); ok {
		details := httpDetails(conflictErr.Endpoint, conflictErr.StatusCode, conflictErr.RequestID)
		if conflictErr.ConflictingNodeID != "" {
			if details == nil {
				details = map[string]any{}
			}
			details["conflicting_node_id"] = aphotos.Redact(conflictErr.ConflictingNodeID)
		}
		ce := NewCommandErrorWithDetails(ErrConflict, conflictErr.Error(), details)
		ce.Cause = err
		return ce
	}

	if netErr, ok := errors.AsType[*aphotos.TransientNetworkError](err); ok {
		details := httpDetails(netErr.Endpoint, netErr.StatusCode, netErr.RequestID)
		ce := NewCommandErrorWithDetails(ErrNetworkError, netErr.Error(), details)
		ce.Cause = err
		return ce
	}

	if protoErr, ok := errors.AsType[*aphotos.RemoteProtocolError](err); ok {
		details := httpDetails(protoErr.Endpoint, protoErr.StatusCode, protoErr.RequestID)
		ce := NewCommandErrorWithDetails(ErrRemoteProtocolError, protoErr.Error(), details)
		ce.Cause = err
		return ce
	}

	if partialErr, ok := errors.AsType[*aphotos.PartialResultError](err); ok {
		var details map[string]any
		if partialErr.Endpoint != "" {
			details = map[string]any{"endpoint": aphotos.Redact(partialErr.Endpoint)}
		}
		if len(partialErr.Warnings) > 0 {
			if details == nil {
				details = map[string]any{}
			}
			redactedWarnings := make([]string, len(partialErr.Warnings))
			for i, w := range partialErr.Warnings {
				redactedWarnings[i] = aphotos.Redact(w)
			}
			details["warnings"] = redactedWarnings
		}
		ce := NewCommandErrorWithDetails(ErrPartialSuccess, partialErr.Error(), details)
		ce.Cause = err
		return ce
	}

	if _, ok := errors.AsType[*fs.PathError](err); ok {
		ce := NewCommandError(ErrFilesystemError, err.Error())
		ce.Cause = err
		return ce
	}

	ce := NewCommandError(ErrValidationFailed, err.Error())
	ce.Cause = err
	return ce
}

func httpDetails(endpoint string, status int, reqID string) map[string]any {
	var d map[string]any
	if endpoint != "" {
		d = map[string]any{"endpoint": aphotos.Redact(endpoint)}
	}
	if status > 0 {
		if d == nil {
			d = map[string]any{}
		}
		d["status_code"] = status
	}
	if reqID != "" {
		if d == nil {
			d = map[string]any{}
		}
		d["request_id"] = aphotos.Redact(reqID)
	}
	return d
}

func sanitizeDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	out := make(map[string]any, len(details))
	for k, v := range details {
		if s, ok := v.(string); ok {
			out[k] = aphotos.Redact(s)
		} else {
			out[k] = v
		}
	}
	return out
}
