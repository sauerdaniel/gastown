package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/workspace"
)

// DeaconSessionName is the tmux session name for the Deacon.
const DeaconSessionName = "gt-deacon"

var deaconCmd = &cobra.Command{
	Use:     "deacon",
	Aliases: []string{"dea"},
	Short:   "Manage the Deacon session",
	Long: `Manage the Deacon tmux session.

The Deacon is the hierarchical health-check orchestrator for Gas Town.
It monitors the Mayor and Witnesses, handles lifecycle requests, and
keeps the town running. Use the subcommands to start, stop, attach,
and check status.`,
}

var deaconStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Deacon session",
	Long: `Start the Deacon tmux session.

Creates a new detached tmux session for the Deacon and launches Claude.
The session runs in the workspace root directory.`,
	RunE: runDeaconStart,
}

var deaconStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Deacon session",
	Long: `Stop the Deacon tmux session.

Attempts graceful shutdown first (Ctrl-C), then kills the tmux session.`,
	RunE: runDeaconStop,
}

var deaconAttachCmd = &cobra.Command{
	Use:     "attach",
	Aliases: []string{"at"},
	Short:   "Attach to the Deacon session",
	Long: `Attach to the running Deacon tmux session.

Attaches the current terminal to the Deacon's tmux session.
Detach with Ctrl-B D.`,
	RunE: runDeaconAttach,
}

var deaconStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check Deacon session status",
	Long:  `Check if the Deacon tmux session is currently running.`,
	RunE:  runDeaconStatus,
}

var deaconRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the Deacon session",
	Long: `Restart the Deacon tmux session.

Stops the current session (if running) and starts a fresh one.`,
	RunE: runDeaconRestart,
}

func init() {
	deaconCmd.AddCommand(deaconStartCmd)
	deaconCmd.AddCommand(deaconStopCmd)
	deaconCmd.AddCommand(deaconAttachCmd)
	deaconCmd.AddCommand(deaconStatusCmd)
	deaconCmd.AddCommand(deaconRestartCmd)

	rootCmd.AddCommand(deaconCmd)
}

func runDeaconStart(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	// Check if session already exists
	running, err := t.HasSession(DeaconSessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if running {
		return fmt.Errorf("Deacon session already running. Attach with: gt deacon attach")
	}

	if err := startDeaconSession(t); err != nil {
		return err
	}

	fmt.Printf("%s Deacon session started. Attach with: %s\n",
		style.Bold.Render("✓"),
		style.Dim.Render("gt deacon attach"))

	return nil
}

// startDeaconSession creates and initializes the Deacon tmux session.
func startDeaconSession(t *tmux.Tmux) error {
	// Find workspace root
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	// Create session in workspace root
	fmt.Println("Starting Deacon session...")
	if err := t.NewSession(DeaconSessionName, townRoot); err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	// Set environment
	_ = t.SetEnvironment(DeaconSessionName, "GT_ROLE", "deacon")

	// Launch Claude in a respawn loop - session survives restarts
	// The startup hook handles context loading automatically
	// Use SendKeysDelayed to allow shell initialization after NewSession
	loopCmd := `while true; do echo "⛪ Starting Deacon session..."; claude --dangerously-skip-permissions; echo ""; echo "Deacon exited. Restarting in 2s... (Ctrl-C to stop)"; sleep 2; done`
	if err := t.SendKeysDelayed(DeaconSessionName, loopCmd, 200); err != nil {
		return fmt.Errorf("sending command: %w", err)
	}

	return nil
}

func runDeaconStop(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	// Check if session exists
	running, err := t.HasSession(DeaconSessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !running {
		return errors.New("Deacon session is not running")
	}

	fmt.Println("Stopping Deacon session...")

	// Try graceful shutdown first
	_ = t.SendKeysRaw(DeaconSessionName, "C-c")
	time.Sleep(100 * time.Millisecond)

	// Kill the session
	if err := t.KillSession(DeaconSessionName); err != nil {
		return fmt.Errorf("killing session: %w", err)
	}

	fmt.Printf("%s Deacon session stopped.\n", style.Bold.Render("✓"))
	return nil
}

func runDeaconAttach(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	// Check if session exists
	running, err := t.HasSession(DeaconSessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !running {
		// Auto-start if not running
		fmt.Println("Deacon session not running, starting...")
		if err := startDeaconSession(t); err != nil {
			return err
		}
	}
	// Session uses a respawn loop, so Claude restarts automatically if it exits

	// Use exec to replace current process with tmux attach
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found: %w", err)
	}

	return execCommand(tmuxPath, "attach-session", "-t", DeaconSessionName)
}

func runDeaconStatus(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	running, err := t.HasSession(DeaconSessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}

	if running {
		// Get session info for more details
		info, err := t.GetSessionInfo(DeaconSessionName)
		if err == nil {
			status := "detached"
			if info.Attached {
				status = "attached"
			}
			fmt.Printf("%s Deacon session is %s\n",
				style.Bold.Render("●"),
				style.Bold.Render("running"))
			fmt.Printf("  Status: %s\n", status)
			fmt.Printf("  Created: %s\n", info.Created)
			fmt.Printf("\nAttach with: %s\n", style.Dim.Render("gt deacon attach"))
		} else {
			fmt.Printf("%s Deacon session is %s\n",
				style.Bold.Render("●"),
				style.Bold.Render("running"))
		}
	} else {
		fmt.Printf("%s Deacon session is %s\n",
			style.Dim.Render("○"),
			"not running")
		fmt.Printf("\nStart with: %s\n", style.Dim.Render("gt deacon start"))
	}

	return nil
}

func runDeaconRestart(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	running, err := t.HasSession(DeaconSessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}

	if running {
		// Graceful restart: send Ctrl-C to exit Claude, loop will restart it
		fmt.Println("Restarting Deacon (sending Ctrl-C to trigger respawn loop)...")
		_ = t.SendKeysRaw(DeaconSessionName, "C-c")
		fmt.Printf("%s Deacon will restart automatically. Session stays attached.\n", style.Bold.Render("✓"))
		return nil
	}

	// Not running, start fresh
	return runDeaconStart(cmd, args)
}
