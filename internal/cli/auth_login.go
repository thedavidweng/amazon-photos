package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/thedavidweng/amazon-photos/internal/config"
	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

type AuthLoginData struct {
	Authenticated bool                   `json:"authenticated"`
	Profile       string                 `json:"profile"`
	Region        aphotos.Region         `json:"region"`
	Evidence      aphotos.EvidenceStatus `json:"evidence"`
	ConfigPath    string                 `json:"config_path"`
}

func newAuthLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use: "login", Short: "Sign in by interactively pasting a browser Cookie header (hidden input)", Args: cobra.NoArgs,
		Long: "Choose your Amazon marketplace, sign in to Amazon Photos in your browser, and paste the Cookie request header. Input is hidden and verified with a read-only account request before saving. For scripts or cookie files, use auth import <cookie-file>.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			app := GetAppContext(cmd.Context())
			r := newRenderer(app, cmd)
			meta := metaInput(app, "auth login")
			read, err := loginInput(cmd.Context(), app.PromptIn, cmd.ErrOrStderr())
			if err != nil {
				return r.FailError(meta, err)
			}
			region := app.Region
			if strings.TrimSpace(region) == "" {
				region, err = read("Marketplace (us, ca, uk, jp, de, fr, it, es): ", false)
				if err != nil {
					return r.FailError(meta, err)
				}
			}
			rc, err := aphotos.ResolveRegion(aphotos.Region(strings.TrimSpace(strings.ToLower(region))))
			if err != nil {
				return r.FailError(meta, err)
			}
			meta.Region = string(rc.Region)
			fmt.Fprintf(cmd.ErrOrStderr(), "1. Sign in at https://www.amazon.%s/photos in your browser.\n2. Open Developer Tools > Network, reload, and select an authenticated request.\n3. Copy its Cookie request header (not Set-Cookie). Paste below, then press Enter.\n", rc.Domain)
			raw, err := read("Cookie (hidden; Ctrl+C to cancel): ", true)
			if err != nil {
				return r.FailError(meta, err)
			}
			cookies, err := config.ParseCookiesData([]byte(raw))
			if err != nil {
				return r.FailError(meta, err)
			}
			stored := make(map[string]string, len(rc.RequiredCookies))
			for _, key := range rc.RequiredCookies {
				if strings.TrimSpace(cookies[key]) == "" {
					return r.FailError(meta, &aphotos.ConfigError{Field: "cookies", Message: fmt.Sprintf("missing required cookie %q; copy the complete Cookie header from Amazon Photos", key)})
				}
				stored[key] = cookies[key]
			}
			client, err := aphotos.NewClient(aphotos.Options{Region: rc.Region, Cookies: stored, HTTPClient: app.HTTPClient, Transport: app.Transport})
			if err != nil {
				return r.FailError(meta, err)
			}
			defer func() { _ = client.Close() }()
			ctx, cancel := context.WithTimeout(cmd.Context(), app.Timeout)
			defer cancel()
			fmt.Fprintln(cmd.ErrOrStderr(), "Checking Amazon Photos session…")
			if _, err = client.GetRoot(ctx); err != nil {
				return r.FailError(meta, err)
			}
			cfgPath := config.ResolveConfigPath(app.ConfigFile)
			cfg, err := config.LoadOrCreate(cfgPath)
			if err != nil {
				return r.FailError(meta, err)
			}
			cfg.SetProfile(app.Profile, &config.Profile{Region: string(rc.Region), Cookies: stored})
			if err = config.Save(cfgPath, cfg); err != nil {
				return r.FailError(meta, err)
			}
			return r.Render(meta, AuthLoginData{Authenticated: true, Profile: app.Profile, Region: rc.Region, Evidence: rc.Evidence, ConfigPath: cfgPath}, nil, func() {
				r.Human("Signed in to Amazon Photos (%s). Saved profile %q. Next: aphotos usage", rc.Region, app.Profile)
			})
		},
	}
}

func loginInput(ctx context.Context, input io.Reader, output io.Writer) (read func(string, bool) (string, error), err error) {
	if input != nil {
		reader := bufio.NewReader(input)
		return func(prompt string, _ bool) (string, error) {
			fmt.Fprint(output, prompt)
			line, err := reader.ReadString('\n')
			return strings.TrimRight(line, "\r\n"), err
		}, nil
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, &aphotos.ConfigError{Field: "stdin", Message: "auth login requires an interactive terminal; use 'aphotos auth import <cookie-file>' for scripts"}
	}
	terminal := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{os.Stdin, output}, "")
	return func(prompt string, secret bool) (string, error) {
		state, err := term.MakeRaw(fd)
		if err != nil {
			return "", err
		}
		defer func() { _ = term.Restore(fd, state) }()
		type result struct {
			line string
			err  error
		}
		done := make(chan result, 1)
		go func() {
			var line string
			var err error
			if secret {
				line, err = terminal.ReadPassword(prompt)
			} else {
				terminal.SetPrompt(prompt)
				line, err = terminal.ReadLine()
			}
			done <- result{line, err}
		}()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case res := <-done:
			return res.line, res.err
		}
	}, nil
}
