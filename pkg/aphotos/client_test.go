package aphotos_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNewClient_ZeroIOAndCloseSafe(t *testing.T) {
	var calls atomic.Int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected network call during NewClient")
	})

	client, err := aphotos.NewClient(aphotos.Options{
		Region: aphotos.RegionUS,
		Cookies: map[string]string{
			"session-id": "111-2222222-3333333",
			"ubid-main":  "130-0000000-0000000",
			"at-main":    "Atza|synthetic-token-value",
		},
		Transport: rt,
	})
	if err != nil {
		t.Fatalf("NewClient returned unexpected error: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("expected 0 HTTP calls during NewClient, got %d", calls.Load())
	}
	if err := client.Close(); err != nil {
		t.Fatalf("first Close returned error: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}
}

func TestRegionRouting_PairingAndEvidence(t *testing.T) {
	cases := []struct {
		name       string
		region     aphotos.Region
		cookies    map[string]string
		wantRegion aphotos.Region
		wantDrive  string
		wantUpload string
		wantThumb  string
		wantStatus aphotos.EvidenceStatus
	}{
		{
			name:   "US explicit",
			region: aphotos.RegionUS,
			cookies: map[string]string{
				"session-id": "111-0000000-0000001",
				"ubid-main":  "130-0000000-0000001",
				"at-main":    "Atza|us-token",
			},
			wantRegion: aphotos.RegionUS,
			wantDrive:  "https://www.amazon.com/drive/v1",
			wantUpload: "https://content-na.drive.amazonaws.com/cdproxy/nodes",
			wantThumb:  "https://thumbnails-photos.amazon.com/v1/thumbnail",
			wantStatus: aphotos.EvidenceVerified,
		},
		{
			name:   "US com alias",
			region: "com",
			cookies: map[string]string{
				"session-id": "111-0000000-0000001",
				"ubid-main":  "130-0000000-0000001",
				"at-main":    "Atza|us-token",
			},
			wantRegion: aphotos.RegionUS,
			wantDrive:  "https://www.amazon.com/drive/v1",
			wantUpload: "https://content-na.drive.amazonaws.com/cdproxy/nodes",
			wantThumb:  "https://thumbnails-photos.amazon.com/v1/thumbnail",
			wantStatus: aphotos.EvidenceVerified,
		},
		{
			name:   "CA explicit",
			region: aphotos.RegionCA,
			cookies: map[string]string{
				"session-id": "111-0000000-0000002",
				"ubid-acbca": "130-0000000-0000002",
				"at-acbca":   "Atza|ca-token",
			},
			wantRegion: aphotos.RegionCA,
			wantDrive:  "https://www.amazon.ca/drive/v1",
			wantUpload: "https://content-na.drive.amazonaws.com/cdproxy/nodes",
			wantThumb:  "https://thumbnails-photos.amazon.ca/v1/thumbnail",
			wantStatus: aphotos.EvidenceExperimental,
		},
		{
			name:   "UK explicit",
			region: aphotos.RegionUK,
			cookies: map[string]string{
				"session-id": "111-0000000-0000003",
				"ubid-acbuk": "130-0000000-0000003",
				"at-acbuk":   "Atza|uk-token",
			},
			wantRegion: aphotos.RegionUK,
			wantDrive:  "https://www.amazon.co.uk/drive/v1",
			wantUpload: "https://content-eu.drive.amazonaws.com/cdproxy/nodes",
			wantThumb:  "https://thumbnails-photos.amazon.co.uk/v1/thumbnail",
			wantStatus: aphotos.EvidenceExperimental,
		},
		{
			name:   "UK co.uk alias",
			region: "co.uk",
			cookies: map[string]string{
				"session-id": "111-0000000-0000003",
				"ubid-acbuk": "130-0000000-0000003",
				"at-acbuk":   "Atza|uk-token",
			},
			wantRegion: aphotos.RegionUK,
			wantDrive:  "https://www.amazon.co.uk/drive/v1",
			wantUpload: "https://content-eu.drive.amazonaws.com/cdproxy/nodes",
			wantThumb:  "https://thumbnails-photos.amazon.co.uk/v1/thumbnail",
			wantStatus: aphotos.EvidenceExperimental,
		},
		{
			name:   "JP explicit",
			region: aphotos.RegionJP,
			cookies: map[string]string{
				"session-id": "111-0000000-0000004",
				"ubid-acbjp": "130-0000000-0000004",
				"at-acbjp":   "Atza|jp-token",
			},
			wantRegion: aphotos.RegionJP,
			wantDrive:  "https://www.amazon.co.jp/drive/v1",
			wantUpload: "https://content-jp.drive.amazonaws.com/cdproxy/nodes",
			wantThumb:  "https://thumbnails-photos.amazon.co.jp/v1/thumbnail",
			wantStatus: aphotos.EvidenceExperimental,
		},
		{
			name:   "JP co.jp alias",
			region: "co.jp",
			cookies: map[string]string{
				"session-id": "111-0000000-0000004",
				"ubid-acbjp": "130-0000000-0000004",
				"at-acbjp":   "Atza|jp-token",
			},
			wantRegion: aphotos.RegionJP,
			wantDrive:  "https://www.amazon.co.jp/drive/v1",
			wantUpload: "https://content-jp.drive.amazonaws.com/cdproxy/nodes",
			wantThumb:  "https://thumbnails-photos.amazon.co.jp/v1/thumbnail",
			wantStatus: aphotos.EvidenceExperimental,
		},
		{
			name:   "EU DE",
			region: aphotos.RegionDE,
			cookies: map[string]string{
				"session-id": "111-0000000-0000005",
				"ubid-acbde": "130-0000000-0000005",
				"at-acbde":   "Atza|de-token",
			},
			wantRegion: aphotos.RegionDE,
			wantDrive:  "https://www.amazon.de/drive/v1",
			wantUpload: "https://content-eu.drive.amazonaws.com/cdproxy/nodes",
			wantThumb:  "https://thumbnails-photos.amazon.de/v1/thumbnail",
			wantStatus: aphotos.EvidenceExperimental,
		},
		{
			name:   "Inferred JP from underscore cookies when Region empty",
			region: "",
			cookies: map[string]string{
				"session_id": "111-0000000-0000004",
				"ubid_acbjp": "130-0000000-0000004",
				"at_acbjp":   "Atza|jp-token",
			},
			wantRegion: aphotos.RegionJP,
			wantDrive:  "https://www.amazon.co.jp/drive/v1",
			wantUpload: "https://content-jp.drive.amazonaws.com/cdproxy/nodes",
			wantThumb:  "https://thumbnails-photos.amazon.co.jp/v1/thumbnail",
			wantStatus: aphotos.EvidenceExperimental,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := aphotos.NewClient(aphotos.Options{
				Region:  tc.region,
				Cookies: tc.cookies,
			})
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}
			defer func() { _ = client.Close() }()

			rc := client.RegionConfig()
			if rc.Region != tc.wantRegion {
				t.Errorf("Region = %q, want %q", rc.Region, tc.wantRegion)
			}
			if rc.DriveURL != tc.wantDrive {
				t.Errorf("DriveURL = %q, want %q", rc.DriveURL, tc.wantDrive)
			}
			if rc.UploadURL != tc.wantUpload {
				t.Errorf("UploadURL = %q, want %q", rc.UploadURL, tc.wantUpload)
			}
			if rc.ThumbnailURL != tc.wantThumb {
				t.Errorf("ThumbnailURL = %q, want %q", rc.ThumbnailURL, tc.wantThumb)
			}
			if rc.Evidence != tc.wantStatus {
				t.Errorf("Evidence = %q, want %q", rc.Evidence, tc.wantStatus)
			}
		})
	}
}

func TestRegionRouting_FailFastConfigError(t *testing.T) {
	cases := []struct {
		name      string
		opts      aphotos.Options
		wantField string
	}{
		{
			name: "missing region and only session-id cookie (no silent .com fallback)",
			opts: aphotos.Options{
				Cookies: map[string]string{
					"session-id": "111-2222222-3333333",
				},
			},
			wantField: "region",
		},
		{
			name: "unknown region string",
			opts: aphotos.Options{
				Region: "mars",
				Cookies: map[string]string{
					"session-id": "111-2222222-3333333",
					"ubid-main":  "130-0000000-0000000",
					"at-main":    "Atza|token",
				},
			},
			wantField: "region",
		},
		{
			name: "ambiguous cookies from US and CA without explicit region",
			opts: aphotos.Options{
				Cookies: map[string]string{
					"session-id": "111-2222222-3333333",
					"ubid-main":  "130-0000000-0000000",
					"at-main":    "Atza|us-token",
					"ubid-acbca": "130-0000000-0000002",
					"at-acbca":   "Atza|ca-token",
				},
			},
			wantField: "region",
		},
		{
			name: "explicit region conflicts with supplied regional cookies",
			opts: aphotos.Options{
				Region: aphotos.RegionCA,
				Cookies: map[string]string{
					"session-id": "111-2222222-3333333",
					"ubid-main":  "130-0000000-0000000",
					"at-main":    "Atza|us-token",
				},
			},
			wantField: "cookies",
		},
		{
			name: "missing session-id cookie",
			opts: aphotos.Options{
				Region: aphotos.RegionUS,
				Cookies: map[string]string{
					"ubid-main": "130-0000000-0000000",
					"at-main":   "Atza|us-token",
				},
			},
			wantField: "cookies",
		},
		{
			name: "invalid http:// override URL rejected",
			opts: aphotos.Options{
				Region: aphotos.RegionUS,
				Cookies: map[string]string{
					"session-id": "111-2222222-3333333",
					"ubid-main":  "130-0000000-0000000",
					"at-main":    "Atza|us-token",
				},
				UploadURL: "http://untrusted.example.com/cdproxy/nodes",
			},
			wantField: "upload_url",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := aphotos.NewClient(tc.opts)
			if err == nil {
				t.Fatalf("expected ConfigError, got nil")
			}
			var cfgErr *aphotos.ConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("expected *aphotos.ConfigError, got %T (%v)", err, err)
			}
			if tc.wantField != "" && cfgErr.Field != tc.wantField {
				t.Errorf("ConfigError.Field = %q, want %q (err: %v)", cfgErr.Field, tc.wantField, err)
			}
		})
	}
}

func TestGetRoot_WireCookiesHeadersAndDecoding(t *testing.T) {
	var capturedReq *http.Request
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		capturedReq = req
		body := `{
			"count": 1,
			"data": [
				{
					"id": "MR9Rb9kiTYC4HdqXWRSJsg",
					"ownerId": "A2NI566MRQ4Y8X",
					"kind": "FOLDER",
					"name": "",
					"status": "AVAILABLE",
					"isRoot": true,
					"createdDate": "2011-04-06T00:00:00.000Z",
					"modifiedDate": "2024-01-01T00:00:00.000Z",
					"parents": [],
					"parentMap": {"FOLDER": []},
					"settings": {"favorite": false, "hidden": false},
					"futureField": "preserved"
				}
			]
		}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(body)),
			Request:    req,
		}, nil
	})

	client, err := aphotos.NewClient(aphotos.Options{
		Region: aphotos.RegionUS,
		Cookies: map[string]string{
			"session_id": "144-1111111-2222222",
			"session-id": "144-1111111-2222222",
			"ubid_main":  "133-3333333-4444444",
			"at_main":    "Atza|synthetic-us-token",
		},
		Transport: rt,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	root, err := client.GetRoot(context.Background())
	if err != nil {
		t.Fatalf("GetRoot failed: %v", err)
	}
	if capturedReq == nil {
		t.Fatal("expected HTTP request to be sent")
	}
	if capturedReq.Method != http.MethodGet {
		t.Errorf("Method = %q, want GET", capturedReq.Method)
	}
	if capturedReq.URL.Scheme != "https" || capturedReq.URL.Host != "www.amazon.com" || capturedReq.URL.Path != "/drive/v1/nodes" {
		t.Errorf("URL = %q, want https://www.amazon.com/drive/v1/nodes", capturedReq.URL.String())
	}
	q := capturedReq.URL.Query()
	if got := q.Get("filters"); got != "isRoot:true" {
		t.Errorf("query filters = %q, want isRoot:true", got)
	}
	if got := q.Get("asset"); got != "ALL" {
		t.Errorf("query asset = %q, want ALL", got)
	}
	if got := q.Get("tempLink"); got != "false" {
		t.Errorf("query tempLink = %q, want false", got)
	}
	if got := q.Get("resourceVersion"); got != "V2" {
		t.Errorf("query resourceVersion = %q, want V2", got)
	}
	if got := q.Get("ContentType"); got != "JSON" {
		t.Errorf("query ContentType = %q, want JSON", got)
	}

	if got := capturedReq.Header.Get("x-amzn-sessionid"); got != "144-1111111-2222222" {
		t.Errorf("x-amzn-sessionid = %q, want 144-1111111-2222222", got)
	}
	if got := capturedReq.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want application/json", got)
	}
	if got := capturedReq.Header.Get("User-Agent"); got == "" {
		t.Error("expected non-empty User-Agent header")
	}

	cookieHeader := capturedReq.Header.Get("Cookie")
	for _, forbidden := range []string{"session_id=", "ubid_main=", "at_main="} {
		if strings.Contains(cookieHeader, forbidden) {
			t.Errorf("Cookie header %q contains non-canonical underscore key %q", cookieHeader, forbidden)
		}
	}
	if strings.Count(cookieHeader, "session-id=") != 1 {
		t.Errorf("Cookie header %q should contain session-id exactly once", cookieHeader)
	}
	if strings.Count(cookieHeader, "ubid-main=") != 1 {
		t.Errorf("Cookie header %q should contain ubid-main exactly once", cookieHeader)
	}
	if strings.Count(cookieHeader, "at-main=") != 1 {
		t.Errorf("Cookie header %q should contain at-main exactly once", cookieHeader)
	}

	if root.ID != "MR9Rb9kiTYC4HdqXWRSJsg" {
		t.Errorf("root.ID = %q, want MR9Rb9kiTYC4HdqXWRSJsg", root.ID)
	}
	if root.OwnerID != "A2NI566MRQ4Y8X" {
		t.Errorf("root.OwnerID = %q, want A2NI566MRQ4Y8X", root.OwnerID)
	}
	if root.Kind != aphotos.NodeKindFolder {
		t.Errorf("root.Kind = %q, want FOLDER", root.Kind)
	}
	if root.Status != aphotos.NodeStatusAvailable {
		t.Errorf("root.Status = %q, want AVAILABLE", root.Status)
	}
	if !root.IsRoot {
		t.Error("expected root.IsRoot = true")
	}
	if root.CreatedDate.IsZero() || root.CreatedDate.Location().String() != "UTC" {
		t.Errorf("expected UTC CreatedDate, got %v", root.CreatedDate)
	}
	if !strings.Contains(string(root.Raw), `"futureField": "preserved"`) {
		t.Errorf("expected root.Raw to preserve unknown fields, got %s", string(root.Raw))
	}
}

func TestGetRoot_RegionalWireRequests(t *testing.T) {
	cases := []struct {
		region         aphotos.Region
		cookies        map[string]string
		wantHost       string
		wantCookieKeys []string
	}{
		{
			region: aphotos.RegionCA,
			cookies: map[string]string{
				"session_id": "144-0000000-0000001",
				"ubid_acbca": "133-0000000-0000001",
				"at_acbca":   "Atza|ca-token",
			},
			wantHost:       "www.amazon.ca",
			wantCookieKeys: []string{"session-id=144-0000000-0000001", "ubid-acbca=133-0000000-0000001", "at-acbca=Atza|ca-token"},
		},
		{
			region: aphotos.RegionUK,
			cookies: map[string]string{
				"session_id": "144-0000000-0000002",
				"ubid_acbuk": "133-0000000-0000002",
				"at_acbuk":   "Atza|uk-token",
			},
			wantHost:       "www.amazon.co.uk",
			wantCookieKeys: []string{"session-id=144-0000000-0000002", "ubid-acbuk=133-0000000-0000002", "at-acbuk=Atza|uk-token"},
		},
		{
			region: aphotos.RegionJP,
			cookies: map[string]string{
				"session_id": "144-0000000-0000003",
				"ubid_acbjp": "133-0000000-0000003",
				"at_acbjp":   "Atza|jp-token",
			},
			wantHost:       "www.amazon.co.jp",
			wantCookieKeys: []string{"session-id=144-0000000-0000003", "ubid-acbjp=133-0000000-0000003", "at-acbjp=Atza|jp-token"},
		},
	}

	for _, tc := range cases {
		t.Run(string(tc.region), func(t *testing.T) {
			var captured *http.Request
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				captured = req
				body := `{"count":1,"data":[{"id":"root-1","ownerId":"owner-1","kind":"FOLDER","status":"AVAILABLE","isRoot":true}]}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewBufferString(body)),
					Request:    req,
				}, nil
			})

			client, err := aphotos.NewClient(aphotos.Options{
				Region:    tc.region,
				Cookies:   tc.cookies,
				Transport: rt,
			})
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}
			defer func() { _ = client.Close() }()

			if _, err := client.GetRoot(context.Background()); err != nil {
				t.Fatalf("GetRoot failed: %v", err)
			}
			if captured.URL.Host != tc.wantHost {
				t.Errorf("URL.Host = %q, want %q", captured.URL.Host, tc.wantHost)
			}
			cookieHdr := captured.Header.Get("Cookie")
			for _, want := range tc.wantCookieKeys {
				if !strings.Contains(cookieHdr, want) {
					t.Errorf("Cookie header %q missing %q", cookieHdr, want)
				}
			}
			if strings.Contains(cookieHdr, "_acb") || strings.Contains(cookieHdr, "session_id") {
				t.Errorf("Cookie header %q contains underscore cookie names", cookieHdr)
			}
		})
	}
}

func TestGetUsage_NumericAndStringCounts(t *testing.T) {
	var captured *http.Request
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		body := `{
			"lastCalculated": "2026-02-21T12:00:00.000Z",
			"photo": {
				"billable": { "bytes": 0, "count": 0 },
				"total": { "bytes": 59500000000, "count": 27599 }
			},
			"video": {
				"billable": { "bytes": "83400000000", "count": "1339" },
				"total": { "bytes": "83400000000", "count": "1339" }
			},
			"other": {
				"billable": { "bytes": 1024, "count": 1 },
				"total": { "bytes": "1024", "count": "1" }
			},
			"doc": {
				"billable": { "bytes": 20480, "count": 5 },
				"total": { "bytes": 20480, "count": 5 }
			},
			"extraCategory": {
				"billable": { "bytes": 512, "count": 2 },
				"total": { "bytes": 512, "count": 2 }
			}
		}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(body)),
			Request:    req,
		}, nil
	})

	client, err := aphotos.NewClient(aphotos.Options{
		Region: aphotos.RegionUS,
		Cookies: map[string]string{
			"session-id": "144-1111111-2222222",
			"ubid-main":  "133-3333333-4444444",
			"at-main":    "Atza|synthetic-us-token",
		},
		Transport: rt,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	usage, err := client.GetUsage(context.Background())
	if err != nil {
		t.Fatalf("GetUsage failed: %v", err)
	}

	if captured == nil || captured.URL.Path != "/drive/v1/account/usage" {
		t.Fatalf("unexpected request URL: %v", captured)
	}
	q := captured.URL.Query()
	if q.Get("asset") != "ALL" || q.Get("tempLink") != "false" || q.Get("resourceVersion") != "V2" || q.Get("ContentType") != "JSON" {
		t.Errorf("unexpected query params: %v", q)
	}

	wantTime := time.Date(2026, 2, 21, 12, 0, 0, 0, time.UTC)
	if !usage.LastCalculated.Equal(wantTime) {
		t.Errorf("LastCalculated = %v, want %v", usage.LastCalculated, wantTime)
	}
	if usage.Photo.Total.Bytes != 59500000000 || usage.Photo.Total.Count != 27599 {
		t.Errorf("Photo.Total = %+v, want {59500000000, 27599}", usage.Photo.Total)
	}
	if usage.Photo.Billable.Bytes != 0 || usage.Photo.Billable.Count != 0 {
		t.Errorf("Photo.Billable = %+v, want {0, 0}", usage.Photo.Billable)
	}
	if usage.Video.Total.Bytes != 83400000000 || usage.Video.Total.Count != 1339 {
		t.Errorf("Video.Total = %+v, want {83400000000, 1339}", usage.Video.Total)
	}
	if usage.Video.Billable.Bytes != 83400000000 || usage.Video.Billable.Count != 1339 {
		t.Errorf("Video.Billable = %+v, want {83400000000, 1339}", usage.Video.Billable)
	}
	if usage.Other.Total.Bytes != 1024 || usage.Other.Total.Count != 1 {
		t.Errorf("Other.Total = %+v, want {1024, 1}", usage.Other.Total)
	}
	if usage.Doc.Total.Bytes != 20480 || usage.Doc.Total.Count != 5 {
		t.Errorf("Doc.Total = %+v, want {20480, 5}", usage.Doc.Total)
	}
	if extra, ok := usage.Categories["extraCategory"]; !ok || extra.Total.Bytes != 512 || extra.Total.Count != 2 {
		t.Errorf("Categories[extraCategory] = %+v, want {512, 2}", extra)
	}
}

func TestRedact_ScrubsCookiesTokensAndSignedURLs(t *testing.T) {
	sigVal := "deadbeef123456"
	raw := `request failed: Cookie: session-id=144-9999999-8888888; ubid-main=133-7777777-6666666; at-main=Atza|SuperSecretToken123; at_acbca=Atza|CanadaSecret456 header x-amzn-sessionid: 144-9999999-8888888 redirect https://content-na.drive.amazonaws.com/cdproxy/templink/secret-node-token?X-Amz-Signature=` + sigVal
	got := aphotos.Redact(raw)

	forbidden := []string{
		"144-9999999-8888888",
		"133-7777777-6666666",
		"Atza|SuperSecretToken123",
		"Atza|CanadaSecret456",
		"secret-node-token",
		sigVal,
	}
	for _, secret := range forbidden {
		if strings.Contains(got, secret) {
			t.Errorf("Redact(%q) still contains secret %q", got, secret)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected Redact output %q to contain [REDACTED]", got)
	}
}

func TestTypedErrors_401FailsImmediatelyAndOtherStatusMappings(t *testing.T) {
	t.Run("401 AuthenticationError fails on first response without retry", func(t *testing.T) {
		var attempts atomic.Int32
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts.Add(1)
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     http.Header{"x-amzn-RequestId": []string{"req-401-id"}},
				Body:       io.NopCloser(bytes.NewBufferString(`{"message":"Invalid session-id=144-9999999-8888888 or at-main=Atza|SecretToken999"}`)),
				Request:    req,
			}, nil
		})

		client, err := aphotos.NewClient(aphotos.Options{
			Region: aphotos.RegionUS,
			Cookies: map[string]string{
				"session-id": "144-9999999-8888888",
				"ubid-main":  "133-7777777-6666666",
				"at-main":    "Atza|SecretToken999",
			},
			Transport:  rt,
			MaxRetries: 4,
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer func() { _ = client.Close() }()

		_, err = client.GetRoot(context.Background())
		if err == nil {
			t.Fatal("expected AuthenticationError, got nil")
		}
		if got := attempts.Load(); got != 1 {
			t.Fatalf("expected exactly 1 attempt on 401, got %d", got)
		}
		var authErr *aphotos.AuthenticationError
		if !errors.As(err, &authErr) {
			t.Fatalf("expected *AuthenticationError, got %T (%v)", err, err)
		}
		if authErr.StatusCode != http.StatusUnauthorized {
			t.Errorf("StatusCode = %d, want 401", authErr.StatusCode)
		}
		if authErr.RequestID != "req-401-id" {
			t.Errorf("RequestID = %q, want req-401-id", authErr.RequestID)
		}
		if strings.Contains(err.Error(), "SecretToken999") || strings.Contains(err.Error(), "144-9999999-8888888") {
			t.Errorf("error string %q leaked unredacted credentials", err.Error())
		}
	})

	t.Run("403 PermissionError", func(t *testing.T) {
		var attempts atomic.Int32
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts.Add(1)
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Header:     http.Header{"x-amzn-RequestId": []string{"req-403-id"}},
				Body:       io.NopCloser(bytes.NewBufferString(`{"message":"Access denied"}`)),
				Request:    req,
			}, nil
		})
		client := newTestUSClient(t, rt, 3)
		_, err := client.GetUsage(context.Background())
		if attempts.Load() != 1 {
			t.Errorf("expected 1 attempt on 403, got %d", attempts.Load())
		}
		var permErr *aphotos.PermissionError
		if !errors.As(err, &permErr) || permErr.StatusCode != http.StatusForbidden {
			t.Fatalf("expected *PermissionError (403), got %T (%v)", err, err)
		}
	})

	t.Run("400 InvalidFilterError", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(bytes.NewBufferString(`{"message":"Offset + limit cannot be greater than 9999"}`)),
				Request:    req,
			}, nil
		})
		client := newTestUSClient(t, rt, 3)
		_, err := client.GetRoot(context.Background())
		var filterErr *aphotos.InvalidFilterError
		if !errors.As(err, &filterErr) || filterErr.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected *InvalidFilterError (400), got %T (%v)", err, err)
		}
	})

	t.Run("404 NotFoundError", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewBufferString(`{"message":"Node not found"}`)),
				Request:    req,
			}, nil
		})
		client := newTestUSClient(t, rt, 3)
		_, err := client.GetRoot(context.Background())
		var notFoundErr *aphotos.NotFoundError
		if !errors.As(err, &notFoundErr) || notFoundErr.StatusCode != http.StatusNotFound {
			t.Fatalf("expected *NotFoundError (404), got %T (%v)", err, err)
		}
	})

	t.Run("409 ConflictError with conflicting node ID", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusConflict,
				Body:       io.NopCloser(bytes.NewBufferString(`{"message":"Node already exists","info":{"nodeId":"conflict-node-99"}}`)),
				Request:    req,
			}, nil
		})
		client := newTestUSClient(t, rt, 3)
		_, err := client.GetRoot(context.Background())
		var conflictErr *aphotos.ConflictError
		if !errors.As(err, &conflictErr) || conflictErr.StatusCode != http.StatusConflict {
			t.Fatalf("expected *ConflictError (409), got %T (%v)", err, err)
		}
		if conflictErr.ConflictingNodeID != "conflict-node-99" {
			t.Errorf("ConflictingNodeID = %q, want conflict-node-99", conflictErr.ConflictingNodeID)
		}
	})

	t.Run("PartialResultError and Unwrap support", func(t *testing.T) {
		cause := &aphotos.RateLimitError{
			StatusCode: http.StatusTooManyRequests,
			Endpoint:   "/search",
			RetryAfter: 2 * time.Second,
			Message:    "Too many requests",
		}
		partial := &aphotos.PartialResultError{
			Endpoint: "/search",
			Message:  "stopped after 200 items",
			Cause:    cause,
		}
		if _, ok := errors.AsType[*aphotos.RateLimitError](partial); !ok {
			t.Fatalf("expected PartialResultError to unwrap to RateLimitError")
		}
		if partial.Error() == "" {
			t.Error("expected non-empty PartialResultError.Error()")
		}
	})
}

func newTestUSClient(t *testing.T, rt http.RoundTripper, maxRetries int) *aphotos.Client {
	t.Helper()
	client, err := aphotos.NewClient(aphotos.Options{
		Region: aphotos.RegionUS,
		Cookies: map[string]string{
			"session-id": "144-1111111-2222222",
			"ubid-main":  "133-3333333-4444444",
			"at-main":    "Atza|synthetic-us-token",
		},
		Transport:  rt,
		MaxRetries: maxRetries,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestRetryEngine_429RetryAfterAndBudgetExhaustion(t *testing.T) {
	t.Run("429 recovers within retry budget and honors Retry-After seconds", func(t *testing.T) {
		var attempts atomic.Int32
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			n := attempts.Add(1)
			if n <= 2 {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     http.Header{"Retry-After": []string{"1"}},
					Body:       io.NopCloser(bytes.NewBufferString(`{"message":"Rate exceeded"}`)),
					Request:    req,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewBufferString(`{"count":1,"data":[{"id":"root-ok","ownerId":"o1","kind":"FOLDER","status":"AVAILABLE","isRoot":true}]}`)),
				Request:    req,
			}, nil
		})

		client, err := aphotos.NewClient(aphotos.Options{
			Region: aphotos.RegionUS,
			Cookies: map[string]string{
				"session-id": "144-1111111-2222222",
				"ubid-main":  "133-3333333-4444444",
				"at-main":    "Atza|synthetic-us-token",
			},
			Transport:     rt,
			MaxRetries:    3,
			BaseBackoff:   1 * time.Millisecond,
			MaxBackoff:    5 * time.Millisecond,
			MaxRetryAfter: 10 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer func() { _ = client.Close() }()

		root, err := client.GetRoot(context.Background())
		if err != nil {
			t.Fatalf("expected GetRoot to succeed after retries, got %v", err)
		}
		if root.ID != "root-ok" {
			t.Errorf("root.ID = %q, want root-ok", root.ID)
		}
		if got := attempts.Load(); got != 3 {
			t.Errorf("attempts = %d, want 3", got)
		}
	})

	t.Run("429 exhausts retry budget and returns RateLimitError with parsed HTTP-date RetryAfter", func(t *testing.T) {
		var attempts atomic.Int32
		futureDate := time.Now().UTC().Add(3 * time.Second).Format(http.TimeFormat)
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts.Add(1)
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{futureDate}},
				Body:       io.NopCloser(bytes.NewBufferString(`{"message":"Too Many Requests"}`)),
				Request:    req,
			}, nil
		})

		client, err := aphotos.NewClient(aphotos.Options{
			Region: aphotos.RegionUS,
			Cookies: map[string]string{
				"session-id": "144-1111111-2222222",
				"ubid-main":  "133-3333333-4444444",
				"at-main":    "Atza|synthetic-us-token",
			},
			Transport:     rt,
			MaxRetries:    2,
			BaseBackoff:   1 * time.Millisecond,
			MaxBackoff:    5 * time.Millisecond,
			MaxRetryAfter: 5 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer func() { _ = client.Close() }()

		_, err = client.GetRoot(context.Background())
		if err == nil {
			t.Fatal("expected RateLimitError, got nil")
		}
		if got := attempts.Load(); got != 3 {
			t.Errorf("attempts = %d, want 3 (1 initial + 2 retries)", got)
		}
		var rlErr *aphotos.RateLimitError
		if !errors.As(err, &rlErr) {
			t.Fatalf("expected *RateLimitError, got %T (%v)", err, err)
		}
		if rlErr.RetryAfter <= 0 {
			t.Errorf("expected positive RetryAfter, got %v", rlErr.RetryAfter)
		}
	})

	t.Run("5xx and transient transport error retry then return TransientNetworkError", func(t *testing.T) {
		var attempts atomic.Int32
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			n := attempts.Add(1)
			if n == 1 {
				return nil, errors.New("connection reset by peer")
			}
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       io.NopCloser(bytes.NewBufferString(`{"message":"Backend unavailable"}`)),
				Request:    req,
			}, nil
		})

		client, err := aphotos.NewClient(aphotos.Options{
			Region: aphotos.RegionUS,
			Cookies: map[string]string{
				"session-id": "144-1111111-2222222",
				"ubid-main":  "133-3333333-4444444",
				"at-main":    "Atza|synthetic-us-token",
			},
			Transport:   rt,
			MaxRetries:  1,
			BaseBackoff: 1 * time.Millisecond,
			MaxBackoff:  2 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer func() { _ = client.Close() }()

		_, err = client.GetUsage(context.Background())
		if got := attempts.Load(); got != 2 {
			t.Errorf("attempts = %d, want 2", got)
		}
		if _, ok := errors.AsType[*aphotos.TransientNetworkError](err); !ok {
			t.Fatalf("expected *TransientNetworkError, got %T (%v)", err, err)
		}
	})

	t.Run("context cancellation aborts retry wait immediately", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		var attempts atomic.Int32
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts.Add(1)
			go func() {
				time.Sleep(10 * time.Millisecond)
				cancel()
			}()
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{"30"}},
				Body:       io.NopCloser(bytes.NewBufferString(`{"message":"Rate limited"}`)),
				Request:    req,
			}, nil
		})

		client, err := aphotos.NewClient(aphotos.Options{
			Region: aphotos.RegionUS,
			Cookies: map[string]string{
				"session-id": "144-1111111-2222222",
				"ubid-main":  "133-3333333-4444444",
				"at-main":    "Atza|synthetic-us-token",
			},
			Transport:     rt,
			MaxRetries:    3,
			MaxRetryAfter: 5 * time.Second,
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer func() { _ = client.Close() }()

		start := time.Now()
		_, err = client.GetRoot(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Errorf("cancellation took too long: %v", elapsed)
		}
		if got := attempts.Load(); got != 1 {
			t.Errorf("attempts = %d, want 1", got)
		}
	})
}

func TestConcurrencySemaphore_BoundsInFlightRequests(t *testing.T) {
	const maxConc = 2
	var inFlight atomic.Int32
	var maxObserved atomic.Int32

	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		cur := inFlight.Add(1)
		for {
			prev := maxObserved.Load()
			if cur <= prev || maxObserved.CompareAndSwap(prev, cur) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		inFlight.Add(-1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"count":1,"data":[{"id":"r1","ownerId":"o1","kind":"FOLDER","status":"AVAILABLE","isRoot":true}]}`)),
			Request:    req,
		}, nil
	})

	client, err := aphotos.NewClient(aphotos.Options{
		Region: aphotos.RegionUS,
		Cookies: map[string]string{
			"session-id": "144-1111111-2222222",
			"ubid-main":  "133-3333333-4444444",
			"at-main":    "Atza|synthetic-us-token",
		},
		Transport:              rt,
		MaxMetadataConcurrency: maxConc,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			if _, err := client.GetRoot(context.Background()); err != nil {
				t.Errorf("GetRoot failed: %v", err)
			}
		})
	}
	wg.Wait()

	if got := maxObserved.Load(); got > maxConc {
		t.Errorf("maxObserved concurrency = %d, exceeded MaxMetadataConcurrency %d", got, maxConc)
	}
}

func TestNodeDecoding_AcceptsFloatVideoBitrate(t *testing.T) {
	// Amazon returns video bitrate as a JSON float even though the documented
	// shape is an integer. A plain int64 field rejected the whole node, so a
	// video was invisible to every caller.
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"id":"vid1","name":"CLIP.MOV","kind":"FILE","status":"AVAILABLE","ownerId":"A1","parents":[],` +
			`"contentProperties":{"size":5592,"md5":"4c7e72afb0b61cbae5a4e6c0b530c93e",` +
			`"contentType":"video/mp4","video":{"width":96,"height":96,"duration":2.0,"videoCodec":"h264","bitrate":22368.0,"frameRate":10.0}}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(body)),
			Request:    req,
		}, nil
	})
	client := newTestUSClient(t, rt, 1)
	node, err := client.GetNode(context.Background(), "vid1")
	if err != nil {
		t.Fatalf("GetNode failed on a float bitrate: %v", err)
	}
	if node.ContentProperties == nil || node.ContentProperties.Video == nil {
		t.Fatal("video properties were dropped")
	}
	if got := node.ContentProperties.Video.Bitrate; got != 22368 {
		t.Fatalf("Bitrate = %d, want 22368", got)
	}
	if node.ContentProperties.Size != 5592 || node.ContentProperties.MD5 != "4c7e72afb0b61cbae5a4e6c0b530c93e" {
		t.Fatalf("content identity changed: %+v", node.ContentProperties)
	}
}

func TestVideoBitrateAcceptsIntegerStringAndNull(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int64
	}{
		{`{"bitrate":15000000}`, 15000000},
		{`{"bitrate":"15000000"}`, 15000000},
		{`{"bitrate":1.5e7}`, 15000000},
		{`{"bitrate":null}`, 0},
		{`{}`, 0},
	} {
		var vp aphotos.VideoProperties
		if err := json.Unmarshal([]byte(tc.raw), &vp); err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if vp.Bitrate != tc.want {
			t.Fatalf("%s: Bitrate = %d, want %d", tc.raw, vp.Bitrate, tc.want)
		}
	}
	var vp aphotos.VideoProperties
	if err := json.Unmarshal([]byte(`{"bitrate":"loud"}`), &vp); err == nil {
		t.Fatal("a non-numeric bitrate must still fail")
	}
}

func TestNodeDecoding_ContentDateVsCreatedDateAndMediaProps(t *testing.T) {
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{
			"count": 1,
			"data": [
				{
					"id": "HeMReF-vvJiTTkdPIeWuoP",
					"name": "IMG_1234.JPG",
					"kind": "FILE",
					"status": "AVAILABLE",
					"ownerId": "A2NI566MRQ4Y8X",
					"parents": ["MR9Rb9kiTYC4HdqXWRSJsg"],
					"parentMap": {"FOLDER": ["MR9Rb9kiTYC4HdqXWRSJsg"]},
					"createdDate": "2024-05-10T10:00:00.000Z",
					"modifiedDate": "2024-05-10T11:00:00.000Z",
					"settings": {"favorite": true, "hidden": false},
					"contentProperties": {
						"version": 1,
						"size": "432777",
						"md5": "d41d8cd98f00b204e9800998ecf8427e",
						"contentType": "image/jpeg",
						"extension": "jpg",
						"contentDate": "2019-07-06T18:22:00.000Z",
						"image": {
							"width": 3024,
							"height": 4032,
							"make": "Apple",
							"model": "iPhone XS",
							"iso": 100,
							"dateTimeOriginal": "2019-07-06T18:22:00.000Z"
						},
						"video": {
							"width": 1920,
							"height": 1080,
							"duration": 12.5,
							"videoCodec": "h264",
							"audioCodec": "aac",
							"frameRate": 30.0,
							"bitrate": 15000000
						}
					}
				}
			]
		}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(body)),
			Request:    req,
		}, nil
	})

	client := newTestUSClient(t, rt, 1)
	node, err := client.GetRoot(context.Background())
	if err != nil {
		t.Fatalf("GetRoot failed: %v", err)
	}
	if node.ContentProperties == nil {
		t.Fatal("expected ContentProperties to be non-nil")
	}
	wantCreated := time.Date(2024, 5, 10, 10, 0, 0, 0, time.UTC)
	wantContent := time.Date(2019, 7, 6, 18, 22, 0, 0, time.UTC)
	if !node.CreatedDate.Equal(wantCreated) {
		t.Errorf("CreatedDate = %v, want %v", node.CreatedDate, wantCreated)
	}
	if !node.ContentProperties.ContentDate.Equal(wantContent) {
		t.Errorf("ContentDate = %v, want %v", node.ContentProperties.ContentDate, wantContent)
	}
	if node.CreatedDate.Equal(node.ContentProperties.ContentDate) {
		t.Error("CreatedDate and ContentDate should remain distinct")
	}
	if node.ContentProperties.Size != 432777 {
		t.Errorf("Size = %d, want 432777", node.ContentProperties.Size)
	}
	if node.ContentProperties.Image == nil || node.ContentProperties.Image.Model != "iPhone XS" {
		t.Errorf("Image = %+v, want Model iPhone XS", node.ContentProperties.Image)
	}
	if node.ContentProperties.Video == nil || node.ContentProperties.Video.VideoCodec != "h264" {
		t.Errorf("Video = %+v, want VideoCodec h264", node.ContentProperties.Video)
	}
	if node.Settings == nil || !node.Settings.Favorite {
		t.Errorf("Settings = %+v, want Favorite=true", node.Settings)
	}
}

func TestMutateJSON_DoesNotBlindlyReplayOnNetworkOr5xxFailure(t *testing.T) {
	var attempts atomic.Int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts.Add(1)
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(bytes.NewBufferString(`{"message":"Upstream timeout"}`)),
			Request:    req,
		}, nil
	})

	client := newTestUSClient(t, rt, 3)
	_, err := client.CreateAlbum(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error from 502 POST, got nil")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("non-idempotent POST must not be blindly replayed on 502; attempts = %d, want 1", got)
	}
	if _, ok := errors.AsType[*aphotos.TransientNetworkError](err); !ok {
		t.Fatalf("expected *TransientNetworkError, got %T (%v)", err, err)
	}
}

func TestGetNodePreservesReadErrors(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			c, err := aphotos.NewClient(aphotos.Options{Region: aphotos.RegionUS, Cookies: map[string]string{"session-id": "test", "ubid-main": "test", "at-main": "test"}, DisableRetries: true, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path == "/drive/v1/nodes/wanted" {
					return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"message":"denied"}`))}, nil
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"wanted"}]}`))}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			_, err = c.GetNode(context.Background(), "wanted")
			if err == nil || calls != 1 {
				t.Fatalf("GetNode error=%v calls=%d; want original error, one request", err, calls)
			}
			switch status {
			case 401:
				if _, ok := errors.AsType[*aphotos.AuthenticationError](err); !ok {
					t.Fatal(err)
				}
			case 403:
				if _, ok := errors.AsType[*aphotos.PermissionError](err); !ok {
					t.Fatal(err)
				}
			case 404:
				var e *aphotos.NotFoundError
				if !errors.As(err, &e) || e.NodeID != "wanted" {
					t.Fatal(err)
				}
			case 429:
				if _, ok := errors.AsType[*aphotos.RateLimitError](err); !ok {
					t.Fatal(err)
				}
			}
		})
	}
}
