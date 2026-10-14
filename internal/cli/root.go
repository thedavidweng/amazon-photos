package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/thedavidweng/amazon-photos/internal/config"
	"github.com/thedavidweng/amazon-photos/internal/model"
	"github.com/thedavidweng/amazon-photos/internal/output"
	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

type contextKey string

const appContextKey contextKey = "appContext"

type RootOptions struct {
	Transport  http.RoundTripper
	HTTPClient *http.Client
	PromptIn   io.Reader
}

type AppContext struct {
	ConfigFile string
	Profile    string
	Region     string
	JSON       bool
	Quiet      bool
	Verbose    bool
	Timeout    time.Duration
	StartedAt  time.Time
	RequestID  string
	Transport  http.RoundTripper
	HTTPClient *http.Client
	PromptIn   io.Reader
}

func WithAppContext(ctx context.Context, app *AppContext) context.Context {
	return context.WithValue(ctx, appContextKey, app)
}

func GetAppContext(ctx context.Context) *AppContext {
	if ctx == nil {
		return &AppContext{StartedAt: time.Now()}
	}
	if v, ok := ctx.Value(appContextKey).(*AppContext); ok && v != nil {
		return v
	}
	return &AppContext{StartedAt: time.Now()}
}

func generateRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return "req-" + hex.EncodeToString(b[:])
}

func NewRootCommand(opts RootOptions) *cobra.Command {
	root := &cobra.Command{
		Use:           "aphotos",
		Short:         "Unofficial Amazon Photos / Drive v1 CLI",
		Long:          "Unofficial Amazon Photos / Drive v1 command-line client with versioned JSON output and verified transfers.",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			app := &AppContext{
				StartedAt:  time.Now(),
				RequestID:  generateRequestID(),
				Transport:  opts.Transport,
				HTTPClient: opts.HTTPClient,
				PromptIn:   opts.PromptIn,
			}
			app.ConfigFile, _ = cmd.Flags().GetString("config")
			app.Profile, _ = cmd.Flags().GetString("profile")
			app.Region, _ = cmd.Flags().GetString("region")
			app.JSON, _ = cmd.Flags().GetBool("json")
			app.Quiet, _ = cmd.Flags().GetBool("quiet")
			app.Verbose, _ = cmd.Flags().GetBool("verbose")
			app.Timeout, _ = cmd.Flags().GetDuration("timeout")

			applyEnvOverrides(app)
			if app.Timeout <= 0 {
				r := newRenderer(app, cmd)
				return r.FailError(metaInput(app, cmd.Name()), model.NewCommandError(model.ErrValidationFailed, "--timeout must be positive"))
			}

			baseCtx := cmd.Context()
			if baseCtx == nil {
				baseCtx = context.Background()
			}
			cmd.SetContext(WithAppContext(baseCtx, app))
			return nil
		},
	}

	root.PersistentFlags().Bool("json", false, "emit versioned JSON envelope to stdout")
	root.PersistentFlags().Bool("quiet", false, "suppress non-JSON human output and progress")
	root.PersistentFlags().Bool("verbose", false, "emit diagnostic logs to stderr")
	root.PersistentFlags().String("region", "", "Amazon marketplace region (us, ca, uk, jp, de, fr, it, es, ...)")
	root.PersistentFlags().String("config", "", "config file path (default: ~/.config/aphotos/config.yaml)")
	root.PersistentFlags().Duration("timeout", 2*time.Minute, "command timeout")
	root.PersistentFlags().String("profile", "default", "credential profile name")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newCompletionCmd(root))
	root.AddCommand(newAuthCmd())
	root.AddCommand(newUsageCmd())
	root.AddCommand(newDoctorCmd())
	for _, build := range subcommandBuilders {
		root.AddCommand(build())
	}

	silenceAllCommands(root)
	return root
}

var subcommandBuilders []func() *cobra.Command

func registerSubcommand(build func() *cobra.Command) {
	subcommandBuilders = append(subcommandBuilders, build)
}

func applyEnvOverrides(app *AppContext) {
	if strings.TrimSpace(app.ConfigFile) == "" {
		app.ConfigFile = strings.TrimSpace(os.Getenv("APHOTOS_CONFIG"))
	}
	if app.Profile == "" || app.Profile == "default" {
		if v := strings.TrimSpace(os.Getenv("APHOTOS_PROFILE")); v != "" {
			app.Profile = v
		}
	}
	if strings.TrimSpace(app.Region) == "" {
		app.Region = strings.TrimSpace(os.Getenv("APHOTOS_REGION"))
	}
	app.JSON = app.JSON || config.ParseBool(os.Getenv("APHOTOS_JSON"))
	app.Quiet = app.Quiet || config.ParseBool(os.Getenv("APHOTOS_QUIET"))
	app.Verbose = app.Verbose || config.ParseBool(os.Getenv("APHOTOS_VERBOSE"))
}

func silenceAllCommands(cmd *cobra.Command) {
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	for _, child := range cmd.Commands() {
		silenceAllCommands(child)
	}
}

func newRenderer(app *AppContext, cmd *cobra.Command) *output.Renderer {
	return &output.Renderer{
		Out:     cmd.OutOrStdout(),
		Err:     cmd.ErrOrStderr(),
		JSON:    app.JSON,
		Quiet:   app.Quiet,
		Verbose: app.Verbose,
	}
}

func metaInput(app *AppContext, command string) output.RuntimeMetaInput {
	return output.RuntimeMetaInput{
		Command:   command,
		Profile:   app.Profile,
		Region:    app.Region,
		RequestID: app.RequestID,
		StartedAt: app.StartedAt,
	}
}

func buildClient(app *AppContext) (*aphotos.Client, config.Credentials, error) {
	creds, err := config.LoadCredentials(app.ConfigFile, app.Profile, app.Region)
	if err != nil {
		return nil, config.Credentials{}, err
	}
	app.Region = string(creds.Region)
	client, err := aphotos.NewClient(aphotos.Options{
		Region:       creds.Region,
		Cookies:      creds.Cookies,
		DriveURL:     creds.DriveURL,
		UploadURL:    creds.UploadURL,
		ThumbnailURL: creds.ThumbnailURL,
		HTTPClient:   app.HTTPClient,
		Transport:    app.Transport,
	})
	if err != nil {
		return nil, creds, err
	}
	return client, creds, nil
}

func withClient(command string, fn func(ctx context.Context, cmd *cobra.Command, args []string, app *AppContext, r *output.Renderer, meta output.RuntimeMetaInput, client *aphotos.Client) error) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		app := GetAppContext(cmd.Context())
		r := newRenderer(app, cmd)
		client, creds, err := buildClient(app)
		meta := metaInput(app, command)
		if creds.Region != "" {
			meta.Region = string(creds.Region)
		}
		if err != nil {
			return r.FailError(meta, mapCredentialInitError(err))
		}
		defer func() { _ = client.Close() }()

		ctx := cmd.Context()
		if app.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, app.Timeout)
			defer cancel()
		}
		r.Verbosef("executing %s (profile=%s, region=%s)", command, app.Profile, meta.Region)
		return fn(ctx, cmd, args, app, r, meta, client)
	}
}

func mapCredentialInitError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "no credentials configured") {
		return model.NewCommandError(model.ErrAuthRequired, msg)
	}
	return err
}

func Execute() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	doneCh := make(chan struct{})

	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-doneCh:
			return
		}
		select {
		case <-sigCh:
			os.Exit(130)
		case <-doneCh:
		}
	}()

	cmd := NewRootCommand(RootOptions{})
	cmd.SetContext(ctx)
	err := cmd.Execute()
	close(doneCh)
	return err
}
