package config

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

type Credentials struct {
	Region       aphotos.Region
	Cookies      map[string]string
	DriveURL     string
	UploadURL    string
	ThumbnailURL string
	Source       string
}

type browserCookieJSON struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
}

func CanonicalCookieName(raw string) string {
	return aphotos.CanonicalCookieKey(raw)
}

func ParseCookieFile(path string) (map[string]string, error) {
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return nil, &aphotos.ConfigError{
			Field:   "cookie_file",
			Message: "cookie file path is required",
		}
	}
	if val, isIndir, err := expandIndirection(cleaned); err != nil {
		return nil, err
	} else if isIndir {
		return ParseCookiesData([]byte(val))
	}
	data, err := os.ReadFile(cleaned)
	if err != nil {
		return nil, &aphotos.ConfigError{
			Field:   "cookie_file",
			Message: fmt.Sprintf("read cookie file %s: %v", cleaned, err),
			Cause:   err,
		}
	}
	return ParseCookiesData(data)
}

func ParseCookiesData(data []byte) (map[string]string, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, &aphotos.ConfigError{
			Field:   "cookies",
			Message: "cookie input is empty",
		}
	}

	switch trimmed[0] {
	case '[':
		var arr []browserCookieJSON
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return nil, &aphotos.ConfigError{
				Field:   "cookies",
				Message: fmt.Sprintf("invalid JSON cookie array: %v", err),
				Cause:   err,
			}
		}
		out := make(map[string]string, len(arr))
		for _, item := range arr {
			name := CanonicalCookieName(item.Name)
			val := strings.TrimSpace(item.Value)
			if name == "" || val == "" {
				continue
			}
			out[name] = val
		}
		if len(out) == 0 {
			return nil, &aphotos.ConfigError{
				Field:   "cookies",
				Message: "JSON cookie array contained no valid cookies",
			}
		}
		return out, nil

	case '{':
		var m map[string]any
		if err := json.Unmarshal(trimmed, &m); err != nil {
			return nil, &aphotos.ConfigError{
				Field:   "cookies",
				Message: fmt.Sprintf("invalid JSON cookie map: %v", err),
				Cause:   err,
			}
		}
		if rawCookies, ok := m["cookies"]; ok {
			if subMap, ok := rawCookies.(map[string]any); ok {
				m = subMap
			}
		}
		out := make(map[string]string, len(m))
		for k, v := range m {
			strVal, ok := v.(string)
			if !ok {
				continue
			}
			name := CanonicalCookieName(k)
			val := strings.TrimSpace(strVal)
			if name == "" || val == "" {
				continue
			}
			out[name] = val
		}
		if len(out) == 0 {
			return nil, &aphotos.ConfigError{
				Field:   "cookies",
				Message: "JSON cookie map contained no valid cookies",
			}
		}
		return out, nil
	}

	if cookies := parseNetscapeCookies(trimmed); len(cookies) > 0 {
		return cookies, nil
	}

	if cookies := parseRawCookieHeader(string(trimmed)); len(cookies) > 0 {
		return cookies, nil
	}

	return nil, &aphotos.ConfigError{
		Field:   "cookies",
		Message: "unrecognized cookie format (expected Netscape cookies.txt, JSON array/map, or Cookie header)",
	}
}

func parseNetscapeCookies(data []byte) map[string]string {
	out := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#HttpOnly_") {
			line = strings.TrimPrefix(line, "#HttpOnly_")
		} else if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			fields = strings.Fields(line)
			if len(fields) < 7 {
				continue
			}
		}
		name := CanonicalCookieName(fields[5])
		val := strings.TrimSpace(fields[6])
		if name != "" && val != "" {
			out[name] = val
		}
	}
	return out
}

func parseRawCookieHeader(raw string) map[string]string {
	out := make(map[string]string)
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if len(l) >= 7 && strings.EqualFold(l[:7], "cookie:") {
			l = strings.TrimSpace(l[7:])
		}
		parts := strings.Split(l, ";")
		for _, part := range parts {
			kv := strings.TrimSpace(part)
			if kv == "" {
				continue
			}
			eqIdx := strings.IndexByte(kv, '=')
			if eqIdx <= 0 {
				continue
			}
			k := CanonicalCookieName(kv[:eqIdx])
			v := strings.TrimSpace(kv[eqIdx+1:])
			v = strings.Trim(v, `"`)
			if k != "" && v != "" && !strings.ContainsAny(k, " \t") {
				out[k] = v
			}
		}
	}
	return out
}

func expandIndirection(value string) (resolvedVal string, isIndirection bool, err error) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "env:") {
		return trimmed, false, nil
	}
	envName := strings.TrimSpace(strings.TrimPrefix(trimmed, "env:"))
	if envName == "" {
		return "", true, &aphotos.ConfigError{
			Field:   "cookies",
			Message: "empty environment variable name in env: indirection",
		}
	}
	resolved := strings.TrimSpace(os.Getenv(envName))
	if resolved == "" {
		return "", true, &aphotos.ConfigError{
			Field:   "cookies",
			Message: fmt.Sprintf("environment variable %q referenced by env:%s is not set", envName, envName),
		}
	}
	return resolved, true, nil
}

func ResolveCredentials(p *Profile, regionFlag string) (Credentials, error) {
	cookies := make(map[string]string)
	var regionStr string
	var driveURL, uploadURL, thumbURL string
	source := "profile"

	if p != nil {
		regionStr = strings.TrimSpace(p.Region)
		driveURL = strings.TrimSpace(p.DriveURL)
		uploadURL = strings.TrimSpace(p.UploadURL)
		thumbURL = strings.TrimSpace(p.ThumbnailURL)
		for k, v := range p.Cookies {
			resolved, _, err := expandIndirection(v)
			if err != nil {
				return Credentials{}, err
			}
			ck := CanonicalCookieName(k)
			if ck != "" && resolved != "" {
				cookies[ck] = resolved
			}
		}
	}

	if envCookieFile := strings.TrimSpace(os.Getenv("APHOTOS_COOKIES_FILE")); envCookieFile != "" {
		fileCookies, err := ParseCookieFile(envCookieFile)
		if err != nil {
			return Credentials{}, err
		}
		for k, v := range fileCookies {
			resolved, _, rErr := expandIndirection(v)
			if rErr != nil {
				return Credentials{}, rErr
			}
			cookies[CanonicalCookieName(k)] = resolved
		}
		source = "env_file"
	}

	if envRegion := strings.TrimSpace(os.Getenv("APHOTOS_REGION")); envRegion != "" {
		regionStr = envRegion
	}
	if strings.TrimSpace(regionFlag) != "" {
		regionStr = strings.TrimSpace(regionFlag)
	}

	if envDrive := strings.TrimSpace(os.Getenv("APHOTOS_DRIVE_URL")); envDrive != "" {
		driveURL = envDrive
	}
	if envUpload := strings.TrimSpace(os.Getenv("APHOTOS_UPLOAD_URL")); envUpload != "" {
		uploadURL = envUpload
	}
	if envThumb := strings.TrimSpace(os.Getenv("APHOTOS_THUMBNAIL_URL")); envThumb != "" {
		thumbURL = envThumb
	}

	envSess := strings.TrimSpace(os.Getenv("APHOTOS_SESSION_ID"))
	envUbid := strings.TrimSpace(os.Getenv("APHOTOS_UBID"))
	envAt := strings.TrimSpace(os.Getenv("APHOTOS_AT"))
	if envSess != "" || envUbid != "" || envAt != "" {
		source = "env"
		if envSess != "" {
			cookies["session-id"] = envSess
		}
		targetRegion := aphotos.Region(strings.ToLower(regionStr))
		if targetRegion == "" {
			if inferred, err := aphotos.InferRegionFromCookies(cookies); err == nil {
				targetRegion = inferred
			}
		}
		ubidKey := "ubid-main"
		atKey := "at-main"
		if targetRegion != "" {
			if rc, err := aphotos.ResolveRegion(targetRegion); err == nil && len(rc.RequiredCookies) >= 3 {
				ubidKey = rc.RequiredCookies[1]
				atKey = rc.RequiredCookies[2]
			}
		}
		if envUbid != "" {
			cookies[ubidKey] = envUbid
		}
		if envAt != "" {
			cookies[atKey] = envAt
		}
	}

	if len(cookies) == 0 {
		return Credentials{}, &aphotos.ConfigError{
			Field:   "cookies",
			Message: "no credentials configured; run 'aphotos auth import <cookie-file>' or set APHOTOS_COOKIES_FILE / APHOTOS_SESSION_ID",
		}
	}

	region := aphotos.Region(strings.ToLower(regionStr))
	if region == "" {
		inferred, err := aphotos.InferRegionFromCookies(cookies)
		if err != nil {
			return Credentials{}, err
		}
		region = inferred
	}

	rc, err := aphotos.ResolveRegion(region)
	if err != nil {
		return Credentials{}, err
	}

	for _, reqCookie := range rc.RequiredCookies {
		if strings.TrimSpace(cookies[reqCookie]) == "" {
			return Credentials{}, &aphotos.ConfigError{
				Field:   "cookies",
				Message: fmt.Sprintf("missing required cookie %q for region %q", reqCookie, rc.Region),
			}
		}
	}

	return Credentials{
		Region:       rc.Region,
		Cookies:      cookies,
		DriveURL:     driveURL,
		UploadURL:    uploadURL,
		ThumbnailURL: thumbURL,
		Source:       source,
	}, nil
}

func LoadCredentials(configPath, profileName, regionFlag string) (Credentials, error) {
	resolvedPath := ResolveConfigPath(configPath)
	var prof *Profile
	cfg, err := Load(resolvedPath)
	if err == nil {
		p, pErr := cfg.GetProfile(profileName)
		if pErr == nil {
			prof = p
		} else if !hasEnvCredentialOverrides() {
			return Credentials{}, pErr
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Credentials{}, err
	}
	return ResolveCredentials(prof, regionFlag)
}

func hasEnvCredentialOverrides() bool {
	return strings.TrimSpace(os.Getenv("APHOTOS_COOKIES_FILE")) != "" ||
		strings.TrimSpace(os.Getenv("APHOTOS_SESSION_ID")) != "" ||
		strings.TrimSpace(os.Getenv("APHOTOS_UBID")) != "" ||
		strings.TrimSpace(os.Getenv("APHOTOS_AT")) != ""
}
