package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to operator and to the CRI API",
	Long: `Log in to operator and to the CRI API, in the browser.

Two sessions: operator only accepts poulpy's client, which cannot get the
"api" scope of the CRI API (the classes behind F and --select-group). The
tokens are stored in ~/.cri-tokens and ~/.cri-api-tokens (see [tokens] in
the config file).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		auth := newAuth()
		if err := auth.Ensure(); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "operator: valid token stored in", auth.File)
		criAuth, err := newCRIAuth()
		if err != nil {
			return err
		}
		if err := criAuth.Ensure(); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "CRI API: valid token stored in", criAuth.File)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
