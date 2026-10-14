package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thedavidweng/amazon-photos/internal/model"
	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

func TestRendererJSONPurityAndRedaction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := &Renderer{
		Out:     &stdout,
		Err:     &stderr,
		JSON:    true,
		Verbose: true,
	}

	meta := RuntimeMetaInput{
		Command:   "search",
		Profile:   "default",
		Region:    "us",
		RequestID: "req-1",
		StartedAt: time.Now().Add(-5 * time.Millisecond),
	}

	r.Verbosef("connecting with Cookie: session-id=111-2222222-3333333; at-main=Atza|secret-tok")
	r.Progressf("uploading 1/2...")

	data := map[string]any{
		"note": "url https://amazon.com/cdproxy/templink/secret-signed-path?X-Amz-Signature=deadbeef",
	}
	err := r.Render(meta, data, []string{"warn with session-id=999-888-777"}, func() {
		r.Human("this human line must not appear in JSON mode")
	})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	var env model.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("stdout is not valid JSON (%q): %v", stdout.String(), err)
	}
	if !env.OK || env.Meta.SchemaVersion != "v1" || env.Meta.Command != "search" || env.Meta.Region != "us" {
		t.Errorf("unexpected envelope: %+v", env)
	}
	if strings.Contains(stdout.String(), "secret-signed-path") || strings.Contains(stdout.String(), "deadbeef") || strings.Contains(stdout.String(), "999-888-777") {
		t.Errorf("expected stdout JSON redaction, got %s", stdout.String())
	}
	if strings.Contains(stderr.String(), "111-2222222-3333333") || strings.Contains(stderr.String(), "Atza|secret-tok") {
		t.Errorf("expected stderr redaction, got %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "uploading 1/2...") {
		t.Errorf("expected progress on stderr, got %s", stderr.String())
	}
}

func TestRendererFailureAndQuietHuman(t *testing.T) {
	t.Run("JSON failure", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		r := &Renderer{Out: &stdout, Err: &stderr, JSON: true}
		meta := RuntimeMetaInput{Command: "usage", Profile: "default", Region: "us", StartedAt: time.Now()}

		sdkErr := &aphotos.AuthenticationError{StatusCode: 401, Endpoint: "/account/usage", Message: "expired session-id=123-456"}
		retErr := r.FailError(meta, sdkErr)
		var cmdErr *model.CommandError
		if !errors.As(retErr, &cmdErr) || cmdErr.ExitCode() != 2 {
			t.Fatalf("expected exit code 2, got %v", retErr)
		}

		var env model.Envelope
		if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
			t.Fatalf("invalid JSON on failure: %v", err)
		}
		if env.OK || env.Error == nil || env.Error.Code != model.ErrAuthFailed {
			t.Errorf("unexpected error envelope: %+v", env)
		}
		if strings.Contains(stdout.String(), "123-456") {
			t.Errorf("unredacted secret in error JSON: %s", stdout.String())
		}
		if stderr.Len() != 0 {
			t.Errorf("expected empty stderr on JSON failure, got %q", stderr.String())
		}
	})

	t.Run("Human quiet suppresses Human and Progressf", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		r := &Renderer{Out: &stdout, Err: &stderr, Quiet: true}
		r.Human("hello")
		r.Progressf("progress")
		if stdout.Len() != 0 || stderr.Len() != 0 {
			t.Errorf("expected quiet output, got stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
	})
}

func TestRendererConcurrentProgressAndVerboseRaceSafe(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := &Renderer{
		Out:     &stdout,
		Err:     &stderr,
		JSON:    true,
		Verbose: true,
	}

	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := range 25 {
				r.Progressf("[%d/25] worker-%d.jpg (%d/1024 bytes)", i+1, id, (i+1)*32)
				r.Verbosef("worker %d diagnostic step %d", id, i)
			}
		}(worker)
	}
	wg.Wait()

	meta := RuntimeMetaInput{
		Command:   "upload",
		Profile:   "default",
		Region:    "us",
		RequestID: "req-race",
		StartedAt: time.Now(),
	}
	if err := r.Success(meta, map[string]any{"total": 8}, nil); err != nil {
		t.Fatalf("Success failed: %v", err)
	}

	var env model.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("stdout is not valid JSON: %v (%s)", err, stdout.String())
	}
	if !env.OK {
		t.Errorf("expected ok=true envelope, got %+v", env)
	}
	if stderr.Len() == 0 {
		t.Errorf("expected progress output on stderr")
	}
}
