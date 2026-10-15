package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

func TestDefaultConfigPath(t *testing.T) {
	t.Run("APHOTOS_CONFIG override", func(t *testing.T) {
		t.Setenv("APHOTOS_CONFIG", "/custom/path/config.yaml")
		if got := ResolveConfigPath(""); got != "/custom/path/config.yaml" {
			t.Errorf("ResolveConfigPath = %q, want /custom/path/config.yaml", got)
		}
		if got := ResolveConfigPath("/flag/config.yaml"); got != "/flag/config.yaml" {
			t.Errorf("ResolveConfigPath(flag) = %q, want /flag/config.yaml", got)
		}
	})

	t.Run("XDG_CONFIG_HOME", func(t *testing.T) {
		t.Setenv("APHOTOS_CONFIG", "")
		xdgDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", xdgDir)
		want := filepath.Join(xdgDir, "aphotos", "config.yaml")
		if got := DefaultConfigPath(); got != want {
			t.Errorf("DefaultConfigPath = %q, want %q", got, want)
		}
	})

	t.Run("HOME fallback", func(t *testing.T) {
		t.Setenv("APHOTOS_CONFIG", "")
		t.Setenv("XDG_CONFIG_HOME", "")
		home := t.TempDir()
		t.Setenv("HOME", home)
		got := DefaultConfigPath()
		if !strings.HasSuffix(got, filepath.Join(".config", "aphotos", "config.yaml")) {
			t.Errorf("DefaultConfigPath = %q, want suffix .config/aphotos/config.yaml", got)
		}
	})
}

func TestSaveAndLoadAtomicPermissions(t *testing.T) {
	baseDir := t.TempDir()
	cfgDir := filepath.Join(baseDir, "nested", "aphotos")
	cfgPath := filepath.Join(cfgDir, "config.yaml")

	cfg := &Config{
		CurrentProfile: "default",
		Profiles: map[string]*Profile{
			"default": {
				Region: "us",
				Cookies: map[string]string{
					"session-id": "111-2222222-3333333",
					"ubid-main":  "133-4444444-5555555",
					"at-main":    "Atza|test-token",
				},
			},
		},
	}

	if err := Save(cfgPath, cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if runtime.GOOS != "windows" {
		dirInfo, err := os.Stat(cfgDir)
		if err != nil {
			t.Fatalf("Stat dir: %v", err)
		}
		if perm := dirInfo.Mode().Perm(); perm != 0o700 {
			t.Errorf("dir perm = %04o, want 0700", perm)
		}
		fileInfo, err := os.Stat(cfgPath)
		if err != nil {
			t.Fatalf("Stat file: %v", err)
		}
		if perm := fileInfo.Mode().Perm(); perm != 0o600 {
			t.Errorf("file perm = %04o, want 0600", perm)
		}
	}

	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	p, err := loaded.GetProfile("default")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if p.Region != "us" || p.Cookies["session-id"] != "111-2222222-3333333" {
		t.Errorf("unexpected profile: %+v", p)
	}
}

func TestSaveReplacesExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, &Config{CurrentProfile: "before"}); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, &Config{CurrentProfile: "after"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentProfile != "after" {
		t.Fatalf("profile = %q, want after", cfg.CurrentProfile)
	}
}

func TestSavePreservesDirectoryDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, &Config{CurrentProfile: "default"}); err == nil {
		t.Fatal("Save to a directory succeeded")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("Save replaced the destination directory")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.yaml" {
		t.Fatalf("temporary config was not cleaned up: %v", entries)
	}
}

func TestParseCookiesFormats(t *testing.T) {
	t.Run("Netscape cookies.txt with HttpOnly and underscore aliases", func(t *testing.T) {
		input := `# Netscape HTTP Cookie File
# https://curl.se/docs/http-cookies.html

.amazon.com	TRUE	/	TRUE	1893456000	session_id	140-1111111-2222222
#HttpOnly_.amazon.com	TRUE	/	TRUE	1893456000	ubid-main	130-3333333-4444444
#HttpOnly_.amazon.com	TRUE	/	TRUE	1893456000	at_main	Atza|netscape-tok
.example.com	TRUE	/	FALSE	1893456000	other	ignored-or-kept
`
		cookies, err := ParseCookiesData([]byte(input))
		if err != nil {
			t.Fatalf("ParseCookiesData failed: %v", err)
		}
		if cookies["session-id"] != "140-1111111-2222222" {
			t.Errorf("session-id = %q", cookies["session-id"])
		}
		if cookies["ubid-main"] != "130-3333333-4444444" {
			t.Errorf("ubid-main = %q", cookies["ubid-main"])
		}
		if cookies["at-main"] != "Atza|netscape-tok" {
			t.Errorf("at-main = %q", cookies["at-main"])
		}
	})

	t.Run("JSON array of browser cookie objects", func(t *testing.T) {
		input := `[
			{"domain": ".amazon.ca", "name": "session-id", "value": "200-111-222"},
			{"domain": ".amazon.ca", "name": "ubid_acbca", "value": "201-333-444"},
			{"domain": ".amazon.ca", "name": "at-acbca", "value": "Atza|ca-tok"}
		]`
		cookies, err := ParseCookiesData([]byte(input))
		if err != nil {
			t.Fatalf("ParseCookiesData failed: %v", err)
		}
		if cookies["session-id"] != "200-111-222" || cookies["ubid-acbca"] != "201-333-444" || cookies["at-acbca"] != "Atza|ca-tok" {
			t.Errorf("unexpected cookies: %+v", cookies)
		}
		r, err := aphotos.InferRegionFromCookies(cookies)
		if err != nil || r != aphotos.RegionCA {
			t.Errorf("InferRegionFromCookies = (%q, %v), want ca", r, err)
		}
	})

	t.Run("JSON map", func(t *testing.T) {
		input := `{"session_id": "300-111-222", "ubid-acbuk": "301-333-444", "at_acbuk": "Atza|uk-tok"}`
		cookies, err := ParseCookiesData([]byte(input))
		if err != nil {
			t.Fatalf("ParseCookiesData failed: %v", err)
		}
		if cookies["session-id"] != "300-111-222" || cookies["ubid-acbuk"] != "301-333-444" || cookies["at-acbuk"] != "Atza|uk-tok" {
			t.Errorf("unexpected cookies: %+v", cookies)
		}
	})

	t.Run("Raw Cookie header string", func(t *testing.T) {
		input := "Cookie: session-id=400-111-222; ubid-acbjp=401-333-444; at-acbjp=Atza|jp-tok\n"
		cookies, err := ParseCookiesData([]byte(input))
		if err != nil {
			t.Fatalf("ParseCookiesData failed: %v", err)
		}
		if cookies["session-id"] != "400-111-222" || cookies["ubid-acbjp"] != "401-333-444" || cookies["at-acbjp"] != "Atza|jp-tok" {
			t.Errorf("unexpected cookies: %+v", cookies)
		}
	})
}

func TestResolveCredentialsIndirectionAndEnvMode(t *testing.T) {
	t.Run("env:VAR_NAME indirection in profile", func(t *testing.T) {
		t.Setenv("TEST_AT_MAIN_SECRET", "Atza|from-env-indirection")
		p := &Profile{
			Region: "us",
			Cookies: map[string]string{
				"session-id": "111-222-333",
				"ubid-main":  "444-555-666",
				"at-main":    "env:TEST_AT_MAIN_SECRET",
			},
		}
		creds, err := ResolveCredentials(p, "")
		if err != nil {
			t.Fatalf("ResolveCredentials failed: %v", err)
		}
		if creds.Cookies["at-main"] != "Atza|from-env-indirection" {
			t.Errorf("at-main = %q, want Atza|from-env-indirection", creds.Cookies["at-main"])
		}
		if creds.Region != aphotos.RegionUS {
			t.Errorf("Region = %q, want us", creds.Region)
		}
	})

	t.Run("missing env:VAR_NAME fails with ConfigError", func(t *testing.T) {
		t.Setenv("MISSING_SECRET_VAR", "")
		p := &Profile{
			Region: "us",
			Cookies: map[string]string{
				"session-id": "111-222-333",
				"ubid-main":  "444-555-666",
				"at-main":    "env:MISSING_SECRET_VAR",
			},
		}
		_, err := ResolveCredentials(p, "")
		if err == nil {
			t.Fatal("expected error for unset env indirection")
		}
	})

	t.Run("non-persistent APHOTOS_SESSION_ID / APHOTOS_UBID / APHOTOS_AT / APHOTOS_REGION", func(t *testing.T) {
		t.Setenv("APHOTOS_SESSION_ID", "900-111-222")
		t.Setenv("APHOTOS_UBID", "901-333-444")
		t.Setenv("APHOTOS_AT", "Atza|env-direct")
		t.Setenv("APHOTOS_REGION", "de")

		creds, err := ResolveCredentials(nil, "")
		if err != nil {
			t.Fatalf("ResolveCredentials from env failed: %v", err)
		}
		if creds.Region != aphotos.RegionDE {
			t.Errorf("Region = %q, want de", creds.Region)
		}
		if creds.Cookies["session-id"] != "900-111-222" || creds.Cookies["ubid-acbde"] != "901-333-444" || creds.Cookies["at-acbde"] != "Atza|env-direct" {
			t.Errorf("unexpected cookies: %+v", creds.Cookies)
		}
	})

	t.Run("non-persistent APHOTOS_COOKIES_FILE", func(t *testing.T) {
		cookieFile := filepath.Join(t.TempDir(), "cookies.json")
		if err := os.WriteFile(cookieFile, []byte(`{"session-id":"800-1-2","ubid-main":"800-3-4","at-main":"Atza|file-env"}`), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		t.Setenv("APHOTOS_COOKIES_FILE", cookieFile)
		creds, err := ResolveCredentials(nil, "")
		if err != nil {
			t.Fatalf("ResolveCredentials with APHOTOS_COOKIES_FILE failed: %v", err)
		}
		if creds.Region != aphotos.RegionUS || creds.Cookies["at-main"] != "Atza|file-env" {
			t.Errorf("unexpected creds: %+v", creds)
		}
	})
}
