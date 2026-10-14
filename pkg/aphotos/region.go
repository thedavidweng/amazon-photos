package aphotos

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type Region string

const (
	RegionUS Region = "us"
	RegionCA Region = "ca"
	RegionUK Region = "uk"
	RegionJP Region = "jp"
	RegionDE Region = "de"
	RegionFR Region = "fr"
	RegionIT Region = "it"
	RegionES Region = "es"
)

type EvidenceStatus string

const (
	EvidenceVerified     EvidenceStatus = "verified"
	EvidenceExperimental EvidenceStatus = "experimental"
	EvidenceUnsupported  EvidenceStatus = "unsupported"
)

type RegionConfig struct {
	Region          Region
	Domain          string
	DriveURL        string
	UploadURL       string
	ThumbnailURL    string
	RequiredCookies []string
	Evidence        EvidenceStatus
}

const (
	naUploadURL = "https://content-na.drive.amazonaws.com/cdproxy/nodes"
	euUploadURL = "https://content-eu.drive.amazonaws.com/cdproxy/nodes"
	jpUploadURL = "https://content-jp.drive.amazonaws.com/cdproxy/nodes"
)

var europeanTLDs = map[string]string{
	"at": "at",
	"be": "be",
	"bg": "bg",
	"ch": "ch",
	"cy": "cy",
	"cz": "cz",
	"de": "de",
	"dk": "dk",
	"ee": "ee",
	"es": "es",
	"fi": "fi",
	"fr": "fr",
	"gr": "gr",
	"hr": "hr",
	"hu": "hu",
	"ie": "ie",
	"is": "is",
	"it": "it",
	"li": "li",
	"lt": "lt",
	"lu": "lu",
	"lv": "lv",
	"mt": "mt",
	"nl": "nl",
	"no": "no",
	"pl": "pl",
	"pt": "pt",
	"ro": "ro",
	"se": "se",
	"si": "si",
	"sk": "sk",
	"uk": "co.uk",
}

func ResolveRegion(r Region) (RegionConfig, error) {
	key := strings.ToLower(strings.TrimSpace(string(r)))
	if key == "" {
		return RegionConfig{}, &ConfigError{
			Field:   "region",
			Message: "region is required when cookies do not unambiguously identify a region",
		}
	}

	switch key {
	case "us", "com":
		return RegionConfig{
			Region:          RegionUS,
			Domain:          "com",
			DriveURL:        "https://www.amazon.com/drive/v1",
			UploadURL:       naUploadURL,
			ThumbnailURL:    "https://thumbnails-photos.amazon.com/v1/thumbnail",
			RequiredCookies: []string{"session-id", "ubid-main", "at-main"},
			Evidence:        EvidenceVerified,
		}, nil
	case "ca":
		return RegionConfig{
			Region:          RegionCA,
			Domain:          "ca",
			DriveURL:        "https://www.amazon.ca/drive/v1",
			UploadURL:       naUploadURL,
			ThumbnailURL:    "https://thumbnails-photos.amazon.ca/v1/thumbnail",
			RequiredCookies: []string{"session-id", "ubid-acbca", "at-acbca"},
			Evidence:        EvidenceExperimental,
		}, nil
	case "uk", "co.uk", "gb":
		return RegionConfig{
			Region:          RegionUK,
			Domain:          "co.uk",
			DriveURL:        "https://www.amazon.co.uk/drive/v1",
			UploadURL:       euUploadURL,
			ThumbnailURL:    "https://thumbnails-photos.amazon.co.uk/v1/thumbnail",
			RequiredCookies: []string{"session-id", "ubid-acbuk", "at-acbuk"},
			Evidence:        EvidenceExperimental,
		}, nil
	case "jp", "co.jp":
		return RegionConfig{
			Region:          RegionJP,
			Domain:          "co.jp",
			DriveURL:        "https://www.amazon.co.jp/drive/v1",
			UploadURL:       jpUploadURL,
			ThumbnailURL:    "https://thumbnails-photos.amazon.co.jp/v1/thumbnail",
			RequiredCookies: []string{"session-id", "ubid-acbjp", "at-acbjp"},
			Evidence:        EvidenceExperimental,
		}, nil
	}

	if domain, ok := europeanTLDs[key]; ok {
		return RegionConfig{
			Region:          Region(key),
			Domain:          domain,
			DriveURL:        fmt.Sprintf("https://www.amazon.%s/drive/v1", domain),
			UploadURL:       euUploadURL,
			ThumbnailURL:    fmt.Sprintf("https://thumbnails-photos.amazon.%s/v1/thumbnail", domain),
			RequiredCookies: []string{"session-id", "ubid-acb" + key, "at-acb" + key},
			Evidence:        EvidenceExperimental,
		}, nil
	}

	return RegionConfig{}, &ConfigError{
		Field:   "region",
		Message: fmt.Sprintf("unsupported region %q", string(r)),
	}
}

func InferRegionFromCookies(cookies map[string]string) (Region, error) {
	found := make(map[Region]struct{})
	for rawKey, val := range cookies {
		if strings.TrimSpace(val) == "" {
			continue
		}
		k := canonicalCookieKey(rawKey)
		switch {
		case k == "ubid-main" || k == "at-main":
			found[RegionUS] = struct{}{}
		case strings.HasPrefix(k, "ubid-acb"):
			suffix := strings.TrimPrefix(k, "ubid-acb")
			rc, err := ResolveRegion(Region(suffix))
			if err != nil {
				return "", &ConfigError{
					Field:   "region",
					Message: fmt.Sprintf("unknown regional cookie suffix in %q", k),
					Cause:   err,
				}
			}
			found[rc.Region] = struct{}{}
		case strings.HasPrefix(k, "at-acb"):
			suffix := strings.TrimPrefix(k, "at-acb")
			rc, err := ResolveRegion(Region(suffix))
			if err != nil {
				return "", &ConfigError{
					Field:   "region",
					Message: fmt.Sprintf("unknown regional cookie suffix in %q", k),
					Cause:   err,
				}
			}
			found[rc.Region] = struct{}{}
		}
	}

	if len(found) == 0 {
		return "", &ConfigError{
			Field:   "region",
			Message: "cannot infer region from cookies; specify region explicitly",
		}
	}
	if len(found) > 1 {
		regions := make([]string, 0, len(found))
		for r := range found {
			regions = append(regions, string(r))
		}
		sort.Strings(regions)
		return "", &ConfigError{
			Field:   "region",
			Message: fmt.Sprintf("ambiguous cookies match multiple regions (%s); specify region explicitly", strings.Join(regions, ", ")),
		}
	}

	for r := range found {
		return r, nil
	}
	return "", nil
}

func canonicalCookieKey(name string) string {
	k := strings.ToLower(strings.TrimSpace(name))
	switch {
	case k == "session_id" || k == "session-id":
		return "session-id"
	case k == "ubid_main" || k == "ubid-main":
		return "ubid-main"
	case k == "at_main" || k == "at-main":
		return "at-main"
	case strings.HasPrefix(k, "ubid_acb"):
		return "ubid-acb" + strings.TrimPrefix(k, "ubid_acb")
	case strings.HasPrefix(k, "at_acb"):
		return "at-acb" + strings.TrimPrefix(k, "at_acb")
	default:
		return strings.TrimSpace(name)
	}
}

func validateOverrideURL(field, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" {
		return "", &ConfigError{
			Field:   field,
			Message: "invalid URL override",
			Cause:   err,
		}
	}
	host := strings.ToLower(u.Hostname())
	isLoopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if u.Scheme != "https" && (u.Scheme != "http" || !isLoopback) {
		return "", &ConfigError{
			Field:   field,
			Message: "URL override must use https (or http on loopback)",
		}
	}
	return strings.TrimRight(trimmed, "/"), nil
}
