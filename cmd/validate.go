package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jesseheady/technician/internal/budget"
	"github.com/jesseheady/technician/internal/check"
	"github.com/jesseheady/technician/internal/config"
	"github.com/jesseheady/technician/internal/metrics"
	"github.com/spf13/cobra"
)

var (
	budgetFile      string
	validateOutput  string
	checkType       string
	excludeType     string
	failOnError     bool
	failOnBudget    bool
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Run checks and check performance budgets (CI mode)",
	Long:  "Run all configured checks, evaluate results against performance budgets, and exit with code 0 (pass) or 1 (violations found).",
	RunE:  runValidate,
}

func init() {
	validateCmd.Flags().StringVar(&budgetFile, "budget", "budgets.yml", "path to budget definitions file")
	validateCmd.Flags().StringVarP(&validateOutput, "output", "o", "text", "output format: text, json, gha")
	validateCmd.Flags().StringVar(&checkType, "check-type", "", "only run checks of this type (comma-separated, e.g. playwright)")
	validateCmd.Flags().StringVar(&excludeType, "exclude-type", "", "skip checks of this type (comma-separated, e.g. playwright)")
	validateCmd.Flags().BoolVar(&failOnError, "fail-on-error", false, "exit 1 if any check has an infrastructure error")
	validateCmd.Flags().BoolVar(&failOnBudget, "fail-on-budget", true, "exit 1 if any budget threshold is violated")
	rootCmd.AddCommand(validateCmd)
}

// parseTypeSet splits a comma-separated flag value into a set of CheckTypes.
func parseTypeSet(flag string) map[config.CheckType]bool {
	if flag == "" {
		return nil
	}
	m := make(map[config.CheckType]bool)
	for _, t := range strings.Split(flag, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			m[config.CheckType(t)] = true
		}
	}
	return m
}

// shouldRun returns true if the check passes the --check-type / --exclude-type filters.
func shouldRun(ct config.CheckType, include, exclude map[config.CheckType]bool) bool {
	if include != nil && !include[ct] {
		return false
	}
	if exclude != nil && exclude[ct] {
		return false
	}
	return true
}

func runValidate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	checksPath := config.ResolveChecksPath(cfgFile)
	checks, err := config.LoadChecks(checksPath)
	if err != nil {
		return fmt.Errorf("loading checks: %w", err)
	}

	budgets, err := budget.LoadBudgets(budgetFile)
	if err != nil {
		return fmt.Errorf("loading budgets: %w", err)
	}

	origin := cfg.ResolveOrigin(originID)
	checkers := newCheckers(cfg)

	include := parseTypeSet(checkType)
	exclude := parseTypeSet(excludeType)

	ctx := context.Background()
	var allViolations []budget.Violation
	var infraErrors []*check.Result

	for i := range checks {
		pc := &checks[i]

		if !shouldRun(pc.Type, include, exclude) {
			continue
		}

		checker, ok := checkers[pc.Type]
		if !ok {
			slog.Warn("No checker for type, skipping", "type", pc.Type, "name", pc.Name)
			continue
		}

		result := checker.Run(ctx, pc, origin)
		metrics.RecordResult(result)

		if result.InfraError {
			infraErrors = append(infraErrors, result)
			slog.Error("Infrastructure error", "name", result.Name, "type", result.Type, "error", result.Error)
			continue
		}

		violations := budget.Evaluate(result, budgets)
		for _, v := range violations {
			metrics.RecordBudgetViolation(v.Check, v.Metric, true, origin)
		}
		allViolations = append(allViolations, violations...)
	}

	reporter := budget.NewReporter(validateOutput, os.Stdout)
	if err := reporter.Report(allViolations); err != nil {
		return fmt.Errorf("reporting: %w", err)
	}

	if failOnError && len(infraErrors) > 0 {
		fmt.Fprintf(os.Stderr, "%d check(s) had infrastructure errors\n", len(infraErrors))
		os.Exit(1)
	}

	if failOnBudget && len(allViolations) > 0 {
		os.Exit(1)
	}

	return nil
}
