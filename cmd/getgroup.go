package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"tp-inspector/internal/cri"
	"tp-inspector/internal/tp"
	"tp-inspector/internal/ui"
)

var getGroupCmd = &cobra.Command{
	Use:   "get-group [<slug>]",
	Short: "Pick CRI classes, print their members' logins (experimental)",
	Long: `Pick CRI classes in an interface and print the logins of their members
on stdout, one per line, so the output feeds --logins. With an assignment
slug, only the classes that fit it are offered.`,
	Example: `  # save the logins of your classes, then open the TUI on them
  tpi get-group prog-206-p-01-2030 > group.txt
  tpi --logins group.txt prog-206-p-01-2030`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeSlug,
	RunE: func(cmd *cobra.Command, args []string) error {
		var filter cri.Filter
		if len(args) > 0 {
			slug, err := tp.Parse(args[0])
			if err != nil {
				return err
			}
			filter = cri.ActivityFilter(slug.Year, slug.Bimester)
		}
		auth, src, err := newCRI()
		if err != nil {
			return err
		}
		if err := auth.Ensure(); err != nil {
			return err
		}
		logins, err := ui.RunGetGroup(src, filter)
		for _, l := range logins {
			fmt.Println(l)
		}
		return err
	},
}

func init() {
	rootCmd.AddCommand(getGroupCmd)
}
