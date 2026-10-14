package cli

import (
	"runtime"

	"github.com/spf13/cobra"
)

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

type VersionData struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"go_version"`
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := GetAppContext(cmd.Context())
			r := newRenderer(app, cmd)
			data := VersionData{
				Version:   Version,
				Commit:    Commit,
				Date:      Date,
				GoVersion: runtime.Version(),
			}
			return r.Render(metaInput(app, "version"), data, nil, func() {
				r.Human("aphotos version %s (commit: %s, date: %s, go: %s)", Version, Commit, Date, data.GoVersion)
			})
		},
	}
}

func newCompletionCmd(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate shell completion scripts",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "bash",
		Short: "Generate bash completion script",
		RunE: func(c *cobra.Command, args []string) error {
			return root.GenBashCompletion(c.OutOrStdout())
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "zsh",
		Short: "Generate zsh completion script",
		RunE: func(c *cobra.Command, args []string) error {
			return root.GenZshCompletion(c.OutOrStdout())
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "fish",
		Short: "Generate fish completion script",
		RunE: func(c *cobra.Command, args []string) error {
			return root.GenFishCompletion(c.OutOrStdout(), true)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "powershell",
		Short: "Generate powershell completion script",
		RunE: func(c *cobra.Command, args []string) error {
			return root.GenPowerShellCompletion(c.OutOrStdout())
		},
	})
	return cmd
}
