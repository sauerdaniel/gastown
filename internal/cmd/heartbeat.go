package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/mail"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

// Heartbeat command flags
var (
	heartbeatType   string
	heartbeatRig    string
	heartbeatHealth string
	heartbeatState  string
	heartbeatWork   string
)

var heartbeatCmd = &cobra.Command{
	Use:     "heartbeat <worker-name>",
	GroupID: GroupAgents,
	Short:   "Send a heartbeat signal from a worker",
	Long: `Send a heartbeat signal from a worker to the Witness.

Workers (polecats, dogs, etc.) send periodic heartbeats to signal liveness.
The Witness monitors these heartbeats and transitions worker health through
the state machine: healthy → stale → dead.

The heartbeat message is sent via mail to the rig's witness and also updates
the worker's agent bead with current state.

Examples:
  gt heartbeat alice --type=polecat --rig=greenplace --health=healthy --state=working --work=gt-123
  gt heartbeat bob --type=dog --rig=sandport --health=healthy --state=idle`,
	Args: cobra.ExactArgs(1),
	RunE: runHeartbeat,
}

func init() {
	heartbeatCmd.Flags().StringVar(&heartbeatType, "type", "", "Worker type (polecat, dog, etc.)")
	heartbeatCmd.Flags().StringVar(&heartbeatRig, "rig", "", "Rig name")
	heartbeatCmd.Flags().StringVar(&heartbeatHealth, "health", "healthy", "Health status (healthy, stale, dead)")
	heartbeatCmd.Flags().StringVar(&heartbeatState, "state", "working", "Work state (working, idle, blocked)")
	heartbeatCmd.Flags().StringVar(&heartbeatWork, "work", "", "Currently assigned work item ID")

	heartbeatCmd.MarkFlagRequired("type")
	heartbeatCmd.MarkFlagRequired("rig")

	rootCmd.AddCommand(heartbeatCmd)
}

func runHeartbeat(cmd *cobra.Command, args []string) error {
	workerName := args[0]

	// Infer town root
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	// Update agent bead with heartbeat timestamp
	beadsDir := beads.ResolveBeadsDir(townRoot)
	b := beads.NewWithBeadsDir(townRoot, beadsDir)

	// Find the agent bead for this worker
	// Agent ID format: <rig>-<role>-<name> or similar
	// For polecats: <rig>-polecat-<name>
	agentID := fmt.Sprintf("%s-%s-%s", heartbeatRig, heartbeatType, workerName)

	// Get existing agent bead
	issue, err := b.Show(agentID)
	if err != nil {
		// Agent bead might not exist yet, log warning and continue with mail
		fmt.Fprintf(os.Stderr, "Warning: agent bead %s not found, sending heartbeat mail only\n", agentID)
	} else {
		// Update agent bead with new heartbeat timestamp and state
		fields := beads.ParseAgentFields(issue.Description)
		if fields == nil {
			fields = &beads.AgentFields{}
		}

		// Update heartbeat timestamp
		fields.LastHeartbeat = time.Now().UTC().Format(time.RFC3339)
		fields.Health = heartbeatHealth
		fields.AssignedWork = heartbeatWork

		// Format updated description
		newDesc := beads.FormatAgentDescription(issue.Title, fields)
		if err := b.Update(agentID, beads.UpdateOptions{
			Description: &newDesc,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to update agent bead: %v\n", err)
		}
	}

	// Send heartbeat mail to witness
	witnessAddr := fmt.Sprintf("%s/witness", heartbeatRig)
	subject := fmt.Sprintf("💓 HEARTBEAT %s", workerName)
	
	body := fmt.Sprintf(`type: %s
rig: %s
health: %s
state: %s`, heartbeatType, heartbeatRig, heartbeatHealth, heartbeatState)

	if heartbeatWork != "" {
		body += fmt.Sprintf("\nassigned_work: %s", heartbeatWork)
	}

	router := mail.NewRouter(townRoot)
	msg := &mail.Message{
		To:      witnessAddr,
		Subject: subject,
		Body:    body,
	}
	if err := router.Send(msg); err != nil {
		return fmt.Errorf("sending heartbeat: %w", err)
	}

	fmt.Printf("%s Heartbeat sent to %s\n", style.Bold.Render("💓"), witnessAddr)
	fmt.Printf("  Worker: %s/%s\n", heartbeatRig, workerName)
	fmt.Printf("  Health: %s\n", heartbeatHealth)
	fmt.Printf("  State: %s\n", heartbeatState)
	if heartbeatWork != "" {
		fmt.Printf("  Work: %s\n", heartbeatWork)
	}

	return nil
}
