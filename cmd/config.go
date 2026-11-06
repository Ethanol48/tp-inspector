package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"tp-inspector/internal/config"
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
  tpi config init`,
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

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func init() {
	configInitCmd.Flags().BoolVarP(&configForce, "force", "f", false, "overwrite an existing file")
	configCmd.AddCommand(configInitCmd)
	rootCmd.AddCommand(configCmd)
}
