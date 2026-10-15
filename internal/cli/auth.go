package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thedavidweng/amazon-photos/internal/config"
	"github.com/thedavidweng/amazon-photos/internal/model"
	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

type AuthImportData struct {
	Profile         string                 `json:"profile"`
	Region          aphotos.Region         `json:"region"`
	Evidence        aphotos.EvidenceStatus `json:"evidence"`
	ConfigPath      string                 `json:"config_path"`
	ImportedCookies []string               `json:"imported_cookies"`
}

type AuthStatusData struct {
	Authenticated   bool                   `json:"authenticated"`
	Profile         string                 `json:"profile"`
	Region          aphotos.Region         `json:"region"`
	Evidence        aphotos.EvidenceStatus `json:"evidence"`
	Source          string                 `json:"source"`
	DriveURL        string                 `json:"drive_url"`
	UploadURL       string                 `json:"upload_url"`
	PresentCookies  []string               `json:"present_cookies"`
	RequiredCookies []string               `json:"required_cookies"`
}

type AuthLogoutData struct {
	Profile    string `json:"profile"`
	Removed    bool   `json:"removed"`
	ConfigPath string `json:"config_path"`
}

func newAuthCmd() *cobra.Command {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage Amazon Photos session cookie credentials",
	}

	importCmd := &cobra.Command{
		Use:   "import <cookie-file>",
		Short: "Import browser cookies from Netscape cookies.txt, JSON array/map, or raw Cookie header",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				app := GetAppContext(cmd.Context())
				r := newRenderer(app, cmd)
				return r.FailError(metaInput(app, "auth import"), model.NewCommandError(model.ErrValidationFailed, "auth import requires exactly 1 <cookie-file> argument"))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			app := GetAppContext(cmd.Context())
			r := newRenderer(app, cmd)
			meta := metaInput(app, "auth import")

			cookies, err := config.ParseCookieFile(args[0])
			if err != nil {
				return r.FailError(meta, err)
			}

			region := aphotos.Region(strings.ToLower(strings.TrimSpace(app.Region)))
			if region == "" {
				inferred, inferErr := aphotos.InferRegionFromCookies(cookies)
				if inferErr != nil {
					return r.FailError(meta, inferErr)
				}
				region = inferred
			}

			rc, err := aphotos.ResolveRegion(region)
			if err != nil {
				return r.FailError(meta, err)
			}
			meta.Region = string(rc.Region)

			for _, reqCookie := range rc.RequiredCookies {
				if strings.TrimSpace(cookies[reqCookie]) == "" {
					return r.FailError(meta, &aphotos.ConfigError{
						Field:   "cookies",
						Message: fmt.Sprintf("missing required cookie %q for region %q", reqCookie, rc.Region),
					})
				}
			}

			cfgPath := config.ResolveConfigPath(app.ConfigFile)
			cfg, err := config.LoadOrCreate(cfgPath)
			if err != nil {
				return r.FailError(meta, err)
			}

			storedCookies := make(map[string]string, len(rc.RequiredCookies))
			cookieNames := make([]string, 0, len(rc.RequiredCookies))
			for _, k := range rc.RequiredCookies {
				storedCookies[k] = cookies[k]
				cookieNames = append(cookieNames, k)
			}
			sort.Strings(cookieNames)

			cfg.SetProfile(app.Profile, &config.Profile{
				Region:  string(rc.Region),
				Cookies: storedCookies,
			})
			if err := config.Save(cfgPath, cfg); err != nil {
				return r.FailError(meta, err)
			}

			data := AuthImportData{
				Profile:         app.Profile,
				Region:          rc.Region,
				Evidence:        rc.Evidence,
				ConfigPath:      cfgPath,
				ImportedCookies: cookieNames,
			}
			return r.Render(meta, data, nil, func() {
				r.Human("Imported %d cookies for profile %q (region: %s, evidence: %s) into %s", len(cookieNames), app.Profile, rc.Region, rc.Evidence, cfgPath)
			})
		},
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show configured credential and region status",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := GetAppContext(cmd.Context())
			r := newRenderer(app, cmd)
			meta := metaInput(app, "auth status")

			creds, err := config.LoadCredentials(app.ConfigFile, app.Profile, app.Region)
			if err != nil {
				return r.FailError(meta, mapCredentialInitError(err))
			}
			rc, err := aphotos.ResolveRegion(creds.Region)
			if err != nil {
				return r.FailError(meta, err)
			}
			meta.Region = string(rc.Region)

			present := make([]string, 0, len(creds.Cookies))
			for k, v := range creds.Cookies {
				if strings.TrimSpace(v) != "" {
					present = append(present, k)
				}
			}
			sort.Strings(present)

			data := AuthStatusData{
				Authenticated:   true,
				Profile:         app.Profile,
				Region:          rc.Region,
				Evidence:        rc.Evidence,
				Source:          creds.Source,
				DriveURL:        rc.DriveURL,
				UploadURL:       rc.UploadURL,
				PresentCookies:  present,
				RequiredCookies: rc.RequiredCookies,
			}
			return r.Render(meta, data, nil, func() {
				r.Human("Profile: %s\nRegion: %s (%s)\nSource: %s\nDrive URL: %s\nUpload URL: %s\nCookies: %s",
					data.Profile, data.Region, data.Evidence, data.Source, data.DriveURL, data.UploadURL, strings.Join(data.PresentCookies, ", "))
			})
		},
	}

	logoutCmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials for the selected profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := GetAppContext(cmd.Context())
			r := newRenderer(app, cmd)
			meta := metaInput(app, "auth logout")

			cfgPath := config.ResolveConfigPath(app.ConfigFile)
			cfg, err := config.LoadOrCreate(cfgPath)
			if err != nil {
				return r.FailError(meta, err)
			}
			removed := cfg.DeleteProfile(app.Profile)
			if removed {
				if err := config.Save(cfgPath, cfg); err != nil {
					return r.FailError(meta, err)
				}
			}
			data := AuthLogoutData{
				Profile:    app.Profile,
				Removed:    removed,
				ConfigPath: cfgPath,
			}
			return r.Render(meta, data, nil, func() {
				if removed {
					r.Human("Logged out profile %q from %s", app.Profile, cfgPath)
				} else {
					r.Human("Profile %q was not present in %s", app.Profile, cfgPath)
				}
			})
		},
	}

	authCmd.AddCommand(newAuthLoginCmd(), importCmd, statusCmd, logoutCmd)
	return authCmd
}
