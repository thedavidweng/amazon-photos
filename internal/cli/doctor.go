package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/thedavidweng/amazon-photos/internal/config"
	"github.com/thedavidweng/amazon-photos/internal/model"
	"github.com/thedavidweng/amazon-photos/internal/output"
	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

func newUsageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "usage",
		Short: "Show Amazon Photos / Drive account storage usage",
		RunE: withClient("usage", func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error {
			usage, err := client.GetUsage(ctx)
			if err != nil {
				return r.FailError(meta, err)
			}
			return r.Render(meta, usage, nil, func() {
				r.Human("Last Calculated: %s", formatTime(usage.LastCalculated))
				r.Human("Photos:  %d items (%d bytes total, %d bytes billable)", usage.Photo.Total.Count, usage.Photo.Total.Bytes, usage.Photo.Billable.Bytes)
				r.Human("Videos:  %d items (%d bytes total, %d bytes billable)", usage.Video.Total.Count, usage.Video.Total.Bytes, usage.Video.Billable.Bytes)
				r.Human("Docs:    %d items (%d bytes total, %d bytes billable)", usage.Doc.Total.Count, usage.Doc.Total.Bytes, usage.Doc.Billable.Bytes)
				r.Human("Other:   %d items (%d bytes total, %d bytes billable)", usage.Other.Total.Count, usage.Other.Total.Bytes, usage.Other.Billable.Bytes)
			})
		}),
	}
}

type DoctorCheck struct {
	Grade   int    `json:"grade"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Hint    string `json:"hint,omitempty"`
}

type DoctorReport struct {
	Healthy      bool          `json:"healthy"`
	Unconfigured bool          `json:"unconfigured,omitempty"`
	Checks       []DoctorCheck `json:"checks"`
}

func newDoctorCmd() *cobra.Command {
	var writeProbe bool
	var confirm bool
	var allowUnconfigured bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run graded diagnostic checks on configuration, credentials, regional routing, and API connectivity",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := GetAppContext(cmd.Context())
			r := newRenderer(app, cmd)
			meta := metaInput(app, "doctor")

			if writeProbe && !confirm {
				return r.FailError(meta, model.NewCommandError(model.ErrConfirmationRequired, "--write-probe creates and trashes a temporary remote folder; pass --confirm to proceed"))
			}

			ctx := cmd.Context()
			if app.Timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, app.Timeout)
				defer cancel()
			}

			report, failErr := runDoctorChecks(ctx, app, writeProbe, allowUnconfigured)
			if app.Region != "" {
				meta.Region = app.Region
			}

			if failErr != nil && !report.Healthy {
				if !r.JSON {
					renderDoctorHuman(r, report)
				}
				return r.FailWithData(meta, report, failErr, nil)
			}

			return r.Render(meta, report, nil, func() {
				renderDoctorHuman(r, report)
			})
		},
	}

	cmd.Flags().BoolVar(&writeProbe, "write-probe", false, "run opt-in remote folder create + trash probe")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "confirm opt-in remote write probe")
	cmd.Flags().BoolVar(&allowUnconfigured, "allow-unconfigured", false, "succeed when no credentials are configured (CI safe)")
	return cmd
}

func renderDoctorHuman(r *output.Renderer, report DoctorReport) {
	for _, c := range report.Checks {
		r.Human("[%s] Grade %d (%s): %s", strings.ToUpper(c.Status), c.Grade, c.Name, c.Message)
		if c.Hint != "" {
			r.Human("  Hint: %s", c.Hint)
		}
	}
}

func runDoctorChecks(ctx context.Context, app *AppContext, writeProbe, allowUnconfigured bool) (DoctorReport, error) {
	var checks []DoctorCheck
	cfgPath := config.ResolveConfigPath(app.ConfigFile)

	info, statErr := os.Stat(cfgPath)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			checks = append(checks, DoctorCheck{
				Grade:   1,
				Name:    "config_permissions",
				Status:  "skipped",
				OK:      true,
				Message: fmt.Sprintf("config file %s not present (checking env credentials)", cfgPath),
			})
		} else {
			checks = append(checks, DoctorCheck{
				Grade:   1,
				Name:    "config_permissions",
				Status:  "fail",
				OK:      false,
				Message: aphotos.Redact(statErr.Error()),
			})
			return DoctorReport{Healthy: false, Checks: checks}, &aphotos.ConfigError{Field: "config_file", Message: statErr.Error(), Cause: statErr}
		}
	} else {
		permOK := true
		msg := fmt.Sprintf("config file %s readable", cfgPath)
		var hint string
		if runtime.GOOS != "windows" {
			filePerm := info.Mode().Perm()
			dirInfo, dErr := os.Stat(filepath.Dir(cfgPath))
			if filePerm&0o077 != 0 {
				permOK = false
				msg = fmt.Sprintf("config file %s has overly permissive mode %04o", cfgPath, filePerm)
				hint = fmt.Sprintf("run: chmod 0600 %s", cfgPath)
			} else if dErr == nil && dirInfo.Mode().Perm()&0o077 != 0 {
				permOK = false
				msg = fmt.Sprintf("config directory %s has overly permissive mode %04o", filepath.Dir(cfgPath), dirInfo.Mode().Perm())
				hint = fmt.Sprintf("run: chmod 0700 %s", filepath.Dir(cfgPath))
			}
		}
		status := "pass"
		if !permOK {
			status = "warn"
		}
		checks = append(checks, DoctorCheck{
			Grade:   1,
			Name:    "config_permissions",
			Status:  status,
			OK:      permOK,
			Message: msg,
			Hint:    hint,
		})
	}

	creds, credErr := config.LoadCredentials(app.ConfigFile, app.Profile, app.Region)
	if credErr != nil {
		isUnconfigured := strings.Contains(credErr.Error(), "no credentials configured") || strings.Contains(credErr.Error(), "not found")
		if isUnconfigured && allowUnconfigured {
			checks = append(checks,
				DoctorCheck{Grade: 2, Name: "credentials_and_region", Status: "skipped", OK: true, Message: "unconfigured (allowed via --allow-unconfigured)"},
				DoctorCheck{Grade: 3, Name: "account_root_and_usage", Status: "skipped", OK: true, Message: "skipped (unconfigured)"},
				DoctorCheck{Grade: 4, Name: "search_read_probe", Status: "skipped", OK: true, Message: "skipped (unconfigured)"},
				DoctorCheck{Grade: 5, Name: "write_probe", Status: "skipped", OK: true, Message: "skipped (unconfigured)"},
			)
			return DoctorReport{Healthy: true, Unconfigured: true, Checks: checks}, nil
		}
		checks = append(checks, DoctorCheck{
			Grade:   2,
			Name:    "credentials_and_region",
			Status:  "fail",
			OK:      false,
			Message: aphotos.Redact(credErr.Error()),
			Hint:    "Run 'aphotos auth import <cookie-file> --region <region>' to configure credentials.",
		})
		return DoctorReport{Healthy: false, Unconfigured: isUnconfigured, Checks: checks}, mapCredentialInitError(credErr)
	}

	app.Region = string(creds.Region)
	client, clientErr := aphotos.NewClient(aphotos.Options{
		Region:       creds.Region,
		Cookies:      creds.Cookies,
		DriveURL:     creds.DriveURL,
		UploadURL:    creds.UploadURL,
		ThumbnailURL: creds.ThumbnailURL,
		HTTPClient:   app.HTTPClient,
		Transport:    app.Transport,
	})
	if clientErr != nil {
		checks = append(checks, DoctorCheck{
			Grade:   2,
			Name:    "credentials_and_region",
			Status:  "fail",
			OK:      false,
			Message: aphotos.Redact(clientErr.Error()),
		})
		return DoctorReport{Healthy: false, Checks: checks}, clientErr
	}
	defer func() { _ = client.Close() }()

	rc := client.RegionConfig()
	checks = append(checks, DoctorCheck{
		Grade:   2,
		Name:    "credentials_and_region",
		Status:  "pass",
		OK:      true,
		Message: fmt.Sprintf("region %s (%s) resolved with %d required cookies", rc.Region, rc.Evidence, len(rc.RequiredCookies)),
	})

	root, rootErr := client.GetRoot(ctx)
	if rootErr != nil {
		checks = append(checks, DoctorCheck{
			Grade:   3,
			Name:    "account_root_and_usage",
			Status:  "fail",
			OK:      false,
			Message: aphotos.Redact(rootErr.Error()),
			Hint:    diagnosticHintForError(rootErr),
		})
		return DoctorReport{Healthy: false, Checks: checks}, rootErr
	}
	usage, usageErr := client.GetUsage(ctx)
	if usageErr != nil {
		checks = append(checks, DoctorCheck{
			Grade:   3,
			Name:    "account_root_and_usage",
			Status:  "fail",
			OK:      false,
			Message: aphotos.Redact(usageErr.Error()),
			Hint:    diagnosticHintForError(usageErr),
		})
		return DoctorReport{Healthy: false, Checks: checks}, usageErr
	}
	checks = append(checks, DoctorCheck{
		Grade:   3,
		Name:    "account_root_and_usage",
		Status:  "pass",
		OK:      true,
		Message: fmt.Sprintf("root node %s (photos: %d, videos: %d)", root.ID, usage.Photo.Total.Count, usage.Video.Total.Count),
	})

	searchPage, searchErr := client.Search(ctx, aphotos.SearchOptions{Limit: 1})
	if searchErr != nil {
		checks = append(checks, DoctorCheck{
			Grade:   4,
			Name:    "search_read_probe",
			Status:  "fail",
			OK:      false,
			Message: aphotos.Redact(searchErr.Error()),
			Hint:    diagnosticHintForError(searchErr),
		})
		return DoctorReport{Healthy: false, Checks: checks}, searchErr
	}
	checks = append(checks, DoctorCheck{
		Grade:   4,
		Name:    "search_read_probe",
		Status:  "pass",
		OK:      true,
		Message: fmt.Sprintf("search probe returned totalCount=%d", searchPage.TotalCount),
	})

	if !writeProbe {
		checks = append(checks, DoctorCheck{
			Grade:   5,
			Name:    "write_probe",
			Status:  "skipped",
			OK:      true,
			Message: "write probe skipped (pass --write-probe --confirm to test remote folder creation and trash)",
		})
		return DoctorReport{Healthy: true, Checks: checks}, nil
	}

	probeName := fmt.Sprintf(".aphotos-doctor-probe-%d", time.Now().UTC().Unix())
	created, createErr := client.CreateFolder(ctx, probeName, root.ID)
	if createErr != nil {
		checks = append(checks, DoctorCheck{
			Grade:   5,
			Name:    "write_probe",
			Status:  "fail",
			OK:      false,
			Message: aphotos.Redact(createErr.Error()),
			Hint:    diagnosticHintForError(createErr),
		})
		return DoctorReport{Healthy: false, Checks: checks}, createErr
	}
	if trashErr := client.MoveToTrash(ctx, []string{created.ID}); trashErr != nil {
		checks = append(checks, DoctorCheck{
			Grade:   5,
			Name:    "write_probe",
			Status:  "fail",
			OK:      false,
			Message: aphotos.Redact(trashErr.Error()),
			Hint:    diagnosticHintForError(trashErr),
		})
		return DoctorReport{Healthy: false, Checks: checks}, trashErr
	}
	checks = append(checks, DoctorCheck{
		Grade:   5,
		Name:    "write_probe",
		Status:  "pass",
		OK:      true,
		Message: fmt.Sprintf("created and trashed probe folder %s", created.ID),
	})

	return DoctorReport{Healthy: true, Checks: checks}, nil
}

func diagnosticHintForError(err error) string {
	var authErr *aphotos.AuthenticationError
	if errors.As(err, &authErr) {
		return "Session cookies were rejected (HTTP 401). Export fresh cookies from an active browser session and run 'aphotos auth import <cookie-file>'."
	}
	var permErr *aphotos.PermissionError
	if errors.As(err, &permErr) {
		return "Access denied (HTTP 403). Verify that your account has access to the configured Amazon marketplace region."
	}
	var rlErr *aphotos.RateLimitError
	if errors.As(err, &rlErr) {
		return "Rate limited by Amazon Photos (HTTP 429). Wait for the Retry-After window and reduce concurrent requests."
	}
	var protoErr *aphotos.RemoteProtocolError
	if errors.As(err, &protoErr) {
		return "Unexpected response shape from the unofficial Amazon Photos Web API; the remote endpoint contract may have changed."
	}
	return ""
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}
