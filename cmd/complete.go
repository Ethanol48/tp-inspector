package cmd

import (
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"tp-inspector/internal/data"
	"tp-inspector/internal/forge"
	"tp-inspector/internal/tp"
)

// Completion only reads the cache: it must be instant and never log in.

// cachedActivities returns the activities saved by a previous run.
func cachedActivities() []forge.Activity {
	var acts []forge.Activity
	(&data.Cache{Root: cacheDir}).ReadJSON("activities.json", &acts)
	return acts
}

// completeSlug offers the activities of your tenants, then the assignments
// cached on disk, with their names as descriptions.
func completeSlug(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	desc := map[string]string{}
	for _, a := range cachedActivities() {
		if _, err := tp.Parse(a.Slug); err == nil {
			desc[a.Slug] = a.Name
		}
	}
	if dirs, err := os.ReadDir(cacheDir); err == nil {
		for _, d := range dirs {
			if _, err := tp.Parse(d.Name()); err == nil && d.IsDir() {
				if _, ok := desc[d.Name()]; !ok {
					desc[d.Name()] = "cached"
				}
			}
		}
	}
	return withPrefix(desc, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// completeTenant offers the tenants of the cached activities.
func completeTenant(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	desc := map[string]string{}
	for _, a := range cachedActivities() {
		if a.TenantSlug != "" {
			desc[a.TenantSlug] = ""
		}
	}
	return withPrefix(desc, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// withPrefix returns the sorted keys starting with prefix, as "key\tdesc".
func withPrefix(desc map[string]string, prefix string) []string {
	var out []string
	for k, d := range desc {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if d != "" {
			k += "\t" + d
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
