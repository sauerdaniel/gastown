package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/refinery"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/workspace"
)

// Refinery command flags
var (
	refineryForeground bool
	refineryStatusJSON bool
	refineryQueueJSON  bool
)

var refineryCmd = &cobra.Command{
	Use:     "refinery",
	Aliases: []string{"ref"},
	Short:   "Manage the merge queue processor",
	Long: `Manage the Refinery merge queue processor for a rig.

The Refinery processes merge requests from polecats, merging their work
into integration branches and ultimately to main.`,
}

var refineryStartCmd = &cobra.Command{
	Use:   "start [rig]",
	Short: "Start the refinery",
	Long: `Start the Refinery for a rig.

Launches the merge queue processor which monitors for polecat work branches
and merges them to the appropriate target branches.

If rig is not specified, infers it from the current directory.

Examples:
  gt refinery start gastown
  gt refinery start gastown --foreground
  gt refinery start              # infer rig from cwd`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRefineryStart,
}

var refineryStopCmd = &cobra.Command{
	Use:   "stop [rig]",
	Short: "Stop the refinery",
	Long: `Stop a running Refinery.

Gracefully stops the refinery, completing any in-progress merge first.
If rig is not specified, infers it from the current directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRefineryStop,
}

var refineryStatusCmd = &cobra.Command{
	Use:   "status [rig]",
	Short: "Show refinery status",
	Long: `Show the status of a rig's Refinery.

Displays running state, current work, queue length, and statistics.
If rig is not specified, infers it from the current directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRefineryStatus,
}

var refineryQueueCmd = &cobra.Command{
	Use:   "queue [rig]",
	Short: "Show merge queue",
	Long: `Show the merge queue for a rig.

Lists all pending merge requests waiting to be processed.
If rig is not specified, infers it from the current directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRefineryQueue,
}

var refineryAttachCmd = &cobra.Command{
	Use:   "attach [rig]",
	Short: "Attach to refinery session",
	Long: `Attach to a running Refinery's Claude session.

Allows interactive access to the Refinery agent for debugging
or manual intervention.

If rig is not specified, infers it from the current directory.

Examples:
  gt refinery attach gastown
  gt refinery attach          # infer rig from cwd`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRefineryAttach,
}

func init() {
	// Start flags
	refineryStartCmd.Flags().BoolVar(&refineryForeground, "foreground", false, "Run in foreground (default: background)")

	// Status flags
	refineryStatusCmd.Flags().BoolVar(&refineryStatusJSON, "json", false, "Output as JSON")

	// Queue flags
	refineryQueueCmd.Flags().BoolVar(&refineryQueueJSON, "json", false, "Output as JSON")

	// Add subcommands
	refineryCmd.AddCommand(refineryStartCmd)
	refineryCmd.AddCommand(refineryStopCmd)
	refineryCmd.AddCommand(refineryStatusCmd)
	refineryCmd.AddCommand(refineryQueueCmd)
	refineryCmd.AddCommand(refineryAttachCmd)

	rootCmd.AddCommand(refineryCmd)
}

// getRefineryManager creates a refinery manager for a rig.
// If rigName is empty, infers the rig from cwd.
func getRefineryManager(rigName string) (*refinery.Manager, *rig.Rig, string, error) {
	// Infer rig from cwd if not provided
	if rigName == "" {
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return nil, nil, "", fmt.Errorf("not in a Gas Town workspace: %w", err)
		}
		rigName, err = inferRigFromCwd(townRoot)
		if err != nil {
			return nil, nil, "", fmt.Errorf("could not determine rig: %w\nUsage: gt refinery <command> <rig>", err)
		}
	}

	_, r, err := getRig(rigName)
	if err != nil {
		return nil, nil, "", err
	}

	mgr := refinery.NewManager(r)
	return mgr, r, rigName, nil
}

func runRefineryStart(cmd *cobra.Command, args []string) error {
	rigName := ""
	if len(args) > 0 {
		rigName = args[0]
	}

	mgr, _, rigName, err := getRefineryManager(rigName)
	if err != nil {
		return err
	}

	fmt.Printf("Starting refinery for %s...\n", rigName)

	if err := mgr.Start(refineryForeground); err != nil {
		if err == refinery.ErrAlreadyRunning {
			fmt.Printf("%s Refinery is already running\n", style.Dim.Render("⚠"))
			return nil
		}
		return fmt.Errorf("starting refinery: %w", err)
	}

	if refineryForeground {
		// This will block until stopped
		return nil
	}

	fmt.Printf("%s Refinery started for %s\n", style.Bold.Render("✓"), rigName)
	fmt.Printf("  %s\n", style.Dim.Render("Use 'gt refinery status' to check progress"))
	return nil
}

func runRefineryStop(cmd *cobra.Command, args []string) error {
	rigName := ""
	if len(args) > 0 {
		rigName = args[0]
	}

	mgr, _, rigName, err := getRefineryManager(rigName)
	if err != nil {
		return err
	}

	if err := mgr.Stop(); err != nil {
		if err == refinery.ErrNotRunning {
			fmt.Printf("%s Refinery is not running\n", style.Dim.Render("⚠"))
			return nil
		}
		return fmt.Errorf("stopping refinery: %w", err)
	}

	fmt.Printf("%s Refinery stopped for %s\n", style.Bold.Render("✓"), rigName)
	return nil
}

func runRefineryStatus(cmd *cobra.Command, args []string) error {
	rigName := ""
	if len(args) > 0 {
		rigName = args[0]
	}

	mgr, _, rigName, err := getRefineryManager(rigName)
	if err != nil {
		return err
	}

	ref, err := mgr.Status()
	if err != nil {
		return fmt.Errorf("getting status: %w", err)
	}

	// JSON output
	if refineryStatusJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(ref)
	}

	// Human-readable output
	fmt.Printf("%s Refinery: %s\n\n", style.Bold.Render("⚙"), rigName)

	stateStr := string(ref.State)
	switch ref.State {
	case refinery.StateRunning:
		stateStr = style.Bold.Render("● running")
	case refinery.StateStopped:
		stateStr = style.Dim.Render("○ stopped")
	case refinery.StatePaused:
		stateStr = style.Dim.Render("⏸ paused")
	}
	fmt.Printf("  State: %s\n", stateStr)

	if ref.StartedAt != nil {
		fmt.Printf("  Started: %s\n", ref.StartedAt.Format("2006-01-02 15:04:05"))
	}

	if ref.CurrentMR != nil {
		fmt.Printf("\n  %s\n", style.Bold.Render("Currently Processing:"))
		fmt.Printf("    Branch: %s\n", ref.CurrentMR.Branch)
		fmt.Printf("    Worker: %s\n", ref.CurrentMR.Worker)
		if ref.CurrentMR.IssueID != "" {
			fmt.Printf("    Issue:  %s\n", ref.CurrentMR.IssueID)
		}
	}

	// Get queue length
	queue, _ := mgr.Queue()
	pendingCount := 0
	for _, item := range queue {
		if item.Position > 0 { // Not currently processing
			pendingCount++
		}
	}
	fmt.Printf("\n  Queue: %d pending\n", pendingCount)

	if ref.LastMergeAt != nil {
		fmt.Printf("  Last merge: %s\n", ref.LastMergeAt.Format("2006-01-02 15:04:05"))
	}

	fmt.Printf("\n  %s\n", style.Bold.Render("Statistics:"))
	fmt.Printf("    Merged today:  %d\n", ref.Stats.TodayMerged)
	fmt.Printf("    Failed today:  %d\n", ref.Stats.TodayFailed)
	fmt.Printf("    Total merged:  %d\n", ref.Stats.TotalMerged)
	fmt.Printf("    Total failed:  %d\n", ref.Stats.TotalFailed)

	return nil
}

func runRefineryQueue(cmd *cobra.Command, args []string) error {
	rigName := ""
	if len(args) > 0 {
		rigName = args[0]
	}

	mgr, _, rigName, err := getRefineryManager(rigName)
	if err != nil {
		return err
	}

	queue, err := mgr.Queue()
	if err != nil {
		return fmt.Errorf("getting queue: %w", err)
	}

	// JSON output
	if refineryQueueJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(queue)
	}

	// Human-readable output
	fmt.Printf("%s Merge queue for '%s':\n\n", style.Bold.Render("📋"), rigName)

	if len(queue) == 0 {
		fmt.Printf("  %s\n", style.Dim.Render("(empty)"))
		return nil
	}

	for _, item := range queue {
		status := ""
		prefix := fmt.Sprintf("  %d.", item.Position)

		if item.Position == 0 {
			prefix = "  ▶"
			status = style.Bold.Render("[processing]")
		} else {
			switch item.MR.Status {
			case refinery.MROpen:
				if item.MR.Error != "" {
					status = style.Dim.Render("[needs-rework]")
				} else {
					status = style.Dim.Render("[pending]")
				}
			case refinery.MRInProgress:
				status = style.Bold.Render("[processing]")
			case refinery.MRClosed:
				switch item.MR.CloseReason {
				case refinery.CloseReasonMerged:
					status = style.Bold.Render("[merged]")
				case refinery.CloseReasonRejected:
					status = style.Dim.Render("[rejected]")
				case refinery.CloseReasonConflict:
					status = style.Dim.Render("[conflict]")
				case refinery.CloseReasonSuperseded:
					status = style.Dim.Render("[superseded]")
				default:
					status = style.Dim.Render("[closed]")
				}
			}
		}

		issueInfo := ""
		if item.MR.IssueID != "" {
			issueInfo = fmt.Sprintf(" (%s)", item.MR.IssueID)
		}

		fmt.Printf("%s %s %s/%s%s %s\n",
			prefix,
			status,
			item.MR.Worker,
			item.MR.Branch,
			issueInfo,
			style.Dim.Render(item.Age))
	}

	return nil
}

func runRefineryAttach(cmd *cobra.Command, args []string) error {
	rigName := ""
	if len(args) > 0 {
		rigName = args[0]
	}

	// Use getRefineryManager to validate rig (and infer from cwd if needed)
	mgr, _, rigName, err := getRefineryManager(rigName)
	if err != nil {
		return err
	}

	// Session name follows the same pattern as refinery manager
	sessionID := fmt.Sprintf("gt-%s-refinery", rigName)

	// Check if session exists
	t := tmux.NewTmux()
	running, err := t.HasSession(sessionID)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !running {
		// Auto-start if not running
		fmt.Printf("Refinery not running for %s, starting...\n", rigName)
		if err := mgr.Start(false); err != nil {
			return fmt.Errorf("starting refinery: %w", err)
		}
		fmt.Printf("%s Refinery started\n", style.Bold.Render("✓"))
	}

	// Attach to session using exec to properly forward TTY
	return attachToTmuxSession(sessionID)
}
