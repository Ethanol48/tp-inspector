package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"tp-inspector/internal/config"
	"tp-inspector/internal/ui"
)

var configForce bool

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show the config file and the settings in use",
	Long: `Show the config file and the settings in use.

The config file is ` + config.DefaultPath() + ` (or --config, or
$` + config.EnvPath + `). Every setting is optional; command line flags and
environment variables take precedence. ` + "`tpi config init`" + ` writes a commented
template.`,
	Example: `  # where the settings come from
  tpi config

  # write a commented template to edit
  tpi config init

  # preview the color themes
  tpi config themes`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		file := cfg.File
		if file == "" {
			p, _ := config.Path(configFile)
			file = p + " (not found, defaults in use)"
		}
		criAuth, err := newCRIAuth()
		if err != nil {
			return err
		}
		w := cmd.OutOrStdout()
		fmt.Fprintln(w, "config file:     ", file)
		fmt.Fprintln(w, "cache_dir:       ", cacheDir)
		fmt.Fprintln(w, "tenant:          ", orDefault(tenant, "(looked up)"))
		fmt.Fprintln(w, "logins:          ", orDefault(loginsFile, "(none)"))
		fmt.Fprintln(w, "tokens.operator: ", newAuth().File)
		fmt.Fprintln(w, "tokens.cri_api:  ", criAuth.File)
		fmt.Fprintln(w, "cri_client.id:   ", criAuth.ClientID)
		fmt.Fprintln(w, "theme:           ", orDefault(cfg.Theme, ui.DefaultTheme), colorsNote())
		fmt.Fprintln(w, "background:      ", orDefault(cfg.Background, "auto"))
		return nil
	},
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Write a commented config file template",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, _ := config.Path(configFile)
		p = config.Expand(p, "")
		if _, err := os.Stat(p); err == nil && !configForce {
			return fmt.Errorf("%s already exists (--force to overwrite)", p)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(config.Template), 0o644); err != nil {
			return err
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "wrote", p)
		return nil
	},
}

var configThemesCmd = &cobra.Command{
	Use:   "themes",
	Short: "Preview the color themes of the TUI",
	Long: `Preview the color themes of the TUI, with your [colors] overrides
left out. Set one with theme = "<name>" in the config file.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		w := cmd.OutOrStdout()
		for _, n := range ui.ThemeNames() {
			mark := "  "
			if n == orDefault(cfg.Theme, ui.DefaultTheme) {
				mark = "* "
			}
			fmt.Fprintf(w, "%s%-11s %s\n\n", mark, n, ui.ThemePreview(n))
		}
	},
}

// colorsNote lists the [colors] overrides of the config.
func colorsNote() string {
	if len(cfg.Colors) == 0 {
		return ""
	}
	names := make([]string, 0, len(cfg.Colors))
	for n := range cfg.Colors {
		names = append(names, n)
	}
	sort.Strings(names)
	return "(overridden: " + strings.Join(names, ", ") + ")"
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func init() {
	configInitCmd.Flags().BoolVarP(&configForce, "force", "f", false, "overwrite an existing file")
	configCmd.AddCommand(configInitCmd, configThemesCmd)
	rootCmd.AddCommand(configCmd)
}
