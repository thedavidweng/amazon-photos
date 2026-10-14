package model

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

func TestExitCodeMapping(t *testing.T) {
	cases := []struct {
		code string
		want int
	}{
		{ErrValidationFailed, 1},
		{ErrInvalidFilter, 1},
		{ErrAuthRequired, 2},
		{ErrAuthFailed, 2},
		{ErrPermissionDenied, 2},
		{ErrAPIError, 3},
		{ErrRemoteProtocolError, 3},
		{ErrResourceNotFound, 3},
		{ErrConflict, 3},
		{ErrNetworkError, 4},
		{ErrRateLimited, 4},
		{ErrPartialSuccess, 5},
		{ErrConfirmationRequired, 6},
		{ErrReadOnlyViolation, 6},
		{ErrFilesystemError, 7},
		{ErrConfigError, 8},
		{ErrInterrupted, 130},
		{"UNKNOWN_ERROR_CODE", 1},
	}

	for _, tc := range cases {
		if got := ExitCode(tc.code); got != tc.want {
			t.Errorf("ExitCode(%q) = %d, want %d", tc.code, got, tc.want)
		}
	}
}

func TestClassifySDKError(t *testing.T) {
	secretMsg := "failed with session-id=111-2222222-3333333 and Atza|secret-token"

	cases := []struct {
		name      string
		err       error
		wantCode  string
		wantExit  int
		wantRetry bool
	}{
		{
			name:     "ConfigError",
			err:      &aphotos.ConfigError{Field: "region", Message: secretMsg},
			wantCode: ErrConfigError,
			wantExit: 8,
		},
		{
			name:     "AuthenticationError",
			err:      &aphotos.AuthenticationError{StatusCode: 401, Endpoint: "/nodes", RequestID: "req-1", Message: secretMsg},
			wantCode: ErrAuthFailed,
			wantExit: 2,
		},
		{
			name:     "PermissionError",
			err:      &aphotos.PermissionError{StatusCode: 403, Endpoint: "/nodes", RequestID: "req-2", Message: secretMsg},
			wantCode: ErrPermissionDenied,
			wantExit: 2,
		},
		{
			name:      "RateLimitError",
			err:       &aphotos.RateLimitError{StatusCode: 429, Endpoint: "/search", RequestID: "req-3", RetryAfter: 3 * time.Second, Message: secretMsg},
			wantCode:  ErrRateLimited,
			wantExit:  4,
			wantRetry: true,
		},
		{
			name:     "InvalidFilterError",
			err:      &aphotos.InvalidFilterError{StatusCode: 400, Endpoint: "/search", Filter: "bad:filter", Message: secretMsg},
			wantCode: ErrInvalidFilter,
			wantExit: 1,
		},
		{
			name:     "NotFoundError",
			err:      &aphotos.NotFoundError{StatusCode: 404, Endpoint: "/nodes/x", NodeID: "x", Message: secretMsg},
			wantCode: ErrResourceNotFound,
			wantExit: 3,
		},
		{
			name:     "ConflictError",
			err:      &aphotos.ConflictError{StatusCode: 409, Endpoint: "/cdproxy/nodes", ConflictingNodeID: "node-9", Message: secretMsg},
			wantCode: ErrConflict,
			wantExit: 3,
		},
		{
			name:      "TransientNetworkError",
			err:       &aphotos.TransientNetworkError{StatusCode: 503, Endpoint: "/search", Message: secretMsg},
			wantCode:  ErrNetworkError,
			wantExit:  4,
			wantRetry: true,
		},
		{
			name:     "RemoteProtocolError",
			err:      &aphotos.RemoteProtocolError{StatusCode: 200, Endpoint: "/nodes", Message: secretMsg},
			wantCode: ErrRemoteProtocolError,
			wantExit: 3,
		},
		{
			name:     "PartialResultError",
			err:      &aphotos.PartialResultError{Endpoint: "/search", Message: secretMsg, Warnings: []string{"warn1"}},
			wantCode: ErrPartialSuccess,
			wantExit: 5,
		},
		{
			name:     "ContextCanceled",
			err:      context.Canceled,
			wantCode: ErrInterrupted,
			wantExit: 130,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmdErr := FromError(tc.err)
			if cmdErr == nil {
				t.Fatal("expected non-nil CommandError")
			}
			if cmdErr.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", cmdErr.Code, tc.wantCode)
			}
			if gotExit := ExitCode(cmdErr.Code); gotExit != tc.wantExit {
				t.Errorf("ExitCode = %d, want %d", gotExit, tc.wantExit)
			}
			if cmdErr.Body.Retryable != tc.wantRetry {
				t.Errorf("Retryable = %v, want %v", cmdErr.Body.Retryable, tc.wantRetry)
			}
			if strings.Contains(cmdErr.Body.Message, "111-2222222-3333333") || strings.Contains(cmdErr.Body.Message, "Atza|secret-token") {
				t.Errorf("expected secret redaction in message, got %q", cmdErr.Body.Message)
			}
			if tc.name == "RateLimitError" && cmdErr.Body.RetryAfterMS != 3000 {
				t.Errorf("RetryAfterMS = %d, want 3000", cmdErr.Body.RetryAfterMS)
			}
		})
	}
}

func TestEnvelopeJSONSerialization(t *testing.T) {
	env := Envelope{
		OK:   true,
		Data: map[string]string{"status": "ok"},
		Meta: Meta{
			Command:       "usage",
			Profile:       "default",
			Region:        "us",
			DurationMS:    12,
			SchemaVersion: SchemaVersion,
			RequestID:     "req-123",
		},
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var decoded Envelope
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if !decoded.OK || decoded.Meta.SchemaVersion != "v1" || decoded.Meta.Region != "us" {
		t.Errorf("unexpected decoded envelope: %+v", decoded)
	}

	var target *CommandError
	err = NewCommandError(ErrConfirmationRequired, "pass --confirm")
	if !errors.As(err, &target) || target.ExitCode() != 6 {
		t.Errorf("expected CommandError exit code 6, got %v", err)
	}
}
