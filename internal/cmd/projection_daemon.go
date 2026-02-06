package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/projection"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

var projectionDaemonCmd = &cobra.Command{
	Use:     "projection-daemon",
	GroupID: GroupServices,
	Short:   "Manage the Beads → Mission Control projection sync daemon",
	RunE:    requireSubcommand,
	Long: `Manage the projection sync daemon.

This daemon continuously syncs Beads data to Mission Control projections,
providing a read-only view of work state for the Mission Control UI.

It syncs:
- Tasks (issues) → tasks table + tasks.json
- Agents → agents table + agents.json
- Activity (events) → activities table + activity.jsonl`,
}

var projectionDaemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the projection sync daemon",
	Long: `Start the projection sync daemon in the background.

The daemon polls Beads every 30 seconds and updates the Mission Control
cache and projection database.`,
	RunE: runProjectionDaemonStart,
}

var projectionDaemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the projection sync daemon",
	Long:  `Stop the running projection sync daemon.`,
	RunE: runProjectionDaemonStop,
}

var projectionDaemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show projection daemon status",
	Long:  `Show the current status of the projection sync daemon.`,
	RunE: runProjectionDaemonStatus,
}

var projectionDaemonLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View projection daemon logs",
	Long:  `View the projection daemon log file.`,
	RunE: runProjectionDaemonLogs,
}

var projectionDaemonRunCmd = &cobra.Command{
	Use:    "run",
	Short:  "Run projection daemon in foreground (internal)",
	Hidden: true,
	RunE:   runProjectionDaemonRun,
}

var projectionDaemonOnceCmd = &cobra.Command{
	Use:   "once",
	Short: "Run a single sync and exit",
	Long:  `Perform a one-time sync from Beads to Mission Control projections.`,
	RunE: runProjectionDaemonOnce,
}

var (
	projectionLogLines     int
	projectionLogFollow    bool
	projectionPollInterval string
)

func init() {
	projectionDaemonCmd.AddCommand(projectionDaemonStartCmd)
	projectionDaemonCmd.AddCommand(projectionDaemonStopCmd)
	projectionDaemonCmd.AddCommand(projectionDaemonStatusCmd)
	projectionDaemonCmd.AddCommand(projectionDaemonLogsCmd)
	projectionDaemonCmd.AddCommand(projectionDaemonRunCmd)
	projectionDaemonCmd.AddCommand(projectionDaemonOnceCmd)

	projectionDaemonLogsCmd.Flags().IntVarP(&projectionLogLines, "lines", "n", 50, "Number of lines to show")
	projectionDaemonLogsCmd.Flags().BoolVarP(&projectionLogFollow, "follow", "f", false, "Follow log output")
	projectionDaemonStartCmd.Flags().StringVarP(&projectionPollInterval, "interval", "i", "30s", "Poll interval (e.g., 30s, 1m)")
	projectionDaemonRunCmd.Flags().String("interval", "30s", "Poll interval (e.g., 30s, 1m)")

	rootCmd.AddCommand(projectionDaemonCmd)
}

func runProjectionDaemonStart(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	// Check if already running
	running, pid, err := projection.IsRunning(townRoot)
	if err != nil {
		return fmt.Errorf("checking daemon status: %w", err)
	}
	if running {
		return fmt.Errorf("daemon already running (PID %d)", pid)
	}

	// Parse poll interval
	var pollInterval time.Duration
	if projectionPollInterval != "" {
		pollInterval, err = time.ParseDuration(projectionPollInterval)
		if err != nil {
			return fmt.Errorf("parsing poll interval: %w", err)
		}
	}

	// Start daemon in background
	gtPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable: %w", err)
	}

	daemonArgs := []string{"projection-daemon", "run"}
	if pollInterval > 0 {
		daemonArgs = append(daemonArgs, "--interval", pollInterval.String())
	}

	daemonCmd := exec.Command(gtPath, daemonArgs...)
	daemonCmd.Dir = townRoot

	// Detach from terminal
	daemonCmd.Stdin = nil
	daemonCmd.Stdout = nil
	daemonCmd.Stderr = nil

	if err := daemonCmd.Start(); err != nil {
		return fmt.Errorf("starting daemon: %w", err)
	}

	// Wait a moment for the daemon to initialize and acquire the lock
	time.Sleep(200 * time.Millisecond)

	// Verify it started
	running, pid, err = projection.IsRunning(townRoot)
	if err != nil {
		return fmt.Errorf("checking daemon status: %w", err)
	}
	if !running {
		return fmt.Errorf("daemon failed to start (check logs with 'gt projection-daemon logs')")
	}

	fmt.Printf("%s Projection sync daemon started (PID %d)\n", style.Bold.Render("✓"), pid)
	return nil
}

func runProjectionDaemonStop(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	running, pid, err := projection.IsRunning(townRoot)
	if err != nil {
		return fmt.Errorf("checking daemon status: %w", err)
	}
	if !running {
		return fmt.Errorf("daemon is not running")
	}

	if err := projection.StopDaemon(townRoot); err != nil {
		return fmt.Errorf("stopping daemon: %w", err)
	}

	fmt.Printf("%s Projection sync daemon stopped (was PID %d)\n", style.Bold.Render("✓"), pid)
	return nil
}

func runProjectionDaemonStatus(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	running, pid, err := projection.IsRunning(townRoot)
	if err != nil {
		return fmt.Errorf("checking daemon status: %w", err)
	}

	if running {
		fmt.Printf("%s Projection sync daemon is %s (PID %d)\n",
			style.Bold.Render("●"),
			style.Bold.Render("running"),
			pid)

		// Load state for more details
		state, err := projection.LoadState(townRoot)
		if err == nil && !state.StartedAt.IsZero() {
			fmt.Printf("  Started: %s\n", state.StartedAt.Format("2006-01-02 15:04:05"))
			fmt.Printf("  Last sync: %s\n", state.LastSync.Format("15:04:05"))
			fmt.Printf("  Total syncs: %d\n", state.SyncCount)
			if state.ErrorCount > 0 {
				fmt.Printf("  Errors: %d\n", state.ErrorCount)
			}
		}
	} else {
		fmt.Printf("%s Projection sync daemon is %s\n",
			style.Dim.Render("○"),
			"not running")
		fmt.Printf("\nStart with: %s\n", style.Dim.Render("gt projection-daemon start"))
	}

	return nil
}

func runProjectionDaemonLogs(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	logFile := filepath.Join(townRoot, "daemon", "projection-sync.log")

	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		return fmt.Errorf("no log file found at %s", logFile)
	}

	if projectionLogFollow {
		// Use tail -f for following
		tailCmd := exec.Command("tail", "-f", logFile)
		tailCmd.Stdout = os.Stdout
		tailCmd.Stderr = os.Stderr
		return tailCmd.Run()
	}

	// Use tail -n for last N lines
	tailCmd := exec.Command("tail", "-n", fmt.Sprintf("%d", projectionLogLines), logFile)
	tailCmd.Stdout = os.Stdout
	tailCmd.Stderr = os.Stderr
	return tailCmd.Run()
}

func runProjectionDaemonRun(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	// Parse interval flag
	var pollInterval time.Duration
	if interval, _ := cmd.Flags().GetString("interval"); interval != "" {
		pollInterval, err = time.ParseDuration(interval)
		if err != nil {
			return fmt.Errorf("parsing interval: %w", err)
		}
	}

	config := projection.DefaultConfig(townRoot, pollInterval)
	d, err := projection.New(config)
	if err != nil {
		return fmt.Errorf("creating daemon: %w", err)
	}

	return d.Run()
}

func runProjectionDaemonOnce(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	config := projection.DefaultConfig(townRoot, 0) // No polling, single sync
	d, err := projection.New(config)
	if err != nil {
		return fmt.Errorf("creating daemon: %w", err)
	}

	fmt.Println("Running one-time sync...")
	if err := d.Sync(); err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	fmt.Println("✓ Sync completed")
	return nil
}
