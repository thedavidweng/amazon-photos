package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/amazon-photos/internal/config"
	"github.com/thedavidweng/amazon-photos/internal/model"
)

func TestInteractiveLogin(t *testing.T) {
	for _, status := range []int{200, 401, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), "config.yaml")
			original := &config.Config{}
			original.SetProfile("default", &config.Profile{Region: "us", Cookies: map[string]string{"at-main": "previous"}})
			if err := config.Save(cfgPath, original); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(cfgPath)
			calls := 0
			cmd := NewRootCommand(RootOptions{PromptIn: strings.NewReader("ca\nsession-id=session; ubid-acbca=ubid; at-acbca=login-secret\n"), Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != "GET" || req.URL.Host != "www.amazon.ca" || req.URL.Query().Get("filters") != "isRoot:true" {
					t.Errorf("unexpected request: %s %s", req.Method, req.URL)
				}
				if !strings.Contains(req.Header.Get("Cookie"), "at-acbca=login-secret") {
					t.Error("missing pasted session")
				}
				if status != 200 {
					return jsonHTTPResponse(status, `{"message":"rejected"}`), nil
				}
				return jsonHTTPResponse(200, `{"data":[{"id":"root","kind":"FOLDER","status":"AVAILABLE"}]}`), nil
			})})
			var out, prompts bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&prompts)
			cmd.SetArgs([]string{"--json", "--config", cfgPath, "auth", "login"})
			err := cmd.Execute()
			if calls != 1 {
				t.Fatalf("probe calls = %d, error=%v", calls, err)
			}
			if strings.Contains(out.String()+prompts.String(), "login-secret") {
				t.Fatal("secret exposed")
			}
			if !strings.Contains(prompts.String(), "Marketplace") || !strings.Contains(prompts.String(), "Cookie") {
				t.Fatal("missing instructions")
			}
			if status != 200 {
				if err == nil {
					t.Fatal("expected rejection")
				}
				after, _ := os.ReadFile(cfgPath)
				if !bytes.Equal(before, after) {
					t.Fatal("failed login overwrote credentials")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var env model.Envelope
			if err := json.Unmarshal(out.Bytes(), &env); err != nil {
				t.Fatal(err)
			}
			if !env.OK || env.Meta.Command != "auth login" {
				t.Fatalf("unexpected result: %s", out.String())
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			p, err := cfg.GetProfile("default")
			if err != nil {
				t.Fatal(err)
			}
			if p.Region != "ca" || p.Cookies["at-acbca"] != "login-secret" || len(p.Cookies) != 3 {
				t.Fatalf("wrong saved profile: region=%s keys=%d", p.Region, len(p.Cookies))
			}
		})
	}
}

func TestInteractiveLoginRejectsInvalidInput(t *testing.T) {
	for _, input := range []string{"\n", "moon\n", "us\n\n", "us\nsession-id=only\n"} {
		t.Run(input, func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), "config.yaml")
			cmd := NewRootCommand(RootOptions{PromptIn: strings.NewReader(input), Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("invalid login made a request"); return nil, nil })})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{"--config", cfgPath, "auth", "login"})
			if err := cmd.Execute(); err == nil {
				t.Fatal("expected invalid input error")
			}
			if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
				t.Fatal("invalid input saved credentials")
			}
		})
	}
}
