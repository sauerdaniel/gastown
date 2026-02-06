package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/witness"
	"github.com/steveyegge/gastown/internal/workspace"
)

// Health command flags
var (
	healthCheckJSON bool
)

var healthCmd = &cobra.Command{
	Use:     "health",
	GroupID: GroupDiag,
	Short:   "Check worker health and heartbeats",
	RunE:    requireSubcommand,
	Long: `Check worker health based on heartbeat signals.

Workers send periodic heartbeats to signal liveness. This command checks
all active workers and updates their health state based on heartbeat age:
- healthy: heartbeat within timeout window
- stale: heartbeat overdue (> timeout, < 2x timeout)
- dead: no heartbeat for 2x timeout

When a worker transitions to dead, any assigned work is reassigned.`,
}

var healthCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check all worker health states",
	Long: `Check all active workers for stale heartbeats and update health states.

This command:
1. Queries all agent beads with heartbeat tracking
2. Checks heartbeat age against timeout thresholds
3. Transitions health states: healthy → stale → dead
4. Reassigns work from dead workers
5. Reports health state changes

Typically called by witness patrol (survey-workers step) to monitor worker health.

Examples:
  gt health check
  gt health check --json`,
	RunE: runHealthCheck,
}

func init() {
	healthCheckCmd.Flags().BoolVar(&healthCheckJSON, "json", false, "Output as JSON")

	healthCmd.AddCommand(healthCheckCmd)
	rootCmd.AddCommand(healthCmd)
}

func runHealthCheck(cmd *cobra.Command, args []string) error {
	// Find town root
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	// Run health check
	results, err := witness.CheckWorkerHealth(townRoot)
	if err != nil {
		return fmt.Errorf("checking worker health: %w", err)
	}

	// JSON output
	if healthCheckJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}

	// Human-readable output
	if len(results) == 0 {
		fmt.Println(style.Dim.Render("No workers with heartbeat tracking found"))
		return nil
	}

	fmt.Printf("%s Worker Health Check (%d workers)\n\n", style.Bold.Render("💓"), len(results))

	healthCounts := make(map[string]int)
	actionCounts := make(map[string]int)
	errorCount := 0

	for _, result := range results {
		healthCounts[result.CurrentHealth]++
		
		if result.Error != nil {
			errorCount++
			fmt.Printf("  %s %s: %s\n", 
				style.Bold.Render("✗"),
				result.WorkerName,
				style.Dim.Render(result.Error.Error()))
			continue
		}

		// Determine icon based on current health
		var icon string
		switch result.CurrentHealth {
		case "healthy":
			icon = "●"
		case "stale":
			icon = "⚠"
		case "dead":
			icon = "✗"
		default:
			icon = "○"
		}

		// Show state change or current state
		if result.Action == "no change" {
			fmt.Printf("  %s %s: %s\n",
				style.Dim.Render(icon),
				result.WorkerName,
				style.Dim.Render(result.CurrentHealth))
		} else {
			actionCounts[result.Action]++
			fmt.Printf("  %s %s: %s\n",
				style.Bold.Render(icon),
				result.WorkerName,
				result.Action)
		}
	}

	// Summary
	fmt.Printf("\n%s Summary:\n", style.Bold.Render("Summary"))
	fmt.Printf("  Healthy: %d\n", healthCounts["healthy"])
	if healthCounts["stale"] > 0 {
		fmt.Printf("  Stale: %d\n", healthCounts["stale"])
	}
	if healthCounts["dead"] > 0 {
		fmt.Printf("  Dead: %d\n", healthCounts["dead"])
	}
	if errorCount > 0 {
		fmt.Printf("  Errors: %d\n", errorCount)
	}

	return nil
}
