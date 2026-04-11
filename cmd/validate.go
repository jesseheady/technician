package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/m0nkey/technician/internal/budget"
	"github.com/m0nkey/technician/internal/config"
	"github.com/m0nkey/technician/internal/metrics"
	"github.com/spf13/cobra"
)

var (
	budgetFile     string
	validateOutput string
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
	rootCmd.AddCommand(validateCmd)
}

func runValidate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	checksDir := config.ResolveChecksDir(cfgFile)
	checks, err := config.LoadChecks(checksDir)
	if err != nil {
		return fmt.Errorf("loading checks: %w", err)
	}

	budgets, err := budget.LoadBudgets(budgetFile)
	if err != nil {
		return fmt.Errorf("loading budgets: %w", err)
	}

	site := cfg.ResolveSite(siteCode)

	probers := newCheckers(cfg)

	ctx := context.Background()
	var allViolations []budget.Violation

	for i := range checks {
		pc := &checks[i]
		checker, ok := probers[pc.Type]
		if !ok {
			slog.Warn("No prober for type, skipping", "type", pc.Type, "name", pc.Name)
			continue
		}

		result := checker.Run(ctx, pc, site)
		metrics.RecordResult(result)

		violations := budget.Evaluate(result, budgets)
		for _, v := range violations {
			metrics.RecordBudgetViolation(v.Check, v.Metric, true, site)
		}
		allViolations = append(allViolations, violations...)
	}

	reporter := budget.NewReporter(validateOutput, os.Stdout)
	if err := reporter.Report(allViolations); err != nil {
		return fmt.Errorf("reporting: %w", err)
	}

	if len(allViolations) > 0 {
		os.Exit(1)
	}

	return nil
}
