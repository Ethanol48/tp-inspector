package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"tp-inspector/internal/data"
	"tp-inspector/internal/ui"
)

var (
	cleanAll   bool
	cleanRepos bool
	cleanYes   bool
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Delete cached TP data and cloned repos",
	Long: `Delete cached TP data and cloned repos.

Without flags, opens an interface to choose what to delete.`,
	Example: `  # choose what to delete
  tpi clean

  # delete every cloned repo, without confirmation
  tpi clean --repos -y

  # delete the whole cache (TP data and repos)
  tpi clean --all`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !cleanAll && !cleanRepos {
			return ui.RunClean(cacheDir)
		}
		return cleanAuto(cacheDir)
	},
}

func init() {
	f := cleanCmd.Flags()
	f.BoolVar(&cleanAll, "all", false, "delete the whole cache: TP data and cloned repos")
	f.BoolVar(&cleanRepos, "repos", false, "delete every cloned repo, keep TP data")
	f.BoolVarP(&cleanYes, "yes", "y", false, "do not ask for confirmation")
	cleanCmd.MarkFlagsMutuallyExclusive("all", "repos")
	rootCmd.AddCommand(cleanCmd)
}

// cleanAuto implements `tpi clean --all|--repos [-y]`.
func cleanAuto(root string) error {
	entries, err := data.ScanCache(root)
	if err != nil {
		return err
	}
	var bytes int64
	var nRepos int
	for _, e := range entries {
		bytes += e.ReposBytes
		nRepos += len(e.Repos)
		if cleanAll {
			bytes += e.DataBytes
		}
	}

	what := fmt.Sprintf("%d cloned repos (%s)", nRepos, data.HumanBytes(bytes))
	if cleanAll {
		what = fmt.Sprintf("the whole cache %s: %d assignments, %d repos (%s)", root, len(entries), nRepos, data.HumanBytes(bytes))
	}

	if len(entries) == 0 || (cleanRepos && nRepos == 0) {
		fmt.Fprintln(os.Stderr, "Nothing to delete.")
		return nil
	}

	if !cleanYes && !confirm("Delete "+what+"?") {
		fmt.Fprintln(os.Stderr, "Cancelled.")
		return nil
	}

	if cleanAll {
		err = data.RemoveEverything(root)
	} else {
		err = data.RemoveAllRepos(root)
	}
	if err == nil {
		fmt.Fprintln(os.Stderr, "Deleted", what+".")
	}
	return err
}

// confirm asks a yes/no question on the terminal; the default is no.
func confirm(q string) bool {
	fmt.Fprint(os.Stderr, q+" [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes"
}
