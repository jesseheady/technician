package cmd

import (
	"fmt"
	"log/slog"

	"github.com/jesseheady/technician/internal/config"
	"github.com/spf13/cobra"
)

// Shared --types / --groups / --tags flag values. Registered per command by
// addFilterFlags so they can override the technician.yml check_filter block.
var (
	filterTypes  string
	filterGroups string
	filterTags   string
)

// addFilterFlags registers the check-filter flags on a command.
func addFilterFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&filterTypes, "types", "", "only run checks of these types (comma-separated, e.g. http,dns)")
	cmd.Flags().StringVar(&filterGroups, "groups", "", "only run checks in these groups (comma-separated)")
	cmd.Flags().StringVar(&filterTags, "tags", "", "only run checks with any of these tags (comma-separated)")
}

// effectiveFilter merges the config check_filter with any --types/--groups/--tags
// overrides. A non-empty flag replaces that dimension entirely.
func effectiveFilter(cfgFilter config.CheckFilter) config.CheckFilter {
	f := cfgFilter
	if filterTypes != "" {
		f.Types = config.SplitCSV(filterTypes)
	}
	if filterGroups != "" {
		f.Groups = config.SplitCSV(filterGroups)
	}
	if filterTags != "" {
		f.Tags = config.SplitCSV(filterTags)
	}
	return f
}

// loadFilteredChecks loads all checks and applies the effective check filter
// (config check_filter merged with CLI overrides). It is the single load path
// for worker, check, and validate so filtering behaves identically everywhere.
func loadFilteredChecks(cfg *config.Config) ([]config.CheckConfig, error) {
	checksPath := config.ResolveChecksPath(cfgFile)
	checks, err := config.LoadChecks(checksPath)
	if err != nil {
		return nil, fmt.Errorf("loading checks: %w", err)
	}

	filter := effectiveFilter(cfg.CheckFilter)
	if err := filter.Validate(); err != nil {
		return nil, err
	}
	if filter.IsEmpty() {
		return checks, nil
	}

	filtered := config.FilterChecks(checks, filter)
	if len(filtered) == 0 && len(checks) > 0 {
		slog.Warn("check_filter matched no checks — this worker will run nothing",
			"types", filter.Types, "groups", filter.Groups, "tags", filter.Tags)
	} else {
		slog.Info("Applied check filter",
			"matched", len(filtered), "total", len(checks),
			"types", filter.Types, "groups", filter.Groups, "tags", filter.Tags)
	}
	return filtered, nil
}
