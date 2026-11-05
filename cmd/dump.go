package cmd

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"tp-inspector/internal/forge"
)

var dumpCmd = &cobra.Command{
	Use:   "dump <path> [key=value...]",
	Short: "Raw GET on the operator API (debugging)",
	Long: `Raw GET on the operator API, for debugging: prints the response,
indented when it is JSON. The key=value pairs become the query string.`,
	Example: `  # path parameters containing / must be URL-encoded
  tpi dump /api/activities/activity/epita-prepa-computer-science%2Fprog-205-p-01-2030
  tpi dump /api/traces/epita-prepa-computer-science%2Fprog-205-p-01-2030/explore pageNum=0 pageSize=10`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completeAPIPath,
	RunE: func(cmd *cobra.Command, args []string) error {
		auth := forge.NewAuth()
		if err := auth.Ensure(); err != nil {
			return err
		}
		q := url.Values{}
		for _, s := range args[1:] {
			k, v, _ := strings.Cut(s, "=")
			q.Set(k, v)
		}
		b, err := forge.NewClient(auth).GetRaw(args[0], q)
		if err != nil {
			return err
		}
		var v any
		if json.Unmarshal(b, &v) == nil {
			b, _ = json.MarshalIndent(v, "", "  ")
		}
		_, err = os.Stdout.Write(append(b, '\n'))
		return err
	},
}

func init() {
	rootCmd.AddCommand(dumpCmd)
}
